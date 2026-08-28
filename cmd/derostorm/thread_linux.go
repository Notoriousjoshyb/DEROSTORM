//go:build linux

package main

// CPU affinity, Linux. See thread_windows.go for why slots are explicit.

import (
	"runtime"

	"golang.org/x/sys/unix"
)

func pinToCPU(slot int) {
	count := runtime.GOMAXPROCS(0)
	if slot < 0 || slot >= count {
		return
	}
	var set unix.CPUSet
	set.Zero()
	set.Set(spreadOverCores(slot, count))
	unix.SchedSetaffinity(0, &set)
}
