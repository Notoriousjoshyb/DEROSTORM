//go:build !windows

package main

// The faster suffix sort is packaged as a Windows DLL embedded in the
// executable; see sa_windows.go. The library itself (libsais) is portable C, so
// the same trick needs a .so or .dylib plus dlopen here, exactly as GPU support
// does. Until that exists these builds use the Go SA-IS, which is what every
// build used before and is correct, just slower.
//
// Shape matches sa_windows.go so main.go needs no build tag of its own.

// InstallFastSuffixSort does nothing here and says so.
func InstallFastSuffixSort() (string, bool) {
	return "suffix sort: built-in (the faster library is Windows-only so far)", false
}

// PairedSHAAvailable reports whether nonces should be hashed two at a time. The
// paired SHA-256 lives in the same library, so it is unavailable here for the
// same reason.
func PairedSHAAvailable() bool { return false }
