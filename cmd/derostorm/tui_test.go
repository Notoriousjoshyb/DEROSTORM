package main

// Tests for the full-screen console.
//
// The console cannot be checked by looking at it, because the thing that goes
// wrong is never on the screen size the author had open. So these tests do the
// looking: they render every screen at every plausible window size, and at a
// few implausible ones, and assert the properties that make a frame correct
// rather than the exact characters in it.
//
// Nothing here starts a miner or opens a terminal. renderFrame draws into a
// plain Canvas, which is the whole reason it is a free function.

import (
	"strings"
	"testing"
	"time"

	"github.com/notoriousjoshyb/derostorm/internal/ui"
)

// allScreens is every screen the console can show.
var allScreens = []screenID{
	screenDashboard, screenMining, screenStats, screenNetwork,
	screenThreads, screenConfig, screenLogs, screenPools, screenHelp,
}

// renderAt draws one frame and returns the canvas.
func renderAt(t *testing.T, cols, rows int, sc screenID, s Snapshot, o frameOpts) *ui.Canvas {
	t.Helper()
	cv := ui.NewCanvas(cols, rows)
	o.screen = sc
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("renderFrame panicked at %dx%d on %s: %v", cols, rows, sc.title(), r)
		}
	}()
	renderFrame(cv, s, ui.Themes["storm"], o)
	return cv
}

// TestEveryScreenDrawsAtEverySize is the test that matters most.
//
// A terminal dashboard breaks by being drawn at a size nobody tried, and the
// failure is almost never a panic -- it is a border one cell out, or a widget
// that wrote past its rectangle. The canvas makes the second impossible, so
// what is left to check is that nothing panics and that every row is exactly
// as wide as the window claims to be.
func TestEveryScreenDrawsAtEverySize(t *testing.T) {
	s := previewSnapshot()
	sizes := [][2]int{
		{20, 5}, {30, 8}, {40, 10}, {55, 13}, {56, 14}, {60, 16},
		{72, 20}, {80, 24}, {96, 30}, {100, 30}, {110, 28}, {120, 40},
		{132, 43}, {160, 48}, {170, 48}, {200, 60}, {240, 70}, {400, 100},
	}
	for _, sz := range sizes {
		for _, sc := range allScreens {
			cv := renderAt(t, sz[0], sz[1], sc, s, frameOpts{frame: 7, anim: true})
			for i, line := range cv.Lines(false) {
				if w := ui.Width(line); w > sz[0] {
					t.Fatalf("%dx%d %s: row %d is %d cells wide, window is %d",
						sz[0], sz[1], sc.title(), i, w, sz[0])
				}
			}
		}
	}
}

// TestTinyWindowSaysSoRatherThanDrawing checks the floor. Below the minimum
// the console must say what is wrong, not draw a broken dashboard: a frame
// with its borders overlapping its values looks like the program is failing
// rather than like the window being small.
func TestTinyWindowSaysSoRatherThanDrawing(t *testing.T) {
	s := previewSnapshot()
	for _, sz := range [][2]int{{20, 6}, {40, 10}, {55, 13}} {
		cv := renderAt(t, sz[0], sz[1], screenDashboard, s, frameOpts{})
		text := strings.Join(cv.Lines(false), "\n")
		if !strings.Contains(text, "mining continues") {
			t.Errorf("%dx%d: expected the too-small notice, got:\n%s", sz[0], sz[1], text)
		}
	}
	// And one cell above the floor it must draw the real thing.
	cv := renderAt(t, minCols, minRows, screenDashboard, s, frameOpts{})
	if strings.Contains(strings.Join(cv.Lines(false), "\n"), "mining continues") {
		t.Errorf("%dx%d is the stated minimum but still showed the too-small notice", minCols, minRows)
	}
}

