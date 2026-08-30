#!/bin/sh
# Builds the suffix-array library the miner binds at run time, for Linux x86-64.
#
# The Linux twin of native/build.bat. Same four translation units, same
# whole-program optimisation, same AVX2 baseline; what differs is the object
# format -- a .so bound with dlopen instead of a .dll bound with LoadLibrary --
# and the -fPIC the shared object needs.
#
# Flag for flag against build.bat:
#
#   -O3                     /O2 /Ob3 /Oi   aggressive inlining; libsais and the
#                           descriptor merge are built around small hot
#                           functions and neither pays off uninlined.
#   -march=x86-64-v3        /arch:AVX2     the same instruction baseline. MSVC's
#                           /arch:AVX2 turns on AVX2, FMA, BMI1/2, LZCNT and
#                           MOVBE together; x86-64-v3 is exactly that set, so
#                           the two compilers are given the same machine rather
#                           than one being quietly allowed more than the other.
#   -flto                   /GL /LTCG      worth +4.7% on Windows and worth it
#                           for the same reason here: without it the descriptor
#                           merge cannot inline suffix_less across a file
#                           boundary and libsais cannot be specialised for the
#                           one way this program calls it.
#   -fno-stack-protector    /GS-
#   -fvisibility=hidden     the DS_API attribute exports the five entry points
#                           and nothing else, which is what /LD gives on
#                           Windows by default.
#
# sha256ni.c gets -msha on top. The SHA extensions are not part of x86-64-v3 --
# they are a separate CPU feature, present on Zen and on Ice Lake and later --
# so the intrinsics will not compile without it. That does not make the library
# require them: dsa_sha_probe asks the CPU at run time and the miner leaves the
# pairing hook unset when the answer is no. Only this one file is given the
# flag, so no other function can pick up an instruction the probe did not
# clear.
#
# AVX2 itself is a hard requirement of the finished .so, exactly as it is of the
# DLL. sa_lib.go checks for it before loading, so a CPU without AVX2 gets the Go
# sort and a sentence saying why, rather than SIGILL.
#
# Run from the repository root:  native/buildlib.sh
set -e

CC=${CC:-gcc}
OUT=native/libderostorm_sa.so

BASE="-O3 -march=x86-64-v3 -flto -fPIC -fno-stack-protector -fvisibility=hidden"

# One compiler invocation per file rather than one for all four, because
# sha256ni.c needs -msha and nothing else may have it. Objects first, then a
# single LTO link.
$CC $BASE -Inative -c native/derostorm_sa.c   -o native/derostorm_sa.o
$CC $BASE -Inative -c native/descriptor.c     -o native/descriptor.o
$CC $BASE -Inative -c native/libsais/libsais.c -o native/libsais.o
$CC $BASE -msha -Inative -c native/sha256ni.c -o native/sha256ni.o

$CC $BASE -shared -o "$OUT" \
    native/derostorm_sa.o native/descriptor.o native/libsais.o native/sha256ni.o \
    -Wl,--no-undefined

rm -f native/derostorm_sa.o native/descriptor.o native/libsais.o native/sha256ni.o

cp -f "$OUT" cmd/derostorm/
echo "built $OUT and copied it to cmd/derostorm/"
