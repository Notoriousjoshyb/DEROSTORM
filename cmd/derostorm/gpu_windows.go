//go:build windows

package main

// GPU mining support, bound at run time rather than at link time.
//
// The CUDA kernels live in a small DLL (gpu/derostorm_gpu.cu). This file
// carries that DLL inside the executable, writes it out on first use, and calls
// its plain-C entry points through LoadLibrary. Three things fall out of doing
// it this way rather than with cgo:
//
//   - One executable. The DLL is embedded, so there is nothing to ship beside
//     it and no separate GPU build to choose between. A machine with no NVIDIA
//     card runs the same binary and simply reports no devices.
//   - No C toolchain. cgo hands the final link to MinGW, and the TDM-GCC 10.3.0
//     on this machine writes debug sections that Windows refuses to load, so
//     every cgo build died with "This app can't run on your PC" before reaching
//     main(). With no cgo, Go's own linker does the work and the problem does
//     not exist.
//   - No CUDA runtime to install. The DLL is built with -cudart static, so it
//     needs only the NVIDIA display driver.
//
// CUDA means NVIDIA. AMD and Intel GPUs are not supported and would need a
// separate port to HIP or Vulkan.

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"
)

// GPUAvailable reports whether this build can use a GPU at all. Whether one is
// actually present is a separate question -- ask GPUDeviceCount.
const GPUAvailable = true

// GPUKind names the hardware this supports, for messages to the user.
const GPUKind = "NVIDIA CUDA"

//go:embed derostorm_gpu.dll
var gpuDLL embed.FS

var (
	dllOnce sync.Once
	dllErr  error

	procDeviceCount *syscall.LazyProc
	procDeviceName  *syscall.LazyProc
	procInfo        *syscall.LazyProc
	procError       *syscall.LazyProc
	procFree        *syscall.LazyProc
	procHashOne     *syscall.LazyProc
	procInit        *syscall.LazyProc
	procSearch      *syscall.LazyProc
	procSetBlocks   *syscall.LazyProc
	procShape       *syscall.LazyProc
)

