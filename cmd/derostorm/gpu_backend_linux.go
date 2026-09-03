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
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/ebitengine/purego"
)

// The NVIDIA library lives in a subdirectory, and the whole subdirectory is
// what is embedded, so a build made on an AMD-only rig with no nvcc still
// compiles -- there is a README in there and go:embed is happy with just that.
// The CUDA backend then finds no file, reports no devices, and the miner
// behaves exactly as it does on a machine with no NVIDIA card. Dropping the
// .so in and rebuilding is the whole of turning NVIDIA support on.
//
//go:embed gpucuda/linux
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
		kind: "NVIDIA CUDA", libFS: gpuLibFS,
		files: []string{"gpucuda/linux/libderostorm_gpu.so"},
	}
	// However many AMD libraries the build has, newest ROCm first.
	//
	// A HIP library links against the ROCm runtime by soname --
	// libamdhip64.so.7 for ROCm 7, .so.6 for ROCm 6, .so.5 for ROCm 5. A rig
	// has one of those installed, not three, and there is no build that
	// satisfies each -- so one is built per generation and dlopen decides.
	//
	// The list is read out of the embedded directory rather than written here,
	// and that is the point. It used to be two names spelled out in this file,
	// which is how ROCm 7 rigs ended up with no AMD support in 1.7.2: the
	// library naming already followed the runtime (gpu/buildlib_hip.sh reads
	// the soname back out of the finished ELF), so a ROCm 7 build produced
	// libderostorm_hip7.so that nothing ever looked for. Scanning means the
	// next generation needs a build and not a code change.
	//
	// Neither ordering nor presence is required. A binary built with none has
	// no AMD support and says so by reporting no devices.
	hipBackend = &gpuBackend{
		kind: "AMD HIP", libFS: hipLibFS,
		files: hipLibFiles(),
	}
)

// hipLibDir is the directory embedded above, and the one hipLibFiles scans.
const hipLibDir = "gpulib/linux"

// hipLibFiles lists the embedded AMD libraries, highest ROCm major first.
//
// Highest first because that is the generation a new rig has and the only one
// that can target the newest cards -- the older builds are the fallback, in the
// order a machine is likely to be on. Anything in the directory that is not a
// libderostorm_hip<major>.so is ignored, which is what keeps the README beside
// them from being tried as a library.
func hipLibFiles() []string {
	ents, err := hipLibFS.ReadDir(hipLibDir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return hipLibOrder(names)
}

// hipLibOrder is the naming rule on its own, split out from the directory read
// so a test can state it without a build that has the libraries in it.
func hipLibOrder(names []string) []string {
	type lib struct {
		major int
		name  string
	}
	var libs []lib
	for _, n := range names {
		rest, ok := strings.CutPrefix(n, "libderostorm_hip")
		if !ok {
			continue
		}
		rest, ok = strings.CutSuffix(rest, ".so")
		if !ok {
			continue
		}
		major, err := strconv.Atoi(rest)
		if err != nil {
			continue
		}
		libs = append(libs, lib{major, n})
	}
	sort.Slice(libs, func(i, j int) bool { return libs[i].major > libs[j].major })

	files := make([]string, 0, len(libs))
	for _, l := range libs {
		files = append(files, path.Join(hipLibDir, l.name))
	}
	return files
}

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
