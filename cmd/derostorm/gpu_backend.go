//go:build windows || (linux && amd64)

package main

// GPU mining support, bound at run time rather than at link time.
//
// The kernels live in a small library (gpu/derostorm_gpu.cu). This file carries
// that library inside the executable, writes it out on first use, and calls its
// plain-C entry points through the platform's dynamic loader -- LoadLibrary on
// Windows, dlopen on Linux, in gpu_backend_windows.go and gpu_backend_linux.go.
// Three things fall out of doing it this way rather than with cgo:
//
//   - One executable per platform. The libraries are embedded, so there is
//     nothing to ship beside it and no separate GPU build to choose between. A
//     machine with no GPU runs the same binary and simply reports no devices.
//   - No C toolchain. cgo hands the final link to MinGW, and the TDM-GCC
//     10.3.0 on this machine writes debug sections that Windows refuses to
//     load, so every cgo build died with "This app can't run on your PC"
//     before reaching main(). cgo would also have ended the Linux build, which
//     is a cross-compile from Windows: the .so is built once under WSL and is
//     just a file on disk by the time Go embeds it.
//   - No CUDA runtime to install. The NVIDIA library is built with
//     -cudart static, so it needs only the display driver.
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
// # Two vendors, one binding
//
// There are two libraries, not one: CUDA for NVIDIA and HIP for AMD. They are
// built from the same source -- see gpu/gpuapi.cuh -- and export the same
// dsg_* names, so what differs between them here is which file is loaded and
// nothing else. A gpuBackend is one loaded library; the miner sees a single
// flat list of devices across both, so --gpu=0 means "the first card" whoever
// made it.
//
// The two are loaded independently and a failure in one is not a failure in
// the other. A machine with an NVIDIA card and no ROCm gets the CUDA backend
// and a quiet nothing from the HIP one, which is the common case on both sides.
//
// The AMD library may not be present at all. It has to be built by someone with
// ROCm installed, and until it has been, cmd/derostorm/gpulib/ holds only its
// README -- so the embedded file is missing and the HIP backend reports no
// devices, exactly as it would on a machine with no AMD card. That is why this
// reads the embedded bytes before anything else and treats "not there" as an
// ordinary absence rather than an error.
//
// Intel GPUs are not supported and would need a third port, to SYCL or Vulkan.
// Linux arm64 has no library built for it and takes the gpu_other.go path,
// mining on the CPU.

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
)

// GPUAvailable reports whether this build can use a GPU at all. Whether one is
// actually present is a separate question -- ask GPUDeviceCount.
const GPUAvailable = true

// gpuBackend is one vendor's library: the file to unpack, and the entry points
// once it is bound. Both instances are package-level and bound at most once, so
// a miner driving two NVIDIA cards loads one copy of the library between them.
type gpuBackend struct {
	kind  string // what to call it to the user: "NVIDIA CUDA", "AMD HIP"
	libFS embed.FS
	file  string // path inside libFS

	once sync.Once
	err  error

	// The library's entry points, as declared in gpu/derostorm_gpu.h. A C int
	// is int32 on both platforms; dsg_context* is opaque and never dereferenced
	// here, so it stays a uintptr rather than pretending to be a Go pointer.
	deviceCount func() int32
	deviceInfo  func(device int32, buf *byte, n int32) int32
	deviceName  func(ctx uintptr, buf *byte, n int32) int32
	deviceShape func(ctx uintptr, sms, maxBlocks, chunk *int32) int32
	errorText   func(buf *byte, n int32) int32
	freeCtx     func(ctx uintptr)
	hashOne     func(ctx uintptr, work *byte, nonce uint32, out *byte) int32
	initCtx     func(device, batch, blocks int32, out *uintptr, batchOut, blocksOut *int32) int32
	search      func(ctx uintptr, work *byte, nonceStart uint32, target *uint64,
		targetAll int32, nonces *uint32, maxNonces int32, found *int32) int32
	setBlocks func(ctx uintptr, blocks int32) int32
	submit    func(ctx uintptr, work *byte, nonceStart uint32, target *uint64,
		targetAll int32) int32
	collect  func(ctx uintptr, nonces *uint32, maxNonces int32, found *int32) int32
	inflight func(ctx uintptr) int32
}

