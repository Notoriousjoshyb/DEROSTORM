//go:build !windows && !(linux && amd64)

package main

// The faster suffix sort for targets that do not embed a pre-built library:
// darwin (Intel and Apple Silicon) and linux/arm64.
//
// Two routes for the sort, then pairing:
//
//   1. The native C library, compiled into the miner with cgo when this is
//      being built on the machine that will run it. That is the same
//      descriptor sort Windows and linux/amd64 embed, plus ARMv8 SHA-2
//      pairing on Apple Silicon -- which has no SMT, so the pairing is the
//      only way to keep the SHA unit fed.
//   2. The same algorithm in Go (internal/dsa). Used when cgo is off, which
//      is every cross-compile. It is the algorithm that is the 3x, not the
//      language it is written in; Macs on the Go SA-IS were at a quarter of
//      Windows on the same silicon because they were running a different
//      sort, not because they were running Go.
//   3. Pairing via internal/shapair when the native pair is not in the binary.
//      Go's SHA-256 already uses the hardware unit; feeding it two blocks in
//      lockstep is the same schedule as the C pair.
//
// Shape matches sa_lib.go so main.go needs no build tag of its own.

import (
	"os"

	"github.com/deroproject/derohe/astrobwt/astrobwtv3"
	"github.com/notoriousjoshyb/derostorm/internal/dsa"
	"github.com/notoriousjoshyb/derostorm/internal/shapair"
)

// nativePairOK is set by sa_cgo.go when the native SHA pair probed clean.
var nativePairOK bool

func InstallFastSuffixSort() (string, bool) {
	note := "suffix sort: descriptor (portable)"
	if n, ok := tryNativeSA(); ok {
		note = n
	} else {
		astrobwtv3.SuffixSort = dsa.SuffixArray
	}
	if !nativePairOK && shapair.Available() {
		astrobwtv3.SHA256Pair = shapair.Pair
		nativePairOK = true
		note += ", paired SHA-256"
	}
	return note, true
}

func PairedSHAAvailable() bool {
	return nativePairOK && os.Getenv("DEROSTORM_NO_PAIR") == ""
}
