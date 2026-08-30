//go:build windows

package main

// The Windows half of the suffix-sort binding: which library is embedded.
// Everything the miner actually calls is in sa_lib.go.
//
// Built by native/build.bat, which needs MSVC. The DLL is checked in beside
// this file because go:embed reads it at build time, which is what lets the
// miner be built -- and cross-compiled -- on a machine with no C toolchain at
// all.

import "embed"

//go:embed derostorm_sa.dll
var saLibFS embed.FS

const saLibFile = "derostorm_sa.dll"
