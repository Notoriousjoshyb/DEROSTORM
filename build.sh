#!/usr/bin/env bash
# DeroStorm build script (Linux / macOS / Git Bash).
#
#   ./build.sh              build for this machine into ./bin
#   ./build.sh --all        cross-compile every supported platform
#   ./build.sh --skip-tests skip the test run (not recommended, see below)
#
# Two non-obvious build flags are used, both deliberate:
#
#   -gcflags '...astrobwtv3=-B'
#       Disables bounds checks in the suffix-sort package only. That package is
#       ~90% of mining CPU time and its inner loops carry two or three bounds
#       checks each; measured +7.8% hashrate. It is only sound because the tests
#       prove no index ever goes out of range -- AstroBWTv3 swallows panics, so
#       an out-of-range access would silently produce a wrong hash rather than
#       crash. Do not skip the tests.
#
#   -pgo=auto
#       Profile-guided optimisation from cmd/derostorm/default.pgo.

set -euo pipefail
cd "$(dirname "$0")"

VERSION=1.2.0
PKG=./cmd/derostorm
BOUNDS_PKG=github.com/deroproject/derohe/astrobwt/astrobwtv3
GCFLAGS="${BOUNDS_PKG}=-B"
LDFLAGS="-s -w"
OUTDIR="$(pwd)/bin"

ALL=0
SKIP_TESTS=0
for arg in "$@"; do
  case "$arg" in
    --all)        ALL=1 ;;
    --skip-tests) SKIP_TESTS=1 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

mkdir -p "$OUTDIR"

# The embedded copies. Checked here because a missing one is a go:embed error
# with no hint as to the cause, and because which ones are needed depends on
# what is being built: the CUDA libraries are per-target, not per-host.
#
#   derostorm_gpu.dll     CUDA kernels for Windows   gpu/buildlib.bat  (needs Windows)
#   libderostorm_gpu.so   CUDA kernels for Linux     gpu/buildlib.sh   (needs Linux)
#   derostorm_sa.dll      libsais suffix sort        native/build.bat  (needs Windows)
#
# nvcc targets the host it runs on, so neither CUDA library can be built from
# the other's platform. Building every target from one machine therefore means
# building the libraries on two -- or copying them across, which is all they
# are by the time Go embeds them.
check_embedded() {
  local missing=0
  for entry in \
    "cmd/derostorm/derostorm_gpu.dll:gpu/buildlib.bat, on Windows" \
    "cmd/derostorm/libderostorm_gpu.so:gpu/buildlib.sh, on Linux" \
    "cmd/derostorm/derostorm_sa.dll:native/build.bat, on Windows"
  do
    local path="${entry%%:*}" how="${entry#*:}"
    if [ ! -f "$path" ]; then
      echo "missing $path -- build it with $how" >&2
      missing=1
    fi
  done
  [ "$missing" -eq 0 ] || exit 1
}
check_embedded

if [ "$SKIP_TESTS" -eq 0 ]; then
  echo "running tests..."
  go test ./cmd/derostorm/
fi

build() {
  local goos="$1" goarch="$2"
  local name="derostorm-${goos}-${goarch}"
  [ "$goos" = "windows" ] && name="${name}.exe"

  echo "building ${name}"
  GOOS="$goos" GOARCH="$goarch" go build \
    -trimpath -pgo=auto \
    -gcflags="$GCFLAGS" -ldflags="$LDFLAGS" \
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
