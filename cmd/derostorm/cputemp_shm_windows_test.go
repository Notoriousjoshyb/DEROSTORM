//go:build windows

package main

// The three shared-memory formats are read by hand from struct offsets, which
// is the part that goes wrong: a field added upstream, or an offset mistyped
// here, produces a number that is plausible and wrong rather than an error.
//
// So each format is built synthetically at the layout this file claims, with
// decoys around the value that must win, and parsed back. That catches an
// offset mistake, a wrong field width, a signature check that is not checking,
// and a ranking that prefers the wrong sensor.

import (
	"encoding/binary"
	"math"
	"testing"
)

func putU32(b []byte, off int, v uint32) { binary.LittleEndian.PutUint32(b[off:], v) }
func putF32(b []byte, off int, v float32) {
	binary.LittleEndian.PutUint32(b[off:], math.Float32bits(v))
}
func putF64(b []byte, off int, v float64) {
	binary.LittleEndian.PutUint64(b[off:], math.Float64bits(v))
}
func putStr(b []byte, off int, s string) { copy(b[off:], s); b[off+len(s)] = 0 }

// ---------------------------------------------------------------- HWiNFO

func buildHWiNFO(t *testing.T) []byte {
	t.Helper()
	const elem = 1024 // deliberately larger than the minimum this file needs
	const hdr = 44

	type reading struct {
		kind  uint32
		label string
		value float64
	}
	// A real sensor list: GPU and drive temperatures around the CPU one, plus
	// a non-temperature reading whose value would be a plausible temperature if
	// the type check were not done.
	readings := []reading{
		{hwinfoReadingTypeTemp, "GPU Temperature", 66},
		{hwinfoReadingTypeTemp, "Drive Temperature", 44},
		{6, "CPU (Tctl/Tdie)", 55}, // wrong type: a clock, not a temperature
		{hwinfoReadingTypeTemp, "Core Max", 84.1},
		{hwinfoReadingTypeTemp, "CPU (Tctl/Tdie)", 71.5},
		{hwinfoReadingTypeTemp, "CPU Package Power", 120},
	}

	b := make([]byte, hdr+len(readings)*elem)
	putU32(b, 0, hwinfoSignature)
	putU32(b, 28, hdr)
	putU32(b, 32, elem)
	putU32(b, 36, uint32(len(readings)))

	for i, r := range readings {
		off := hdr + i*elem
		putU32(b, off+hwiOffType, r.kind)
		putStr(b, off+hwiOffLabelOrig, r.label)
		putStr(b, off+hwiOffLabelUser, r.label)
		putF64(b, off+hwiOffValue, r.value)
	}
	return b
}

func TestParseHWiNFO(t *testing.T) {
	got, ok := parseHWiNFO(buildHWiNFO(t))
	if !ok {
		t.Fatal("found no CPU temperature in a list that contains one")
	}
	// Tctl/Tdie outranks Core Max, the GPU and drive sensors must not match at
	// all, and the clock reading labelled Tctl must be excluded by its type.
	if got < 71.4 || got > 71.6 {
		t.Errorf("got %.2f, want the 71.5 Tctl/Tdie reading", got)
	}
}

func TestParseHWiNFORejectsRubbish(t *testing.T) {
	good := buildHWiNFO(t)

	bad := append([]byte(nil), good...)
	putU32(bad, 0, 0xDEADBEEF)
	if _, ok := parseHWiNFO(bad); ok {
		t.Error("accepted a buffer with the wrong signature")
	}

	bad = append([]byte(nil), good...)
	putU32(bad, 32, 8) // element far smaller than the fields read from it
	if _, ok := parseHWiNFO(bad); ok {
		t.Error("accepted an element size too small to hold the fields")
	}

	bad = append([]byte(nil), good...)
	putU32(bad, 36, 1<<20) // a count that would walk off the end
	if _, ok := parseHWiNFO(bad); ok {
		t.Error("accepted an implausible reading count")
	}

	if _, ok := parseHWiNFO(nil); ok {
		t.Error("accepted an empty buffer")
	}
	if _, ok := parseHWiNFO(make([]byte, 8)); ok {
		t.Error("accepted a buffer shorter than the header")
	}
}

// ---------------------------------------------------------------- Afterburner

func buildAfterburner(t *testing.T, entrySize int, entries [][2]interface{}) []byte {
	t.Helper()
	const hdr = 32
	b := make([]byte, hdr+len(entries)*entrySize)
	putU32(b, 0, mahmSignature)
	putU32(b, 4, 0x00020000)
	putU32(b, 8, hdr)
	putU32(b, 12, uint32(len(entries)))
	putU32(b, 16, uint32(entrySize))
	for i, e := range entries {
		off := hdr + i*entrySize
		putStr(b, off, e[0].(string))
		putF32(b, off+entrySize-mahmTrailerSize, float32(e[1].(float64)))
	}
	return b
}

func TestParseAfterburner(t *testing.T) {
	// 1324 is the entry size a real Afterburner publishes; the parser derives
	// the value offset from it rather than hard-coding one, so a different size
	// must work too. Both are checked.
	for _, size := range []int{1324, 1600} {
		b := buildAfterburner(t, size, [][2]interface{}{
			{"GPU1 temperature", 66.0},
			{"GPU1 power", 215.0},
			{"CPU temperature", 71.0},
			{"CPU usage", 99.0}, // a percentage, rejected by the plausibility filter
		})
		got, ok := parseAfterburner(b)
		if !ok {
			t.Fatalf("entry size %d: found no CPU temperature", size)
		}
		if got < 70.9 || got > 71.1 {
			t.Errorf("entry size %d: got %.2f, want 71", size, got)
		}
	}
}

