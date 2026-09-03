# Linux NVIDIA kernels

`libderostorm_gpu.so` goes here, built by `gpu/buildlib.sh` under Linux
(needs nvcc):

    libderostorm_gpu.so    CUDA kernels for NVIDIA cards

None of it is in git — see `../gpulib/README.md` for what that means, how to
build it, and how to check it once it is here. This file exists so `go:embed`
has something to find in an otherwise empty directory, which is what lets the
miner build with no NVIDIA support rather than failing. An AMD-only rig has no
nvcc and needs none: the finished miner reports no NVIDIA devices, exactly as
it would on a machine with no NVIDIA card.
