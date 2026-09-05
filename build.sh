#!/usr/bin/env bash
# DeroStorm build script (Linux / macOS / Git Bash).
#
#   ./build.sh              build for this machine into ./bin
#   ./build.sh --all        cross-compile every supported platform
#   ./build.sh --native     build this platform's embedded libraries first
#   ./build.sh --skip-tests skip the test run (not recommended, see below)
#   ./build.sh --clean      delete build output and one-off test binaries
#
# Two non-obvious build flags are used, both deliberate:
#
#   -gcflags '...astrobwtv3=-B'      AMD64 ONLY -- see below
#       Disables bounds checks in the suffix-sort package only. That package is
#       ~90% of mining CPU time and its inner loops carry two or three bounds
#       checks each; measured +7.8% hashrate. It is only sound because the tests
#       prove no index ever goes out of range -- AstroBWTv3 swallows panics, so
#       an out-of-range access would silently produce a wrong hash rather than
#       crash. Do not skip the tests.
#
#       It is passed on amd64 and nowhere else, because on arm64 it does not
#       produce a wrong hash, it produces a crash:
#
#         unexpected fault address 0x7b681b88b333
#         fatal error: fault
#
#       Reported from a Mac (darwin/arm64) and reproduced on linux/arm64 under
#       qemu, in the miner and in a bare hashing loop. The same loop is clean on
#       amd64 with -B, and clean on arm64 *without* it -- 16,000 hashes each way,
#       astrobwtv3.RecoveredPanics zero on both, so this is not an out-of-range
#       index the checks were hiding. The algorithm is sound on arm64; what is
#       not sound is turning the checks off there.
#
#       This is the failure that flag was always going to have: it was licensed
#       by a test suite that only ever ran on the machine doing the building, and
#       the cross-compiled targets inherited a guarantee nothing had checked for
#       them. amd64 keeps it because amd64 is what the tests run on.
#
#   -pgo=auto
#       Profile-guided optimisation from cmd/derostorm/default.pgo.

set -euo pipefail
cd "$(dirname "$0")"

VERSION=1.9.1
PKG=./cmd/derostorm
BOUNDS_PKG=github.com/deroproject/derohe/astrobwt/astrobwtv3
LDFLAGS="-s -w"
OUTDIR="$(pwd)/bin"

ALL=0
SKIP_TESTS=0
CLEAN=0
NATIVE=0
for arg in "$@"; do
  case "$arg" in
    --all)        ALL=1 ;;
    --native)     NATIVE=1 ;;
    --skip-tests) SKIP_TESTS=1 ;;
    --clean)      CLEAN=1 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

