//go:build !linux && !windows && !darwin

package main

// CPU affinity is a no-op on platforms we have no implementation for.
func pinToCPU(slot int) {}
