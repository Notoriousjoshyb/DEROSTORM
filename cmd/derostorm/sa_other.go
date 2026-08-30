//go:build !windows && !(linux && amd64)

package main

// The faster suffix sort is a C library embedded in the executable and bound at
// run time; see sa_lib.go. There is one built for windows/amd64 and one for
// linux/amd64, and nothing for the targets this file covers, so these builds
// use the Go SA-IS -- what every build used before, correct, just slower.
//
// The reason differs by target, and only one of them is real work:
//
//   darwin/amd64   Nothing but packaging. The sort is the same x86 code and
//                  would run; it needs a .dylib built on a Mac, which is a
//                  machine this project does not have.
//   darwin/arm64   A port. The descriptor merge's scatter is AVX2 and the
//   linux/arm64    paired hash is the x86 SHA extensions, so both need
//                  rewriting in NEON before there is anything to embed.
//
// Shape matches sa_lib.go so main.go needs no build tag of its own.

// InstallFastSuffixSort does nothing here and says so.
func InstallFastSuffixSort() (string, bool) {
	return "suffix sort: built-in (no faster library is built for this platform)", false
}

// PairedSHAAvailable reports whether nonces should be hashed two at a time. The
// paired SHA-256 lives in the same library, so it is unavailable here for the
// same reason.
func PairedSHAAvailable() bool { return false }
