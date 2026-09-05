//go:build linux

package main

// CPU affinity, Linux. See thread_windows.go for why slots are explicit.

import (
	"runtime"

	"golang.org/x/sys/unix"
)

func pinToCPU(slot int) {
	count := runtime.GOMAXPROCS(0)
	// Guard the mapped CPU, not the slot: with an odd CPU count the spread
	// can land past the top (e.g. slot 14 of 15 maps to CPU 15). See
	// thread_windows.go.
	cpu := spreadOverCores(slot, count)
	if slot < 0 || cpu < 0 || cpu >= count {
		return
	}
	var set unix.CPUSet
	set.Zero()
	set.Set(cpu)
	unix.SchedSetaffinity(0, &set)
}
