//go:build windows || (linux && amd64)

package main

// NVML -- the NVIDIA Management Library -- is where GPU temperature, power, fan
// and utilisation come from.
//
// It is bound the same way the CUDA kernels are: at run time, through the
// platform's dynamic loader, with no cgo. That is not just consistency. NVML
// ships inside the display driver, so on a machine with a working NVIDIA card
// it is already present and on the default search path; and on a machine
// without one the load simply fails and the miner carries on with no telemetry
// rather than refusing to start. Linking it would have turned "no NVIDIA card"
// into "will not run".
//
// Only the read-only queries are bound. NVML can also set clocks, fan curves
// and power limits, and none of that belongs in a miner: a program that quietly
// changes a card's power limit is a program its owner cannot reason about.
//
// Device index note: NVML numbers devices by PCI bus order, while CUDA by
// default numbers them fastest-first, so the two can disagree on a mixed rig.
// main.go pins CUDA_DEVICE_ORDER=PCI_BUS_ID before anything opens a device,
// which makes the two orderings the same one.

import (
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
)

// The subset of NVML we call. Every one returns an nvmlReturn_t, where 0 is
// success; the out-parameters are only meaningful when it is.
var (
	nvmlInit                 func() int32
	nvmlShutdown             func() int32
	nvmlDeviceGetHandleByIdx func(index uint32, dev *uintptr) int32
	nvmlDeviceGetName        func(dev uintptr, buf *byte, n uint32) int32
	nvmlDeviceGetTemperature func(dev uintptr, sensor uint32, temp *uint32) int32
	nvmlDeviceGetFanSpeed    func(dev uintptr, pct *uint32) int32
	nvmlDeviceGetUtilization func(dev uintptr, out *uint32) int32
	nvmlDeviceGetPowerUsage  func(dev uintptr, milliwatts *uint32) int32
	nvmlDeviceGetPowerCap    func(dev uintptr, milliwatts *uint32) int32
	nvmlDeviceGetMemoryInfo  func(dev uintptr, out *uint64) int32
	nvmlDeviceGetClockInfo   func(dev uintptr, kind uint32, mhz *uint32) int32
)

const (
	nvmlSuccess = 0

	nvmlTempGPU = 0 // NVML_TEMPERATURE_GPU

	nvmlClockGraphics = 0 // NVML_CLOCK_GRAPHICS
	nvmlClockMem      = 2 // NVML_CLOCK_MEM
)

var (
	nvmlOnce    sync.Once
	nvmlReady   bool
	nvmlHandles map[int]uintptr
	nvmlNames   map[int]string
	nvmlMu      sync.Mutex
)

// openNVML loads the library and resolves a handle per device. It returns a
// short note for the event log when telemetry is unavailable, and "" when
// everything came up.
//
// A failure here is never fatal and never retried: a driver that has no NVML at
// this moment will not grow one later in the same run, and retrying a failing
// load every two seconds is a good way to turn a missing feature into a
// performance bug.
func openNVML(devices []int) string {
	if len(devices) == 0 {
		return ""
	}
	note := ""
	nvmlOnce.Do(func() {
		sym, err := openGPULibrary(nvmlLibName)
		if err != nil {
			note = "GPU telemetry unavailable: cannot load " + nvmlLibName
			return
		}
		for _, b := range []struct {
			name string
			fn   any
		}{
			{"nvmlInit_v2", &nvmlInit},
			{"nvmlShutdown", &nvmlShutdown},
			{"nvmlDeviceGetHandleByIndex_v2", &nvmlDeviceGetHandleByIdx},
			{"nvmlDeviceGetName", &nvmlDeviceGetName},
			{"nvmlDeviceGetTemperature", &nvmlDeviceGetTemperature},
			{"nvmlDeviceGetFanSpeed", &nvmlDeviceGetFanSpeed},
			{"nvmlDeviceGetUtilizationRates", &nvmlDeviceGetUtilization},
			{"nvmlDeviceGetPowerUsage", &nvmlDeviceGetPowerUsage},
			{"nvmlDeviceGetEnforcedPowerLimit", &nvmlDeviceGetPowerCap},
			{"nvmlDeviceGetMemoryInfo", &nvmlDeviceGetMemoryInfo},
			{"nvmlDeviceGetClockInfo", &nvmlDeviceGetClockInfo},
		} {
			addr, err := sym(b.name)
			if err != nil {
				note = fmt.Sprintf("GPU telemetry unavailable: NVML has no %s", b.name)
				return
			}
			purego.RegisterFunc(b.fn, addr)
		}
		if r := nvmlInit(); r != nvmlSuccess {
			note = fmt.Sprintf("GPU telemetry unavailable: nvmlInit returned %d", r)
			return
		}
		nvmlReady = true
	})
	if !nvmlReady {
		return note
	}

	nvmlMu.Lock()
	defer nvmlMu.Unlock()
	if nvmlHandles == nil {
		nvmlHandles = make(map[int]uintptr, len(devices))
		nvmlNames = make(map[int]string, len(devices))
	}
	for _, d := range devices {
		if _, ok := nvmlHandles[d]; ok {
			continue
		}
		var h uintptr
		if nvmlDeviceGetHandleByIdx(uint32(d), &h) != nvmlSuccess {
			continue
		}
		nvmlHandles[d] = h
		var name [96]byte
		if nvmlDeviceGetName(h, &name[0], uint32(len(name))) == nvmlSuccess {
			nvmlNames[d] = cstr(name[:])
		}
	}
	if len(nvmlHandles) == 0 {
		return "GPU telemetry unavailable: NVML reported no matching device"
	}
	return ""
}

