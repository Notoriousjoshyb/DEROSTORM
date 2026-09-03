//go:build windows || (linux && amd64)

package main

// ROCm SMI -- the AMD System Management Interface -- is where an AMD card's
// temperature, power, fan and utilisation come from. It is the twin of nvml.go
// and it is bound the same way: at run time, through the platform's dynamic
// loader, with no cgo.
//
// One difference from NVML matters in practice. NVML ships inside the NVIDIA
// display driver, so any machine that can mine on an NVIDIA card can also be
// asked about it. ROCm SMI ships with ROCm, not with the driver. On Linux that
// is the same thing for our purposes -- the HIP kernels need ROCm anyway, so if
// the card is mining the library is there. On Windows it is not: Adrenalin
// carries the HIP runtime but not ROCm SMI, so a Windows AMD rig usually mines
// happily and shows dashes in the temperature column. That costs nothing but
// the column, so it is reported once and dropped.
//
// Only the read-only queries are bound. ROCm SMI can also set fan curves,
// clocks and power caps, and none of that belongs in a miner: a program that
// quietly changes a card's power limit is a program its owner cannot reason
// about.
//
// Device index note: ROCm SMI numbers devices in PCI bus order, and so does
// HIP, so index n here is index n there. That is the same assumption nvml.go
// makes and it fails the same way -- a wrong row in the sensor panel, never a
// wrong hash.

import (
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
)

// The subset of ROCm SMI we call. Every one returns an rsmi_status_t, where 0
// is RSMI_STATUS_SUCCESS; the out-parameters are only meaningful when it is.
var (
	rsmiInit      func(flags uint64) int32
	rsmiShutDown  func() int32
	rsmiName      func(dv uint32, buf *byte, n uint64) int32
	rsmiTemp      func(dv, sensor, metric uint32, milliC *int64) int32
	rsmiFan       func(dv, sensor uint32, speed *int64) int32
	rsmiFanMax    func(dv, sensor uint32, max *uint64) int32
	rsmiBusy      func(dv uint32, pct *uint32) int32
	rsmiPower     func(dv, sensor uint32, microW *uint64) int32
	rsmiPowerCap  func(dv, sensor uint32, microW *uint64) int32
	rsmiMemTotal  func(dv uint32, kind uint32, bytes *uint64) int32
	rsmiMemUsed   func(dv uint32, kind uint32, bytes *uint64) int32
	rsmiClockFreq func(dv uint32, kind uint32, out *byte) int32
)

const (
	rsmiSuccess = 0

	// rsmi_temperature_type_t. Edge is the one every card reports; junction and
	// memory are not universal, so edge is what the panel shows.
	rsmiTempEdge = 0
	// rsmi_temperature_metric_t: RSMI_TEMP_CURRENT.
	rsmiTempCurrent = 0

	// rsmi_memory_type_t: RSMI_MEM_TYPE_VRAM.
	rsmiMemVRAM = 0

	// rsmi_clk_type_t: system (shader) clock and memory clock.
	rsmiClkSys = 0x0
	rsmiClkMem = 0x4

	// Bytes of rsmi_frequency_t to hand the library. The struct starts
	// { uint32 num_supported; uint32 current; uint64 frequency[N]; } and has
	// grown a trailing field between ROCm releases but never a leading one, so
	// only the two counters and the array need a fixed layout. N has been 32
	// and then 33; 64 entries plus slack is comfortably more than any release
	// writes, and over-allocating a buffer the library only fills part of is
	// free.
	rsmiFreqBufBytes = 8 + 64*8 + 64
)

var (
	rsmiOnce    sync.Once
	rsmiReady   bool
	rsmiNames   map[int]string
	rsmiPresent map[int]bool
	rsmiMu      sync.Mutex
)

// rsmiOpen loads the library and checks that each device we mine on exists.
// It returns a short note for the event log when telemetry is unavailable, and
// "" when everything came up.
//
// A failure here is never fatal and never retried, for the same reason
// nvml.go's is not: a machine with no ROCm SMI at this moment will not grow one
// later in the same run.
//
// The indices are ROCm SMI's own, counted within AMD cards alone.
// gpu_sensors.go translates from the miner's numbering.
func rsmiOpen(devices []int) string {
	if len(devices) == 0 {
		return ""
	}
	note := ""
	rsmiOnce.Do(func() {
		sym, err := openNativeLibrary(rocmSMILibName)
		if err != nil {
			note = "AMD GPU telemetry unavailable: cannot load " + rocmSMILibName
			return
		}
		for _, b := range []struct {
			name string
			fn   any
		}{
			{"rsmi_init", &rsmiInit},
			{"rsmi_shut_down", &rsmiShutDown},
			{"rsmi_dev_name_get", &rsmiName},
			{"rsmi_dev_temp_metric_get", &rsmiTemp},
			{"rsmi_dev_fan_speed_get", &rsmiFan},
			{"rsmi_dev_fan_speed_max_get", &rsmiFanMax},
			{"rsmi_dev_busy_percent_get", &rsmiBusy},
			{"rsmi_dev_power_ave_get", &rsmiPower},
			{"rsmi_dev_power_cap_get", &rsmiPowerCap},
			{"rsmi_dev_memory_total_get", &rsmiMemTotal},
			{"rsmi_dev_memory_usage_get", &rsmiMemUsed},
			{"rsmi_dev_gpu_clk_freq_get", &rsmiClockFreq},
		} {
			addr, err := sym(b.name)
			if err != nil {
				note = fmt.Sprintf("AMD GPU telemetry unavailable: ROCm SMI has no %s", b.name)
				return
			}
			purego.RegisterFunc(b.fn, addr)
		}
		if r := rsmiInit(0); r != rsmiSuccess {
			note = fmt.Sprintf("AMD GPU telemetry unavailable: rsmi_init returned %d", r)
			return
		}
		rsmiReady = true
	})
	if !rsmiReady {
		return note
	}

	rsmiMu.Lock()
	defer rsmiMu.Unlock()
	if rsmiPresent == nil {
		rsmiPresent = make(map[int]bool, len(devices))
		rsmiNames = make(map[int]string, len(devices))
	}
	// There is no handle to fetch: every query takes the device index directly.
	// Asking for the name is what proves the index is one ROCm SMI knows about,
	// which is the same thing nvmlDeviceGetHandleByIndex does for NVML.
	for _, d := range devices {
		if _, ok := rsmiPresent[d]; ok {
			continue
		}
		var name [96]byte
		if rsmiName(uint32(d), &name[0], uint64(len(name))) != rsmiSuccess {
			continue
		}
		rsmiPresent[d] = true
		rsmiNames[d] = cstr(name[:])
	}
	if len(rsmiPresent) == 0 {
		return "AMD GPU telemetry unavailable: ROCm SMI reported no matching device"
	}
	return ""
}

