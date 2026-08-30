//go:build windows

package main

// Windows implementations of the machine metrics.
//
// All three go through kernel32 or powrprof by hand rather than through a
// dependency, which is the same choice the rest of this program makes: three
// syscalls do not justify pulling a system-information library, its transitive
// dependencies and its vendored copy into a miner.

import (
	"syscall"
	"unsafe"
)

var (
	procGetSystemTimes    = kernel32.NewProc("GetSystemTimes")
	procGlobalMemoryEx    = kernel32.NewProc("GlobalMemoryStatusEx")
	powrprof              = syscall.NewLazyDLL("powrprof.dll")
	procCallNtPowerInfo   = powrprof.NewProc("CallNtPowerInformation")
	lastIdle, lastBusy    uint64
	haveLastCPUSampleOnce bool
)

type filetime struct{ low, high uint32 }

func (f filetime) u64() uint64 { return uint64(f.high)<<32 | uint64(f.low) }

// readCPULoad returns whole-machine CPU use since the previous call.
//
// GetSystemTimes reports cumulative 100ns ticks, so a load is the difference
// between two readings and the first call can only prime the counter. The
// kernel figure already includes idle, which is the detail that makes a naive
// implementation report about half the real load.
func readCPULoad() (float64, bool) {
	var idle, kernel, user filetime
	r, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)))
	if r == 0 {
		return 0, false
	}
	i := idle.u64()
	total := kernel.u64() + user.u64()
	busy := total - i

	if !haveLastCPUSampleOnce {
		lastIdle, lastBusy = i, busy
		haveLastCPUSampleOnce = true
		return 0, false
	}
	di := i - lastIdle
	db := busy - lastBusy
	lastIdle, lastBusy = i, busy
	if di+db == 0 {
		return 0, false
	}
	return float64(db) / float64(di+db) * 100, true
}

type memoryStatusEx struct {
	length               uint32
	memoryLoad           uint32
	totalPhys            uint64
	availPhys            uint64
	totalPageFile        uint64
	availPageFile        uint64
	totalVirtual         uint64
	availVirtual         uint64
	availExtendedVirtual uint64
}

func readMemory() (usedMB, totalMB int, ok bool) {
	var m memoryStatusEx
	m.length = uint32(unsafe.Sizeof(m))
	r, _, _ := procGlobalMemoryEx.Call(uintptr(unsafe.Pointer(&m)))
	if r == 0 || m.totalPhys == 0 {
		return 0, 0, false
	}
	total := int(m.totalPhys >> 20)
	return total - int(m.availPhys>>20), total, true
}

// processorPowerInformation is the per-logical-CPU record CallNtPowerInformation
// fills in for ProcessorInformation. Six 32-bit fields, one entry per CPU.
type processorPowerInformation struct {
	number           uint32
	maxMhz           uint32
	currentMhz       uint32
	mhzLimit         uint32
	maxIdleState     uint32
	currentIdleState uint32
}

const processorInformation = 11

// readCPUFreqMHz reports the current clock of the first logical CPU.
//
// The first CPU rather than an average across all of them: on a machine with
// preferred cores the average is a number no core is actually running at,
// whereas one core's clock is at least a real reading of a real thing. It is
// labelled as an approximation in the panel for the same reason.
func readCPUFreqMHz() (int, bool) {
	if err := procCallNtPowerInfo.Find(); err != nil {
		return 0, false
	}
	n := 64
	buf := make([]processorPowerInformation, n)
	size := uintptr(n) * unsafe.Sizeof(buf[0])
	r, _, _ := procCallNtPowerInfo.Call(
		uintptr(processorInformation), 0, 0,
		uintptr(unsafe.Pointer(&buf[0])), size)
	// STATUS_SUCCESS is 0; anything else means the buffer or the call was
	// rejected and there is no reading to report.
	if r != 0 {
		return 0, false
	}
	if buf[0].currentMhz == 0 {
		return 0, false
	}
	return int(buf[0].currentMhz), true
}
