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
	Node    string `json:"node"`
	Wallet  string `json:"wallet"`
	Threads int    `json:"threads"`
	Testnet bool   `json:"testnet"`
	Theme   string `json:"theme"`

	// CUDA device indices to mine on beside the CPU. Empty means CPU only.
	// GPUBatch is the nonces per kernel launch; 0 lets the library size it from
	// free VRAM. Larger batches hash a little faster but take longer to notice
	// a new job, so the whole batch is wasted work when one arrives mid-launch.
	GPUs     []int `json:"gpus,omitempty"`
	GPUBatch int   `json:"gpu_batch,omitempty"`

	// GPUBlocks pins the suffix kernel's resident block count. 0 means measure
	// it: the miner sweeps a few multiples of the SM count over its first
	// batches and keeps the fastest. Those batches are real mining work, so the
	// sweep costs nothing but the difference between the settings it tries.
	//
	// Worth pinning once a machine is known: it saves the sweep on every start,
	// and the console prints the value the sweep chose so it can be copied here.
	GPUBlocks int `json:"gpu_blocks,omitempty"`
}

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
// without asking the user anything.
func (c *Config) Complete() bool {
	return c != nil && c.Wallet != "" && c.Node != "" && c.Threads > 0
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
