#!/bin/sh
# Builds the CUDA library the miner embeds, for Linux x86-64.
#
# The Linux twin of gpu/buildlib.bat. Same source, same architectures, same
# -cudart static so the finished miner needs only the NVIDIA driver; what
# differs is the object format -- a .so bound with dlopen instead of a .dll
# bound with LoadLibrary -- and the -fPIC the host compiler needs for one.
#
# Windows cannot build this. nvcc targets the host it runs on, so a Linux .so
# has to come from a Linux toolchain. WSL is enough: the build needs nvcc, not
# a GPU, so no card has to be visible to the distribution doing it.
#
# The copy in cmd/derostorm/gpucuda/linux/ is what go:embed picks up. It is
# checked in the same place the DLL is, in build.ps1, because a missing one
# used to be a go:embed error with no hint as to the cause -- since the CUDA
# library went optional that check only fires when the file is present but
# stale. Cross-compiling the Linux miner from Windows works precisely because
# the .so is a file on disk by then.
#
# Run from the repository root:  gpu/buildlib.sh
set -e

NVCC=${NVCC:-nvcc}
command -v "$NVCC" >/dev/null 2>&1 || NVCC=/usr/local/cuda/bin/nvcc

# One cubin per architecture, all in the one library (a "fat binary"). Building
# for the local card alone is what makes dsg_init fail on someone else's with
# "no kernel image is available for execution": the driver will not run a cubin
# built for a different compute capability, and it will not lower one either.
# CUDA 13 dropped Pascal and Volta, so sm_75 (Turing, RTX 20xx) is the floor.
# compute_120 PTX rides along last so a card newer than this toolkit JITs from
# it at load instead of failing. Keep this list in step with buildlib.bat.
#
#   sm_75  RTX 20xx, GTX 16xx, T4
#   sm_80  A100
#   sm_86  RTX 30xx, A40
#   sm_89  RTX 40xx, L4, L40
#   sm_90  H100, H200
#   sm_120 RTX 50xx
GENCODE="-gencode arch=compute_75,code=sm_75 \
 -gencode arch=compute_80,code=sm_80 \
 -gencode arch=compute_86,code=sm_86 \
 -gencode arch=compute_89,code=sm_89 \
 -gencode arch=compute_90,code=sm_90 \
 -gencode arch=compute_120,code=sm_120 \
 -gencode arch=compute_120,code=compute_120"

"$NVCC" -O3 $GENCODE -diag-suppress 177 -DDSG_BUILD_DLL -DBR_BLOCK=256 \
        -cudart static -Xcompiler -fPIC -shared \
        -o gpu/libderostorm_gpu.so gpu/derostorm_gpu.cu

mkdir -p cmd/derostorm/gpucuda/linux
cp -f gpu/libderostorm_gpu.so cmd/derostorm/gpucuda/linux/
echo "built gpu/libderostorm_gpu.so and copied it to cmd/derostorm/gpucuda/linux/"