func closeNVML() {
	nvmlMu.Lock()
	defer nvmlMu.Unlock()
	if nvmlReady && nvmlShutdown != nil {
		nvmlShutdown()
		nvmlReady = false
	}
}

// readGPUSensors polls every device once. Each query is independent: a card
// that will not report its fan speed still reports its temperature, so one
// unsupported field must not cost the others.
func readGPUSensors(devices []int) []GPUSensor {
	if len(devices) == 0 {
		return nil
	}
	nvmlMu.Lock()
	defer nvmlMu.Unlock()
	if !nvmlReady {
		return nil
	}

	out := make([]GPUSensor, 0, len(devices))
	for _, d := range devices {
		h, ok := nvmlHandles[d]
		if !ok {
			continue
		}
		g := GPUSensor{Index: d, Name: nvmlNames[d], TempC: tempUnknown}

		var u32 uint32
		if nvmlDeviceGetTemperature(h, nvmlTempGPU, &u32) == nvmlSuccess {
			g.TempC = float64(u32)
		}
		if nvmlDeviceGetFanSpeed(h, &u32) == nvmlSuccess {
			g.FanPct, g.HaveFan = int(u32), true
		}
		// nvmlUtilization_t is two consecutive unsigned ints, GPU then memory.
		// Passing the first element of an array rather than a Go struct keeps
		// the layout something this file states outright instead of something
		// the compiler is trusted to have arranged the same way.
		var util [2]uint32
		if nvmlDeviceGetUtilization(h, &util[0]) == nvmlSuccess {
			g.UtilPct, g.HaveUtil = int(util[0]), true
		}
		if nvmlDeviceGetPowerUsage(h, &u32) == nvmlSuccess {
			g.PowerW, g.HavePower = float64(u32)/1000, true
		}
		if nvmlDeviceGetPowerCap(h, &u32) == nvmlSuccess {
			g.PowerCapW = float64(u32) / 1000
		}
		// nvmlMemory_t is three unsigned long longs: total, free, used.
		var mem [3]uint64
		if nvmlDeviceGetMemoryInfo(h, &mem[0]) == nvmlSuccess {
			g.MemTotalMB = int(mem[0] >> 20)
			g.MemUsedMB = int(mem[2] >> 20)
			g.HaveMem = g.MemTotalMB > 0
		}
		if nvmlDeviceGetClockInfo(h, nvmlClockGraphics, &u32) == nvmlSuccess {
			g.ClockMHz, g.HaveClock = int(u32), true
		}
		if nvmlDeviceGetClockInfo(h, nvmlClockMem, &u32) == nvmlSuccess {
			g.MemClockMHz = int(u32)
		}
		out = append(out, g)
	}
	return out
}
