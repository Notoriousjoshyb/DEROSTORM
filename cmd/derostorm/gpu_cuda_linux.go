//go:build linux && amd64

package main

// The Linux half of the CUDA binding: which library is embedded, and how a
// symbol is found in it. Everything the miner actually calls is in gpu_cuda.go.
//
// amd64 only, because gpu/buildlib.sh builds an x86-64 .so and nothing else.
// An arm64 Linux build would need nvcc's aarch64 cross tools and a Jetson to
// test on; until then those builds take the gpu_other.go path and mine on the
// CPU, which is what they did before this file existed.

import (
	"embed"

	"github.com/ebitengine/purego"
)

//go:embed libderostorm_gpu.so
var gpuLibFS embed.FS

const gpuLibFile = "libderostorm_gpu.so"

// openGPULibrary dlopens the extracted library and returns a lookup for its
// symbols. purego does this without cgo, which is the whole point: the Linux
// miner is cross-compiled from Windows, and cgo would have ended that.
func openGPULibrary(path string) (func(string) (uintptr, error), error) {
	// RTLD_NOW so an unresolved symbol is an error here rather than a crash at
	// some later call, and RTLD_LOCAL because nothing else in the process wants
	// these names -- the CUDA runtime is linked in statically, so there is no
	// second copy for them to have to agree with.
	h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	return func(name string) (uintptr, error) { return purego.Dlsym(h, name) }, nil
}