// The two vendors, in the order their devices are numbered. NVIDIA first
// because that is the order the miner has always reported and a rig's
// --gpu list should not renumber itself when a build gains AMD support.
var gpuBackends = []*gpuBackend{cudaBackend, hipBackend}

// errLibAbsent is the HIP backend on a build where nobody has put the library
// in place yet. Not an error the user should see: it means "no such hardware
// here", the same as a machine with no AMD card.
var errLibAbsent = errors.New("no library embedded for this backend")

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
	// path.Base, not the whole of `file`: the AMD libraries sit in a
	// subdirectory of the embedded filesystem so that a Windows build does not
	// carry the Linux one, and joining that path onto the cache directory would
	// name a directory that does not exist.
	base := path.Base(file)
	sum := sha256.Sum256(data)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext) + "-" + hex.EncodeToString(sum[:6]) + ext

	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "DeroStorm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	target := filepath.Join(dir, name)

	// Already unpacked by an earlier run, and the hash in the name means the
	// contents cannot have drifted.
	if st, err := os.Stat(target); err == nil && st.Size() == int64(len(data)) {
		return target, nil
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
	if err := os.Rename(tmpName, target); err != nil {
		// Another process won the race; its copy is byte for byte this one.
		os.Remove(tmpName)
		if _, statErr := os.Stat(target); statErr != nil {
			return "", err
		}
	}
	return target, nil
}

// load unpacks and binds this backend's library. Safe to call repeatedly; the
// result, including a failure, is remembered.
func (b *gpuBackend) load() error {
	b.once.Do(func() {
		// Absent is not broken. The AMD library has to be built by someone with
		// ROCm, and a build made before that happened simply has no AMD
		// support -- which is the same thing, to the user, as having no AMD
		// card.
		if _, err := b.libFS.ReadFile(b.file); err != nil {
			b.err = errLibAbsent
			return
		}
		libPath, err := extractEmbeddedLib(b.libFS, b.file)
		if err != nil {
			b.err = fmt.Errorf("cannot unpack the %s library: %w", b.kind, err)
			return
		}
		sym, err := openNativeLibrary(libPath)
		if err != nil {
			// The usual cause on the AMD side is a machine with no ROCm
			// runtime: the library itself is fine, the amdhip64 it links
			// against is not there. Nothing to report and nothing to fix
			// unless the user has an AMD card, so this stays quiet the way a
			// missing NVIDIA driver does.
			b.err = fmt.Errorf("cannot load the %s library: %w", b.kind, err)
			return
		}
		// Every entry point is resolved here rather than on first use, so a
		// library missing one is a single clear error at start-up instead of a
		// nil call somewhere down the hot path.
		for _, e := range []struct {
			name string
			fn   any
		}{
			{"dsg_device_count", &b.deviceCount},
			{"dsg_device_info", &b.deviceInfo},
			{"dsg_device_name", &b.deviceName},
			{"dsg_device_shape", &b.deviceShape},
			{"dsg_error", &b.errorText},
			{"dsg_free", &b.freeCtx},
			{"dsg_hash_one", &b.hashOne},
			{"dsg_init", &b.initCtx},
			{"dsg_search", &b.search},
			{"dsg_set_blocks", &b.setBlocks},
			{"dsg_submit", &b.submit},
			{"dsg_collect", &b.collect},
			{"dsg_inflight", &b.inflight},
		} {
			addr, err := sym(e.name)
			if err != nil {
				b.err = fmt.Errorf("%s library is missing %s: %w", b.kind, e.name, err)
				return
			}
			purego.RegisterFunc(e.fn, addr)
		}
	})
	return b.err
}

// count is how many devices this backend's driver reports, or zero if the
// library would not load.
func (b *gpuBackend) count() int {
	if b.load() != nil {
		return 0
	}
	return int(b.deviceCount())
}