// TestPanelsNeverOverlap checks the grid directly. Two panels sharing a cell
// is the one layout fault the canvas cannot catch, because the second write
// simply wins and the result looks like a rendering glitch rather than like a
// layout bug.
func TestPanelsNeverOverlap(t *testing.T) {
	bands := []bandDef{
		{minH: 8, grow: 3, panels: []panelDef{
			{minW: 32, weight: 30, draw: func(*ui.Canvas, ui.Rect) {}},
			{minW: 30, weight: 34, draw: func(*ui.Canvas, ui.Rect) {}},
			{minW: 32, weight: 36, draw: func(*ui.Canvas, ui.Rect) {}},
		}},
		{minH: 8, grow: 4, panels: []panelDef{
			{minW: 28, weight: 30, draw: func(*ui.Canvas, ui.Rect) {}},
			{minW: 28, weight: 34, draw: func(*ui.Canvas, ui.Rect) {}},
			{minW: 30, weight: 36, draw: func(*ui.Canvas, ui.Rect) {}},
		}},
	}
	for w := 40; w <= 240; w += 7 {
		for h := 6; h <= 60; h += 3 {
			body := ui.Rect{X: 1, Y: 2, W: w, H: h}
			seen := map[[2]int]bool{}
			for _, p := range planBands(body, bands) {
				if p.r.X < body.X || p.r.Y < body.Y ||
					p.r.Right() > body.Right() || p.r.Bottom() > body.Bottom() {
					t.Fatalf("%dx%d: panel %+v escapes the body %+v", w, h, p.r, body)
				}
				for y := p.r.Y; y < p.r.Bottom(); y++ {
					for x := p.r.X; x < p.r.Right(); x++ {
						if seen[[2]int{x, y}] {
							t.Fatalf("%dx%d: two panels both claim cell (%d,%d)", w, h, x, y)
						}
						seen[[2]int{x, y}] = true
					}
				}
			}
		}
	}
}

// TestRowsAlwaysReachTheRightEdge is the ragged-margin check: a row of panels
// must add up to the width it was given, or the dashboard grows a right edge
// that moves as the window is resized.
func TestRowsAlwaysReachTheRightEdge(t *testing.T) {
	panels := []panelDef{
		{minW: 26, weight: 26}, {minW: 26, weight: 27},
		{minW: 36, weight: 47}, {minW: 16, weight: 15},
	}
	for w := 20; w <= 300; w++ {
		strip := ui.Rect{X: 3, Y: 0, W: w, H: 5}
		out := splitRow(strip, panels)
		total, x := 0, strip.X
		for _, r := range out {
			if r.X != x {
				t.Fatalf("w=%d: column starts at %d, expected %d", w, r.X, x)
			}
			x += r.W
			total += r.W
		}
		if total != w {
			t.Fatalf("w=%d: columns add up to %d", w, total)
		}
	}
}

// TestRapidResizeKeepsTheScreenConsistent drives the diffing screen through a
// stream of sizes, which is what dragging a window corner actually produces.
func TestRapidResizeKeepsTheScreenConsistent(t *testing.T) {
	var out strings.Builder
	scr := ui.NewScreen(&out, 100, 30, true)
	scr.SetBG(ui.Themes["storm"].PanelBG)
	s := previewSnapshot()

	sizes := []int{100, 80, 240, 41, 300, 57, 12, 160, 1, 200}
	for i, w := range sizes {
		h := 4 + (i*7)%60
		scr.Resize(w, h)
		cv := scr.Buf()
		if cv.W != w || cv.H != h {
			t.Fatalf("resize to %dx%d gave %dx%d", w, h, cv.W, cv.H)
		}
		renderFrame(cv, s, ui.Themes["storm"], frameOpts{screen: allScreens[i%len(allScreens)], frame: i, anim: true})
		scr.Flush()
	}
	if out.Len() == 0 {
		t.Fatal("ten frames produced no output at all")
	}
}