// extractDLL writes the embedded library somewhere stable and returns its path.
// The name carries a hash of the contents, so a rebuilt DLL lands beside the
// old one instead of failing to overwrite a copy some other process has open.
func extractDLL() (string, error) {
	data, err := gpuDLL.ReadFile("derostorm_gpu.dll")
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	name := "derostorm_gpu-" + hex.EncodeToString(sum[:6]) + ".dll"

	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "DeroStorm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)

	// Already unpacked by an earlier run, and the hash in the name means the
	// contents cannot have drifted.
	if st, err := os.Stat(path); err == nil && st.Size() == int64(len(data)) {
		return path, nil
	}

	// Write to a unique temporary name and rename into place, so two miners
	// starting at once cannot see a half-written library.
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

// loadGPU unpacks and binds the library. Safe to call repeatedly; the result,
// including a failure, is remembered.
func loadGPU() error {
	dllOnce.Do(func() {
		path, err := extractDLL()
		if err != nil {
			dllErr = fmt.Errorf("cannot unpack the GPU library: %w", err)
			return
		}
		dll := syscall.NewLazyDLL(path)
		if err := dll.Load(); err != nil {
			dllErr = fmt.Errorf("cannot load the GPU library: %w", err)
			return
		}
		procDeviceCount = dll.NewProc("dsg_device_count")
		procDeviceName = dll.NewProc("dsg_device_name")
		procInfo = dll.NewProc("dsg_device_info")
		procError = dll.NewProc("dsg_error")
		procFree = dll.NewProc("dsg_free")
		procHashOne = dll.NewProc("dsg_hash_one")
		procInit = dll.NewProc("dsg_init")
		procSearch = dll.NewProc("dsg_search")
		procSetBlocks = dll.NewProc("dsg_set_blocks")
		procShape = dll.NewProc("dsg_device_shape")

		for _, p := range []*syscall.LazyProc{
			procDeviceCount, procDeviceName, procError, procFree,
			procHashOne, procInfo, procInit, procSearch,
			procSetBlocks, procShape,
		} {
			if err := p.Find(); err != nil {
				dllErr = fmt.Errorf("GPU library is missing %s: %w", p.Name, err)
				return
			}
		}
	})
	return dllErr
}

// cstr trims a buffer the library filled at its NUL. Every string entry point
// writes into a caller-owned buffer rather than returning a pointer, so nothing
// here has to reconstruct a Go string from a raw address.
func cstr(buf []byte) string {
	for i, c := range buf {
		if c == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

func lastGPUError() string {
	if procError == nil {
		return "GPU library not loaded"
	}
	var buf [512]byte
	procError.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(int32(len(buf))))
	if s := cstr(buf[:]); s != "" {
		return s
	}
	return "unknown error"
}

// GPUDeviceCount is how many CUDA devices the driver reports. Zero is not an
// error: it just means this machine mines on the CPU.
func GPUDeviceCount() int {
	if err := loadGPU(); err != nil {
		return 0
	}
	r, _, _ := procDeviceCount.Call()
	return int(int32(r))
}

// GPUDeviceInfo describes a device without opening it, so the setup wizard can
// name the card without a multi-gigabyte allocation appearing mid-question.
// Returns "" when the device cannot be described.
func GPUDeviceInfo(device int) string {
	if err := loadGPU(); err != nil {
		return ""
	}
	proc := procInfo
	if proc == nil {
		return ""
	}
	var buf [256]byte
	rc, _, _ := proc.Call(
		uintptr(int32(device)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(int32(len(buf))),
	)
	if int32(rc) != 0 {
		return ""
	}
	return cstr(buf[:])
}

// GPUContext owns one device's scratch memory. Not safe for concurrent use;
// one context per device, driven by one goroutine.
type GPUContext struct {
	ctx   uintptr
	batch int
	name  string

	// blocks is the resident block count of the suffix kernel currently in
	// force, and maxBlocks the largest the context was allocated for. This is
	// the one knob worth tuning per card; see tuneBlocks in gpu_worker.go.
	blocks    int
	maxBlocks int
	sms       int
	chunk     int

	// Landing pad for winning nonces. Owned by this struct so Search does no
	// allocation on the hot path; 64 is far more than a batch can produce at
	// any real difficulty.
	hits [64]uint32
}

// NewGPUContext opens a device. batch is the number of nonces hashed per call;
// pass 0 to let the library size it. blocks is the resident block count to
// reserve suffix-kernel scratch for; pass 0 for the library default, which is
// deliberately generous so a sweep has room to move.
func NewGPUContext(device, batch, blocks int) (*GPUContext, error) {
	if err := loadGPU(); err != nil {
		return nil, err
	}

	var ctx uintptr
	var gotBatch, gotBlocks int32
	rc, _, _ := procInit.Call(
		uintptr(int32(device)),
		uintptr(int32(batch)),
		uintptr(int32(blocks)),
		uintptr(unsafe.Pointer(&ctx)),
		uintptr(unsafe.Pointer(&gotBatch)),
		uintptr(unsafe.Pointer(&gotBlocks)),
	)
	if int32(rc) != 0 {
		return nil, fmt.Errorf("gpu init: %s", lastGPUError())
	}

	var name [256]byte
	procDeviceName.Call(ctx, uintptr(unsafe.Pointer(&name[0])), uintptr(int32(len(name))))

	g := &GPUContext{
		ctx: ctx, batch: int(gotBatch), blocks: int(gotBlocks), name: cstr(name[:]),
	}

	var sms, maxBlocks, chunk int32
	procShape.Call(ctx,
		uintptr(unsafe.Pointer(&sms)),
		uintptr(unsafe.Pointer(&maxBlocks)),
		uintptr(unsafe.Pointer(&chunk)))
	g.sms, g.maxBlocks, g.chunk = int(sms), int(maxBlocks), int(chunk)

	return g, nil
}

// Blocks is the resident block count the suffix kernel is running with.
func (g *GPUContext) Blocks() int { return g.blocks }

// MaxBlocks is the largest value SetBlocks will accept.
func (g *GPUContext) MaxBlocks() int { return g.maxBlocks }

// SMs is the device's streaming-multiprocessor count, which is what the block
// candidates are expressed as multiples of.
func (g *GPUContext) SMs() int { return g.sms }

// SetBlocks changes the resident block count. Call between searches, not
// during one.
func (g *GPUContext) SetBlocks(n int) error {
	if g == nil || g.ctx == 0 {
		return errors.New("gpu: context is closed")
	}
	rc, _, _ := procSetBlocks.Call(g.ctx, uintptr(int32(n)))
	if int32(rc) != 0 {
		return fmt.Errorf("gpu set blocks: %s", lastGPUError())
	}
	g.blocks = n
	return nil
}

func (g *GPUContext) Close() {
	if g != nil && g.ctx != 0 {
		procFree.Call(g.ctx)
		g.ctx = 0
	}
}

// Batch is how many nonces one Search call covers.
func (g *GPUContext) Batch() int { return g.batch }

// Name describes the device, for the console.
func (g *GPUContext) Name() string { return g.name }

// Search hashes nonces nonceStart .. nonceStart+Batch()-1 and returns those
// meeting the target. The returned slice aliases an internal buffer and is only
// valid until the next call.
func (g *GPUContext) Search(work []byte, nonceStart uint32, t *Target) ([]uint32, error) {
	if g == nil || g.ctx == 0 {
		return nil, errors.New("gpu: context is closed")
	}
	if len(work) != gpuWorkSize {
		return nil, fmt.Errorf("gpu: work is %d bytes, want %d", len(work), gpuWorkSize)
	}

	limbs := t.limb
	all := int32(0)
	if t.all {
		all = 1
	}
	var found int32

	rc, _, _ := procSearch.Call(
		g.ctx,
		uintptr(unsafe.Pointer(&work[0])),
		uintptr(nonceStart),
		uintptr(unsafe.Pointer(&limbs[0])),
		uintptr(all),
		uintptr(unsafe.Pointer(&g.hits[0])),
		uintptr(int32(len(g.hits))),
		uintptr(unsafe.Pointer(&found)),
	)
	if int32(rc) != 0 {
		return nil, fmt.Errorf("gpu search: %s", lastGPUError())
	}
	if found < 0 || int(found) > len(g.hits) {
		found = int32(len(g.hits))
	}
	return g.hits[:found], nil
}

// HashOne runs a single nonce through the GPU and returns the PoW hash. Used at
// start-up to prove the device agrees with the CPU before any share is trusted.
func (g *GPUContext) HashOne(work []byte, nonce uint32) ([32]byte, error) {
	var out [32]byte
	if g == nil || g.ctx == 0 {
		return out, errors.New("gpu: context is closed")
	}
	if len(work) != gpuWorkSize {
		return out, fmt.Errorf("gpu: work is %d bytes, want %d", len(work), gpuWorkSize)
	}
	rc, _, _ := procHashOne.Call(
		g.ctx,
		uintptr(unsafe.Pointer(&work[0])),
		uintptr(nonce),
		uintptr(unsafe.Pointer(&out[0])),
	)
	if int32(rc) != 0 {
		return out, fmt.Errorf("gpu hash: %s", lastGPUError())
	}
	return out, nil
}
