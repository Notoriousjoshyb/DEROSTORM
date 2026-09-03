#!/bin/sh
# Builds the suffix-array library the miner binds at run time.
#
# Linux amd64 is the original target: a .so bound with dlopen, AVX2, SHA-NI.
# Darwin and linux/arm64 produce a dylib/.so with NEON and ARMv8 SHA-2; the
# miner on those platforms prefers compiling the same sources in via cgo
# (internal/sacgo) when it is being built on the machine that will run it.
# This script is for a standalone library, and for linux/amd64 which is
# cross-compiled from Windows and cannot use cgo.
#
# Run from the repository root:  native/buildlib.sh
set -e

CC=${CC:-cc}
OS=$(uname -s 2>/dev/null || echo unknown)
ARCH=$(uname -m 2>/dev/null || echo unknown)

SHA=native/sha256ni.c
SHAFLAGS="-msha"
LINKFLAGS="-shared -Wl,--no-undefined"
OUT=native/libderostorm_sa.so
BASE="-O3 -flto -fPIC -fvisibility=hidden"

case "$OS/$ARCH" in
  Darwin/arm64|Darwin/aarch64)
    OUT=native/libderostorm_sa.dylib
    BASE="$BASE -mcpu=apple-m1"
    SHA=native/sha256arm.c
    SHAFLAGS=""
    LINKFLAGS="-dynamiclib"
    ;;
  Darwin/x86_64)
    OUT=native/libderostorm_sa.dylib
    BASE="$BASE -march=x86-64-v3 -fno-stack-protector"
    LINKFLAGS="-dynamiclib"
    ;;
  Linux/aarch64|Linux/arm64)
    OUT=native/libderostorm_sa.so
    BASE="$BASE -march=armv8-a+crypto -fno-stack-protector"
    SHA=native/sha256arm.c
    SHAFLAGS=""
    ;;
  *)
    BASE="$BASE -march=x86-64-v3 -fno-stack-protector"
    # Tune for the Zen 4/5 miners actually run on without changing the ISA:
    # -mtune only schedules, so the library still runs everywhere v3 does.
    # Probed because older GCCs do not know znver5.
    if echo 'int x;' | $CC -mtune=znver5 -x c -c - -o /dev/null 2>/dev/null; then
      BASE="$BASE -mtune=znver5"
    elif echo 'int x;' | $CC -mtune=znver4 -x c -c - -o /dev/null 2>/dev/null; then
      BASE="$BASE -mtune=znver4"
    fi
    ;;
esac

$CC $BASE -Inative -c native/derostorm_sa.c   -o native/derostorm_sa.o
$CC $BASE -Inative -c native/descriptor.c     -o native/descriptor.o
$CC $BASE -Inative -c native/libsais/libsais.c -o native/libsais.o
$CC $BASE $SHAFLAGS -Inative -c "$SHA" -o native/sha256.o

$CC $BASE $LINKFLAGS -o "$OUT" \
    native/derostorm_sa.o native/descriptor.o native/libsais.o native/sha256.o

rm -f native/derostorm_sa.o native/descriptor.o native/libsais.o native/sha256.o

cp -f "$OUT" cmd/derostorm/
echo "built $OUT and copied it to cmd/derostorm/"
