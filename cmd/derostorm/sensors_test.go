package main

import (
	"strings"
	"testing"
	"time"
)

// Sensor readings come from hardware, so the tests split in two: everything
// that can be checked without hardware is checked always, and the live NVML
// path is checked only on a machine that has a card to ask.

func TestTempTextAndUnknown(t *testing.T) {
	cases := []struct {
		c    float64
		want string
	}{
		{tempUnknown, "--"},
		{-300, "--"},
		{61.4, "61°C"},
		{61.6, "62°C"},
		// 0°C is a real reading from a cold machine, not a missing one. This is
		// the case that says why the sentinel is -273 and not zero.
		{0, "0°C"},
	}

	for _, c := range cases {
		if got := tempText(c.c); got != c.want {
			t.Errorf("tempText(%v) = %q, want %q", c.c, got, c.want)
		}
	}
}

func TestTempColourBands(t *testing.T) {
	th := themes["default"]
	for _, c := range []struct {
		temp float64
		want string
		name string
	}{
		{tempUnknown, th.Dim, "unknown"},
		{45, th.Good, "cool"},
		{64.9, th.Good, "just under warm"},
		{65, th.Warn, "warm"},
		{79.9, th.Warn, "just under hot"},
		{80, th.Err, "hot"},
		{101, th.Err, "very hot"},
	} {
		if got := tempColour(th, c.temp); got != c.want {
			t.Errorf("tempColour(%s, %v): wrong band", c.name, c.temp)
		}
	}
}

func TestSensorSampleLookup(t *testing.T) {
	s := SensorSample{GPUs: []GPUSensor{{Index: 0, TempC: 61}, {Index: 3, TempC: 70}}}
	if g := s.gpuByIndex(3); g == nil || g.TempC != 70 {
		t.Fatalf("gpuByIndex(3) did not find device 3")
	}
	if g := s.gpuByIndex(1); g != nil {
		t.Fatalf("gpuByIndex(1) found a device that is not being polled")
	}
	if s.HaveCPUTemp() {
		t.Fatalf("a zero SensorSample must not claim a CPU temperature")
	}
}

// TestSensorsPollsWithoutHardware is the case every machine runs: no devices,
// nothing to load, and a sample that says so without blocking or panicking.
func TestSensorsPollsWithoutHardware(t *testing.T) {
	s := NewSensors(nil)
	defer s.Close()

	got := s.Sample()
	if got.At.IsZero() {
		t.Fatal("NewSensors must take its first sample before returning")
	}
	if len(got.GPUs) != 0 {
		t.Fatalf("no devices were asked for, got %d", len(got.GPUs))
	}
}

// TestNVMLLive checks the binding against a real driver. Skipped where there is
// no card, which includes every CI runner -- the point is to catch a wrong
// signature on the machines that can catch it, not to demand a GPU.
func TestNVMLLive(t *testing.T) {
	if !GPUAvailable || GPUDeviceCount() == 0 {
		t.Skip("no CUDA device on this machine")
	}
	if note := openNVML([]int{0}); note != "" {
		t.Skipf("NVML not usable here: %s", note)
	}
	defer closeNVML()

	got := readGPUSensors([]int{0})
	if len(got) != 1 {
		t.Fatalf("asked for device 0, got %d readings", len(got))
	}
	g := got[0]

	// A wrong argument list shows up here as a value that is not a
	// temperature, not as a crash, so the range check is the real assertion.
	if g.TempC <= tempUnknown {
		t.Error("no GPU temperature reported")
	} else if g.TempC < 5 || g.TempC > 120 {
		t.Errorf("GPU temperature %.0f is outside anything a working card reports", g.TempC)
	}
	if g.Name == "" {
		t.Error("no device name reported")
	}
	if g.HavePower && (g.PowerW < 1 || g.PowerW > 1500) {
		t.Errorf("power draw %.1f W is not plausible", g.PowerW)
	}
	if g.HaveMem && g.MemTotalMB < 512 {
		t.Errorf("memory total %d MB is not plausible", g.MemTotalMB)
	}
	if g.HaveUtil && (g.UtilPct < 0 || g.UtilPct > 100) {
		t.Errorf("utilisation %d%% is not a percentage", g.UtilPct)
	}
	t.Logf("device 0: %s  %.0f°C  %.0f/%.0f W  fan %d%%  util %d%%  %d/%d MB  %d MHz",
		g.Name, g.TempC, g.PowerW, g.PowerCapW, g.FanPct, g.UtilPct,
		g.MemUsedMB, g.MemTotalMB, g.ClockMHz)
}

