# Linux AMD kernels

Two files go here, both built by `gpu/buildlib_hip.sh`:

    libderostorm_hip6.so    built against ROCm 6
    libderostorm_hip5.so    built against ROCm 5

A HIP library names the ROCm runtime it links to by soname, and a rig has one
generation installed, not both — so both are built and `dlopen` picks. Either
one alone is a perfectly good library for the rigs on its generation. Only
ROCm 6 can target RDNA 4.

Neither is in git — see `../README.md` for what that means, how to build them,
and how to check them once they are here. This file exists so `go:embed` has
something to find in an otherwise empty directory, which is what lets the miner
build with no AMD support rather than failing.
