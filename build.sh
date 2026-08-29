#!/usr/bin/env bash
# DeroStorm build script (Linux / macOS / Git Bash).
#
#   ./build.sh              build for this machine into ./bin
#   ./build.sh --all        cross-compile every supported platform
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

VERSION=1.3.0
PKG=./cmd/derostorm
BOUNDS_PKG=github.com/deroproject/derohe/astrobwt/astrobwtv3
LDFLAGS="-s -w"
OUTDIR="$(pwd)/bin"

ALL=0
SKIP_TESTS=0
CLEAN=0
for arg in "$@"; do
  case "$arg" in
    --all)        ALL=1 ;;
    --skip-tests) SKIP_TESTS=1 ;;
    --clean)      CLEAN=1 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

# --clean removes build output and the disposable measurement binaries, then
# exits. It is a separate flag rather than something a build does on its own,
# because a build that quietly deletes things is a build nobody trusts.
#
# What it never touches: the three embedded libraries under cmd/derostorm. They
# are inputs to the build, not outputs of it -- go:embed fails without them and
# rebuilding them needs nvcc, MSVC and WSL. gpu/vectors.bin is left for the same
# reason: the GPU tests read it.
if [ "$CLEAN" -eq 1 ]; then
  for p in bin bench derostorm.exe cmd/derostorm/derostorm.exe            native/sabench*.exe native/saprof*.exe native/sapro_*.exe            native/shabench*.exe native/sabench native/saprof            gpu/*_test*.exe gpu/desc_test*.exe gpu/hash*.exe gpu/prof/prof.exe            gpu/*.exp gpu/*.lib native/*.exp native/*.lib native/*.obj; do
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
# Which ones a build needs is decided by build tags, not by the host:
#
#   windows/amd64   derostorm_gpu.dll   gpu/buildlib.bat   (built on Windows)
#                   derostorm_sa.dll    native/build.bat   (built on Windows)
#   linux/amd64     libderostorm_gpu.so gpu/buildlib.sh    (built on Linux)
#   everything else nothing
#
# Checking all three whatever the target is what an earlier version did, and it
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
    windows/*)   entries="cmd/derostorm/derostorm_gpu.dll:gpu/buildlib.bat, on Windows
cmd/derostorm/derostorm_sa.dll:native/build.bat, on Windows" ;;
    linux/amd64) entries="cmd/derostorm/libderostorm_gpu.so:gpu/buildlib.sh, on Linux" ;;
    *)           return 0 ;;
  esac
  while IFS= read -r entry; do
    [ -n "$entry" ] || continue
    local path="${entry%%:*}" how="${entry#*:}"
    if [ ! -f "$path" ]; then
      echo "$goos/$goarch embeds $path, which is missing -- build it with $how" >&2
      missing=1
    fi
  done <<EOF
$entries
EOF
  [ "$missing" -eq 0 ] || exit 1
}

if [ "$SKIP_TESTS" -eq 0 ]; then
  echo "running tests..."
  go test ./cmd/derostorm/
fi

build() {
  local goos="$1" goarch="$2"
  local name="derostorm-${goos}-${goarch}"
  [ "$goos" = "windows" ] && name="${name}.exe"

  check_embedded "$goos" "$goarch"
  echo "building ${name}"

  # -B on amd64 and nowhere else; see the note at the top of this file. Empty
  # is a valid -gcflags spec, so the build line stays one shape either way.
  local gc=""
  [ "$goarch" = "amd64" ] && gc="${BOUNDS_PKG}=-B"

  GOOS="$goos" GOARCH="$goarch" go build \
    -trimpath -pgo=auto \
    -gcflags="$gc" -ldflags="$LDFLAGS" \
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
