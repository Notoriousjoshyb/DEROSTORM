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

// openGPULibrary loads the extracted DLL and returns a lookup for its exports.
//
// LazyProc rather than GetProcAddress by hand because Find reports a missing
// export as an error instead of returning zero, and Addr is then a plain
// function address -- which is all purego.RegisterFunc wants. Nothing is
// actually lazy here: gpu_cuda.go resolves every name at load.
func openGPULibrary(path string) (func(string) (uintptr, error), error) {
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
