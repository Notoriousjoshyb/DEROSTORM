//go:build windows

package main

// CPU temperature from a monitoring tool that is already running.
//
// Windows has no user-mode API for a package temperature (see the header of
// cputemp_windows.go), so the value has to come from something with a kernel
// driver. Rather than ask the user to install one, this reads the shared memory
// the common monitors already publish. Three are supported, and between them
// they cover most machines that have any monitoring at all:
//
//   HWiNFO           Global\HWiNFO_SENS_SM2   needs its Shared Memory Support on
//   MSI Afterburner  MAHMSharedMemory         needs CPU temperature ticked
//   Core Temp        CoreTempMappingObjectEx  works as soon as it is running
//
// All three are opened read-only and need no privileges: a mapping created by
// an elevated process is still readable from a normal one. Nothing is installed,
// started or configured by us -- if none is running, the panel says so and shows
// "--".
//
// Each layout below was checked against the real thing rather than copied from
// memory, and each parser derives its offsets from the sizes in the header
// where the format provides them, so a version that grows a field does not
// silently return a wrong number. Where a size cannot be checked, the reading
// still has to survive plausibleCPUTemp before it is used.

import (
	"strings"
	"syscall"
	"unsafe"
)

// mapView opens an existing named section read-only and returns a byte slice
// over it plus a closer. The slice aliases the mapping and must not outlive it.
//
// The size is discovered by mapping zero bytes, which asks the kernel for the
// whole section; VirtualQuery then says how much that was. There is no
// GetFileSize for a section object, and guessing a size is how a reader ends up
// either truncating a large sensor list or faulting past the end of a small one.
func mapView(name string) ([]byte, func(), bool) {
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, nil, false
	}
	r, _, _ := openFileMappingW.Call(fileMapRead, 0, uintptr(unsafe.Pointer(p)))
	if r == 0 {
		return nil, nil, false // nothing is publishing under that name
	}
	h := syscall.Handle(r)

	// Length zero maps the whole section, which is the only way to take a view
	// of something whose size this process was never told.
	addr, err := syscall.MapViewOfFile(h, fileMapRead, 0, 0, 0)
	if err != nil || addr == 0 {
		syscall.CloseHandle(h)
		return nil, nil, false
	}
	close := func() {
		syscall.UnmapViewOfFile(addr)
		syscall.CloseHandle(h)
	}

	var mbi memoryBasicInformation
	n, _, _ := virtualQuery.Call(addr, uintptr(unsafe.Pointer(&mbi)), unsafe.Sizeof(mbi))
	size := int(mbi.RegionSize)
	if n == 0 || size <= 0 || size > 64<<20 {
		close()
		return nil, nil, false
	}
	// go vet's unsafeptr check flags this uintptr-to-pointer conversion, and it
	// is right to flag the shape in general: a uintptr is not a reference and
	// the collector will not keep what it points at alive. Here it is sound and
	// there is no other way to reach a mapped section. The memory belongs to
	// the kernel, not the Go heap, so there is nothing for the collector to
	// move or free; the mapping is held open by the closer returned beside it;
	// and nothing derived from the slice outlives that closer -- every parser
	// below returns float64s and strings, both of which are copies.
	return unsafe.Slice((*byte)(unsafe.Pointer(addr)), size), close, true
}

// fileMapRead is FILE_MAP_READ, which is SECTION_MAP_READ. A section another
// process created while elevated is still readable from an unelevated one, so
// nothing here needs privileges of its own.
const fileMapRead = 0x0004

var (
	kernel32shm      = syscall.NewLazyDLL("kernel32.dll")
	openFileMappingW = kernel32shm.NewProc("OpenFileMappingW")
	virtualQuery     = kernel32shm.NewProc("VirtualQuery")
)

type memoryBasicInformation struct {
	BaseAddress       uintptr
	AllocationBase    uintptr
	AllocationProtect uint32
	_                 uint32
	RegionSize        uintptr
	State             uint32
	Protect           uint32
	Type              uint32
	_                 uint32
}

// ---------------------------------------------------------------- reading helpers

func u32(b []byte, off int) (uint32, bool) {
	if off < 0 || off+4 > len(b) {
		return 0, false
	}
	return *(*uint32)(unsafe.Pointer(&b[off])), true
}

func f32(b []byte, off int) (float32, bool) {
	if off < 0 || off+4 > len(b) {
		return 0, false
	}
	return *(*float32)(unsafe.Pointer(&b[off])), true
}

func f64(b []byte, off int) (float64, bool) {
	if off < 0 || off+8 > len(b) {
		return 0, false
	}
	return *(*float64)(unsafe.Pointer(&b[off])), true
}

