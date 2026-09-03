#!/bin/sh
# Builds the AMD library the miner embeds, for Linux x86-64.
#
# Same source as the NVIDIA one -- gpu/derostorm_gpu.cu and the headers beside
# it -- through a HIP compiler instead of nvcc. gpu/gpuapi.cuh is what makes
# that possible: every name the two runtimes disagree about is defined there,
# so there is no second copy of the kernels and no hipify step in the middle.
#
# Needs ROCm. It does not need an AMD card: the compiler targets whatever
# architectures it is asked for, not the machine it runs on.
#
#   ROCm 6, from AMD's own repository (repo.radeon.com), gives amdclang++ and
#   can target RDNA 4. This is the one to have.
#
#   ROCm 5, which on Ubuntu 24.04 is three packages from universe --
#   `apt-get install hipcc libamdhip64-dev rocm-device-libs-17` -- gives hipcc
#   and stops at RDNA 3.5.
#
# # Why the output is named after the runtime
#
# A HIP library links to the ROCm runtime by soname: libamdhip64.so.6 for
# ROCm 6, .so.5 for ROCm 5. A mining rig has one of those installed, not both,
# and no single build satisfies each -- a .so.5 library will not load on a
# ROCm 6 rig and the reverse is equally true.
#
# So the library is named for the runtime it actually ended up linked against,
# read back out of the finished ELF rather than assumed from the compiler.
# cmd/derostorm/gpu_backend_linux.go carries both names and lets dlopen pick:
# whichever one the rig can resolve is the one that runs. Building on a machine
# with only one ROCm produces one of them, which is a perfectly good library
# for every rig on that generation.
#
# Run from the repository root:  gpu/buildlib_hip.sh
set -e

# ---------------------------------------------------------------------------
# find a compiler
# ---------------------------------------------------------------------------
#
# amdclang++ first: ROCm 6 ships it and deprecated the hipcc wrapper, and it is
# the only one of the two that knows gfx1200. DSG_HIPCC overrides, which is how
# both libraries get built on a machine that has both toolchains installed.
if [ -n "${DSG_HIPCC:-}" ]; then
    HIPCC=$DSG_HIPCC
elif [ -x /opt/rocm/bin/amdclang++ ]; then
    HIPCC=/opt/rocm/bin/amdclang++
elif command -v hipcc >/dev/null 2>&1; then
    HIPCC=hipcc
elif [ -x /opt/rocm/bin/hipcc ]; then
    HIPCC=/opt/rocm/bin/hipcc
else
    echo "no HIP compiler found -- install ROCm, or set DSG_HIPCC" >&2
    exit 1
fi

# hipcc is a front end for two back ends and it picks by looking around: on a
# machine with CUDA installed it decides this is an NVIDIA build and hands the
# file to nvcc, which then fails on the HIP headers. The build machine here has
# an NVIDIA card in it, so this is not hypothetical. amdclang++ ignores it.
export HIP_PLATFORM=amd