// TestZeroAndExtremeDataDoNotBreakAFrame covers the states a real run passes
// through that a sample snapshot never does: nothing yet, and far too much.
func TestZeroAndExtremeDataDoNotBreakAFrame(t *testing.T) {
	cases := map[string]Snapshot{
		"zero":       {},
		"connecting": {State: StateConnecting, Node: "node:10100"},
		"cpu only":   {State: StateMining, Threads: 8, CPURate: 4200, Hashrate: 4200},
		"gpu only": {State: StateMining, Threads: 0, GPUs: 1, GPURate: 90000, Hashrate: 90000,
			Devices: []DeviceStat{{Label: "CPU off", TempC: tempUnknown}, {Label: "GPU 0", Rate: 90000, IsGPU: true, TempC: 61}}},
		"six gpus":   sixGPUSnapshot(),
		"huge":       {State: StateMining, Threads: 256, Hashrate: 9.4e12, CPURate: 4e12, GPURate: 5.4e12, TotalHashes: 1 << 62, MiniBlocks: 1 << 40},
		"disconnect": {State: StateReconnecting, Node: strings.Repeat("very-long-node-name.", 12) + ":10100"},
	}
	for name, s := range cases {
		for _, sz := range [][2]int{{60, 16}, {100, 30}, {170, 48}} {
			for _, sc := range allScreens {
				renderAt(t, sz[0], sz[1], sc, s, frameOpts{frame: 3, anim: true, gpuSel: 4})
			}
		}
		_ = name
	}
}

func sixGPUSnapshot() Snapshot {
	s := Snapshot{State: StateMining, Threads: 64, GPUs: 6, Hashrate: 1.2e6, GPURate: 1.1e6, CPURate: 1e5}
	s.Devices = append(s.Devices, DeviceStat{Label: "CPU", Rate: 1e5, TempC: 71})
	for i := 0; i < 6; i++ {
		s.Devices = append(s.Devices, DeviceStat{
			Label: "GPU " + string(rune('0'+i)), Rate: 1.1e6 / 6, TempC: 60 + float64(i), IsGPU: true, Note: "215W",
		})
	}
	s.ThreadRates = make([]float64, 64)
	for i := range s.ThreadRates {
		s.ThreadRates[i] = 1500 + float64(i%7)*40
	}
	return s
}

// ---------------------------------------------------------------- input

// TestNavigationKeys walks the keyboard the way a user does and checks the
// console ends up where they asked.
func TestNavigationKeys(t *testing.T) {
	tui := newTestTUI()

	for _, n := range navItems {
		tui.Key(keyEvent{code: keyRune, r: n.key})
		if got := tui.Screen(); got != n.id {
			t.Errorf("%c: went to %s, expected %s", n.key, got.title(), n.id.title())
		}
		// The same key again is the way back.
		tui.Key(keyEvent{code: keyRune, r: n.key})
		if got := tui.Screen(); got != screenDashboard {
			t.Errorf("%c twice: ended on %s, expected the dashboard", n.key, got.title())
		}
	}

	// Tab forwards all the way round and back to where it started.
	start := tui.Screen()
	for i := 0; i <= int(screenHelp); i++ {
		tui.Key(keyEvent{code: keyTab})
	}
	if tui.Screen() != start {
		t.Errorf("a full cycle of Tab ended on %s, expected %s", tui.Screen().title(), start.title())
	}
	tui.Key(keyEvent{code: keyTab})
	tui.Key(keyEvent{code: keyBackTab})
	if tui.Screen() != start {
		t.Errorf("Tab then Shift-Tab ended on %s, expected %s", tui.Screen().title(), start.title())
	}

	// Escape always comes home.
	tui.Key(keyEvent{code: keyRune, r: 'L'})
	tui.Key(keyEvent{code: keyEsc})
	if tui.Screen() != screenDashboard {
		t.Errorf("Esc ended on %s", tui.Screen().title())
	}

	if _, quit := tui.Key(keyEvent{code: keyRune, r: 'q'}); !quit {
		t.Error("q did not ask to quit")
	}
	if _, quit := tui.Key(keyEvent{code: keyCtrlC}); !quit {
		t.Error("Ctrl-C did not ask to quit")
	}
}

// TestFocusIsNotAKeyPress is the bug this could easily have been. Focus
// reports arrive while the user is typing in a different window, and letting
// them through the key switch would mean alt-tabbing away scrolled the log.
func TestFocusIsNotAKeyPress(t *testing.T) {
	tui := newTestTUI()
	tui.Key(keyEvent{code: keyRune, r: 'N'})
	before := tui.Screen()
	tui.log.scroll = 4

	tui.Key(keyEvent{code: keyFocusOut})
	if tui.focused {
		t.Error("a focus-out did not clear the focus flag")
	}
	tui.Key(keyEvent{code: keyFocusIn})
	if !tui.focused {
		t.Error("a focus-in did not restore the focus flag")
	}
	if tui.Screen() != before || tui.log.scroll != 4 {
		t.Errorf("focus events changed the view: screen %s, scroll %d",
			tui.Screen().title(), tui.log.scroll)
	}
}