// str reads a fixed-width NUL-terminated field.
func str(b []byte, off, width int) string {
	if off < 0 || off+width > len(b) {
		return ""
	}
	return cstr(b[off : off+width])
}

// ---------------------------------------------------------------- HWiNFO

// HWiNFO_SENSORS_SHARED_MEM2, then a table of sensors and a table of readings.
// Everything needed to walk the reading table is in the header, so nothing here
// depends on the size of a reading element being what it was last release.
const (
	hwinfoSignature = 0x53695748 // "SiWH", little-endian

	hwinfoReadingTypeTemp = 1

	// Field offsets inside a reading element. These are the part the header
	// does not describe; dwSizeOfReadingElement is checked against the end of
	// the last one used, so a shorter element is rejected rather than read.
	hwiOffType      = 0
	hwiOffLabelOrig = 12
	hwiOffLabelUser = 12 + 128
	hwiOffValue     = 12 + 128 + 128 + 16
	hwiMinElement   = hwiOffValue + 8
)

func readCPUTempHWiNFO() (float64, string, bool) {
	b, done, ok := mapView(`Global\HWiNFO_SENS_SM2`)
	if !ok {
		return 0, "", false
	}
	defer done()
	if v, ok := parseHWiNFO(b); ok {
		return v, "HWiNFO", true
	}
	return 0, "", false
}

// parseHWiNFO is the layout half, split out so the offsets can be tested
// against a synthetic buffer on a machine with no HWiNFO on it. Every reader
// here is split the same way: mapping a section is not the part that goes
// wrong, reading a struct by hand is.
func parseHWiNFO(b []byte) (float64, bool) {
	sig, _ := u32(b, 0)
	if sig != hwinfoSignature {
		return 0, false
	}
	base, ok1 := u32(b, 28)  // dwOffsetOfReadingSection
	size, ok2 := u32(b, 32)  // dwSizeOfReadingElement
	count, ok3 := u32(b, 36) // dwNumReadingElements
	if !ok1 || !ok2 || !ok3 || size < hwiMinElement || count == 0 || count > 8192 {
		return 0, false
	}

	best, bestRank := 0.0, len(cpuTempNames)
	for i := uint32(0); i < count; i++ {
		off := int(base) + int(i)*int(size)
		if off+int(size) > len(b) {
			break
		}
		if t, _ := u32(b, off+hwiOffType); t != hwinfoReadingTypeTemp {
			continue
		}
		// The user label is the one shown in HWiNFO and the one a user would
		// have renamed; the original is the stable name. Rank both.
		for _, lo := range []int{hwiOffLabelUser, hwiOffLabelOrig} {
			rank, ok := rankCPUTempLabel(str(b, off+lo, 128))
			if !ok || rank >= bestRank {
				continue
			}
			v, okv := f64(b, off+hwiOffValue)
			if okv && plausibleCPUTemp(v) {
				best, bestRank = v, rank
			}
		}
	}
	if bestRank == len(cpuTempNames) {
		return 0, false
	}
	return best, true
}

// ---------------------------------------------------------------- Afterburner

// MAHM_SHARED_MEMORY_HEADER, then dwNumEntries fixed-size entries. Verified
// against a running Afterburner: header 32 bytes, entry 1324, with the source
// name at offset 0 and the value 24 bytes before the end of the entry --
// data, minLimit, maxLimit, dwFlags, dwGpu, dwSrcId. Deriving the value offset
// from the entry size rather than hard-coding 1300 is what makes this survive a
// release that lengthens one of the three name fields.
const (
	mahmSignature   = 0x4D41484D // "MHAM", little-endian
	mahmTrailerSize = 24
	mahmNameWidth   = 520
)

func readCPUTempAfterburner() (float64, string, bool) {
	b, done, ok := mapView("MAHMSharedMemory")
	if !ok {
		return 0, "", false
	}
	defer done()
	if v, ok := parseAfterburner(b); ok {
		return v, "MSI Afterburner", true
	}
	return 0, "", false
}

func parseAfterburner(b []byte) (float64, bool) {
	sig, _ := u32(b, 0)
	if sig != mahmSignature {
		return 0, false
	}
	hdr, ok1 := u32(b, 8)
	count, ok2 := u32(b, 12)
	size, ok3 := u32(b, 16)
	if !ok1 || !ok2 || !ok3 ||
		size < mahmNameWidth+mahmTrailerSize || count == 0 || count > 8192 {
		return 0, false
	}

	best, bestRank := 0.0, len(cpuTempNames)
	for i := uint32(0); i < count; i++ {
		off := int(hdr) + int(i)*int(size)
		if off+int(size) > len(b) {
			break
		}
		rank, ok := rankCPUTempLabel(str(b, off, mahmNameWidth))
		if !ok || rank >= bestRank {
			continue
		}
		v, okv := f32(b, off+int(size)-mahmTrailerSize)
		if okv && plausibleCPUTemp(float64(v)) {
			best, bestRank = float64(v), rank
		}
	}
	if bestRank == len(cpuTempNames) {
		return 0, false
	}
	return best, true
}

