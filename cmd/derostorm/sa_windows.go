//go:build windows

package main

// The faster suffix sort, bound at run time.
//
// AstroBWTv3 spends ~90% of a hash building the suffix array of its stage-1
// state. libsais does that faster than the Go SA-IS on exactly this data --
// measured on 512 real stage-1 texts, interleaved so background load cancels:
// 1.35x on one thread, 1.42x on fifteen, byte-for-byte identical output. Since
// the suffix array of a string is unique, faster cannot mean different.
//
// Packaged the same way as the CUDA library: the DLL is embedded in the
// executable, written out on first use, and bound with LoadLibrary. One
// executable to ship, no C toolchain needed to build the miner, and no cgo. See
// gpu_windows.go for the longer version of why that matters here.
//
// If anything at all goes wrong -- library missing, entry point missing,
// self-test disagreeing -- this reports why and the miner uses the Go sort. The
// only thing lost is the speed.

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	"github.com/deroproject/derohe/astrobwt/astrobwtv3"
)

//go:embed derostorm_sa.dll
var saDLL embed.FS

var (
	saOnce    sync.Once
	saErr     error
	saVersion string

	procSA        *syscall.LazyProc
	procSAVersion *syscall.LazyProc
	procSAProbe   *syscall.LazyProc

	// The paired SHA-256. Optional in a way the sort is not: the SHA extensions
	// are a separate CPU feature, so a machine can have this library and still
	// not be able to run these entry points. procSHAPair stays nil then and the
	// hash keeps Go's SHA-256.
	procSHAPair  *syscall.LazyProc
	procSHAProbe *syscall.LazyProc
	shaPairOK    bool
)

// extractSaDLL writes the embedded library somewhere stable and returns its
// path. The name carries a hash of the contents, so a rebuilt library lands
// beside the old one instead of failing to overwrite a copy that is still open.
func extractSaDLL() (string, error) {
	data, err := saDLL.ReadFile("derostorm_sa.dll")
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	name := "derostorm_sa-" + hex.EncodeToString(sum[:6]) + ".dll"

	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "DeroStorm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)

	if st, err := os.Stat(path); err == nil && st.Size() == int64(len(data)) {
		return path, nil
	}

	tmp, err := os.CreateTemp(dir, name+".*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		// Another process won the race; its copy is byte for byte this one.
		os.Remove(tmpName)
		if _, statErr := os.Stat(path); statErr != nil {
			return "", err
		}
	}
	return path, nil
}

func loadSA() error {
	saOnce.Do(func() {
		path, err := extractSaDLL()
		if err != nil {
			saErr = fmt.Errorf("cannot unpack the suffix-sort library: %w", err)
			return
		}
		dll := syscall.NewLazyDLL(path)
		if err := dll.Load(); err != nil {
			saErr = fmt.Errorf("cannot load the suffix-sort library: %w", err)
			return
		}
		procSA = dll.NewProc("dsa_suffix_array")
		procSAVersion = dll.NewProc("dsa_version")
		procSAProbe = dll.NewProc("dsa_probe")
		for _, p := range []*syscall.LazyProc{procSA, procSAVersion, procSAProbe} {
			if err := p.Find(); err != nil {
				saErr = fmt.Errorf("suffix-sort library is missing %s: %w", p.Name, err)
				return
			}
		}

		// Prove it before trusting it with a hash. A library that is present but
		// wrong would not crash: AstroBWTv3 would simply produce hashes that
		// never meet any target, and the miner would look like it was working.
		if rc, _, _ := procSAProbe.Call(); int32(rc) != 0 {
			saErr = fmt.Errorf("suffix-sort library failed its self-test (%d)", int32(rc))
			return
		}

		var vbuf [64]byte
		procSAVersion.Call(uintptr(unsafe.Pointer(&vbuf[0])), uintptr(int32(len(vbuf))))
		saVersion = cstr(vbuf[:])

		// The paired SHA-256 is looked up separately and is allowed to be
		// absent: an older copy of the library will not have it, and a CPU
		// without the SHA extensions cannot run it. Neither is an error --
		// shaPairOK stays false and the hook is left unset.
		procSHAPair = dll.NewProc("dsa_sha256_pair_go")
		procSHAProbe = dll.NewProc("dsa_sha_probe")
		if procSHAPair.Find() == nil && procSHAProbe.Find() == nil {
			if rc, _, _ := procSHAProbe.Call(); int32(rc) == 1 {
				shaPairOK = true
			}
		}
	})
	return saErr
}

// pairSHA256 is the astrobwtv3.SHA256Pair hook. Returns false when the library
// cannot do it, and the caller then runs two ordinary SHA-256s.
func pairSHA256(a, b []byte, outA, outB *[32]byte) bool {
	if !shaPairOK || len(a) == 0 || len(b) == 0 {
		return false
	}
	procSHAPair.Call(
		uintptr(unsafe.Pointer(&a[0])), uintptr(int32(len(a))),
		uintptr(unsafe.Pointer(&outA[0])),
		uintptr(unsafe.Pointer(&b[0])), uintptr(int32(len(b))),
		uintptr(unsafe.Pointer(&outB[0])),
	)
	return true
}

// fastSuffixSort is the astrobwtv3.SuffixSort hook. Returns false when the
// library could not do it, and the caller then runs the Go sort.
func fastSuffixSort(text []byte, sa []int32) bool {
	if procSA == nil || len(text) == 0 || len(sa) < len(text) {
		return false
	}
	// The slack after sa[len(text)] is real: the miner's scratch holds a
	// fixed-size array and the text is shorter than it. libsais will borrow it
	// rather than allocate.
	fs := cap(sa) - len(text)
	if fs < 0 {
		fs = 0
	}
	rc, _, _ := procSA.Call(
		uintptr(unsafe.Pointer(&text[0])),
		uintptr(unsafe.Pointer(&sa[0])),
		uintptr(int32(len(text))),
		uintptr(int32(fs)),
	)
	return int32(rc) == 0
}

// InstallFastSuffixSort points the hash at the faster sort, and reports what it
// did in words fit for the console. Called once, before mining starts.
func InstallFastSuffixSort() (string, bool) {
	if err := loadSA(); err != nil {
		return err.Error() + " — using the built-in sort", false
	}
	astrobwtv3.SuffixSort = fastSuffixSort
	v := saVersion
	if v == "" {
		v = "unknown version"
	}
	note := "suffix sort: descriptor + libsais " + v
	if shaPairOK {
		astrobwtv3.SHA256Pair = pairSHA256
		note += ", paired SHA-256"
	}
	return note, true
}

// PairedSHAAvailable reports whether nonces should be hashed two at a time.
// False leaves the mining loop doing one at a time, which is what it did before
// the pairing existed.
//
// DEROSTORM_NO_PAIR=1 forces it off. That exists so the pairing can be measured
// against its own absence in the same binary -- it costs a second scratch buffer
// a thread, which is ~350 KB of cache that the suffix sort would otherwise have,
// so whether it wins is a question about the machine and not about the
// arithmetic.
func PairedSHAAvailable() bool {
	return shaPairOK && os.Getenv("DEROSTORM_NO_PAIR") == ""
}
