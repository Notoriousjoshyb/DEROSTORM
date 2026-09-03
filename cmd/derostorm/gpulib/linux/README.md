# Linux AMD kernels

`libderostorm_hip.so` goes here, built by `gpu/buildlib_hip.sh`.

It is not in git — see `../README.md` for what that means, how to build it, and
how to check it once it is here. This file exists so `go:embed` has something to
find in an otherwise empty directory, which is what lets the miner build with no
AMD support rather than failing.
