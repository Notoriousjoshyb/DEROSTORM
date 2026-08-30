//go:build windows || (linux && amd64)

package main

// The faster suffix sort, bound at run time.
//
// AstroBWTv3 spends ~90% of a hash building the suffix array of its stage-1
// state. The descriptor sort in native/descriptor.c does that far faster than
// the Go SA-IS on exactly this data, with libsais behind it as a fallback --
// measured over 512 real stage-1 texts, +251.6% of whole hashes at 15 threads,
// byte-for-byte identical output. Since the suffix array of a string is unique,
// faster cannot mean different.
//
// Packaged the same way as the CUDA library: the library is embedded in the
// executable, written out on first use, and bound through the platform's
// dynamic loader -- LoadLibrary on Windows, dlopen on Linux. One executable to
// ship, no C toolchain needed to build the miner, and no cgo. See gpu_cuda.go
// for the longer version of why that matters here.
//
// Only which file is embedded is per-platform; sa_windows.go and sa_linux.go
// carry that and nothing else. Everything below is shared, because a binding
// per platform is two copies of one thing falling out of step unnoticed.
//
// If anything at all goes wrong -- no AVX2, library missing, entry point
// missing, self-test disagreeing -- this reports why and the miner uses the Go
// sort. The only thing lost is the speed.

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/deroproject/derohe/astrobwt/astrobwtv3"
	"github.com/ebitengine/purego"
	"golang.org/x/sys/cpu"
)

var (
	saOnce    sync.Once
	saErr     error
	saVersion string
)

// The library's entry points, as declared in native/derostorm_sa.c. A C int32_t
// is int32 on both platforms, and every buffer stays a Go pointer all the way
// into the call, so the garbage collector can see it -- there is no window in
// which a live scratch buffer looks unreferenced.
var (
	dsaSuffixArray func(t *byte, sa *int32, n, fs int32) int32
	dsaVersion     func(buf *byte, n int32)
	dsaProbe       func() int32

	// The paired SHA-256. Optional in a way the sort is not: the SHA extensions
	// are a separate CPU feature, so a machine can have this library and still
	// not be able to run these entry points. They stay nil then and the hash
	// keeps Go's SHA-256.
	dsaSHAPair  func(a *byte, na int32, outA *byte, b *byte, nb int32, outB *byte)
	dsaSHAProbe func() int32
	shaPairOK   bool
)

// loadSA unpacks and binds the library. Safe to call repeatedly; the result,
// including a failure, is remembered.
func loadSA() error {
	saOnce.Do(func() {
		// The library is compiled with an AVX2 baseline on both platforms
		// (/arch:AVX2, -march=x86-64-v3), so an older CPU cannot run it at all.
		// Asked here rather than discovered at the first vector instruction,
		// because the second one is not an error the miner can report -- it is
		// SIGILL, and the process is gone before it can say anything.
		if !cpu.X86.HasAVX2 {
			saErr = errors.New("this CPU has no AVX2, which the suffix-sort library needs")
			return
		}

		path, err := extractEmbeddedLib(saLibFS, saLibFile)
		if err != nil {
			saErr = fmt.Errorf("cannot unpack the suffix-sort library: %w", err)
			return
		}
		sym, err := openNativeLibrary(path)
		if err != nil {
			saErr = fmt.Errorf("cannot load the suffix-sort library: %w", err)
			return
		}

		// Every required entry point is resolved here rather than on first use,
		// so a library missing one is a single clear error at start-up instead
		// of a nil call somewhere down the hot path.
		for _, b := range []struct {
			name string
			fn   any
		}{
			{"dsa_suffix_array", &dsaSuffixArray},
			{"dsa_version", &dsaVersion},
			{"dsa_probe", &dsaProbe},
		} {
			addr, err := sym(b.name)
			if err != nil {
				saErr = fmt.Errorf("suffix-sort library is missing %s: %w", b.name, err)
				return
			}
			purego.RegisterFunc(b.fn, addr)
		}

		// Prove it before trusting it with a hash. A library that is present but
		// wrong would not crash: AstroBWTv3 would simply produce hashes that
		// never meet any target, and the miner would look like it was working.
		if rc := dsaProbe(); rc != 0 {
			saErr = fmt.Errorf("suffix-sort library failed its self-test (%d)", rc)
			return
		}

		var vbuf [64]byte
		dsaVersion(&vbuf[0], int32(len(vbuf)))
		saVersion = cstr(vbuf[:])

		// The paired SHA-256 is looked up separately and is allowed to be
		// absent: an older copy of the library will not have it, and a CPU
		// without the SHA extensions cannot run it. Neither is an error --
		// shaPairOK stays false and the hook is left unset.
		pairAddr, pairErr := sym("dsa_sha256_pair_go")
		probeAddr, probeErr := sym("dsa_sha_probe")
		if pairErr == nil && probeErr == nil {
			purego.RegisterFunc(&dsaSHAPair, pairAddr)
			purego.RegisterFunc(&dsaSHAProbe, probeAddr)
			if dsaSHAProbe() == 1 {
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
	dsaSHAPair(&a[0], int32(len(a)), &outA[0], &b[0], int32(len(b)), &outB[0])
	return true
}

// fastSuffixSort is the astrobwtv3.SuffixSort hook. Returns false when the
// library could not do it, and the caller then runs the Go sort.
func fastSuffixSort(text []byte, sa []int32) bool {
	if dsaSuffixArray == nil || len(text) == 0 || len(sa) < len(text) {
		return false
	}
	// The slack after sa[len(text)] is real: the miner's scratch holds a
	// fixed-size array and the text is shorter than it. libsais will borrow it
	// rather than allocate.
	fs := cap(sa) - len(text)
	if fs < 0 {
		fs = 0
	}
	return dsaSuffixArray(&text[0], &sa[0], int32(len(text)), int32(fs)) == 0
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
