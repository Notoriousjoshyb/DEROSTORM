//go:build linux && amd64

package main

// The Linux half of the suffix-sort binding: which library is embedded.
// Everything the miner actually calls is in sa_lib.go.
//
// amd64 only, because native/buildlib.sh's default target is an x86-64 .so.
// darwin and linux/arm64 take sa_other.go: the same descriptor algorithm, either
// compiled in with cgo (internal/sacgo) when the miner is built on the machine
// that runs it, or the portable Go port (internal/dsa) when it is not.
//
// Built by native/buildlib.sh, which needs gcc. Windows cannot build it -- a
// Linux .so has to come from a Linux toolchain -- but WSL is enough, and by the
// time Go embeds it the result is just a file on disk, so the Linux miner is
// still cross-compiled from Windows like every other target.

import "embed"

//go:embed libderostorm_sa.so
var saLibFS embed.FS

const saLibFile = "libderostorm_sa.so"