func TestParseAfterburnerWithNoCPUSensor(t *testing.T) {
	// This is the state of a stock Afterburner: it is running, its shared
	// memory is valid, and it is monitoring nothing but the card. It must be a
	// clean miss, so the probe moves on to the next source.
	b := buildAfterburner(t, 1324, [][2]interface{}{
		{"GPU1 temperature", 66.0},
		{"GPU1 power", 215.0},
		{"GPU1 fan speed", 40.0},
	})
	if v, ok := parseAfterburner(b); ok {
		t.Errorf("claimed %.1f°C from a GPU-only sensor list", v)
	}
}

// ---------------------------------------------------------------- Core Temp

func buildCoreTemp(t *testing.T, temps []float32, tjMax uint32, delta, fahrenheit bool) []byte {
	t.Helper()
	b := make([]byte, ctMinimumSize+16)
	putU32(b, ctOffCoreCnt, uint32(len(temps)))
	putU32(b, ctOffCPUCnt, 1)
	putU32(b, ctOffTjMax, tjMax)
	for i, v := range temps {
		putF32(b, ctOffTemp+i*4, v)
	}
	if delta {
		b[ctOffDeltaTj] = 1
	}
	if fahrenheit {
		b[ctOffFahren] = 1
	}
	return b
}

func TestParseCoreTempAbsolute(t *testing.T) {
	// The package temperature is the hottest core, because that is the one
	// that throttles.
	got, ok := parseCoreTemp(buildCoreTemp(t, []float32{61, 68, 72.5, 65}, 95, false, false))
	if !ok {
		t.Fatal("found no temperature")
	}
	if got < 72.4 || got > 72.6 {
		t.Errorf("got %.2f, want the hottest core at 72.5", got)
	}
}

func TestParseCoreTempDeltaToTjMax(t *testing.T) {
	// With ucDeltaToTjMax set the floats are the distance below TjMax, not the
	// temperature. Reading the wrong one is about 40 degrees of error in a
	// number that looks entirely reasonable, which is why it has its own test.
	got, ok := parseCoreTemp(buildCoreTemp(t, []float32{34, 27, 22.5, 30}, 95, true, false))
	if !ok {
		t.Fatal("found no temperature")
	}
	if got < 72.4 || got > 72.6 {
		t.Errorf("got %.2f, want 95 - 22.5 = 72.5", got)
	}
}

func TestParseCoreTempFahrenheit(t *testing.T) {
	got, ok := parseCoreTemp(buildCoreTemp(t, []float32{162.5}, 95, false, true))
	if !ok {
		t.Fatal("found no temperature")
	}
	if got < 72.4 || got > 72.6 {
		t.Errorf("got %.2f, want 162.5F converted to 72.5C", got)
	}
}

func TestParseCoreTempRejectsRubbish(t *testing.T) {
	if _, ok := parseCoreTemp(make([]byte, 16)); ok {
		t.Error("accepted a buffer far shorter than the struct")
	}
	if _, ok := parseCoreTemp(make([]byte, ctMinimumSize+16)); ok {
		t.Error("accepted an all-zero buffer, which has no cores and no temperature")
	}
	// A delta reading with no TjMax cannot be turned into a temperature.
	if _, ok := parseCoreTemp(buildCoreTemp(t, []float32{34}, 0, true, false)); ok {
		t.Error("accepted a delta reading with TjMax zero")
	}
}

// ---------------------------------------------------------------- ranking

func TestRankCPUTempLabelIgnoresOtherHardware(t *testing.T) {
	for _, l := range []string{
		"GPU Temperature", "GPU1 temperature", "GPU Hot Spot",
		"Drive Temperature", "Motherboard", "VRM", "", "   ",
		"System Fan", "Ambient",
	} {
		if _, ok := rankCPUTempLabel(l); ok {
			t.Errorf("%q was ranked as a CPU sensor", l)
		}
	}

	// The same sensor as each monitor spells it.
	better, ok1 := rankCPUTempLabel("CPU (Tctl/Tdie)")
	worse, ok2 := rankCPUTempLabel("Core Max")
	if !ok1 || !ok2 {
		t.Fatal("a known CPU label was not ranked")
	}
	if better >= worse {
		t.Error("Tctl/Tdie should outrank a per-core maximum")
	}
	for _, l := range []string{"Core (Tctl/Tdie)", "CPU Package", "CPU temperature", "CPU Die (average)"} {
		if _, ok := rankCPUTempLabel(l); !ok {
			t.Errorf("%q was not recognised as a CPU sensor", l)
		}
	}
}

// ---------------------------------------------------------------- live

// TestSharedMemorySourcesDoNotMisbehave runs every reader against whatever is
// actually on this machine. It asserts nothing about finding a value -- most
// machines have none of these running -- only that a reader either declines or
// returns something a CPU could produce, and that mapping and unmapping a real
// section is clean.
func TestSharedMemorySourcesDoNotMisbehave(t *testing.T) {
	for _, s := range []struct {
		name string
		fn   func() (float64, string, bool)
	}{
		{"HWiNFO", readCPUTempHWiNFO},
		{"Core Temp", readCPUTempCoreTemp},
		{"Afterburner", readCPUTempAfterburner},
	} {
		// Twice, because the second call is the one that would fault if the
		// first had unmapped something it still held a reference into.
		for i := 0; i < 2; i++ {
			v, src, ok := s.fn()
			if !ok {
				continue
			}
			if !plausibleCPUTemp(v) {
				t.Errorf("%s returned %.1f°C, which the filter should have stopped", s.name, v)
			}
			if src == "" {
				t.Errorf("%s returned a value with no source name", s.name)
			}
			t.Logf("%s: %.1f°C", s.name, v)
		}
	}
}
