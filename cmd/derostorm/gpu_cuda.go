//go:build windows || (linux && amd64)

package main

// GPU mining support, bound at run time rather than at link time.
//
// The CUDA kernels live in a small library (gpu/derostorm_gpu.cu). This file
// carries that library inside the executable, writes it out on first use, and
// calls its plain-C entry points through the platform's dynamic loader --
// LoadLibrary on Windows, dlopen on Linux, in gpu_cuda_windows.go and
// gpu_cuda_linux.go. Three things fall out of doing it this way rather than
// with cgo:
//
//   - One executable per platform. The library is embedded, so there is
//     nothing to ship beside it and no separate GPU build to choose between. A
//     machine with no NVIDIA card runs the same binary and simply reports no
//     devices.
//   - No C toolchain. cgo hands the final link to MinGW, and the TDM-GCC
//     10.3.0 on this machine writes debug sections that Windows refuses to
//     load, so every cgo build died with "This app can't run on your PC"
//     before reaching main(). cgo would also have ended the Linux build, which
//     is a cross-compile from Windows: the .so is built once under WSL and is
//     just a file on disk by the time Go embeds it.
//   - No CUDA runtime to install. The library is built with -cudart static, so
//     it needs only the NVIDIA display driver.
//
// Only the loader is per-platform. Everything below it is shared, because the
// alternative -- a whole binding per platform -- is the drift gpu_other.go
// warns about, two copies of one thing falling out of step unnoticed.
//
// The entry points are bound as typed Go functions rather than called by hand
// through uintptrs. That is what removes the unsafe.Pointer-to-uintptr
// laundering the hand-written Windows binding needed: a *byte stays a *byte all
// the way into the call, so the garbage collector can see it and there is no
// window in which a live buffer looks unreferenced.
//
// CUDA means NVIDIA. AMD and Intel GPUs are not supported and would need a
// separate port to HIP or Vulkan. Linux arm64 has no library built for it and
// takes the gpu_other.go path, mining on the CPU.

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
)

// GPUAvailable reports whether this build can use a GPU at all. Whether one is
// actually present is a separate question -- ask GPUDeviceCount.
const GPUAvailable = true

// GPUKind names the hardware this supports, for messages to the user.
const GPUKind = "NVIDIA CUDA"

// The library's entry points, as declared in gpu/derostorm_gpu.h. A C int is
// int32 on both platforms; dsg_context* is opaque and never dereferenced here,
// so it stays a uintptr rather than pretending to be a Go pointer.
var (
	dsgDeviceCount func() int32
	dsgDeviceInfo  func(device int32, buf *byte, n int32) int32
	dsgDeviceName  func(ctx uintptr, buf *byte, n int32) int32
	dsgDeviceShape func(ctx uintptr, sms, maxBlocks, chunk *int32) int32
	dsgError       func(buf *byte, n int32) int32
	dsgFree        func(ctx uintptr)
	dsgHashOne     func(ctx uintptr, work *byte, nonce uint32, out *byte) int32
	dsgInit        func(device, batch, blocks int32, out *uintptr, batchOut, blocksOut *int32) int32
	dsgSearch      func(ctx uintptr, work *byte, nonceStart uint32, target *uint64,
		targetAll int32, nonces *uint32, maxNonces int32, found *int32) int32
	dsgSetBlocks func(ctx uintptr, blocks int32) int32
)

var (
	libOnce sync.Once
	libErr  error
)

