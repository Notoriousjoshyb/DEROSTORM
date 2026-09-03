package main

import (
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/notoriousjoshyb/derostorm/internal/ui"
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;?]*[a-zA-Z]")

func visibleWidth(s string) int {
	return utf8.RuneCountInString(ansi.ReplaceAllString(s, ""))
}

func sampleSnapshot() Snapshot {
	now := time.Date(2026, 8, 28, 10, 4, 12, 0, time.UTC)
	return Snapshot{
		State:      StateMining,
		Hashrate:   7643.7,
		Threads:    16,
		History:    []float64{100, 400, 900, 1500, 3000, 5000, 7000, 7643, 7500, 7600},
		Height:     2481903,
		Difficulty: 11221,
		NetHashes:  10000,
		Blocks:     9,
		MiniBlocks: 89,
		Rejected:   1,
		Uptime:     12*time.Minute + 47*time.Second,
		Node:       "minernode1.dero.live:10100",
		Log: []LogEntry{
			{At: now, Level: LogGood, Tag: "accepted", Text: "miniblock 89 at height 2481903"},
			{At: now, Level: LogInfo, Tag: "job", Text: "height 2481903 · difficulty 11221"},
		},
		Input:     "threads 8",
		ShowInput: true,
	}
}

// TestFrameLinesAreRectangular is the guard against box-drawing drift. Every
// line that belongs to the panel must have the same visible width, counting
// runes and ignoring colour escapes -- a single miscounted pad shows up as a
// ragged right edge on a real terminal.
func TestFrameLinesAreRectangular(t *testing.T) {
	// Both layouts: the CPU/GPU split rows only exist when a GPU is running, so
	// checking one snapshot would leave them unmeasured.
	gpu := sampleSnapshot()
	gpu.GPUs = 1
	gpu.CPURate = 8738
	gpu.GPURate = 6161

	for _, name := range themeNames() {
		th := themes[name]
		c := NewConsole(io.Discard, th, true, 6)
		lines := append(c.frame(sampleSnapshot()), c.frame(gpu)...)

		for i, ln := range lines {
			plain := ansi.ReplaceAllString(ln, "")
			// Only the panel rows are width-constrained; the log and command
			// rows below it are free-length.
			if !strings.HasPrefix(plain, boxV) &&
				!strings.HasPrefix(plain, boxTL) &&
				!strings.HasPrefix(plain, boxTeeL) &&
				!strings.HasPrefix(plain, boxBL) {
				continue
			}
			if got := visibleWidth(ln); got != c.width {
				t.Errorf("theme %s line %d: visible width %d, want %d\n%q", name, i, got, c.width, plain)
			}
		}
	}
}

// TestFrameHeightIsStable matters because the console redraws by moving the
// cursor up by the previous frame's line count. If the height changed between
// frames the panel would walk down the screen.
func TestFrameHeightIsStable(t *testing.T) {
	c := NewConsole(io.Discard, themes["default"], true, 6)

	base := len(c.frame(sampleSnapshot()))

	// Every variant here keeps the configuration -- thread and GPU counts --
	// alone. Those may legitimately change the height (see
	// TestFrameHeightVariesOnlyWithGPUPresence); nothing else may.
	variants := []func(s *Snapshot){
		func(s *Snapshot) { s.Log = nil },
		func(s *Snapshot) { s.Log = make([]LogEntry, 50) },
		func(s *Snapshot) { s.State = StateConnecting },
		func(s *Snapshot) { s.Hashrate = 0 },
		func(s *Snapshot) { s.History = nil },
		func(s *Snapshot) { s.Input = "" },
		func(s *Snapshot) { s.Input = strings.Repeat("x", 400) },
		func(s *Snapshot) { s.Node = strings.Repeat("long.host.name:10100/", 10) },
		func(s *Snapshot) { s.Height = 0; s.Node = "" },
		func(s *Snapshot) { s.Difficulty = 0; s.NetHashes = 0 },
		func(s *Snapshot) { s.Hashrate = 0; s.Difficulty = 1 << 62 },
	}
	for i, mut := range variants {
		s := sampleSnapshot()
		mut(&s)
		if got := len(c.frame(s)); got != base {
			t.Errorf("variant %d changed frame height: %d, want %d", i, got, base)
		}
	}
}

// TestMonoThemeEmitsNoEscapes: piping the miner to a log file must produce
// clean text.
func TestMonoThemeEmitsNoEscapes(t *testing.T) {
	c := NewConsole(io.Discard, themes["mono"], true, 6)
	for _, ln := range c.frame(sampleSnapshot()) {
		if strings.Contains(ln, "\x1b") {
			t.Fatalf("mono theme emitted an escape sequence: %q", ln)
		}
	}
}

