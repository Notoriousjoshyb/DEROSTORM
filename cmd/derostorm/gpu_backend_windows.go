//go:build windows

package main

// The Windows half of the GPU binding: which libraries are embedded, and how a
// symbol is found in one. Everything the miner actually calls is in
// gpu_backend.go.

import (
	"embed"
	"syscall"
)

//go:embed derostorm_gpu.dll
var gpuLibFS embed.FS

// The AMD library lives in a subdirectory, and the whole subdirectory is what
// is embedded, so a build made before anyone has run gpu\buildlib_hip.bat still
// compiles -- there is a README in there and go:embed is happy with just that.
// The HIP backend then finds no file, reports no devices, and the miner behaves
// exactly as it does on a machine with no AMD card. Dropping the DLL in and
// rebuilding is the whole of turning AMD support on.
//
// Per-platform rather than one shared directory because a Windows binary has no
// use for the Linux .so and would otherwise carry a few megabytes of it.
//
//go:embed gpulib/windows
var hipLibFS embed.FS

var (
	cudaBackend = &gpuBackend{
		kind: "NVIDIA CUDA", libFS: gpuLibFS,
		files: []string{"derostorm_gpu.dll"},
	}
	// One candidate, where Linux has two. Windows has only ever had the ROCm 6
	// line -- amdhip64_6.dll, which ships inside the Adrenalin driver -- so
	// there is no second generation to fall back to.
	hipBackend = &gpuBackend{
		kind: "AMD HIP", libFS: hipLibFS,
		files: []string{"gpulib/windows/derostorm_hip.dll"},
	}
)

// nvmlLibName is the NVIDIA Management Library, which sensors.go asks for GPU
// temperature and power. It is not embedded and never shipped: it belongs to
// the display driver, and the loader finds it in System32 on any machine with
// one installed. A machine without simply gets no telemetry.
const nvmlLibName = "nvml.dll"

// rocmSMILibName is the AMD equivalent, and unlike NVML it is not part of the
// display driver: it comes with ROCm, which on Windows means the HIP SDK rather
// than the Adrenalin package most miners install. So AMD telemetry is usually
// absent on Windows and usually present on Linux, and absent costs nothing but
// the temperature column.
const rocmSMILibName = "rocm_smi64.dll"

// openNativeLibrary loads a library by path and returns a lookup for its
// exports. Used for both embedded libraries and for the telemetry ones.
//
// LazyProc rather than GetProcAddress by hand because Find reports a missing
// export as an error instead of returning zero, and Addr is then a plain
// function address -- which is all purego.RegisterFunc wants. Nothing is
// actually lazy here: gpu_backend.go resolves every name at load.
func openNativeLibrary(path string) (func(string) (uintptr, error), error) {
	dll := syscall.NewLazyDLL(path)
	if err := dll.Load(); err != nil {
		return nil, err
	}
	return func(name string) (uintptr, error) {
		p := dll.NewProc(name)
		if err := p.Find(); err != nil {
			return 0, err
		}
		return p.Addr(), nil
	}, nil
}
