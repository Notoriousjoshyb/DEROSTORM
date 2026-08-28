//go:build !windows && !linux && !darwin && !freebsd && !netbsd && !openbsd

package main

// No raw-mode implementation for this platform: the console falls back to plain
// line input and skips the in-frame command line.
func EnableRawInput() (restore func(), ok bool) { return func() {}, false }