// lastError is the text of this backend's most recent failure.
func (b *gpuBackend) lastError() string {
	if b.errorText == nil {
		return b.kind + " library not loaded"
	}
	var buf [512]byte
	b.errorText(&buf[0], int32(len(buf)))
	if s := cstr(buf[:]); s != "" {
		return s
	}
	return "unknown error"
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

// ---------------------------------------------------------------------------
// the device list
// ---------------------------------------------------------------------------

// gpuDevice is one card: which library drives it, and its index within that
// library. The miner numbers cards across both backends, so index 0 is the
// first NVIDIA card if there is one and the first AMD card otherwise.
type gpuDevice struct {
	backend *gpuBackend
	local   int
}

var (
	gpuDevOnce sync.Once
	gpuDevs    []gpuDevice
)

// gpuDeviceList enumerates once and remembers the answer.
//
// Once, and not per call, because the count is what every index in this file is
// resolved against: a list that grew or shrank between two calls would mean
// --gpu=1 naming a different card at start-up than during a sensor poll. A
// driver that reports no devices now will not report one later in the same run,
// so nothing is lost by fixing the list here.
func gpuDeviceList() []gpuDevice {
	gpuDevOnce.Do(func() {
		for _, b := range gpuBackends {
			n := b.count()
			for i := 0; i < n; i++ {
				gpuDevs = append(gpuDevs, gpuDevice{backend: b, local: i})
			}
		}
	})
	return gpuDevs
}

// GPUDeviceCount is how many devices the drivers report, across both vendors.
// Zero is not an error: it just means this machine mines on the CPU.
func GPUDeviceCount() int { return len(gpuDeviceList()) }

// GPUKind names the hardware this build supports, for messages to the user.
// When cards are actually present it names what they are, so a rig with one
// AMD card is not told about CUDA.
func GPUKind() string {
	devs := gpuDeviceList()
	seen := make([]string, 0, len(gpuBackends))
	for _, b := range gpuBackends {
		for _, d := range devs {
			if d.backend == b {
				seen = append(seen, b.kind)
				break
			}
		}
	}
	if len(seen) == 0 {
		for _, b := range gpuBackends {
			seen = append(seen, b.kind)
		}
	}
	return strings.Join(seen, " and ")
}

// GPUDeviceKind names the vendor of one device, or "" if there is no such
// device.
func GPUDeviceKind(device int) string {
	devs := gpuDeviceList()
	if device < 0 || device >= len(devs) {
		return ""
	}
	return devs[device].backend.kind
}

// GPUDeviceInfo describes a device without opening it, so the setup wizard can
// name the card without a multi-gigabyte allocation appearing mid-question.
// Returns "" when the device cannot be described.
func GPUDeviceInfo(device int) string {
	devs := gpuDeviceList()
	if device < 0 || device >= len(devs) {
		return ""
	}
	d := devs[device]
	var buf [256]byte
	if d.backend.deviceInfo(int32(d.local), &buf[0], int32(len(buf))) != 0 {
		return ""
	}
	return cstr(buf[:])
}

// GPUContext owns one device's scratch memory. Not safe for concurrent use;
// one context per device, driven by one goroutine.
type GPUContext struct {
	backend *gpuBackend
	ctx     uintptr
	batch   int
	name    string

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

	// The target limbs a Submit was given, kept alive here rather than on the
	// caller's stack. The library copies them into page-locked memory before it
	// returns, so this is belt and braces, but a pointer handed to a C function
	// should not be to something the collector may move under it.
	limbs [4]uint64
}

// NewGPUContext opens a device. batch is the number of nonces hashed per call;
// pass 0 to let the library size it. blocks is the resident block count to
// reserve suffix-kernel scratch for; pass 0 for four blocks per SM, which is
// where this kernel plateaus.
func NewGPUContext(device, batch, blocks int) (*GPUContext, error) {
	devs := gpuDeviceList()
	if device < 0 || device >= len(devs) {
		return nil, fmt.Errorf("gpu: no device %d (%d present)", device, len(devs))
	}
	d := devs[device]

	var ctx uintptr
	var gotBatch, gotBlocks int32
	if d.backend.initCtx(int32(d.local), int32(batch), int32(blocks), &ctx, &gotBatch, &gotBlocks) != 0 {
		return nil, fmt.Errorf("gpu init: %s", d.backend.lastError())
	}

	var name [256]byte
	d.backend.deviceName(ctx, &name[0], int32(len(name)))

	g := &GPUContext{
		backend: d.backend, ctx: ctx,
		batch: int(gotBatch), blocks: int(gotBlocks), name: cstr(name[:]),
	}

	var sms, maxBlocks, chunk int32
	d.backend.deviceShape(ctx, &sms, &maxBlocks, &chunk)
	g.sms, g.maxBlocks, g.chunk = int(sms), int(maxBlocks), int(chunk)

	return g, nil
}

// Blocks is the resident block count the suffix kernel is running with.
func (g *GPUContext) Blocks() int { return g.blocks }

// MaxBlocks is the largest value SetBlocks will accept.
func (g *GPUContext) MaxBlocks() int { return g.maxBlocks }

// SMs is the device's streaming-multiprocessor count -- compute units on an
// AMD card, which is the same field -- and it is what the block candidates are
// expressed as multiples of.
func (g *GPUContext) SMs() int { return g.sms }

// Kind names the vendor driving this context.
func (g *GPUContext) Kind() string {
	if g == nil || g.backend == nil {
		return ""
	}
	return g.backend.kind
}

// SetBlocks changes the resident block count. Call it with nothing in flight:
// it takes effect at the next launch, so a change made between a Submit and its
// Collect lands on the batch after the one being measured. That is why the
// block sweep uses Search rather than the pipeline.
func (g *GPUContext) SetBlocks(n int) error {
	if g == nil || g.ctx == 0 {
		return errors.New("gpu: context is closed")
	}
	if g.backend.setBlocks(g.ctx, int32(n)) != 0 {
		return fmt.Errorf("gpu set blocks: %s", g.backend.lastError())
	}
	g.blocks = n
	return nil
}

func (g *GPUContext) Close() {
	if g != nil && g.ctx != 0 {
		g.backend.freeCtx(g.ctx)
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

	if g.backend.search(g.ctx, &work[0], nonceStart, &limbs[0], all,
		&g.hits[0], int32(len(g.hits)), &found) != 0 {
		return nil, fmt.Errorf("gpu search: %s", g.backend.lastError())
	}
	if found < 0 || int(found) > len(g.hits) {
		found = int32(len(g.hits))
	}
	return g.hits[:found], nil
}

// Submit queues a batch and returns without waiting for it. Collect takes the
// results of the oldest queued batch.
//
// Splitting the two is what keeps the card busy. A Search leaves the GPU idle
// from the moment its last kernel ends until the host has woken up, read the
// results and enqueued the next batch -- and on a machine mining on every core,
// waking up means waiting for a scheduler slot. Submitting the next batch
// before collecting the current one moves that whole gap off the critical path:
// the card starts the queued batch the instant the running one ends.
//
// Two batches may be in flight. A third Submit fails rather than overwriting a
// batch still on the card.
func (g *GPUContext) Submit(work []byte, nonceStart uint32, t *Target) error {
	if g == nil || g.ctx == 0 {
		return errors.New("gpu: context is closed")
	}
	if len(work) != gpuWorkSize {
		return fmt.Errorf("gpu: work is %d bytes, want %d", len(work), gpuWorkSize)
	}

	g.limbs = t.limb
	all := int32(0)
	if t.all {
		all = 1
	}
	if g.backend.submit(g.ctx, &work[0], nonceStart, &g.limbs[0], all) != 0 {
		return fmt.Errorf("gpu submit: %s", g.backend.lastError())
	}
	return nil
}

// Collect waits for the oldest batch Submit queued and returns the nonces that
// met its target. The slice aliases an internal buffer and is only valid until
// the next call.
func (g *GPUContext) Collect() ([]uint32, error) {
	if g == nil || g.ctx == 0 {
		return nil, errors.New("gpu: context is closed")
	}
	var found int32
	if g.backend.collect(g.ctx, &g.hits[0], int32(len(g.hits)), &found) != 0 {
		return nil, fmt.Errorf("gpu collect: %s", g.backend.lastError())
	}
	if found < 0 || int(found) > len(g.hits) {
		found = int32(len(g.hits))
	}
	return g.hits[:found], nil
}

// InFlight is how many batches are queued and not yet collected.
func (g *GPUContext) InFlight() int {
	if g == nil || g.ctx == 0 {
		return 0
	}
	return int(g.backend.inflight(g.ctx))
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
	if g.backend.hashOne(g.ctx, &work[0], nonce, &out[0]) != 0 {
		return out, fmt.Errorf("gpu hash: %s", g.backend.lastError())
	}
	return out, nil
}