func TestPickThemeRespectsEnvironment(t *testing.T) {
	// the developer's own shell may set NO_COLOR; clear it for the colour cases
	// (t.Setenv restores whatever was there when the test finishes).
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")

	if got, _ := PickTheme("default", false); got.Name != "mono" {
		t.Errorf("non-TTY should force mono, got %s", got.Name)
	}
	if got, _ := PickTheme("copper", true); got.Name != "copper" {
		t.Errorf("want copper, got %s", got.Name)
	}
	got, note := PickTheme("nonsense", true)
	if got.Name != ui.DefaultTheme || note == "" {
		t.Errorf("unknown theme should fall back with a note, got %s %q", got.Name, note)
	}

	t.Setenv("NO_COLOR", "1")
	if got, _ := PickTheme("copper", true); got.Name != "mono" {
		t.Errorf("NO_COLOR should force mono, got %s", got.Name)
	}
}

func TestParseThreadArg(t *testing.T) {
	cases := []struct {
		in      string
		current int
		want    int
		wantErr bool
	}{
		{"8", 16, 8, false},
		{"+2", 8, 10, false},
		{"-4", 16, 12, false},
		{"1", 16, 1, false},
		{"0", 16, 0, false},
		{"-20", 16, 0, true},
		{"abc", 16, 0, true},
		{"999", 16, 0, true},
	}
	for _, c := range cases {
		got, err := parseThreadArg(c.in, c.current)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseThreadArg(%q,%d) = %d, want error", c.in, c.current, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("parseThreadArg(%q,%d) = %d,%v want %d", c.in, c.current, got, err, c.want)
		}
	}
}

func TestEditorFeed(t *testing.T) {
	var e Editor
	for _, b := range []byte("threads 8") {
		if _, done := e.Feed(b); done {
			t.Fatal("unexpected completion mid-line")
		}
	}
	if got := e.Buffer(); got != "threads 8" {
		t.Fatalf("buffer = %q", got)
	}
	e.Feed(0x08) // backspace
	if got := e.Buffer(); got != "threads " {
		t.Fatalf("after backspace buffer = %q", got)
	}
	line, done := e.Feed('\r')
	if !done || line != "threads" {
		t.Fatalf("Feed(CR) = %q,%v", line, done)
	}
	if got := e.Buffer(); got != "" {
		t.Fatalf("buffer not cleared: %q", got)
	}
}

func TestSpreadOverCoresCoversEveryCPU(t *testing.T) {
	for _, count := range []int{2, 4, 8, 12, 16} {
		seen := map[int]bool{}
		for slot := 0; slot < count; slot++ {
			cpu := spreadOverCores(slot, count)
			if cpu < 0 || cpu >= count {
				t.Fatalf("count %d slot %d -> cpu %d out of range", count, slot, cpu)
			}
			if seen[cpu] {
				t.Fatalf("count %d: cpu %d assigned twice", count, cpu)
			}
			seen[cpu] = true
		}
		// The first half must land on distinct physical cores (even indices).
		for slot := 0; slot < count/2; slot++ {
			if cpu := spreadOverCores(slot, count); cpu%2 != 0 {
				t.Fatalf("count %d slot %d -> cpu %d, want an even (first-of-core) index", count, slot, cpu)
			}
		}
	}
}

// TestThemeSwitchCannotReintroduceColour: once mono has been forced (piped
// output, NO_COLOR), a runtime "theme" command must not start emitting escapes.
func TestThemeSwitchCannotReintroduceColour(t *testing.T) {
	c := NewConsole(io.Discard, themes["mono"], false, 6)
	if c.SetTheme(themes["default"]) {
		t.Fatal("mono console accepted a colour theme")
	}
	if c.Theme().Name != "mono" {
		t.Fatalf("theme changed to %s", c.Theme().Name)
	}
	for _, ln := range c.frame(sampleSnapshot()) {
		if strings.Contains(ln, "\x1b") {
			t.Fatalf("escape leaked after theme switch: %q", ln)
		}
	}

	// A colour console may switch freely, including back to mono.
	c2 := NewConsole(io.Discard, themes["default"], true, 6)
	if !c2.SetTheme(themes["copper"]) || c2.Theme().Name != "copper" {
		t.Fatal("colour console refused a theme switch")
	}
	if !c2.SetTheme(themes["mono"]) || c2.Theme().Name != "mono" {
		t.Fatal("colour console refused a switch to mono")
	}
}