# --clean removes build output and the disposable measurement binaries, then
# exits. It is a separate flag rather than something a build does on its own,
# because a build that quietly deletes things is a build nobody trusts.
#
# What it never touches: the embedded libraries under cmd/derostorm. They
# are inputs to the build, not outputs of it -- go:embed needs the suffix-sort
# pair there, and rebuilding anything needs its own toolchain. gpu/vectors.bin
# is left for the same reason: the GPU tests read it.
if [ "$CLEAN" -eq 1 ]; then
  # bin is emptied of build output rather than removed. It also holds
  # derostorm.json, which carries the wallet address the setup wizard asked
  # for, and deleting a user's config as part of "clean the build" is the kind
  # of helpfulness nobody asks for twice.
  for p in \
      bench derostorm.exe cmd/derostorm/derostorm.exe \
      bin/derostorm-* bin/*.dll bin/*.so bin/*.log \
      native/sabench*.exe native/saprof*.exe native/sapro_*.exe \
      native/shabench*.exe native/sabench native/saprof \
      gpu/*_test*.exe gpu/desc_test*.exe gpu/hash*.exe gpu/prof/prof.exe \
      gpu/*.exp gpu/*.lib native/*.exp native/*.lib native/*.obj
  do
    [ -e "$p" ] || continue
    rm -rf "$p"
    echo "  removed $p"
  done
  echo
  echo "cleaned"
  exit 0
fi

mkdir -p "$OUTDIR"

# The embedded copies, checked per target rather than up front.
#
# Which ones a build needs is decided by build tags, not by the host. The
# suffix-sort library is the only required one: it needs just gcc, which an
# AMD-only rig has as well. The GPU libraries are all optional -- see
# check_optional_embedded -- because each needs its own vendor toolchain and
# an AMD-only rig has no nvcc at all:
#
#   windows/amd64   derostorm_sa.dll     native/build.bat     (built on Windows)
#   linux/amd64     libderostorm_sa.so   native/buildlib.sh   (built on Linux)
#   everything else nothing
#
# Checking all whatever the target is what an earlier version did, and it
# stopped macOS building at all: a Mac needs none of them -- gpu_other.go and
# sa_other.go cover that build and embed nothing -- so the check failed on files
# the compiler would never have asked for. A missing one is still worth catching,
# because go:embed reports it with no hint as to the cause, but only when the
# target actually embeds it.
#
# nvcc targets the host it runs on, so neither CUDA library can be built from the
# other's platform. Building every target from one machine therefore means
# building the libraries on two -- or copying them across, which is all they are
# by the time Go embeds them.
check_embedded() {
  local goos="$1" goarch="$2" missing=0 entries=""
  case "$goos/$goarch" in
    windows/*)   entries="cmd/derostorm/derostorm_sa.dll:native/build.bat, on Windows" ;;
    linux/amd64) entries="cmd/derostorm/libderostorm_sa.so:native/buildlib.sh, on Linux" ;;
    *)           return 0 ;;
  esac
  while IFS= read -r entry; do
    [ -n "$entry" ] || continue
    local path="${entry%%:*}" how="${entry#*:}"
    if [ ! -f "$path" ]; then
      echo "$goos/$goarch embeds $path, which is missing -- build it with $how" >&2
      missing=1
      continue
    fi
    # Present is not the same as current, and the difference has already
    # shipped once. The 1.4.0 Linux archives carried the *previous* CUDA
    # kernels: nvcc cannot build a .so under Windows, the machine cutting the
    # release had no Linux toolchain, and nothing objected because the stale
    # file was sitting exactly where go:embed wanted it. Linux mined at 1.3.0
    # speed for a whole release while Windows had everything, and 1.4.1 exists
    # only to undo it.
    #
    # So a library older than any source it is compiled from stops the build.
    # It is a timestamp comparison and it will occasionally fire on a file that
    # changed nothing -- gpu/prof.cuh compiles out of the shipped kernel and
    # still counts. Rebuilding is cheap and shipping the wrong kernels is not.
    local sources
    case "$path" in
      *derostorm_gpu.*) sources="gpu/derostorm_gpu.cu gpu/derostorm_gpu.h $(echo gpu/*.cuh gpu/*.inc)" ;;
      *)                sources="native/derostorm_sa.c native/descriptor.c native/descriptor.h native/sha256ni.c native/sha256ni.h native/sha256arm.c native/libsais/libsais.c native/libsais/libsais.h" ;;
    esac
    local src stale=""
    for src in $sources; do
      [ -f "$src" ] || continue
      [ "$src" -nt "$path" ] && stale="$stale $src"
    done
    if [ -n "$stale" ]; then
      echo "$path is older than the source it is built from ($(echo $stale | cut -c1-120)) -- rebuild it with $how" >&2
      missing=1
    fi
  done <<EOF
$entries
EOF
  [ "$missing" -eq 0 ] || exit 1
}

# The GPU libraries, which may legitimately not be there.
#
# go:embed takes the whole gpucuda/<goos> and gpulib/<goos> directories rather
# than the files, so a tree without them still compiles -- see
# cmd/derostorm/gpu_backend.go. That makes absence a build configuration and
# not an error: the finished miner reports no devices for that vendor, exactly
# as it would on a machine with no such card. An AMD-only rig has no nvcc, so
# it can never build the CUDA library; before it went optional that ended the
# build in a go:embed error naming the missing file and nothing else.
#
# What is still an error is a stale one. The reasoning is check_embedded's, and
# it bites harder here: nobody on this side of the build may have the card to
# notice with.
check_optional_embedded() {
  local goos="$1" goarch="$2" paths="" how=""
  case "$goos/$goarch" in
    windows/*)   paths="cmd/derostorm/gpucuda/windows/derostorm_gpu.dll
cmd/derostorm/gpulib/windows/derostorm_hip.dll"
                 how="a GPU toolchain on Windows (nvcc for CUDA, the AMD HIP SDK for HIP)" ;;
    # One per ROCm generation, because a HIP library names the runtime it links
    # to by soname and a rig has one generation installed, not three. Whichever
    # ones are here get checked; none is required.
    linux/amd64) paths="cmd/derostorm/gpucuda/linux/libderostorm_gpu.so
cmd/derostorm/gpulib/linux/libderostorm_hip7.so
cmd/derostorm/gpulib/linux/libderostorm_hip6.so
cmd/derostorm/gpulib/linux/libderostorm_hip5.so"
                 how="a GPU toolchain on Linux (nvcc for CUDA, ROCm for HIP)" ;;
    *)           return 0 ;;
  esac
  local path src stale
  for path in $paths; do
    [ -f "$path" ] || continue
    stale=""
    for src in gpu/derostorm_gpu.cu gpu/derostorm_gpu.h gpu/*.cuh gpu/*.inc; do
      [ -f "$src" ] || continue
      [ "$src" -nt "$path" ] && stale="$stale $src"
    done
    if [ -n "$stale" ]; then
      echo "$path is older than the source it is built from ($(echo $stale | cut -c1-120)) -- rebuild it with $how" >&2
      exit 1
    fi
  done
}

# The host's own libraries are checked before the tests, not with the first
# target, because `go test ./cmd/derostorm/` compiles that package for the host
# and therefore embeds them too. Checking per target only was enough to give a
# clear message on a build and no message at all on a clone: the tests ran
# first and failed with
#
#     pattern gpucuda/linux: no matching files found
#     FAIL github.com/notoriousjoshyb/derostorm/cmd/derostorm [setup failed]
#
# which says nothing about what to do. The embedded libraries are gitignored --
# a 3 MB fat binary in git is 3 MB in git forever -- so a fresh clone never has
# them and this is the first thing anyone building from one hits.
# --native builds the embedded libraries this platform can build, which is the
# Linux pair -- the twin of build.ps1 -Native. It is not the default: the
# libraries only need rebuilding when their sources change, and a CUDA build
# across six architectures is minutes against seconds for the Go build.
#
# Only Linux has anything to do here. A Mac embeds nothing (gpu_other.go and
# sa_other.go cover that build), and the Windows DLLs cannot be produced from a
# Linux toolchain at all -- nvcc and the host compiler both target the machine
# they run on -- so `--all` from Linux still needs those two copied across from
# a Windows build. That is not a limitation of this script; it is what a
if [ "$NATIVE" -eq 1 ]; then
  case "$(go env GOOS)" in
    linux)
      # The CUDA kernels only if this machine can: an AMD-only rig has no
      # nvcc, and since the library went optional that is a narrower build,
      # not a failure -- the miner builds and mines without it, on everything
      # but NVIDIA.
      NVCC_BIN="${NVCC:-nvcc}"
      command -v "$NVCC_BIN" >/dev/null 2>&1 || NVCC_BIN=/usr/local/cuda/bin/nvcc
      if command -v "$NVCC_BIN" >/dev/null 2>&1; then
        echo "building CUDA kernels (Linux)"
        sh gpu/buildlib.sh
      else
        echo "no CUDA compiler found -- skipping the NVIDIA kernels (this build will have no NVIDIA support)"
      fi
      # The AMD kernels only if this machine can: ROCm is a large install and
      # most build machines have none. Skipping is not a failure -- the miner
      # builds and mines without them, on everything but AMD.
      # amdclang++ counts as well as hipcc, and has to: ROCm 6 deprecated the
      # hipcc wrapper and ROCm 7 on Arch ships amdclang++ without it, so a
      # hipcc-only test skipped the AMD kernels on exactly the machines that
      # could build the newest ones. This mirrors buildlib_hip.sh's own search.
      if [ -n "${DSG_HIPCC:-}" ] || [ -x /opt/rocm/bin/amdclang++ ] ||
         command -v hipcc >/dev/null 2>&1 || [ -x /opt/rocm/bin/hipcc ]; then
        echo "building HIP kernels (Linux)"
        sh gpu/buildlib_hip.sh
      else
        echo "no HIP compiler found -- skipping the AMD kernels (this build will have no AMD support)"
      fi
      echo "building descriptor suffix sort (Linux)"
      sh native/buildlib.sh
      ;;
    *)
      echo "--native has nothing to build on $(go env GOOS); skipping"
      ;;
  esac
fi

check_embedded "$(go env GOOS)" "$(go env GOARCH)"
check_optional_embedded "$(go env GOOS)" "$(go env GOARCH)"

if [ "$SKIP_TESTS" -eq 0 ]; then
  echo "running tests..."
  go test ./cmd/derostorm/
fi

build() {
  local goos="$1" goarch="$2"
  local name="derostorm-${goos}-${goarch}"
  [ "$goos" = "windows" ] && name="${name}.exe"

  check_embedded "$goos" "$goarch"
  check_optional_embedded "$goos" "$goarch"
  echo "building ${name}"

  # -B on amd64 and nowhere else; see the note at the top of this file. Empty
  # is a valid -gcflags spec, so the build line stays one shape either way.
  local gc="" gc2=""
  if [ "$goarch" = "amd64" ]; then
    gc="${BOUNDS_PKG}=-B"
    gc2="github.com/notoriousjoshyb/derostorm/internal/dsa=-B"
  fi

  # Cross-compiles cannot use cgo: darwin's native sort is compiled in with
  # cgo only when this script is running on the Mac that will mine.
  local cgo=""
  if [ "$goos" != "$(go env GOOS)" ] || [ "$goarch" != "$(go env GOARCH)" ]; then
    cgo="CGO_ENABLED=0"
  fi

  env $cgo GOOS="$goos" GOARCH="$goarch" go build \
    -trimpath -pgo=auto \
    ${gc:+-gcflags="$gc"} ${gc2:+-gcflags="$gc2"} -ldflags="$LDFLAGS" \
    -o "$OUTDIR/$name" "$PKG"
  echo "  -> $OUTDIR/$name"
}

if [ "$ALL" -eq 1 ]; then
  build windows amd64
  build linux   amd64
  build linux   arm64
  build darwin  amd64
  build darwin  arm64
else
  build "$(go env GOOS)" "$(go env GOARCH)"
fi

echo
echo "DeroStorm ${VERSION} built into ${OUTDIR}"
