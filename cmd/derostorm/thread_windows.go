//go:build windows

package main

// CPU affinity, Windows.
//
// Threads are pinned by explicit slot rather than by a shared counter, because
// DeroStorm can add and remove mining threads at runtime -- a counter would
// drift every time the thread count changed and stop matching the slot a worker
// actually occupies.

import (
	"math/bits"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	libkernel32affinity  = mustLoad("kernel32.dll")
	setThreadAffinityPtr = mustProc(libkernel32affinity, "SetThreadAffinityMask")
)

func mustLoad(name string) uintptr {
	lib, _ := syscall.LoadLibrary(name)
	return uintptr(lib)
}

func mustProc(lib uintptr, name string) uintptr {
	addr, _ := syscall.GetProcAddress(syscall.Handle(lib), name)
	return uintptr(addr)
}

// currentThread is the pseudo-handle for the calling thread; it needs no close.
func currentThread() syscall.Handle { return syscall.Handle(^uintptr(0)) }

// pinToCPU binds the calling OS thread to one logical CPU, spreading slots over
// physical cores first (see spreadOverCores).
func pinToCPU(slot int) {
	count := runtime.GOMAXPROCS(0)
	// Guard the mapped CPU, not the slot: with an odd CPU count the spread
	// can land past the top (e.g. slot 14 of 15 maps to CPU 15), and pinning
	// there leaves the thread wherever the scheduler puts it — or nowhere.
	cpu := spreadOverCores(slot, count)
	if slot < 0 || cpu < 0 || cpu >= count || cpu >= bits.UintSize {
		return // more threads than CPUs: leave this one to the scheduler
	}
	mask := uint(1) << uint(cpu)
	syscall.Syscall(setThreadAffinityPtr, 2, uintptr(currentThread()), uintptr(mask), 0)
	_ = unsafe.Pointer(nil)
}
