//go:build windows

package main

// The Windows half of the CUDA binding: which library is embedded, and how a
// symbol is found in it. Everything the miner actually calls is in gpu_cuda.go.

import (
	"embed"
	"syscall"
)

//go:embed derostorm_gpu.dll
var gpuLibFS embed.FS

const gpuLibFile = "derostorm_gpu.dll"

// nvmlLibName is the NVIDIA Management Library, which sensors.go asks for GPU
// temperature and power. It is not embedded and never shipped: it belongs to
// the display driver, and the loader finds it in System32 on any machine with
// one installed. A machine without simply gets no telemetry.
const nvmlLibName = "nvml.dll"

// openNativeLibrary loads a library by path and returns a lookup for its
// exports. Used for both embedded libraries and for NVML.
//
// LazyProc rather than GetProcAddress by hand because Find reports a missing
// export as an error instead of returning zero, and Addr is then a plain
// function address -- which is all purego.RegisterFunc wants. Nothing is
// actually lazy here: gpu_cuda.go resolves every name at load.
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
