//go:build windows

package main

// CPU temperature on Windows, which is the hard case.
//
// There is no user-mode API for a modern AMD or Intel package temperature here.
// The value lives behind an MSR (Intel) or the SMU mailbox (AMD), both of which
// are ring-0. That is why HWiNFO, LibreHardwareMonitor and Ryzen Master all
// install a kernel driver, and why a miner that will not install one has to
// make do with what user mode can see:
//
//  1. LibreHardwareMonitor or OpenHardwareMonitor, over their optional local
//     web server. If one of those is already running -- and on a tuned mining
//     box one usually is -- this is the real package temperature, read through
//     the driver they already installed. Nothing is installed by us.
//  2. The ACPI thermal zone, through the performance counter the kernel
//     publishes for it. This needs no privileges and no other software, and on
//     laptops and most servers it tracks the CPU closely. On many desktop
//     boards it is a chipset zone that reports a fixed, obviously wrong number,
//     so a plausibility filter is applied and an implausible zone is dropped.
//     A missing reading is much better than a confidently wrong one.
//
// When neither works the panel shows "--" and the event log says once what
// would make it work. It does not guess.

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// cpuTempHint is what the event log offers once when nothing could be read. It
// names the fix rather than the failure, because "no CPU temperature" on its
// own leaves the reader with nowhere to go -- and the fix is almost always
// "run the thing you already installed", not "install something".
const cpuTempHint = "CPU temperature unavailable — run HWiNFO (with Shared Memory " +
	"Support on), Core Temp, MSI Afterburner (with CPU temperature ticked) or " +
	"LibreHardwareMonitor (with its web server on), and it appears by itself"

// The web server LibreHardwareMonitor and OpenHardwareMonitor both expose, on
// the port both default to. Overridable so a non-default port, or a monitor on
// another machine in the rig, still works.
const lhmDefaultURL = "http://127.0.0.1:8085/data.json"

// cpuTempSources are tried in order and the first that answers is kept. The
// order is quality, not speed: source 1 is a driver-read package temperature
// and source 2 is a guess that happens to be right on some hardware.
var (
	cpuTempMu      sync.Mutex
	cpuTempChosen  func() (float64, string, bool)
	cpuTempNextTry time.Time
)

// readCPUTemp returns the CPU temperature in Celsius, and where it came from.
//
// The chosen source is remembered, so the steady state is one cheap read and
// not a probe of everything. When nothing works the probe is repeated at most
// once a minute -- a monitor started after the miner should be picked up, but
// not at the cost of a failing HTTP dial every two seconds.
func readCPUTemp() (float64, string, bool) {
	cpuTempMu.Lock()
	probe := cpuTempChosen
	retry := cpuTempChosen == nil && time.Now().After(cpuTempNextTry)
	cpuTempMu.Unlock()

	if probe != nil {
		if c, src, ok := probe(); ok {
			return c, src, true
		}
		// The source that was working has stopped -- the monitor was closed,
		// say. Fall through and probe again rather than reporting nothing
		// forever.
		cpuTempMu.Lock()
		cpuTempChosen = nil
		cpuTempMu.Unlock()
		retry = true
	}
	if !retry {
		return 0, "", false
	}

	// Order is quality. The first three read a driver another program already
	// installed and are the real package temperature; the fourth is the same
	// thing over HTTP; the last is a guess that happens to be right on some
	// hardware and is filtered hard because of it.
	for _, src := range []func() (float64, string, bool){
		readCPUTempHWiNFO,
		readCPUTempCoreTemp,
		readCPUTempAfterburner,
		readCPUTempMonitor,
		readCPUTempThermalZone,
	} {
		if c, name, ok := src(); ok {
			cpuTempMu.Lock()
			cpuTempChosen = src
			cpuTempMu.Unlock()
			return c, name, true
		}
	}
	cpuTempMu.Lock()
	cpuTempNextTry = time.Now().Add(time.Minute)
	cpuTempMu.Unlock()
	return 0, "", false
}

// ---------------------------------------------------------------- source 1

// lhmNode is one node of the sensor tree both monitors serve. Only the three
// fields that matter are decoded; the tree also carries ids, images and
// min/max columns that nothing here reads.
type lhmNode struct {
	Text     string    `json:"Text"`
	Value    string    `json:"Value"`
	Children []lhmNode `json:"Children"`
}

// cpuTempNames are the sensor labels that mean "the CPU package", best first.
// Matched as substrings by rankCPUTempLabel, because the four monitors name the
// same sensor four ways -- "Core (Tctl/Tdie)" in LibreHardwareMonitor, "CPU
// (Tctl/Tdie)" in HWiNFO, "CPU temperature" in Afterburner -- while still being
// specific enough that no drive or chipset sensor scores.
//
// Order is quality, not popularity. A package or Tctl reading is the one a
// throttling decision is made on; an average across cores is a summary of it,
// and a per-core maximum is noisier than either.
var cpuTempNames = []string{
	"tctl/tdie",         // AMD package, the number Ryzen Master shows
	"cpu package",       // Intel package
	"cpu die (average)", // AMD, HWiNFO's name for the same thing
	"package id 0",      // Intel package, the Linux label some tools copy
	"cpu temperature",   // MSI Afterburner
	"core average",
	"cpu cores",
	"core max",
}

var lhmClient = &http.Client{Timeout: 700 * time.Millisecond}

