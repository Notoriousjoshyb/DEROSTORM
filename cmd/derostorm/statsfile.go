package main

// A machine-readable status file, for rig managers.
//
// The consoles are for a person: they draw a panel, they colour it, and they
// reflow it to the window. A rig manager wants none of that -- it wants numbers,
// on a schedule, in a format it can parse without regretting it. HiveOS is the
// case this exists for (its h-stats.sh reads this file with jq), but nothing
// here is HiveOS-specific and any monitor can read it.
//
// The file is written whole and renamed into place, so a reader either sees the
// previous document or the next one and never half of one. It is written on a
// timer rather than every frame, because a frame is 125 ms and no monitor polls
// that fast.
//
// It is off unless --stats-file names a path, and when it is off this file
// costs one nil check per frame.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// statsInterval is how often the document is rewritten. HiveOS asks its miners
// every ten seconds; five is comfortably inside that without being noise.
const statsInterval = 5 * time.Second

// statsDoc is the document itself. The field names are the contract -- a
// monitor's config refers to them by name -- so they are snake_case, stable,
// and always present even when the value is unknown. An absent field and a
// zero one are different things to a parser, and only one of them is easy.
type statsDoc struct {
	Version string `json:"version"`
	State   string `json:"state"` // connecting, mining, reconnecting, stopping
	Node    string `json:"node"`
	Testnet bool   `json:"testnet"`
	Uptime  int64  `json:"uptime"` // seconds

	// Hashes per second. Total, and split by where it came from -- a rig with a
	// dead card reads the same total as a rig with a slow one until you look at
	// the split.
	Hashrate float64 `json:"hashrate"`
	CPURate  float64 `json:"hashrate_cpu"`
	GPURate  float64 `json:"hashrate_gpu"`
	AvgRate  float64 `json:"hashrate_avg"`
	PeakRate float64 `json:"hashrate_peak"`

	Threads int `json:"threads"`
	GPUs    int `json:"gpus"`

	// Solo mining, so these are what "accepted" means here: a miniblock is a
	// share of a block and a block is a block. There is no pool account and
	// no pool share counter to report instead.
	MiniBlocks uint64 `json:"miniblocks"`
	Blocks     uint64 `json:"blocks"`
	Rejected   uint64 `json:"rejected"`

	Height     int64  `json:"height"`
	Difficulty uint64 `json:"difficulty"`
	NetHashes  uint64 `json:"net_hashrate"`

	// GPUTuning is true while a card is still measuring its block count, which
	// is the one time its hashrate should not be believed.
	GPUTuning bool `json:"gpu_tuning"`

	Devices []statsDevice `json:"devices"`
}

// statsDevice is one hashing source. The CPU is first and then a row per GPU,
// which is the order the panel uses and the order a monitor's per-card arrays
// want.
type statsDevice struct {
	Label string  `json:"label"`
	IsGPU bool    `json:"is_gpu"`
	Index int     `json:"index"` // GPU index, or -1 for the CPU
	Rate  float64 `json:"hashrate"`

	// Ailing marks a source that is running and returning nothing, which is the
	// most expensive thing that can go quietly wrong on a rig.
	Ailing bool `json:"ailing"`

	// Sensor readings. A missing reading is null rather than zero: 0 degrees
	// and 0% fan are both real values a card can report, and a monitor that
	// cannot tell them from "no sensor" will alarm on a cold rig.
	TempC   *float64 `json:"temp_c"`
	FanPct  *int     `json:"fan_pct"`
	PowerW  *float64 `json:"power_w"`
	UtilPct *int     `json:"util_pct"`
	MemMB   *int     `json:"mem_used_mb"`
	Name    string   `json:"name,omitempty"`
}

// statsWriter rewrites the document on a timer. The zero value is unusable;
// newStatsWriter returns nil when no path was asked for, and every method
// tolerates a nil receiver so the call site needs no branch.
type statsWriter struct {
	path string
	next time.Time
}

