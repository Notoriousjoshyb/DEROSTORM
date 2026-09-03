# Linux AMD kernels

One file per ROCm generation goes here, all built by `gpu/buildlib_hip.sh`:

    libderostorm_hip7.so    built against ROCm 7
    libderostorm_hip6.so    built against ROCm 6
    libderostorm_hip5.so    built against ROCm 5

A HIP library names the ROCm runtime it links to by soname, and a rig has one
generation installed, not three — so one is built per generation and `dlopen`
picks. Any one alone is a perfectly good library for the rigs on its
generation. ROCm 6 and up can target RDNA 4.

`gpu_backend_linux.go` scans this directory and tries the highest number first,
so a new generation needs a build here and no code change. Adding one to a
hard-coded list is what nobody did for ROCm 7 before 1.7.3, and ROCm 7 rigs got
no AMD support out of it.

None of them are in git — see `../README.md` for what that means, how to build them,
and how to check them once they are here. This file exists so `go:embed` has
something to find in an otherwise empty directory, which is what lets the miner
build with no AMD support rather than failing.