// extractEmbeddedLib writes an embedded library somewhere stable and returns
// its path. The name carries a hash of the contents, so a rebuilt library lands
// beside the old one instead of failing to overwrite a copy some other process
// has open.
//
// Shared with the suffix-sort library in sa_lib.go, which is packaged exactly
// this way and for the same reasons. Both are embedded on precisely the
// platforms this file is built for, so one copy serves both.
func extractEmbeddedLib(libFS embed.FS, file string) (string, error) {
	data, err := libFS.ReadFile(file)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	ext := filepath.Ext(file)
	name := strings.TrimSuffix(file, ext) + "-" + hex.EncodeToString(sum[:6]) + ext

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
	// dlopen maps the file executable, so it needs more than the 0600
	// CreateTemp gives it. Windows ignores the mode and LoadLibrary does not
	// consult it.
	if err := os.Chmod(tmpName, 0o755); err != nil {
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
	libOnce.Do(func() {
		path, err := extractEmbeddedLib(gpuLibFS, gpuLibFile)
		if err != nil {
			libErr = fmt.Errorf("cannot unpack the GPU library: %w", err)
			return
		}
		sym, err := openNativeLibrary(path)
		if err != nil {
			libErr = fmt.Errorf("cannot load the GPU library: %w", err)
			return
		}
		// Every entry point is resolved here rather than on first use, so a
		// library missing one is a single clear error at start-up instead of a
		// nil call somewhere down the hot path.
		for _, b := range []struct {
			name string
			fn   any
		}{
			{"dsg_device_count", &dsgDeviceCount},
			{"dsg_device_info", &dsgDeviceInfo},
			{"dsg_device_name", &dsgDeviceName},
			{"dsg_device_shape", &dsgDeviceShape},
			{"dsg_error", &dsgError},
			{"dsg_free", &dsgFree},
			{"dsg_hash_one", &dsgHashOne},
			{"dsg_init", &dsgInit},
			{"dsg_search", &dsgSearch},
			{"dsg_set_blocks", &dsgSetBlocks},
		} {
			addr, err := sym(b.name)
			if err != nil {
				libErr = fmt.Errorf("GPU library is missing %s: %w", b.name, err)
				return
			}
			purego.RegisterFunc(b.fn, addr)
		}
	})
	return libErr
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
	if dsgError == nil {
		return "GPU library not loaded"
	}
	var buf [512]byte
	dsgError(&buf[0], int32(len(buf)))
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
	return int(dsgDeviceCount())
}

// GPUDeviceInfo describes a device without opening it, so the setup wizard can
// name the card without a multi-gigabyte allocation appearing mid-question.
// Returns "" when the device cannot be described.
func GPUDeviceInfo(device int) string {
	if err := loadGPU(); err != nil {
		return ""
	}
	var buf [256]byte
	if dsgDeviceInfo(int32(device), &buf[0], int32(len(buf))) != 0 {
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
// reserve suffix-kernel scratch for; pass 0 for four blocks per SM, which is
// where this kernel plateaus.
func NewGPUContext(device, batch, blocks int) (*GPUContext, error) {
	if err := loadGPU(); err != nil {
		return nil, err
	}

	var ctx uintptr
	var gotBatch, gotBlocks int32
	if dsgInit(int32(device), int32(batch), int32(blocks), &ctx, &gotBatch, &gotBlocks) != 0 {
		return nil, fmt.Errorf("gpu init: %s", lastGPUError())
	}

	var name [256]byte
	dsgDeviceName(ctx, &name[0], int32(len(name)))

	g := &GPUContext{
		ctx: ctx, batch: int(gotBatch), blocks: int(gotBlocks), name: cstr(name[:]),
	}

	var sms, maxBlocks, chunk int32
	dsgDeviceShape(ctx, &sms, &maxBlocks, &chunk)
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
	if dsgSetBlocks(g.ctx, int32(n)) != 0 {
		return fmt.Errorf("gpu set blocks: %s", lastGPUError())
	}
	g.blocks = n
	return nil
}

func (g *GPUContext) Close() {
	if g != nil && g.ctx != 0 {
		dsgFree(g.ctx)
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

	if dsgSearch(g.ctx, &work[0], nonceStart, &limbs[0], all,
		&g.hits[0], int32(len(g.hits)), &found) != 0 {
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
	if dsgHashOne(g.ctx, &work[0], nonce, &out[0]) != 0 {
		return out, fmt.Errorf("gpu hash: %s", lastGPUError())
	}
	return out, nil
}