// TestUnfocusedStopsTheAnimation checks the gate: there is no point spending
// frames turning a globe in a window nobody is looking at.
func TestUnfocusedStopsTheAnimation(t *testing.T) {
	tui := newTestTUI()
	if !tui.focused {
		t.Fatal("a console must assume it is focused until told otherwise")
	}
	tui.Key(keyEvent{code: keyFocusOut})
	if tui.anim && tui.focused {
		t.Error("the animation gate is still open with the window unfocused")
	}
}

// TestScrollingPausesTheFollow is the property that makes the log usable: a
// reader who scrolls back must not have the line they are reading pushed off
// the top by the next event.
func TestScrollingPausesTheFollow(t *testing.T) {
	tui := newTestTUI()
	if tui.log.paused {
		t.Fatal("a fresh console should be following the log")
	}
	tui.Key(keyEvent{code: keyPgUp})
	if !tui.log.paused || tui.log.scroll != 10 {
		t.Fatalf("PgUp gave scroll=%d paused=%v", tui.log.scroll, tui.log.paused)
	}
	tui.Key(keyEvent{code: keyEnd})
	if tui.log.paused || tui.log.scroll != 0 {
		t.Fatalf("End gave scroll=%d paused=%v", tui.log.scroll, tui.log.paused)
	}
	// Scrolling down past the newest line must not go negative.
	for i := 0; i < 5; i++ {
		tui.Key(keyEvent{code: keyDown})
	}
	if tui.log.scroll != 0 {
		t.Fatalf("scrolled past live to %d", tui.log.scroll)
	}
	// Space is a pause on its own.
	tui.Key(keyEvent{code: keyRune, r: ' '})
	if !tui.log.paused {
		t.Error("Space did not pause the log")
	}
}

// TestHistoryRangeCyclesBothWays checks the chart window wraps rather than
// sticking at either end.
func TestHistoryRangeCyclesBothWays(t *testing.T) {
	tui := newTestTUI()
	for i := 0; i < len(histRanges); i++ {
		tui.Key(keyEvent{code: keyRight})
	}
	if tui.histRange != 0 {
		t.Errorf("a full cycle forwards ended at %d", tui.histRange)
	}
	tui.Key(keyEvent{code: keyLeft})
	if tui.histRange != len(histRanges)-1 {
		t.Errorf("stepping back from the first window gave %d, expected the last", tui.histRange)
	}
	if !tui.SetRange("1H") {
		t.Error("SetRange(1H) was refused")
	}
	if histRanges[tui.histRange].label != "1H" {
		t.Errorf("SetRange(1H) selected %s", histRanges[tui.histRange].label)
	}
	if tui.SetRange("nope") {
		t.Error("SetRange accepted a window that does not exist")
	}
}

// TestEveryHistoryRangeRendersWithAndWithoutData is the case that used to be
// found only by leaving a miner running for a day: the 24-hour window on a
// run that is four minutes old.
func TestEveryHistoryRangeRendersWithAndWithoutData(t *testing.T) {
	full := previewSnapshot()
	empty := Snapshot{State: StateMining, Threads: 4}
	for i := range histRanges {
		for _, s := range []Snapshot{full, empty} {
			renderAt(t, 170, 48, screenDashboard, s, frameOpts{histRange: i, anim: true})
			renderAt(t, 60, 16, screenDashboard, s, frameOpts{histRange: i})
		}
	}
}

// ---------------------------------------------------------------- log view

