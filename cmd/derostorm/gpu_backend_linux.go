//go:build linux && amd64

package main

// The Linux half of the GPU binding: which libraries are embedded, and how a
// symbol is found in one. Everything the miner actually calls is in
// gpu_backend.go.
//
// amd64 only, because gpu/buildlib.sh and gpu/buildlib_hip.sh build x86-64
// objects and nothing else. An arm64 Linux build would need nvcc's aarch64
// cross tools and a Jetson to test on; until then those builds take the
// gpu_other.go path and mine on the CPU, which is what they did before this
// file existed.

import (
	"embed"

	"github.com/ebitengine/purego"
)

//go:embed libderostorm_gpu.so
var gpuLibFS embed.FS

// The AMD library lives in a subdirectory, and the whole subdirectory is what
// is embedded, so a build made before anyone has run gpu/buildlib_hip.sh still
// compiles -- there is a README in there and go:embed is happy with just that.
// The HIP backend then finds no file, reports no devices, and the miner behaves
// exactly as it does on a machine with no AMD card. Dropping the .so in and
// rebuilding is the whole of turning AMD support on.
//
// Per-platform rather than one shared directory because a Linux binary has no
// use for the Windows DLL and would otherwise carry a few megabytes of it.
//
//go:embed gpulib/linux
var hipLibFS embed.FS

var (
	cudaBackend = &gpuBackend{
		kind: "NVIDIA CUDA", libFS: gpuLibFS, file: "libderostorm_gpu.so",
	}
	hipBackend = &gpuBackend{
		kind: "AMD HIP", libFS: hipLibFS, file: "gpulib/linux/libderostorm_hip.so",
	}
)

// nvmlLibName is the NVIDIA Management Library, which sensors.go asks for GPU
// temperature and power. The versioned soname is deliberate: plain
// libnvidia-ml.so is a development symlink that only exists where the CUDA
// toolkit is installed, while .so.1 ships with the driver itself.
const nvmlLibName = "libnvidia-ml.so.1"

// rocmSMILibName is the AMD equivalent. Same reasoning about the soname, and
// the same graceful nothing when it is absent -- though it rarely is on a
// machine that can run the HIP kernels at all, since both come from ROCm.
const rocmSMILibName = "librocm_smi64.so.1"

// openNativeLibrary dlopens a library by path and returns a lookup for its
// symbols. Used for both embedded libraries and for the telemetry ones. purego
// does this without cgo, which is the whole point: the Linux miner is
// cross-compiled from Windows, and cgo would have ended that.
func openNativeLibrary(path string) (func(string) (uintptr, error), error) {
	// RTLD_NOW so an unresolved symbol is an error here rather than a crash at
	// some later call, and RTLD_LOCAL because nothing else in the process wants
	// these names -- the CUDA runtime is linked in statically, so there is no
	// second copy for them to have to agree with. The HIP library is the one
	// exception: it links against ROCm's amdhip64 dynamically, because there is
	// no static ROCm runtime to fold in. dlopen resolves that itself, and a
	// machine without ROCm is where the load fails and the backend goes quiet.
	h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	return func(name string) (uintptr, error) { return purego.Dlsym(h, name) }, nil
}
