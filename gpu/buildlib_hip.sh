#!/bin/sh
# Builds the AMD library the miner embeds, for Linux x86-64.
#
# Same source as the NVIDIA one -- gpu/derostorm_gpu.cu and the headers beside
# it -- through hipcc instead of nvcc. gpu/gpuapi.cuh is what makes that
# possible: every name the two runtimes disagree about is defined there, so
# there is no second copy of the kernels and no hipify step in the middle.
#
# Needs ROCm installed (hipcc). It does not need an AMD card: the compiler
# targets whatever architectures are named below, not the machine it runs on.
#
# Run from the repository root:  gpu/buildlib_hip.sh
set -e

HIPCC=${HIPCC:-hipcc}
command -v "$HIPCC" >/dev/null 2>&1 || HIPCC=/opt/rocm/bin/hipcc

# One code object per architecture, all in the one library, exactly as the CUDA
# build carries one cubin per compute capability.
#
# The difference is what happens to a card that is not in the list, and it is
# worse here. CUDA ships PTX alongside the cubins and the driver JITs it, so a
# newer NVIDIA card still runs. AMD code objects are final -- there is no
# portable form to fall back on -- so a gfx target missing from this list is a
# card that reports "no kernel image is available for execution" and mines on
# the CPU instead. Keep the list ahead of the hardware.
#
#   gfx1010, gfx1012   RDNA 1   RX 5700 XT, RX 5500 XT
#   gfx1030 .. gfx1036 RDNA 2   RX 6900/6800/6700/6600, and the laptop parts
#   gfx1100 .. gfx1103 RDNA 3   RX 7900/7800/7700/7600
#   gfx1200, gfx1201   RDNA 4   RX 9070
#
# Wave64 parts are deliberately absent: Vega, Polaris and the CDNA MI cards.
# The block radix sort's shared-memory layout is 32 lanes wide, and gpuapi.cuh
# turns a wave64 build into a compile error rather than a wrong hash.
#
# An older ROCm will not know the newest targets, and a target its compiler has
# never heard of stops the build. Override the whole list when that happens:
#   DSG_HIP_ARCHS="gfx1030 gfx1100" gpu/buildlib_hip.sh
ARCHS=${DSG_HIP_ARCHS:-"gfx1010 gfx1012 gfx1030 gfx1031 gfx1032 gfx1034 gfx1035 gfx1036 gfx1100 gfx1101 gfx1102 gfx1103 gfx1200 gfx1201"}

OFFLOAD=""
for a in $ARCHS; do OFFLOAD="$OFFLOAD --offload-arch=$a"; done

# -x hip so a .cu extension is read as HIP rather than guessed at, and
# -mno-wavefrontsize64 to pin wave32 on every RDNA target. RDNA defaults to
# wave32 already; pinning it means a driver or compiler default that changes
# under us is a compile error and not a silent miscompile.
"$HIPCC" -O3 -x hip $OFFLOAD -mno-wavefrontsize64 \
         -DDSG_BUILD_DLL -DBR_BLOCK=256 \
         -fPIC -shared \
         -o gpu/libderostorm_hip.so gpu/derostorm_gpu.cu

mkdir -p cmd/derostorm/gpulib
cp -f gpu/libderostorm_hip.so cmd/derostorm/gpulib/
echo "built gpu/libderostorm_hip.so and copied it to cmd/derostorm/gpulib/"