func TestLogFilter(t *testing.T) {
	now := time.Now()
	log := []LogEntry{
		{At: now.Add(-4 * time.Minute), Level: LogInfo, Tag: "start", Text: "mining with 15 threads"},
		{At: now.Add(-3 * time.Minute), Level: LogGood, Tag: "connect", Text: "connected to node"},
		{At: now.Add(-2 * time.Minute), Level: LogWarn, Tag: "sensors", Text: "no CPU temperature"},
		{At: now.Add(-1 * time.Minute), Level: LogError, Tag: "gpu", Text: "device 0 fell over"},
	}

	if got := (logView{}).apply(nil, log); len(got) != 4 {
		t.Errorf("no filter kept %d of 4", len(got))
	}
	if got := (logView{min: LogWarn}).apply(nil, log); len(got) != 2 {
		t.Errorf("warn filter kept %d, expected 2", len(got))
	}
	if got := (logView{text: "NODE"}).apply(nil, log); len(got) != 1 {
		t.Errorf("text filter is not case-insensitive: kept %d, expected 1", len(got))
	}
	// The tag counts as well as the message, which is how "filter gpu" finds
	// a line whose text does not contain the word.
	if got := (logView{text: "gpu"}).apply(nil, log); len(got) != 1 {
		t.Errorf("tag filter kept %d, expected 1", len(got))
	}
	cleared := logView{clearedAt: now.Add(-90 * time.Second)}
	if got := cleared.apply(nil, log); len(got) != 1 {
		t.Errorf("clear kept %d, expected only the newest", len(got))
	}
	// A filter must never mutate the slice it was handed: it is the run
	// loop's ring, shared with the log file and the stats writer.
	before := append([]LogEntry(nil), log...)
	(logView{min: LogError}).apply(make([]LogEntry, 0, 8), log)
	for i := range log {
		if log[i] != before[i] {
			t.Fatalf("apply modified the source log at %d", i)
		}
	}
}

func TestLogFilterNames(t *testing.T) {
	for _, c := range []struct {
		v    logView
		want string
	}{
		{logView{}, "ALL"},
		{logView{min: LogGood}, "SHARES+"},
		{logView{min: LogWarn}, "WARN+"},
		{logView{min: LogError}, "ERRORS"},
		{logView{text: "gpu"}, `"gpu"`},
	} {
		if got := c.v.filterName(); got != c.want {
			t.Errorf("filterName() = %q, want %q", got, c.want)
		}
	}
}

// TestFilterCycleReturnsToAll checks F is a cycle and not a one-way door.
func TestFilterCycleReturnsToAll(t *testing.T) {
	tui := newTestTUI()
	seen := map[LogLevel]bool{}
	for i := 0; i < 4; i++ {
		seen[tui.log.min] = true
		tui.Key(keyEvent{code: keyRune, r: 'f'})
	}
	if len(seen) != 4 {
		t.Errorf("F cycled through %d levels, expected 4", len(seen))
	}
	if tui.log.min != LogInfo {
		t.Errorf("four presses of F ended at %v, expected back at ALL", tui.log.min)
	}
}

// TestAVeryLongLogDrawsAndScrolls is the endurance case: a console that has
// been up for days is holding a full ring, and someone scrolls to the top of
// it while lines are still arriving.
func TestAVeryLongLogDrawsAndScrolls(t *testing.T) {
	now := time.Now()
	log := make([]LogEntry, 4000)
	for i := range log {
		log[i] = LogEntry{
			At:    now.Add(time.Duration(i-4000) * time.Second),
			Level: LogLevel(i % 4),
			Tag:   strings.Repeat("tag", 1+i%6),
			Text:  strings.Repeat("a very long message that will not fit on any row ", 1+i%4),
		}
	}
	s := previewSnapshot()
	s.Log = log

	for _, scroll := range []int{0, 1, 37, 3999, 4000, 1 << 20} {
		for _, sz := range [][2]int{{60, 16}, {100, 30}, {170, 48}} {
			cv := renderAt(t, sz[0], sz[1], screenLogs, s,
				frameOpts{log: logView{scroll: scroll}})
			for i, line := range cv.Lines(false) {
				if w := ui.Width(line); w > sz[0] {
					t.Fatalf("scroll=%d %dx%d: row %d is %d cells wide", scroll, sz[0], sz[1], i, w)
				}
			}
		}
	}
	// And with a filter that matches nothing, which is the case that used to
	// index an empty slice.
	renderAt(t, 170, 48, screenLogs, s, frameOpts{log: logView{text: "no such text anywhere"}})
	renderAt(t, 170, 48, screenDashboard, s, frameOpts{log: logView{text: "no such text anywhere", scroll: 99}})
}

