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

VERSION=1.1.1
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
