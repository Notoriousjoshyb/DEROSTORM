//go:build !linux && !windows

package main

// CPU affinity is a no-op on platforms we have no implementation for.
func pinToCPU(slot int) {}