// ---------------------------------------------------------------- title

func TestWindowTitle(t *testing.T) {
	for _, c := range []struct {
		s    Snapshot
		want string
	}{
		{Snapshot{State: StateMining, Hashrate: 248680}, "DEROSTORM | 248.68 KH/s | MINING"},
		{Snapshot{State: StateConnecting}, "DEROSTORM | CONNECTING"},
		{Snapshot{State: StateReconnecting, Hashrate: 1200}, "DEROSTORM | 1.20 KH/s | RECONNECTING"},
	} {
		if got := titleFor(c.s); got != c.want {
			t.Errorf("titleFor = %q, want %q", got, c.want)
		}
	}

	// A control character in a title would end the escape sequence early and
	// leave the rest of it printed on the screen.
	var out strings.Builder
	setTitle(&out, "safe title")
	if out.String() != "\x1b]2;safe title\x07" {
		t.Errorf("setTitle wrote %q", out.String())
	}
	out.Reset()
	setTitle(&out, "bad\x07title")
	if out.Len() != 0 {
		t.Errorf("setTitle emitted a title containing a control character: %q", out.String())
	}
}

// ---------------------------------------------------------------- animation

// TestAnimationOffFreezesEveryMovingPart renders the same snapshot at two
// different frame numbers with the animation off. If any widget is still
// reading the real frame counter, the two frames differ.
func TestAnimationOffFreezesEveryMovingPart(t *testing.T) {
	s := previewSnapshot()
	a := renderAt(t, 170, 48, screenDashboard, s, frameOpts{frame: 1, anim: false})
	b := renderAt(t, 170, 48, screenDashboard, s, frameOpts{frame: 999, anim: false})
	la, lb := a.Lines(false), b.Lines(false)
	for i := range la {
		if la[i] != lb[i] {
			t.Fatalf("with animation off, frame 1 and frame 999 differ on row %d:\n%q\n%q", i, la[i], lb[i])
		}
	}

	// And with it on, something must actually move -- otherwise the flag is
	// switching off nothing at all.
	c := renderAt(t, 170, 48, screenDashboard, s, frameOpts{frame: 1, anim: true})
	d := renderAt(t, 170, 48, screenDashboard, s, frameOpts{frame: 40, anim: true})
	if strings.Join(c.Lines(false), "\n") == strings.Join(d.Lines(false), "\n") {
		t.Error("with animation on, two distant frames were identical")
	}
}

// ---------------------------------------------------------------- splash

func TestSplashDrawsAtEverySize(t *testing.T) {
	th := ui.Themes["storm"]
	for _, sz := range [][2]int{{20, 5}, {40, 12}, {80, 24}, {170, 48}, {300, 80}} {
		cv := ui.NewCanvas(sz[0], sz[1])
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("drawSplash panicked at %dx%d: %v", sz[0], sz[1], r)
				}
			}()
			drawSplash(cv, th, map[string]bool{"NODE": true}, map[string]bool{"GPU": true})
		}()
		for i, line := range cv.Lines(false) {
			if w := ui.Width(line); w > sz[0] {
				t.Fatalf("splash %dx%d: row %d is %d cells wide", sz[0], sz[1], i, w)
			}
		}
	}
}

// ---------------------------------------------------------------- prefs

func TestScreenNamesRoundTrip(t *testing.T) {
	for _, sc := range allScreens {
		if got := screenByName(sc.name()); got != sc {
			t.Errorf("%s: name %q parsed back as %s", sc.title(), sc.name(), got.title())
		}
	}
	// Anything unknown is the dashboard rather than an error: a config file
	// written by a newer version must not stop an older one from starting.
	if screenByName("no such screen") != screenDashboard {
		t.Error("an unknown screen name did not fall back to the dashboard")
	}
}

