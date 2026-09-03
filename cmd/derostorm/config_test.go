package main

// Zero threads is GPU-only mode: complete when a card is named, incomplete
// when nothing would mine.

import "testing"

func TestConfigCompleteGPUOnly(t *testing.T) {
	cases := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{"cpu only", &Config{Wallet: "w", Node: "n:1", Threads: 4}, true},
		{"cpu and gpu", &Config{Wallet: "w", Node: "n:1", Threads: 4, GPUs: []int{0}}, true},
		{"gpu only", &Config{Wallet: "w", Node: "n:1", Threads: 0, GPUs: []int{0}}, true},
		{"nothing to mine with", &Config{Wallet: "w", Node: "n:1", Threads: 0}, false},
		{"negative threads", &Config{Wallet: "w", Node: "n:1", Threads: -1, GPUs: []int{0}}, false},
		{"no wallet", &Config{Node: "n:1", Threads: 4}, false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := c.cfg.Complete(); got != c.want {
			t.Errorf("%s: Complete() = %v, want %v", c.name, got, c.want)
		}
	}
}
