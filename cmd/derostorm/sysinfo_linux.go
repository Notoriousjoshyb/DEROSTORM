//go:build linux

package main

// Linux implementations of the machine metrics. All three come out of procfs
// and sysfs, so there is nothing to link and nothing that needs privileges.

import (
	"os"
	"strconv"
	"strings"
)

var lastCPUIdle, lastCPUTotal uint64
var haveLastCPUSample bool

// readCPULoad returns whole-machine CPU use since the previous call, from the
// aggregate line of /proc/stat.
//
// The counters are cumulative jiffies, so this is a difference between two
// readings and the first call can only prime them. Idle is fields 4 and 5
// (idle and iowait) -- counting iowait as busy would make a machine waiting on
// a disk look like a machine that is working.
func readCPULoad() (float64, bool) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, false
	}
	line := b
	if i := strings.IndexByte(string(b), '\n'); i > 0 {
		line = b[:i]
	}
	f := strings.Fields(string(line))
	if len(f) < 8 || f[0] != "cpu" {
		return 0, false
	}
	var total, idle uint64
	for i, s := range f[1:] {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			continue
		}
		total += v
		if i == 3 || i == 4 { // idle, iowait
			idle += v
		}
	}
	if !haveLastCPUSample {
		lastCPUIdle, lastCPUTotal, haveLastCPUSample = idle, total, true
		return 0, false
	}
	dt := total - lastCPUTotal
	di := idle - lastCPUIdle
	lastCPUIdle, lastCPUTotal = idle, total
	if dt == 0 {
		return 0, false
	}
	return float64(dt-di) / float64(dt) * 100, true
}

// readMemory reports used and total physical memory in MB.
//
// Used is total minus MemAvailable, not total minus MemFree. MemFree excludes
// the page cache, which on a machine that has been up for a week is most of
// memory, so MemFree would report a healthy machine as nearly out of RAM.
func readMemory() (usedMB, totalMB int, ok bool) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	var total, avail uint64
	var haveTotal, haveAvail bool
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, err := strconv.ParseUint(f[1], 10, 64) // kB
		if err != nil {
			continue
		}
		switch f[0] {
		case "MemTotal:":
			total, haveTotal = v, true
		case "MemAvailable:":
			avail, haveAvail = v, true
		}
	}
	if !haveTotal || !haveAvail || total == 0 {
		return 0, 0, false
	}
	return int((total - avail) / 1024), int(total / 1024), true
}

// readCPUFreqMHz reports the current clock of CPU 0, from cpufreq where the
// driver exposes it and from /proc/cpuinfo otherwise.
func readCPUFreqMHz() (int, bool) {
	if b, err := os.ReadFile("/sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq"); err == nil {
		if khz, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && khz > 0 {
			return khz / 1000, true
		}
	}
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "cpu MHz") {
			continue
		}
		_, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && f > 0 {
			return int(f + 0.5), true
		}
	}
	return 0, false
}
