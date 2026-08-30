package main

// Whole-machine metrics: CPU load, memory, and the CPU's current clock.
//
// This sits beside sensors.go and follows the same rules. It is a reporting
// path only -- nothing here feeds back into mining -- and a metric this
// platform cannot produce is reported as absent rather than as zero. A memory
// panel that says 0.0 GB on macOS is worse than one that says "--", because
// only one of the two is believable enough to act on.
//
// Polled on its own goroutine at a fixed interval for the same reason the
// sensors are: the render loop runs at 8 frames a second and every one of
// these readings crosses into the kernel, so at render rate the cost would be
// paid forty times for each time the number visibly changes.

import (
	"sync"
	"time"
)

// SysSample is one poll of the machine.
//
// Every field is paired with a Have flag rather than using a sentinel, because
// zero is a real value for all of them: a machine can genuinely be at 0% load,
// and a reader has to be able to tell that from "this platform has no way to
// ask".
type SysSample struct {
	At time.Time

	HaveLoad bool
	LoadPct  float64 // 0..100, whole machine, all cores

	HaveMem               bool
	MemUsedMB, MemTotalMB int

	HaveFreq bool
	FreqMHz  int
}

// MemUsedFrac is memory in use as a fraction, for a gauge.
func (s SysSample) MemUsedFrac() float64 {
	if !s.HaveMem || s.MemTotalMB <= 0 {
		return 0
	}
	return float64(s.MemUsedMB) / float64(s.MemTotalMB)
}

// SysMonitor polls the machine in the background and hands out the latest
// sample.
type SysMonitor struct {
	mu     sync.RWMutex
	cur    SysSample
	closed chan struct{}
	once   sync.Once
}

// sysInterval is how often the machine is asked. Load is a rate and needs two
// readings a measurable distance apart; a second is comfortably that, and
// keeps the number responsive enough to see a thread count change land.
const sysInterval = time.Second

func NewSysMonitor() *SysMonitor {
	m := &SysMonitor{closed: make(chan struct{})}
	// Prime the load counter. The first reading of a rate has nothing to
	// subtract from, so it is taken and thrown away here rather than shown as
	// a spurious 0% on the opening frame.
	readCPULoad()
	m.poll()
	go func() {
		t := time.NewTicker(sysInterval)
		defer t.Stop()
		for {
			select {
			case <-m.closed:
				return
			case <-t.C:
				m.poll()
			}
		}
	}()
	return m
}

func (m *SysMonitor) Sample() SysSample {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cur
}

func (m *SysMonitor) Close() { m.once.Do(func() { close(m.closed) }) }

func (m *SysMonitor) poll() {
	s := SysSample{At: time.Now()}
	if v, ok := readCPULoad(); ok {
		s.LoadPct, s.HaveLoad = v, true
	}
	if used, total, ok := readMemory(); ok {
		s.MemUsedMB, s.MemTotalMB, s.HaveMem = used, total, true
	}
	if v, ok := readCPUFreqMHz(); ok {
		s.FreqMHz, s.HaveFreq = v, true
	}

	m.mu.Lock()
	m.cur = s
	m.mu.Unlock()
}
