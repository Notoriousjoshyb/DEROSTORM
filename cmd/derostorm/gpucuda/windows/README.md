# Windows NVIDIA kernels

`derostorm_gpu.dll` goes here, built by `gpu\buildlib.bat` (needs nvcc):

    derostorm_gpu.dll    CUDA kernels for NVIDIA cards

None of it is in git — see `../gpulib/README.md` for what that means, how to
build it, and how to check it once it is here. This file exists so `go:embed`
has something to find in an otherwise empty directory, which is what lets the
miner build with no NVIDIA support rather than failing. An AMD-only rig has no
nvcc and needs none: the finished miner reports no NVIDIA devices, exactly as
it would on a machine with no NVIDIA card.
