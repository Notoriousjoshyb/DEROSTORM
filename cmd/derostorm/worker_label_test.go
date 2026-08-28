package main

// The worker label is the only place the live panel says what is mining, and it
// is what the start-up banner deliberately no longer duplicates. A banner is
// printed once and cannot be redrawn, so a thread count there goes stale the
// moment someone types "threads 12"; these cases pin the replacement.

import (
	"strings"
	"testing"
)

func TestWorkerLabel(t *testing.T) {
	cases := []struct {
		threads, gpus int
		want          string
	}{
		{15, 0, "15 CPU threads"},
		{1, 0, "1 CPU thread"},
		{8, 1, "8 CPU · 1 GPU"},
		{15, 2, "15 CPU · 2 GPU"},
		{0, 1, "1 GPU"},
		{0, 3, "3 GPU"},
	}
	for _, c := range cases {
		got := workerLabel(Snapshot{Threads: c.threads, GPUs: c.gpus})
		if got != c.want {
			t.Errorf("threads=%d gpus=%d: got %q, want %q", c.threads, c.gpus, got, c.want)
		}
	}
}

// The frame is now allowed to be taller when a GPU is running, because the
// CPU/GPU split rows only exist then and sharing the sparkline row with them
// meant neither had enough space. What must not happen is the height varying
// with anything else: a height that moved with the hashrate or the device count
// would make the panel breathe on screen for no reason.
func TestFrameHeightVariesOnlyWithGPUPresence(t *testing.T) {
	c := NewConsole(discardWriter{}, themes["mono"], true, 6)

	cpuOnly := len(c.frame(Snapshot{Threads: 8}))
	withGPU := len(c.frame(Snapshot{Threads: 8, GPUs: 1, CPURate: 4000, GPURate: 6800}))

	if withGPU <= cpuOnly {
		t.Fatalf("expected the GPU layout to be taller: %d vs %d", withGPU, cpuOnly)
	}

	for _, s := range []Snapshot{
		{Threads: 1},
		{Threads: 16, Hashrate: 99999, Node: strings.Repeat("host.", 40)},
		{Threads: 0, Difficulty: 1 << 40, NetHashes: 1 << 40},
	} {
		if n := len(c.frame(s)); n != cpuOnly {
			t.Errorf("CPU-only height changed: %d vs %d for %+v", n, cpuOnly, s)
		}
	}
	for _, s := range []Snapshot{
		{Threads: 15, GPUs: 4, CPURate: 7000, GPURate: 20000},
		{Threads: 8, GPUs: 1, GPUTuning: true},
		{Threads: 0, GPUs: 8, GPURate: 0}, // a GPU contributing nothing
	} {
		if n := len(c.frame(s)); n != withGPU {
			t.Errorf("GPU height changed: %d vs %d for %+v", n, withGPU, s)
		}
	}
}