# Where this compiler's ROCm lives, so the right libamdhip64 is linked. Without
# it a ROCm 6 amdclang++ on a machine that also has ROCm 5's development
# symlink in /usr/lib picks up the ROCm 5 one, and the finished library asks
# for a runtime that its own compiler never came from.
ROCM_PATH=${ROCM_PATH:-}
if [ -z "$ROCM_PATH" ]; then
    case "$HIPCC" in
        /*) ROCM_PATH=$(dirname "$(dirname "$HIPCC")") ;;
        *)  ROCM_PATH=$(dirname "$(dirname "$(command -v "$HIPCC")")") ;;
    esac
fi
#
# Written out with if/then rather than `[ ... ] && x=y`, which under `set -e`
# exits the whole script when the test is false. That is a real trap and not a
# style point: the ROCm 5 path takes the false branch on both of these.
LIBDIRS=""
if [ -d "$ROCM_PATH/lib" ]; then
    LIBDIRS="-L$ROCM_PATH/lib"
fi
ROCMOPT=""
if [ -d "$ROCM_PATH/lib/llvm" ]; then
    ROCMOPT="--rocm-path=$ROCM_PATH"
fi

# ---------------------------------------------------------------------------
# which architectures
# ---------------------------------------------------------------------------
#
# One code object per architecture, all in the one library, exactly as the CUDA
# build carries one cubin per compute capability.
#
# The difference is what happens to a card that is not in the list, and it is
# worse here. CUDA ships PTX alongside the cubins and the driver JITs it, so a
# newer NVIDIA card still runs. AMD code objects are final -- there is no
# portable form to fall back on -- so a gfx target missing from this library is
# a card that reports "no kernel image is available for execution" and mines on
# the CPU instead. Keep the list ahead of the hardware.
#
#   gfx1010 .. gfx1013 RDNA 1    RX 5700 XT, RX 5500 XT
#   gfx1030 .. gfx1036 RDNA 2    RX 6900/6800/6700/6600, and the laptop parts
#   gfx1100 .. gfx1103 RDNA 3    RX 7900/7800/7700/7600
#   gfx1150, gfx1151   RDNA 3.5  Strix Point APUs
#   gfx1200, gfx1201   RDNA 4    RX 9070
#
# Wave64 parts are deliberately absent: Vega, Polaris and the CDNA MI cards.
# The block radix sort's shared-memory layout is 32 lanes wide, and gpuapi.cuh
# turns a wave64 build into a compile error rather than a wrong hash.
ARCHS=${DSG_HIP_ARCHS:-"gfx1010 gfx1011 gfx1012 gfx1013 \
gfx1030 gfx1031 gfx1032 gfx1033 gfx1034 gfx1035 gfx1036 \
gfx1100 gfx1101 gfx1102 gfx1103 gfx1150 gfx1151 gfx1200 gfx1201"}

# An older ROCm does not know the newest targets, and naming one it has never
# heard of is a hard error that stops the whole build:
#
#   clang++-17: error: invalid target ID 'gfx1200'
#
# So each is offered to the compiler first and only the ones it accepts are
# built. That is the difference between "this ROCm cannot build for RX 9000" --
# true, and worth saying once -- and "this ROCm cannot build DeroStorm", which
# is what a fixed list turns it into. What gets skipped is printed, because a
# silently narrower library is exactly the kind of thing that ships and then
# fails on somebody else's card.
#
# The probe compiles an empty file: a target ID the compiler does not know is
# rejected by the driver before it looks at any source, so this costs
# milliseconds and needs no headers.
PROBE=$(mktemp /tmp/dsg_probe_XXXXXX.hip)
PROBE_O="$PROBE.o"
: > "$PROBE"

USE=""
SKIP=""
for a in $ARCHS; do
    if "$HIPCC" -x hip $ROCMOPT --offload-arch="$a" -c -o "$PROBE_O" "$PROBE" \
        >/dev/null 2>&1; then
        USE="$USE $a"
    else
        SKIP="$SKIP $a"
    fi
done
rm -f "$PROBE" "$PROBE_O"

if [ -z "$USE" ]; then
    echo "$HIPCC accepts none of the gfx targets -- is this a HIP compiler?" >&2
    exit 1
fi
if [ -n "$SKIP" ]; then
    echo "note: $HIPCC cannot target$SKIP"
    echo "      those cards will mine on the CPU; a newer ROCm builds them."
fi
echo "compiler: $HIPCC"
echo "targets: $USE"

OFFLOAD=""
for a in $USE; do OFFLOAD="$OFFLOAD --offload-arch=$a"; done

# ---------------------------------------------------------------------------
# build
# ---------------------------------------------------------------------------
#
# -x hip so a .cu extension is read as HIP rather than guessed at, and
# -mno-wavefrontsize64 to pin wave32 on every RDNA target. RDNA defaults to
# wave32 already; pinning it means a driver or compiler default that changes
# under us is a compile error and not a silent miscompile.
#
# -Wno-deprecated-pragma silences one warning per translation unit about
# __AMDGCN_WAVEFRONT_SIZE__ being on its way out. gpuapi.cuh reads it on
# purpose, to turn a wave64 build into a compile error, and there is no
# replacement for that yet.
TMPLIB=$(mktemp /tmp/dsg_hip_XXXXXX.so)
"$HIPCC" -O3 -x hip $ROCMOPT $OFFLOAD -mno-wavefrontsize64 \
         -Wno-deprecated-pragma \
         -DDSG_BUILD_DLL -DBR_BLOCK=256 \
         -fPIC -shared $LIBDIRS \
         -o "$TMPLIB" gpu/derostorm_gpu.cu

# The runtime this actually links to, read from the ELF rather than guessed
# from the compiler's version string. That is the whole naming scheme; see the
# note at the top.
MAJOR=$(readelf -d "$TMPLIB" 2>/dev/null |
        sed -n 's/.*libamdhip64\.so\.\([0-9]*\).*/\1/p' | head -1)
if [ -z "$MAJOR" ]; then
    echo "cannot tell which ROCm runtime $TMPLIB links to" >&2
    rm -f "$TMPLIB"
    exit 1
fi

OUT="cmd/derostorm/gpulib/linux/libderostorm_hip${MAJOR}.so"
mkdir -p cmd/derostorm/gpulib/linux
cp -f "$TMPLIB" "$OUT"
chmod 0755 "$OUT"
cp -f "$TMPLIB" "gpu/libderostorm_hip${MAJOR}.so"
rm -f "$TMPLIB"
echo "built $OUT (links libamdhip64.so.$MAJOR)"
