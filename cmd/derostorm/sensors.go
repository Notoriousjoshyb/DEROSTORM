package main

// Hardware sensors: CPU and GPU temperature, and what else the GPU will tell us
// for free while it is being asked.
//
// This is deliberately a *reporting* path and nothing else. Nothing here feeds
// back into mining: no thermal throttle, no fan control, no clock changes. A
// miner that silently backs off when a number it half-trusts goes up is worse
// than one that shows the number and lets its owner decide.
//
// Two facts shape the whole file:
//
//   - GPU temperature is easy and reliable. NVML ships with every NVIDIA
//     driver, so the same machine that can mine on CUDA can always be asked
//     what the card is doing. See nvml.go.
//   - CPU temperature is neither. There is no user-mode API for a modern AMD or
//     Intel package temperature on Windows: HWiNFO and LibreHardwareMonitor
//     read it through a kernel driver they install. So Windows gets a chain of
//     best-effort sources and, quite often, an honest "--". Linux has hwmon and
//     is fine. See cputemp_*.go.
//
// Polling is done on its own goroutine at a fixed interval rather than inside
// the render tick. The render tick is 200ms and a sensor read crosses into a
// driver; at 5 reads a second that cost would be paid 25 times for every time
// the number actually changes.

import (
	"strconv"
	"sync"
	"time"
)

// tempUnknown is the temperature value meaning "not available". Celsius, so a
// real reading can never collide with it.
const tempUnknown = -273.0

// GPUSensor is one device's live telemetry. Any field may be zero when the
// driver does not report it -- fan speed on a passively cooled card, for
// instance -- and TempC is tempUnknown when even the temperature is missing.
type GPUSensor struct {
	Index int
	Name  string

	TempC       float64
	FanPct      int
	UtilPct     int
	PowerW      float64
	PowerCapW   float64
	MemUsedMB   int
	MemTotalMB  int
	ClockMHz    int
	MemClockMHz int

	// Have* say whether the field above was actually reported, so a real zero
	// (an idle card at 0% fan) is not drawn as a missing reading.
	HaveFan, HaveUtil, HavePower, HaveMem, HaveClock bool
}

// SensorSample is one poll of everything.
type SensorSample struct {
	At time.Time

	// HaveCPU is what says the temperature is real, rather than the value
	// itself. 0°C is a temperature a cold machine genuinely reports, so a
	// zero SensorSample -- one nothing has filled in yet -- must not be
	// readable as a freezing CPU.
	HaveCPU   bool
	CPUTempC  float64 // only meaningful when HaveCPU
	CPUSource string  // where the number came from, for the config listing

	GPUs []GPUSensor
}

// gpuByIndex finds a device's sample. Returns nil when the device is not being
// polled, which is the normal case for a device that is present but not mining.
func (s *SensorSample) gpuByIndex(i int) *GPUSensor {
	for k := range s.GPUs {
		if s.GPUs[k].Index == i {
			return &s.GPUs[k]
		}
	}
	return nil
}

// HaveCPUTemp reports whether the CPU temperature in this sample is real.
func (s *SensorSample) HaveCPUTemp() bool { return s.HaveCPU }

// CPUTemp is the CPU temperature, or tempUnknown when there is none, so a
// caller can hand it straight to tempText without a branch of its own.
func (s *SensorSample) CPUTemp() float64 {
	if !s.HaveCPU {
		return tempUnknown
	}
	return s.CPUTempC
}

// Sensors polls the machine in the background and hands out the latest sample.
type Sensors struct {
	mu     sync.RWMutex
	cur    SensorSample
	note   string
	closed chan struct{}
	once   sync.Once
}

// sensorInterval is how often the hardware is asked. Temperatures move on the
// scale of seconds, not frames, and every read crosses into a driver.
const sensorInterval = 2 * time.Second

// NewSensors starts polling. devices are the GPU indices being mined on; pass
// nil for a CPU-only run. The first sample is taken synchronously, so the very
// first frame already has numbers in it rather than a row of dashes that fills
// in two seconds later.
func NewSensors(devices []int) *Sensors {
	s := &Sensors{closed: make(chan struct{})}

	note := openNVML(devices)
	s.note = note

	s.poll(devices)
	go func() {
		t := time.NewTicker(sensorInterval)
		defer t.Stop()
		for {
			select {
			case <-s.closed:
				closeNVML()
				return
			case <-t.C:
				s.poll(devices)
			}
		}
	}()
	return s
}

// Notes are the lines worth putting in the event log once at start-up: what
// could not be read, and what would make it readable. Empty when everything
// worked.
//
// Said once, at start-up, and never again. A miner that repeats "no CPU
// temperature" every two seconds has turned a missing nicety into the loudest
// thing on the screen.
func (s *Sensors) Notes() []string {
	var out []string
	if s.note != "" {
		out = append(out, s.note)
	}
	sample := s.Sample()
	if sample.HaveCPUTemp() {
		// Say where it came from, once. On Windows the answer decides how much
		// to trust the number -- a driver-read package temperature and an ACPI
		// zone are not the same claim -- and the panel has no room to say so.
		out = append(out, "CPU temperature from "+sample.CPUSource)
	} else {
		out = append(out, cpuTempHint)
	}
	return out
}

// Sample is the most recent poll.
func (s *Sensors) Sample() SensorSample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

func (s *Sensors) Close() {
	s.once.Do(func() { close(s.closed) })
}

func (s *Sensors) poll(devices []int) {
	sample := SensorSample{At: time.Now(), CPUTempC: tempUnknown}
	if c, src, ok := readCPUTemp(); ok {
		sample.HaveCPU, sample.CPUTempC, sample.CPUSource = true, c, src
	}
	sample.GPUs = readGPUSensors(devices)

	s.mu.Lock()
	s.cur = sample
	s.mu.Unlock()
}

// ---------------------------------------------------------------- formatting

// tempText is a temperature for a table cell. Rounded to a whole degree,
// because a tenth of a degree from a sensor with a two-degree offset is
// precision the number does not have.
func tempText(c float64) string {
	if c <= tempUnknown {
		return "--"
	}
	return strconv.Itoa(int(c+0.5)) + "°C"
}

// tempColour bands a temperature so a glance is enough. The boundaries are
// deliberately conservative: 75 is where a card starts trading clocks for
// safety, and 85 is where most of them are throttling in earnest.
func tempColour(t *Theme, c float64) string {
	switch {
	case c <= tempUnknown:
		return t.Dim
	case c < 65:
		return t.Good
	case c < 80:
		return t.Warn
	default:
		return t.Err
	}
}