func newStatsWriter(path string) *statsWriter {
	if path == "" {
		return nil
	}
	return &statsWriter{path: path}
}

// Write emits the document if the interval has elapsed. Errors are dropped on
// purpose: a rig manager that cannot read its stats file is a monitoring
// problem, and stopping the miner over one would turn it into a mining problem.
func (w *statsWriter) Write(s Snapshot, version string, force bool) {
	if w == nil {
		return
	}
	now := time.Now()
	if !force && now.Before(w.next) {
		return
	}
	w.next = now.Add(statsInterval)

	b, err := json.MarshalIndent(w.doc(s, version), "", "  ")
	if err != nil {
		return
	}
	b = append(b, '\n')

	// Whole file or nothing. A reader polling this while it is being written
	// would otherwise get a truncated document and, if it is a shell script,
	// report a hashrate of zero and trigger somebody's alarm at 3am.
	tmp := w.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, w.path); err != nil {
		os.Remove(tmp)
	}
}

// Remove deletes the file. Called on the way out, so a monitor polling a miner
// that has stopped sees the file vanish rather than a stale hashrate that never
// changes again.
func (w *statsWriter) Remove() {
	if w == nil {
		return
	}
	os.Remove(w.path)
	os.Remove(w.path + ".tmp")
}

func (w *statsWriter) doc(s Snapshot, version string) statsDoc {
	d := statsDoc{
		Version:    version,
		State:      stateName(s.State),
		Node:       s.Node,
		Testnet:    s.Testnet,
		Uptime:     int64(s.Uptime / time.Second),
		Hashrate:   s.Hashrate,
		CPURate:    s.CPURate,
		GPURate:    s.GPURate,
		AvgRate:    s.AvgRate,
		PeakRate:   s.PeakRate,
		Threads:    s.Threads,
		GPUs:       s.GPUs,
		MiniBlocks: s.MiniBlocks,
		Blocks:     s.Blocks,
		Rejected:   s.Rejected,
		Height:     s.Height,
		Difficulty: s.Difficulty,
		NetHashes:  s.NetHashes,
		GPUTuning:  s.GPUTuning,
	}

	gpu := 0
	for _, r := range s.deviceRows() {
		e := statsDevice{
			Label:  r.Label,
			IsGPU:  r.IsGPU,
			Index:  -1,
			Rate:   r.Rate,
			Ailing: r.Ailing,
		}
		if r.TempC != tempUnknown {
			t := r.TempC
			e.TempC = &t
		}
		if r.IsGPU {
			e.Index = gpu
			// The panel's row carries a temperature and a note; the sensor
			// sample carries the rest, and it is keyed by device index.
			if g := s.Sensors.gpuByIndex(gpu); g != nil {
				e.Name = g.Name
				if g.HaveFan {
					v := g.FanPct
					e.FanPct = &v
				}
				if g.HavePower {
					v := g.PowerW
					e.PowerW = &v
				}
				if g.HaveUtil {
					v := g.UtilPct
					e.UtilPct = &v
				}
				if g.HaveMem {
					v := g.MemUsedMB
					e.MemMB = &v
				}
			}
			gpu++
		}
		d.Devices = append(d.Devices, e)
	}
	if d.Devices == nil {
		d.Devices = []statsDevice{}
	}
	return d
}

func stateName(s MinerState) string {
	switch s {
	case StateMining:
		return "mining"
	case StateReconnecting:
		return "reconnecting"
	case StateStopping:
		return "stopping"
	default:
		return "connecting"
	}
}

// statsPathOK rejects a path whose directory does not exist, so a typo is a
// start-up error with a clear message rather than a file that silently never
// appears.
func statsPathOK(path string) error {
	dir := filepath.Dir(path)
	if dir == "" {
		return nil
	}
	_, err := os.Stat(dir)
	return err
}
