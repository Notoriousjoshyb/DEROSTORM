package main

// Persistent settings. DeroStorm is meant to be portable, so the config sits
// next to the executable by default and the whole thing is one small JSON file
// a person can read and edit by hand.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const configName = "derostorm.json"

type Config struct {
	Node   string `json:"node"`
	Wallet string `json:"wallet"`
	// CPU mining threads. Zero turns the CPU off for GPU-only mining, for
	// running a separate CPU miner beside this one; it needs a GPU in
	// `gpus`, or nothing mines at all.
	Threads int    `json:"threads"`
	Testnet bool   `json:"testnet"`
	Theme   string `json:"theme"`

	// GPU device indices to mine on. Empty means CPU only; combined with
	// Threads 0 the GPU mines alone. GPUBatch is the nonces per kernel
	// launch; 0 lets the library size it from free VRAM. Larger batches hash
	// a little faster but take longer to notice a new job, so the whole
	// batch is wasted work when one arrives mid-launch.
	GPUs     []int `json:"gpus,omitempty"`
	GPUBatch int   `json:"gpu_batch,omitempty"`

	// GPUBlocks pins the suffix kernel's grid-block count. 0 uses the
	// runtime-derived occupancy/VRAM ceiling.
	GPUBlocks int `json:"gpu_blocks,omitempty"`

	// UI is the console's own remembered preferences. Nothing in it can
	// change how the miner mines, which is why it is a separate object: a
	// person editing this file by hand should be able to see at a glance
	// which half of it matters.
	UI UIPrefs `json:"ui,omitempty"`
}

// UIPrefs is the harmless part of the config: what the console looked like
// when it was last saved.
//
// The two booleans are pointers so that "absent" and "false" are different
// things. Both default to on, and a config written before they existed must
// keep getting the defaults rather than silently turning the animation off.
type UIPrefs struct {
	Screen  string `json:"screen,omitempty"`  // dashboard, mining, stats...
	Range   string `json:"range,omitempty"`   // 5M, 30M, 1H, 6H, 24H
	Refresh int    `json:"refresh,omitempty"` // console frames per second
	Animate *bool  `json:"animation,omitempty"`
	Title   *bool  `json:"title,omitempty"`
}

// On is the value of an optional boolean preference, defaulting to true.
func prefOn(p *bool) bool { return p == nil || *p }

func boolPtr(v bool) *bool { return &v }

// ConfigPath returns where the config lives: beside the executable, falling
// back to the working directory if the executable path cannot be resolved.
func ConfigPath(override string) string {
	if override != "" {
		return override
	}
	if exe, err := os.Executable(); err == nil {
		if dir := filepath.Dir(exe); dir != "" {
			return filepath.Join(dir, configName)
		}
	}
	return configName
}

func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	return &c, nil
}

func (c *Config) Save(path string) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		os.MkdirAll(dir, 0o755)
	}
	return os.WriteFile(path, b, 0o644)
}

// Complete reports whether the config has everything needed to start mining
// without asking the user anything. Zero threads counts as complete when a
// GPU is configured, for GPU-only mining.
func (c *Config) Complete() bool {
	return c != nil && c.Wallet != "" && c.Node != "" &&
		c.Threads >= 0 && (c.Threads > 0 || len(c.GPUs) > 0)
}

// DefaultNode is the node suggested for each network during setup.
func DefaultNode(testnet bool) string {
	if testnet {
		return "127.0.0.1:10100"
	}
	return "minernode1.dero.live:10100"
}

// DefaultThreads leaves one logical CPU free.
//
// The benchmark disagrees with this: on a 9800X3D it measures 16 threads at
// 8,733 H/s against 15 at 8,531, because in a benchmark there is nothing else to
// run. Mining, there is -- the getwork socket, the console redraw, and the
// thread that feeds a GPU, which spends its time blocked in the driver waiting
// for a batch and needs a CPU the moment one lands. Live runs are what this
// default follows, and they put the peak one below the CPU count.
//
// Anyone who wants the last thread can ask: --mining-threads=16, or "threads 16"
// while it runs.
func DefaultThreads() int {
	if n := runtime.GOMAXPROCS(0) - 1; n > 0 {
		return n
	}
	return 1
}