// TestCPUTempIsPlausibleOrAbsent never fails for want of a sensor. It fails
// only when a sensor answers with something no CPU produces, which is the bug
// worth catching: a source read in the wrong unit is far more damaging than a
// source that is missing.
func TestCPUTempIsPlausibleOrAbsent(t *testing.T) {
	c, src, ok := readCPUTemp()
	if !ok {
		t.Skipf("no CPU temperature source here: %s", cpuTempHint)
	}
	if c < 5 || c > 125 {
		t.Errorf("CPU temperature %.1f from %q is not plausible", c, src)
	}
	if strings.TrimSpace(src) == "" {
		t.Error("a working source must name itself")
	}
	t.Logf("CPU %.1f°C from %s", c, src)
}

func TestSensorIntervalIsNotPerFrame(t *testing.T) {
	// The render tick is 200ms. Polling a driver at that rate is the mistake
	// this constant exists to prevent, so it is worth a test rather than only a
	// comment.
	if sensorInterval < time.Second {
		t.Fatalf("sensorInterval is %v, which is close enough to the render tick to matter", sensorInterval)
	}
}

// The panel is where a temperature is actually of any use, so the wiring from
// a sensor sample to a drawn row is worth pinning: everything else in this file
// could pass with the number never reaching the screen.
func TestDeviceRowsCarryTheSensorReadings(t *testing.T) {
	c := NewConsole(discardWriter{}, themes["mono"], true, 6)
	s := Snapshot{
		Threads: 15, GPUs: 1, Hashrate: 103150,
		CPURate: 33510, GPURate: 69640,
		Sensors: SensorSample{
			HaveCPU: true, CPUTempC: 71, CPUSource: "hardware monitor",
			GPUs: []GPUSensor{{Index: 0, TempC: 66, PowerW: 215, HavePower: true}},
		},
	}

	var cpu, gpu string
	for _, ln := range c.frame(s) {
		if strings.Contains(ln, "CPU") && strings.Contains(ln, "KH/s") {
			cpu = ln
		}
		if strings.Contains(ln, "GPU 0") {
			gpu = ln
		}
	}
	if cpu == "" || gpu == "" {
		t.Fatal("the device section did not draw a CPU and a GPU 0 row")
	}
	if !strings.Contains(cpu, "71°C") {
		t.Errorf("CPU row has no temperature: %q", cpu)
	}
	if !strings.Contains(gpu, "66°C") {
		t.Errorf("GPU row has no temperature: %q", gpu)
	}
	if !strings.Contains(gpu, "215W") {
		t.Errorf("GPU row has no power draw: %q", gpu)
	}

	// With no sensors at all the rows still draw, with "--" where a reading
	// would be. A machine that cannot read a temperature must not lose the
	// hashrate rows along with it.
	bare := c.frame(Snapshot{Threads: 15, GPUs: 1, CPURate: 1, GPURate: 1})
	found := false
	for _, ln := range bare {
		if strings.Contains(ln, "GPU 0") && strings.Contains(ln, "--") {
			found = true
		}
	}
	if !found {
		t.Error("with no sensor readings the GPU row should still draw, showing --")
	}
}

// GPU efficiency is a number someone would use to choose a power limit, so it
// must be absent rather than wrong whenever the power reading is.
func TestGPUEfficiencyNeedsRealPower(t *testing.T) {
	withPower := Snapshot{
		GPUs: 1, GPURate: 70000, CPURate: 34000,
		Sensors: SensorSample{GPUs: []GPUSensor{{Index: 0, PowerW: 215, HavePower: true}}},
	}
	eff, ok := gpuEfficiency(withPower)
	if !ok {
		t.Fatal("power was reported but no efficiency came out")
	}
	if want := 70000.0 / 215.0; eff < want-1 || eff > want+1 {
		t.Errorf("efficiency %.1f H/W, want about %.1f", eff, want)
	}

	for _, s := range []Snapshot{
		{GPUs: 1, GPURate: 70000}, // no sensors at all
		{GPUs: 1, GPURate: 70000, Sensors: SensorSample{GPUs: []GPUSensor{{Index: 0}}}},     // sensor, no power
		{GPUs: 1, Sensors: SensorSample{GPUs: []GPUSensor{{PowerW: 215, HavePower: true}}}}, // power, no hashes
	} {
		if _, ok := gpuEfficiency(s); ok {
			t.Errorf("produced an efficiency from %+v", s.Sensors)
		}
	}
}