func TestPrefsSurviveAConstructor(t *testing.T) {
	tui := NewTUI(nil, ui.Themes["storm"], 100, 30, TUIOpts{
		Anim: false, Screen: "network", HistRange: "6H",
	})
	screen, rng, anim := tui.Prefs()
	if screen != "network" || rng != "6H" || anim {
		t.Errorf("Prefs() = %q, %q, %v", screen, rng, anim)
	}
}

func TestRefreshIntervalIsClamped(t *testing.T) {
	if got := refreshInterval(defaultRefreshFPS); got != 125*time.Millisecond {
		t.Errorf("default refresh is %v", got)
	}
	if got := refreshInterval(0); got != time.Second/time.Duration(minRefreshFPS) {
		t.Errorf("refreshInterval(0) = %v, expected the floor", got)
	}
	if got := refreshInterval(10000); got != time.Second/time.Duration(maxRefreshFPS) {
		t.Errorf("refreshInterval(10000) = %v, expected the ceiling", got)
	}
}

// ---------------------------------------------------------------- keys

func TestDecodeKeys(t *testing.T) {
	cases := []struct {
		in   string
		want []keyEvent
	}{
		{"\x1b[A", []keyEvent{{code: keyUp}}},
		{"\x1b[B", []keyEvent{{code: keyDown}}},
		{"\x1b[Z", []keyEvent{{code: keyBackTab}}},
		{"\x1b[5~", []keyEvent{{code: keyPgUp}}},
		{"\x1b[6~", []keyEvent{{code: keyPgDn}}},
		{"\t", []keyEvent{{code: keyTab}}},
		{"\x03", []keyEvent{{code: keyCtrlC}}},
		{"\x1b", []keyEvent{{code: keyEsc}}},
		{"qM", []keyEvent{{code: keyRune, r: 'q'}, {code: keyRune, r: 'M'}}},
		// An escape sequence arrives in one read, which is the whole reason
		// the decoder takes a chunk: split byte by byte it is indistinguishable
		// from someone pressing Escape and then typing.
		{"\x1b[Aq", []keyEvent{{code: keyUp}, {code: keyRune, r: 'q'}}},
		// Focus reports arrive on stdin mixed in with the key presses.
		{"\x1b[I", []keyEvent{{code: keyFocusIn}}},
		{"\x1b[O", []keyEvent{{code: keyFocusOut}}},
	}
	for _, c := range cases {
		got := decodeKeys([]byte(c.in))
		if len(got) != len(c.want) {
			t.Errorf("decodeKeys(%q) gave %d events, want %d (%v)", c.in, len(got), len(c.want), got)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("decodeKeys(%q)[%d] = %v, want %v", c.in, i, got[i], c.want[i])
			}
		}
	}
}

// newTestTUI is a console with no terminal behind it. Every method used by
// these tests either draws into a canvas or changes a field, so nothing here
// needs a real one.
func newTestTUI() *TUI {
	return NewTUI(nil, ui.Themes["storm"], 120, 40, TUIOpts{Anim: true})
}

// ---------------------------------------------------------------- cost

// BenchmarkRenderFrame is the console's whole CPU cost, per frame.
//
// This is the number the performance budget is judged against: multiply the
// nanoseconds by the frame rate to get the fraction of one core the console
// uses. At the default eight frames a second anything under a millisecond a
// frame is under one per cent of one core, which on a machine with sixteen
// hashing threads is under a sixteenth of one per cent of the mining capacity.
func BenchmarkRenderFrame(b *testing.B) {
	s := previewSnapshot()
	th := ui.Themes["storm"]
	cv := ui.NewCanvas(170, 48)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cv.Clear()
		renderFrame(cv, s, th, frameOpts{screen: screenDashboard, frame: i, anim: true})
	}
}

// BenchmarkRenderFrameStill is the same frame drawn twice with nothing
// changing, through the diffing screen: what a console actually costs while a
// miner sits at a steady hashrate.
func BenchmarkRenderFrameStill(b *testing.B) {
	s := previewSnapshot()
	th := ui.Themes["storm"]
	scr := ui.NewScreen(discardWriter{}, 170, 48, true)
	scr.SetBG(th.PanelBG)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		renderFrame(scr.Buf(), s, th, frameOpts{screen: screenDashboard, frame: 7, anim: false})
		scr.Flush()
	}
}