// Because the height may change, Draw has to clean up after a frame that
// shrinks -- a GPU stopping -- or the tail of the taller frame stays on screen
// under the shorter one for the rest of the session. This is the test that
// makes the variable height safe.
func TestDrawWipesRowsWhenTheFrameShrinks(t *testing.T) {
	var buf strings.Builder
	c := NewConsole(&buf, themes["mono"], true, 6)

	tall := Snapshot{Threads: 8, GPUs: 1, CPURate: 4000, GPURate: 6800, ShowInput: true}
	short := Snapshot{Threads: 8, ShowInput: true}

	nTall := len(c.frame(tall))
	nShort := len(c.frame(short))
	if nShort >= nTall {
		t.Fatalf("test needs the CPU-only frame to be shorter: %d vs %d", nShort, nTall)
	}
	drop := nTall - nShort

	c.Draw(tall)
	buf.Reset()
	c.Draw(short)
	out := buf.String()

	// One clear per row of the *taller* frame: the new content, then the rows
	// the old frame owned and this one does not.
	if got := strings.Count(out, "\x1b[2K"); got != nTall {
		t.Errorf("cleared %d rows, want %d (%d content + %d wiped)", got, nTall, nShort, drop)
	}

	// And the cursor must come back up over the wiped rows, or the next frame
	// starts that many lines too low and the panel walks down the screen.
	if want := "\x1b[" + itoa(drop) + "A"; !strings.HasSuffix(out, want) {
		t.Errorf("output does not end by moving the cursor up %d rows\n%q", drop, tailOf(out, 24))
	}

	// Growing again must be clean too: the next frame is written in full and
	// nothing is left over.
	buf.Reset()
	c.Draw(tall)
	if got := strings.Count(buf.String(), "\x1b[2K"); got != nTall {
		t.Errorf("after regrowing, cleared %d rows, want %d", got, nTall)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// The window is sized once, before any output, from bannerRows plus the tallest
// frame. If a row is ever added to the panel without that arithmetic seeing it,
// the panel ends up taller than the window it asked for and walks down the
// screen. This pins the two together.
func TestStartupReservesRoomForTheTallestFrame(t *testing.T) {
	c := NewConsole(discardWriter{}, themes["default"], true, 6)

	const spare = 2
	for _, note := range []string{"", "NO_COLOR is set, using mono"} {
		want := bannerRows(note) + c.FrameHeight() + spare
		// What the banner and the panel actually occupy, with the GPU rows
		// present and the command line drawn.
		need := bannerRows(note) + len(c.frame(Snapshot{ShowInput: true, GPUs: 1}))
		if want < need {
			t.Errorf("themeNote %q: reserving %d rows for %d rows of output", note, want, need)
		}
		if want-need != spare {
			t.Errorf("themeNote %q: %d rows of slack, want %d", note, want-need, spare)
		}
	}
}

// fitTo is what makes the variable-height panel safe in a window that refused to
// grow, or was resized later. It must always either fit the panel or say it
// could not, and it must give rows back when the window grows again.
func TestFitToTrimsGrowsBackAndRefusesTheImpossible(t *testing.T) {
	tall := Snapshot{ShowInput: true, GPUs: 1}
	fit := func(c *Console, cols, rows int) bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.fitTo(cols, rows)
	}

	c := NewConsole(discardWriter{}, themes["default"], true, 6)
	full := len(c.frame(tall))
	const wide = 200 // never the constraint

	// Room to spare: nothing is trimmed.
	if !fit(c, wide, full+10) || c.LogLines() != 6 {
		t.Errorf("with room to spare it trimmed to %d log lines", c.LogLines())
	}

	// One row short: exactly one log line goes, and the frame then fits.
	if !fit(c, wide, full-1) {
		t.Fatal("could not fit in one row less than the full panel")
	}
	if c.LogLines() != 5 {
		t.Errorf("trimmed to %d log lines, want 5", c.LogLines())
	}
	if got := len(c.frame(tall)); got > full-1 {
		t.Errorf("frame is still %d rows, want at most %d", got, full-1)
	}

	// Far too short: stop at the floor and report failure rather than trim the
	// event log away entirely.
	if fit(c, wide, 4) {
		t.Error("claimed to fit a full panel in 4 rows")
	}
	if c.LogLines() != minLogLines {
		t.Errorf("trimmed to %d log lines, want the floor of %d", c.LogLines(), minLogLines)
	}

	// The window growing back must give the rows back.
	if !fit(c, wide, full+10) || c.LogLines() != 6 {
		t.Errorf("after the window grew back, log lines = %d, want 6", c.LogLines())
	}

	// Too narrow is refused however much height there is: wrapping would break
	// the redraw, and no amount of trimming the log fixes a width problem.
	if fit(c, minPanelWidth, 200) {
		t.Errorf("claimed a %d-column panel fits a %d-column window",
			c.Width(), minPanelWidth)
	}

	// And a window one column wider than the panel is accepted.
	if !fit(c, minPanelWidth+2, 200) {
		t.Errorf("refused a window with room for a %d-column panel", c.Width())
	}
}

// Adapt must be safe when the terminal size cannot be read at all, which is the
// case under `go test` and whenever output is redirected.
func TestAdaptLeavesTheLayoutAloneWithNoTerminal(t *testing.T) {
	c := NewConsole(discardWriter{}, themes["default"], true, 6)
	before := len(c.frame(Snapshot{ShowInput: true, GPUs: 1}))
	if !c.Adapt() {
		t.Error("Adapt reported the panel does not fit when the size is unknown")
	}
	if c.LogLines() != 6 {
		t.Errorf("Adapt trimmed to %d log lines with no terminal to measure", c.LogLines())
	}
	if got := len(c.frame(Snapshot{ShowInput: true, GPUs: 1})); got != before {
		t.Errorf("Adapt changed the frame height: %d vs %d", got, before)
	}
}

// panelWidth is what turns a terminal width into a layout width, and the clamp
// at each end is deliberate: too narrow to be readable, or sprawling across an
// ultrawide window, are both worse than a fixed size.
func TestPanelWidthClamps(t *testing.T) {
	cases := []struct{ term, want int }{
		{0, 76},                 // could not be read: a sane default
		{40, minPanelWidth},     // genuinely narrow: the narrowest that works
		{62, minPanelWidth},     // exactly at the floor
		{80, 78},                // honoured
		{maxPanelWidth + 2, 96}, // honoured, at the ceiling
		{200, maxPanelWidth},    // clamped
	}
	for _, c := range cases {
		if got := panelWidth(c.term); got != c.want {
			t.Errorf("panelWidth(%d) = %d, want %d", c.term, got, c.want)
		}
	}
}