func readCPUTempMonitor() (float64, string, bool) {
	url := lhmDefaultURL
	if v := strings.TrimSpace(os.Getenv("DEROSTORM_CPU_TEMP_URL")); v != "" {
		url = v
	}
	resp, err := lhmClient.Get(url)
	if err != nil {
		return 0, "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, "", false
	}

	var root lhmNode
	// The tree is a few hundred kilobytes on a busy machine. Capped so a
	// service answering on that port with something else cannot make the miner
	// read forever.
	if err := json.NewDecoder(&limitedReader{r: resp.Body, n: 4 << 20}).Decode(&root); err != nil {
		return 0, "", false
	}

	best, bestRank := 0.0, len(cpuTempNames)
	var walk func(n *lhmNode)
	walk = func(n *lhmNode) {
		if rank, ok := rankCPUTempLabel(n.Text); ok && rank < bestRank {
			if c, ok := parseCelsius(n.Value); ok {
				best, bestRank = c, rank
			}
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(&root)

	if bestRank == len(cpuTempNames) || !plausibleCPUTemp(best) {
		return 0, "", false
	}
	return best, "hardware monitor", true
}

// parseCelsius reads a value column such as "55.6 °C". The monitors format with
// the machine's locale, so a comma is a decimal point rather than a thousands
// separator -- no temperature has thousands.
func parseCelsius(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	if !strings.Contains(v, "C") {
		return 0, false
	}
	v = strings.ReplaceAll(v, ",", ".")
	end := 0
	for end < len(v) && (v[end] == '-' || v[end] == '.' || (v[end] >= '0' && v[end] <= '9')) {
		end++
	}
	f, err := strconv.ParseFloat(v[:end], 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// limitedReader is io.LimitedReader without the import, and without turning a
// truncated body into a silent short read: the decoder sees EOF and fails,
// which is the outcome wanted.
type limitedReader struct {
	r interface{ Read([]byte) (int, error) }
	n int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, errBodyTooBig
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	return n, err
}

var errBodyTooBig = stringError("hardware monitor response is too large")

type stringError string

func (e stringError) Error() string { return string(e) }

// ---------------------------------------------------------------- source 2

// The performance-counter API. PDH is a plain C API in a system DLL, so it
// binds with syscall and needs neither cgo nor COM -- which the equivalent WMI
// query would.
var (
	pdhDLL  = syscall.NewLazyDLL("pdh.dll")
	pdhOpen = pdhDLL.NewProc("PdhOpenQueryW")
	pdhAdd  = pdhDLL.NewProc("PdhAddEnglishCounterW")
	pdhColl = pdhDLL.NewProc("PdhCollectQueryData")
	pdhFmt  = pdhDLL.NewProc("PdhGetFormattedCounterArrayW")
	pdhShut = pdhDLL.NewProc("PdhCloseQuery")
)

const (
	pdhFmtDouble       = 0x00000200
	pdhMoreData        = 0x800007D2
	thermalCounterPath = `\Thermal Zone Information(*)\Temperature`
)

// pdhCounterItem is PDH_FMT_COUNTERVALUE_ITEM_W: an instance name and the value
// for it. The four bytes of padding are the compiler's, not ours -- the double
// that follows CStatus is 8-byte aligned.
type pdhCounterItem struct {
	name   *uint16
	status uint32
	_      uint32
	value  float64
}

// readCPUTempThermalZone reads the ACPI thermal zones, in Kelvin, and returns
// the hottest plausible one.
//
// Hottest rather than first because a board with several zones lists them in no
// useful order, and the CPU is normally the warmest thing in the case. The
// plausibility filter is what makes this safe to offer at all: boards that
// publish a fixed 290 K placeholder are common, and drawing 17°C beside a
// running miner would be worse than drawing nothing.
func readCPUTempThermalZone() (float64, string, bool) {
	var query syscall.Handle
	if r, _, _ := pdhOpen.Call(0, 0, uintptr(unsafe.Pointer(&query))); r != 0 {
		return 0, "", false
	}
	defer pdhShut.Call(uintptr(query))

	path, err := syscall.UTF16PtrFromString(thermalCounterPath)
	if err != nil {
		return 0, "", false
	}
	var counter syscall.Handle
	if r, _, _ := pdhAdd.Call(uintptr(query), uintptr(unsafe.Pointer(path)), 0,
		uintptr(unsafe.Pointer(&counter))); r != 0 {
		return 0, "", false
	}
	if r, _, _ := pdhColl.Call(uintptr(query)); r != 0 {
		return 0, "", false
	}

	// Ask for the buffer size, then for the data. PDH reports the size it wants
	// through the same call, signalled by PDH_MORE_DATA.
	var size, count uint32
	r, _, _ := pdhFmt.Call(uintptr(counter), pdhFmtDouble,
		uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), 0)
	if uint32(r) != pdhMoreData || size == 0 {
		return 0, "", false
	}
	buf := make([]byte, size)
	if r, _, _ := pdhFmt.Call(uintptr(counter), pdhFmtDouble,
		uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)),
		uintptr(unsafe.Pointer(&buf[0]))); r != 0 {
		return 0, "", false
	}

	items := unsafe.Slice((*pdhCounterItem)(unsafe.Pointer(&buf[0])), int(count))
	best, found := 0.0, false
	for i := range items {
		if items[i].status != 0 {
			continue
		}
		c := items[i].value - 273.15 // the counter is Kelvin
		if plausibleCPUTemp(c) && (!found || c > best) {
			best, found = c, true
		}
	}
	if !found {
		return 0, "", false
	}
	return best, "ACPI thermal zone", true
}

// plausibleCPUTemp rejects readings no running CPU produces. The low bound is
// what catches the fixed placeholder zones: a machine whose CPU is genuinely
// below 25°C is a machine that is switched off.
func plausibleCPUTemp(c float64) bool { return c >= 25 && c <= 125 }