// ---------------------------------------------------------------- Core Temp

// CoreTempSharedDataEx. Unlike the other two this has no signature and no
// self-describing sizes, so the sanity checks are the whole defence: a core
// count in range, and a temperature that survives plausibleCPUTemp.
//
// The awkward part is ucDeltaToTjMax. Core Temp can publish either an absolute
// temperature or the distance below TjMax, and which one it is changes with a
// checkbox in its own settings. Reading the wrong one gives about 40 degrees of
// error in a number that looks perfectly reasonable, which is the sort of quiet
// wrongness this file exists to avoid.
const (
	ctOffLoad     = 0                  // unsigned int uiLoad[256]
	ctOffTjMax    = ctOffLoad + 256*4  // unsigned int uiTjMax[128]
	ctOffCoreCnt  = ctOffTjMax + 128*4 // unsigned int uiCoreCnt
	ctOffCPUCnt   = ctOffCoreCnt + 4   // unsigned int uiCPUCnt
	ctOffTemp     = ctOffCPUCnt + 4    // float fTemp[256]
	ctOffVID      = ctOffTemp + 256*4  // float fVID
	ctOffSpeed    = ctOffVID + 4       // float fCPUSpeed
	ctOffFSB      = ctOffSpeed + 4     // float fFSBSpeed
	ctOffMulti    = ctOffFSB + 4       // float fMultiplier
	ctOffName     = ctOffMulti + 4     // char sCPUName[100]
	ctOffFahren   = ctOffName + 100    // unsigned char ucFahrenheit
	ctOffDeltaTj  = ctOffFahren + 1    // unsigned char ucDeltaToTjMax
	ctMinimumSize = ctOffDeltaTj + 1
)

func readCPUTempCoreTemp() (float64, string, bool) {
	b, done, ok := mapView("CoreTempMappingObjectEx")
	if !ok {
		return 0, "", false
	}
	defer done()
	if v, ok := parseCoreTemp(b); ok {
		return v, "Core Temp", true
	}
	return 0, "", false
}

func parseCoreTemp(b []byte) (float64, bool) {
	if len(b) < ctMinimumSize {
		return 0, false
	}

	cores, _ := u32(b, ctOffCoreCnt)
	cpus, _ := u32(b, ctOffCPUCnt)
	if cores == 0 || cores > 256 || cpus == 0 || cpus > 8 {
		return 0, false
	}
	total := int(cores) * int(cpus)
	if total > 256 {
		total = 256
	}

	fahrenheit := b[ctOffFahren] != 0
	delta := b[ctOffDeltaTj] != 0

	// The package temperature is the hottest core. Core Temp reports per core
	// and no package figure, and the hottest is what throttles.
	hottest, found := 0.0, false
	for i := 0; i < total; i++ {
		v, ok := f32(b, ctOffTemp+i*4)
		if !ok {
			break
		}
		t := float64(v)
		if delta {
			tj, ok := u32(b, ctOffTjMax+(i/int(cores))*4)
			if !ok || tj == 0 || tj > 150 {
				return 0, false
			}
			t = float64(tj) - t
		} else if fahrenheit {
			t = (t - 32) * 5 / 9
		}
		if !plausibleCPUTemp(t) {
			continue
		}
		if !found || t > hottest {
			hottest, found = t, true
		}
	}
	if !found {
		return 0, false
	}
	return hottest, true
}

// ---------------------------------------------------------------- label ranking

// rankCPUTempLabel scores a sensor label against cpuTempNames, lower is better.
//
// Matching is on a normalised substring rather than equality, because the three
// monitors name the same sensor differently -- "CPU (Tctl/Tdie)" in HWiNFO,
// "CPU temperature" in Afterburner, "Core (Tctl/Tdie)" in LibreHardwareMonitor.
// The names are still specific enough that no GPU, drive or chipset sensor
// scores: "GPU Temperature" contains none of them.
func rankCPUTempLabel(label string) (int, bool) {
	l := strings.ToLower(strings.TrimSpace(label))
	if l == "" {
		return 0, false
	}
	// A GPU sensor can otherwise match "cpu" inside a longer string on some
	// naming schemes; exclude the whole family up front rather than trying to
	// make every pattern below GPU-proof.
	if strings.Contains(l, "gpu") {
		return 0, false
	}
	for rank, want := range cpuTempNames {
		if strings.Contains(l, want) {
			return rank, true
		}
	}
	return 0, false
}
