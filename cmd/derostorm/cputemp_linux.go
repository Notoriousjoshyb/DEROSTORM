//go:build linux

package main

// CPU temperature on Linux, which is the easy case: the kernel already read the
// register and published the answer under /sys.
//
// Two places are looked at, in order:
//
//  1. hwmon. Every CPU temperature driver registers here -- k10temp and
//     zenpower for AMD, coretemp for Intel -- and the label files name what
//     each sensor is, so the package temperature can be picked out rather than
//     guessed at. This is the same number lm-sensors prints.
//  2. the thermal zone framework, as a fallback for the boards and ARM SoCs
//     that have no hwmon driver but do register a zone.
//
// Values are millidegrees Celsius throughout, which is where the /1000 comes
// from. Nothing here needs root.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// cpuTempHint is what the event log offers once when nothing could be read.
const cpuTempHint = "CPU temperature unavailable — no hwmon or thermal-zone " +
	"sensor for this CPU (try: modprobe k10temp, or coretemp on Intel)"

// hwmonDrivers are the driver names that publish a CPU temperature, best
// first. zenpower is a third-party replacement for k10temp that exposes more
// sensors on Zen parts; where both are loaded it is the better one.
var hwmonDrivers = []string{"zenpower", "k10temp", "coretemp"}

// hwmonLabels are the sensor labels that mean "the package", best first.
// Anything unlabelled falls back to temp1, which every one of these drivers
// uses for the package.
var hwmonLabels = []string{"tctl", "tdie", "package id 0", "cpu"}

var (
	cpuTempMu      sync.Mutex
	cpuTempPath    string // the file that answered last time
	cpuTempSrcName string
	cpuTempNextTry time.Time
)

// readCPUTemp returns the CPU temperature in Celsius, and where it came from.
//
// The file that worked is remembered, so the steady state is a single ~30-byte
// read rather than a walk of /sys. A driver loaded after the miner started is
// picked up on the next retry, at most once a minute.
func readCPUTemp() (float64, string, bool) {
	cpuTempMu.Lock()
	path, name := cpuTempPath, cpuTempSrcName
	retry := path == "" && time.Now().After(cpuTempNextTry)
	cpuTempMu.Unlock()

	if path != "" {
		if c, ok := readMilliC(path); ok {
			return c, name, true
		}
		cpuTempMu.Lock()
		cpuTempPath = ""
		cpuTempMu.Unlock()
		retry = true
	}
	if !retry {
		return 0, "", false
	}

	if p, n, ok := findHwmonTemp(); ok {
		cpuTempMu.Lock()
		cpuTempPath, cpuTempSrcName = p, n
		cpuTempMu.Unlock()
		c, _ := readMilliC(p)
		return c, n, true
	}
	if p, n, ok := findThermalZone(); ok {
		cpuTempMu.Lock()
		cpuTempPath, cpuTempSrcName = p, n
		cpuTempMu.Unlock()
		c, _ := readMilliC(p)
		return c, n, true
	}

	cpuTempMu.Lock()
	cpuTempNextTry = time.Now().Add(time.Minute)
	cpuTempMu.Unlock()
	return 0, "", false
}

// findHwmonTemp picks the best CPU sensor under /sys/class/hwmon. The two
// rankings are independent -- which driver, then which of its sensors -- so a
// zenpower Tdie beats a coretemp package, and either beats an unlabelled input
// on the same chip.
func findHwmonTemp() (string, string, bool) {
	dirs, err := filepath.Glob("/sys/class/hwmon/hwmon*")
	if err != nil {
		return "", "", false
	}

	bestPath, bestName := "", ""
	bestDrv, bestLbl := len(hwmonDrivers), len(hwmonLabels)

	for _, dir := range dirs {
		drv := strings.ToLower(readTrim(filepath.Join(dir, "name")))
		drvRank := -1
		for i, want := range hwmonDrivers {
			if drv == want {
				drvRank = i
				break
			}
		}
		if drvRank < 0 || drvRank > bestDrv {
			continue
		}

		inputs, _ := filepath.Glob(filepath.Join(dir, "temp*_input"))
		for _, in := range inputs {
			lbl := strings.ToLower(readTrim(strings.TrimSuffix(in, "_input") + "_label"))
			lblRank := len(hwmonLabels)
			for i, want := range hwmonLabels {
				if strings.Contains(lbl, want) {
					lblRank = i
					break
				}
			}
			// An unlabelled temp1 is the package on all three of these
			// drivers, so it ranks just behind a real label rather than last.
			if lbl == "" && strings.HasSuffix(in, "temp1_input") {
				lblRank = len(hwmonLabels) - 1
			}
			if _, ok := readMilliC(in); !ok {
				continue
			}
			if drvRank < bestDrv || (drvRank == bestDrv && lblRank < bestLbl) {
				bestPath, bestDrv, bestLbl = in, drvRank, lblRank
				bestName = drv
				if lbl != "" {
					bestName = drv + " " + lbl
				}
			}
		}
	}
	return bestPath, bestName, bestPath != ""
}

// findThermalZone is the fallback: a zone whose type names the CPU package.
func findThermalZone() (string, string, bool) {
	dirs, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
	for _, dir := range dirs {
		typ := strings.ToLower(readTrim(filepath.Join(dir, "type")))
		if !strings.Contains(typ, "x86_pkg_temp") && !strings.Contains(typ, "cpu") {
			continue
		}
		p := filepath.Join(dir, "temp")
		if _, ok := readMilliC(p); ok {
			return p, typ, true
		}
	}
	return "", "", false
}

// readMilliC reads a millidegree-Celsius sysfs file. A zero is treated as a
// missing reading: these files report 0 when the driver is present but the
// sensor is not, and no running CPU is at 0°C.
func readMilliC(path string) (float64, bool) {
	s := readTrim(path)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return float64(v) / 1000, true
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
