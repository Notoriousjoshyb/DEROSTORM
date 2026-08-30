//go:build linux && amd64

package main

// The Linux half of the suffix-sort binding: which library is embedded.
// Everything the miner actually calls is in sa_lib.go.
//
// amd64 only, because native/buildlib.sh builds an x86-64 .so and nothing else.
// The sort is C and would build for arm64, but two of the three things that
// make it fast here are not portable: descriptor.c's merge scatter is AVX2, and
// the paired hash is the x86 SHA extensions. An arm64 port is a NEON rewrite of
// both, not a recompile. Until then those builds take the sa_other.go path and
// use the Go sort, which is what they did before this file existed.
//
// Built by native/buildlib.sh, which needs gcc. Windows cannot build it -- a
// Linux .so has to come from a Linux toolchain -- but WSL is enough, and by the
// time Go embeds it the result is just a file on disk, so the Linux miner is
// still cross-compiled from Windows like every other target.

import "embed"

//go:embed libderostorm_sa.so
var saLibFS embed.FS

const saLibFile = "libderostorm_sa.so"