func rsmiClose() {
	rsmiMu.Lock()
	defer rsmiMu.Unlock()
	if rsmiReady && rsmiShutDown != nil {
		rsmiShutDown()
		rsmiReady = false
	}
}

// rsmiCurrentClock reads one clock domain and returns the frequency in MHz.
//
// rsmi_frequency_t is a list, not a value: the supported frequencies and an
// index saying which is in force. Only the leading two counters and the array
// have a layout worth relying on, and passing a byte buffer rather than a Go
// struct keeps that assumption stated here instead of trusted to the compiler.
func rsmiCurrentClock(dv uint32, kind uint32) (int, bool) {
	var buf [rsmiFreqBufBytes]byte
	if rsmiClockFreq(dv, kind, &buf[0]) != rsmiSuccess {
		return 0, false
	}
	n := le32(buf[0:4])
	cur := le32(buf[4:8])
	if n == 0 || cur >= n || cur >= 64 {
		return 0, false
	}
	hz := le64(buf[8+8*cur : 16+8*cur])
	if hz == 0 {
		return 0, false
	}
	return int(hz / 1e6), true
}

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func le64(b []byte) uint64 {
	return uint64(le32(b[0:4])) | uint64(le32(b[4:8]))<<32
}

// rsmiRead polls every device once. Each query is independent: a card that will
// not report its fan speed still reports its temperature, so one unsupported
// field must not cost the others.
func rsmiRead(devices []int) []GPUSensor {
	if len(devices) == 0 {
		return nil
	}
	rsmiMu.Lock()
	defer rsmiMu.Unlock()
	if !rsmiReady {
		return nil
	}

	out := make([]GPUSensor, 0, len(devices))
	for _, d := range devices {
		if !rsmiPresent[d] {
			continue
		}
		dv := uint32(d)
		g := GPUSensor{Index: d, Name: rsmiNames[d], TempC: tempUnknown}

		var milliC int64
		if rsmiTemp(dv, rsmiTempEdge, rsmiTempCurrent, &milliC) == rsmiSuccess {
			g.TempC = float64(milliC) / 1000
		}
		// Fan speed comes back on the card's own scale, 0..max, not as a
		// percentage. Both halves have to succeed for the number to mean
		// anything.
		var speed int64
		var maxSpeed uint64
		if rsmiFan(dv, 0, &speed) == rsmiSuccess &&
			rsmiFanMax(dv, 0, &maxSpeed) == rsmiSuccess && maxSpeed > 0 {
			g.FanPct, g.HaveFan = int(float64(speed)/float64(maxSpeed)*100+0.5), true
		}
		var pct uint32
		if rsmiBusy(dv, &pct) == rsmiSuccess {
			g.UtilPct, g.HaveUtil = int(pct), true
		}
		var microW uint64
		if rsmiPower(dv, 0, &microW) == rsmiSuccess {
			g.PowerW, g.HavePower = float64(microW)/1e6, true
		}
		if rsmiPowerCap(dv, 0, &microW) == rsmiSuccess {
			g.PowerCapW = float64(microW) / 1e6
		}
		var total, used uint64
		if rsmiMemTotal(dv, rsmiMemVRAM, &total) == rsmiSuccess {
			g.MemTotalMB = int(total >> 20)
			g.HaveMem = g.MemTotalMB > 0
			if rsmiMemUsed(dv, rsmiMemVRAM, &used) == rsmiSuccess {
				g.MemUsedMB = int(used >> 20)
			}
		}
		if mhz, ok := rsmiCurrentClock(dv, rsmiClkSys); ok {
			g.ClockMHz, g.HaveClock = mhz, true
		}
		if mhz, ok := rsmiCurrentClock(dv, rsmiClkMem); ok {
			g.MemClockMHz = mhz
		}
		out = append(out, g)
	}
	return out
}
