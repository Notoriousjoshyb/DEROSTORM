package ui

// Tests for the widget package: colour, measurement, the cell grid and the
// diffing renderer.
//
// The colour tests are the reason this file exists. A theme is authored once
// in 24-bit and then rendered three other ways, and the failure mode of that
// arrangement is not a crash -- it is a dashboard whose labels are invisible on
// somebody else's terminal, which nobody reports because they assume it is
// meant to look like that.

import (
	"os"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------- colour

func TestEveryThemeBuildsAtEveryDepth(t *testing.T) {
	defer SetDepth(DetectDepth())

	for _, d := range []Depth{DepthTrue, Depth256, Depth16, DepthNone} {
		SetDepth(d)
		for _, name := range ThemeNames() {
			th := Themes[name]
			if th == nil {
				t.Fatalf("%s at %v: theme is missing", name, d)
			}
			if name == "mono" {
				if !th.Mono() {
					t.Errorf("the mono theme reported colour at %v", d)
				}
				continue
			}
			tokens := map[string]string{
				"Accent": th.Accent, "Accent2": th.Accent2, "Accent3": th.Accent3,
				"Text": th.Text, "Muted": th.Muted, "Dim": th.Dim,
				"Border": th.Border, "Good": th.Good, "Warn": th.Warn,
				"Err": th.Err, "Glow": th.Glow, "Track": th.Track, "Fill": th.Fill,
			}
			for tok, esc := range tokens {
				switch d {
				case DepthNone:
					if esc != "" {
						t.Errorf("%s.%s at depth none is %q, expected nothing", name, tok, esc)
					}
				default:
					if !strings.HasPrefix(esc, "\x1b[") || !strings.HasSuffix(esc, "m") {
						t.Errorf("%s.%s at %v is %q, which is not an SGR sequence", name, tok, d, esc)
					}
				}
			}
			// The ramp must be usable at every depth, including the one where
			// every stop collapses onto the same colour.
			for _, f := range []float64{-1, 0, 0.5, 1, 2} {
				if got := th.Ramp(f); d != DepthNone && got == "" {
					t.Errorf("%s.Ramp(%v) at %v returned nothing", name, f, d)
				}
			}
		}
	}
}

func TestDepthSpellings(t *testing.T) {
	c := RGB{0x35, 0xE6, 0xF2}
	if got, want := c.FG(DepthTrue), "\x1b[38;2;53;230;242m"; got != want {
		t.Errorf("truecolor = %q, want %q", got, want)
	}
	if got := c.FG(Depth256); !strings.HasPrefix(got, "\x1b[38;5;") {
		t.Errorf("256 = %q", got)
	}
	if got := c.FG(Depth16); got != "\x1b[96m" {
		t.Errorf("bright cyan came out as %q", got)
	}
	if got := c.FG(DepthNone); got != "" {
		t.Errorf("depth none = %q", got)
	}
	if got := c.BG(Depth16); got != "\x1b[106m" {
		t.Errorf("bright cyan background came out as %q", got)
	}
}

// TestSixteenColourKeepsEveryTokenVisible is the readability check. At sixteen
// colours the risk is not that a colour is wrong, it is that two tokens
// collapse onto black and vanish against the background.
func TestSixteenColourKeepsEveryTokenVisible(t *testing.T) {
	for name, p := range palettes {
		for tok, c := range map[string]RGB{
			"Accent": p.Accent, "Accent2": p.Accent2, "Text": p.Text,
			"Muted": p.Muted, "Good": p.Good, "Warn": p.Warn, "Err": p.Err,
			"Border": p.Border, "Dim": p.Dim,
		} {
			idx, bright := c.ansi16()
			if idx == 0 && !bright {
				t.Errorf("%s.%s falls to plain black at sixteen colours, which is invisible", name, tok)
			}
			if idx < 0 || idx > 7 {
				t.Errorf("%s.%s gave ANSI index %d", name, tok, idx)
			}
		}
	}
}

// TestGreysDoNotPickUpATint checks the separate grey ramp in the 256-colour
// mapping: without it a near-grey label lands on a visibly coloured cube entry.
func TestGreysDoNotPickUpATint(t *testing.T) {
	for _, v := range []uint8{0x20, 0x50, 0x80, 0xB0, 0xE0} {
		got := RGB{v, v, v}.xterm256()
		if got >= 232 {
			continue // the 24-step grey ramp
		}
		// The cube has a grey diagonal of its own, and for several of these
		// values it is the closer match. What must never happen is landing on
		// a cube entry whose channels differ, because that is a visibly
		// tinted grey.
		i := got - 16
		if r, g, b := i/36, (i/6)%6, i%6; r != g || g != b {
			t.Errorf("grey %#02x mapped to cube entry %d (%d,%d,%d), which is tinted", v, got, r, g, b)
		}
	}
}

func TestPickThemeFallsBack(t *testing.T) {
	// The depth is detected once, at package init, from whatever environment
	// the test binary started in. A shell with NO_COLOR set therefore freezes
	// it at DepthNone, and PickTheme sends every colour theme to mono for a
	// reason that has nothing to do with what is being tested here. Clearing
	// the variable is not enough on its own; the depth has to be set as well.
	saved := CurrentDepth()
	SetDepth(DepthTrue)
	t.Cleanup(func() { SetDepth(saved) })
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")

	if th, note := PickTheme("no such theme", true); th.Name != DefaultTheme || note == "" {
		t.Errorf("unknown theme gave %s, note %q", th.Name, note)
	}
	if th, note := PickTheme("storm", false); th.Name != "mono" || note == "" {
		t.Errorf("a non-terminal gave %s, note %q", th.Name, note)
	}
	if th, _ := PickTheme("", true); th.Name != DefaultTheme {
		t.Errorf("the empty name gave %s", th.Name)
	}
	t.Setenv("NO_COLOR", "1")
	if th, note := PickTheme("storm", true); th.Name != "mono" || note == "" {
		t.Errorf("NO_COLOR gave %s, note %q", th.Name, note)
	}
}

func TestMix(t *testing.T) {
	a, b := RGB{0, 0, 0}, RGB{100, 200, 255}
	if got := a.Mix(b, 0); got != a {
		t.Errorf("Mix at 0 = %v", got)
	}
	if got := a.Mix(b, 1); got != b {
		t.Errorf("Mix at 1 = %v", got)
	}
	if got := a.Mix(b, 0.5); got != (RGB{50, 100, 128}) {
		t.Errorf("Mix at 0.5 = %v", got)
	}
}

// ---------------------------------------------------------------- text

func TestWidthAndClip(t *testing.T) {
	cases := []struct {
		s string
		w int
	}{
		{"", 0}, {"abc", 3}, {"日本語", 6}, {"é", 1},
		{"█▀▀█", 4}, {"⣿", 1}, {"\x00abc", 3},
	}
	for _, c := range cases {
		if got := Width(c.s); got != c.w {
			t.Errorf("Width(%q) = %d, want %d", c.s, got, c.w)
		}
	}

	// Clip never produces something wider than it was asked for, and never
	// cuts a wide rune in half.
	for _, s := range []string{"abcdefgh", "日本語テスト", "mixed 日本 text"} {
		for w := 0; w <= Width(s)+3; w++ {
			got := Clip(s, w)
			if Width(got) > w {
				t.Errorf("Clip(%q, %d) = %q, which is %d cells", s, w, got, Width(got))
			}
		}
	}
}

func TestPadding(t *testing.T) {
	if got := Pad("ab", 5); got != "ab   " {
		t.Errorf("Pad = %q", got)
	}
	if got := LPad("ab", 5); got != "   ab" {
		t.Errorf("LPad = %q", got)
	}
	if got := Center("ab", 6); got != "  ab  " {
		t.Errorf("Center = %q", got)
	}
	// Over-long input is clipped rather than allowed to overflow the column.
	for _, w := range []int{0, 1, 2, 3} {
		if got := Pad("abcdef", w); Width(got) > w {
			t.Errorf("Pad(%q, %d) = %q", "abcdef", w, got)
		}
	}
}

// ---------------------------------------------------------------- format

func TestHumanRate(t *testing.T) {
	cases := []struct {
		v    float64
		want string
	}{
		{0, "--"}, {950, "950 H/s"}, {1000, "1.00 KH/s"},
		{248680, "248.68 KH/s"}, {4.5e6, "4.50 MH/s"},
		{2.5e9, "2.50 GH/s"}, {7e12, "7.00 TH/s"},
	}
	for _, c := range cases {
		if got := HumanRate(c.v); got != c.want {
			t.Errorf("HumanRate(%g) = %q, want %q", c.v, got, c.want)
		}
	}
	if n, u := SplitRate(248680); n != "248.68" || u != "KH/s" {
		t.Errorf("SplitRate = %q %q", n, u)
	}
	if n, u := SplitRate(0); n != "--" || u != "" {
		t.Errorf("SplitRate(0) = %q %q", n, u)
	}
}

func TestCommasAndPercents(t *testing.T) {
	for v, want := range map[uint64]string{
		0: "0", 12: "12", 999: "999", 1000: "1,000",
		1234567: "1,234,567", 1 << 62: "4,611,686,018,427,387,904",
	} {
		if got := Commas(v); got != want {
			t.Errorf("Commas(%d) = %q, want %q", v, got, want)
		}
	}
	// A percentage of nothing is not zero per cent.
	if got := Pct(0, 0); got != "--" {
		t.Errorf("Pct(0,0) = %q", got)
	}
	if got := Pct1(999, 1000); got != "99.9%" {
		t.Errorf("Pct1 = %q", got)
	}
}

func TestDurations(t *testing.T) {
	if got := HMS(3*time.Hour + 12*time.Minute + 8*time.Second); got != "03:12:08" {
		t.Errorf("HMS = %q", got)
	}
	if got := HMS(-time.Second); got != "00:00:00" {
		t.Errorf("HMS of a negative duration = %q", got)
	}
	if got := ShortDur(0); got != "--" {
		t.Errorf("ShortDur(0) = %q", got)
	}
	if got := Ago(time.Time{}); got != "never" {
		t.Errorf("Ago(zero) = %q", got)
	}
}

// ---------------------------------------------------------------- canvas

func TestCanvasClipsAtEveryEdge(t *testing.T) {
	cv := NewCanvas(10, 4)
	// None of these may panic and none may appear on the canvas.
	cv.Set(-1, 0, 'x', Style{})
	cv.Set(0, -1, 'x', Style{})
	cv.Set(10, 0, 'x', Style{})
	cv.Set(0, 4, 'x', Style{})
	cv.Text(8, 0, "abcdefgh", Style{})
	cv.HLine(-5, 1, 30, '-', Style{})
	cv.VLine(3, -5, 30, '|', Style{})
	cv.Fill(Rect{-3, -3, 40, 40}, '.', Style{})

	for _, line := range cv.Lines(false) {
		if Width(line) > 10 {
			t.Fatalf("a row came out %d cells wide: %q", Width(line), line)
		}
	}
}

func TestWideRunesNeverLeaveAnOrphan(t *testing.T) {
	cv := NewCanvas(6, 1)
	cv.Set(0, 0, '日', Style{})
	cv.Set(1, 0, 'x', Style{}) // lands on the second half
	if got := cv.Lines(false)[0]; Width(got) > 6 {
		t.Errorf("overwriting half a wide rune gave %q (%d cells)", got, Width(got))
	}
	// A wide rune with one column left must not spill onto the next row.
	cv2 := NewCanvas(3, 2)
	cv2.Set(2, 0, '日', Style{})
	for _, line := range cv2.Lines(false) {
		if Width(line) > 3 {
			t.Errorf("a wide rune at the margin gave %q", line)
		}
	}
}

func TestRectSplitting(t *testing.T) {
	r := Rect{0, 0, 100, 20}

	rows := r.Rows(3, -1, 1)
	if rows[0].H != 3 || rows[2].H != 1 || rows[1].H != 16 {
		t.Errorf("Rows gave %v", rows)
	}
	if rows[2].Bottom() != 20 {
		t.Errorf("Rows did not fill the rectangle: %v", rows)
	}

	// Columns always add up, at every width, with every weighting.
	for w := 1; w <= 200; w++ {
		for _, weights := range [][]int{{1, 1}, {30, 34, 36}, {1, 2, 3, 4}, {0, 1}} {
			cols := Rect{5, 0, w, 3}.Cols(weights...)
			total, x := 0, 5
			for _, c := range cols {
				if c.X != x {
					t.Fatalf("w=%d %v: column at %d, expected %d", w, weights, c.X, x)
				}
				x += c.W
				total += c.W
			}
			if total != w {
				t.Fatalf("w=%d %v: columns total %d", w, weights, total)
			}
		}
	}

	// Inset past nothing becomes empty rather than inverted.
	if got := (Rect{0, 0, 4, 4}).Inset(5); !got.Empty() || got.W < 0 || got.H < 0 {
		t.Errorf("Inset past nothing gave %v", got)
	}
	// A height of -1 with nothing left is zero, not negative.
	if got := (Rect{0, 0, 10, 2}).Rows(5, -1); got[1].H != 0 {
		t.Errorf("an over-committed Rows gave %v", got)
	}
}

// ---------------------------------------------------------------- screen

func TestScreenWritesOnlyWhatChanged(t *testing.T) {
	var out strings.Builder
	scr := NewScreen(&out, 40, 6, true)

	scr.Buf().Text(0, 0, "hello", Style{FG: "\x1b[31m"})
	scr.Flush()
	first := out.Len()
	if first == 0 {
		t.Fatal("the first flush wrote nothing")
	}

	// The identical frame again must cost almost nothing.
	out.Reset()
	scr.Buf().Text(0, 0, "hello", Style{FG: "\x1b[31m"})
	scr.Flush()
	if out.Len() > 16 {
		t.Errorf("an unchanged frame wrote %d bytes: %q", out.Len(), out.String())
	}

	// One changed cell costs a cursor move and a character, not a screen.
	out.Reset()
	scr.Buf().Text(0, 0, "hellp", Style{FG: "\x1b[31m"})
	scr.Flush()
	if out.Len() > 64 {
		t.Errorf("one changed cell wrote %d bytes", out.Len())
	}
	if out.Len() == 0 {
		t.Error("a changed cell wrote nothing at all")
	}
}

func TestScreenEnterAndLeaveAreBalanced(t *testing.T) {
	var out strings.Builder
	scr := NewScreen(&out, 20, 5, true)
	scr.Enter()
	entered := out.String()
	for _, want := range []string{"\x1b[?1049h", "\x1b[?7l", "\x1b[?25l", "\x1b[?1004h"} {
		if !strings.Contains(entered, want) {
			t.Errorf("Enter did not send %q", want)
		}
	}
	out.Reset()
	scr.Leave()
	left := out.String()
	// Every one of the three has to be undone, or the user is left in the
	// alternate buffer with no cursor and a terminal that truncates lines.
	for _, want := range []string{"\x1b[?1049l", "\x1b[?7h", "\x1b[?25h", "\x1b[0m", "\x1b[?1004l"} {
		if !strings.Contains(left, want) {
			t.Errorf("Leave did not send %q", want)
		}
	}
}

func TestScreenBackgroundReachesEveryCell(t *testing.T) {
	var out strings.Builder
	scr := NewScreen(&out, 12, 2, true)
	scr.SetBG("\x1b[48;2;7;11;22m")
	scr.Buf().Text(0, 0, "hi", Style{FG: "\x1b[31m"})
	scr.Flush()
	if !strings.Contains(out.String(), "48;2;7;11;22") {
		t.Errorf("the themed background never reached the terminal: %q", out.String())
	}
}

// ---------------------------------------------------------------- widgets

func TestWidgetsSurviveDegenerateRectangles(t *testing.T) {
	th := Themes["storm"]
	cv := NewCanvas(40, 12)
	vals := []float64{1, 2, 3, 2, 1, 4, 9, 3}

	for _, r := range []Rect{
		{0, 0, 0, 0}, {0, 0, 1, 1}, {0, 0, 40, 1}, {0, 0, 1, 12},
		{35, 10, 20, 20}, {-5, -5, 10, 10}, {0, 0, 40, 12},
	} {
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("a widget panicked on %v: %v", r, p)
				}
			}()
			Panel(cv, r, th, PanelOpts{Title: "T", Mark: "◈", Right: "R"})
			Ring(cv, r, th, RingOpts{Frac: 0.5, FG: th.Accent, Track: th.Track, Lines: []string{"50%", "LOAD"}})
			Chart(cv, r, th, ChartOpts{Values: vals, XLabels: []string{"a", "b"}, Fill: true})
			Spectrum(cv, r, vals, th, th.Accent)
			Globe(cv, r, th, 1.2)
			WireCube(cv, r, th, 0.7)
			RidgeWire(cv, r, th, 0.3)
			Ridgeline(cv, r, th, 0.3, "", "")
			SignalRamp(cv, r, 3, th, true)
			Mast(cv, r, th, 3, 4, true)
			Tank(cv, r.X, r.Y, r.W, r.H, 0.4, th, th.Accent2)
			Thermometer(cv, r.X, r.Y, r.H, 62, true, th)
			GradientArt(cv, r.X, r.Y, WordmarkSmall, th, true)
			Tabs(cv, r.X, r.Y, []string{"5M", "1H"}, 0, th)
			Chevrons(cv, r.X, r.Y, 6, th)
			Swatch(cv, r.X, r.Y, th.Good)
		}()
	}
	for _, line := range cv.Lines(false) {
		if Width(line) > 40 {
			t.Fatalf("a widget drew past the canvas: %q", line)
		}
	}
}

func TestBarIsProportionalToTheEighth(t *testing.T) {
	// Eighths are the whole point: without them two values a few per cent
	// apart draw identically and the bar answers nothing.
	seen := map[string]bool{}
	for i := 0; i <= 16; i++ {
		s := Bar(float64(i)/16, 8)
		if Width(s) != 8 {
			t.Fatalf("Bar(%d/16, 8) is %d cells: %q", i, Width(s), s)
		}
		seen[s] = true
	}
	if len(seen) < 14 {
		t.Errorf("seventeen fractions produced only %d distinct bars", len(seen))
	}
	if Bar(0.5, 0) != "" {
		t.Error("a zero-width bar drew something")
	}
	if got := Bar(-3, 4); Width(got) != 4 {
		t.Errorf("Bar of a negative fraction = %q", got)
	}
	if got := Bar(9, 4); !strings.Contains(got, string(BlockFull)) {
		t.Errorf("Bar of an over-unity fraction = %q", got)
	}
}

func TestResample(t *testing.T) {
	in := []float64{1, 2, 3, 4, 5, 6, 7, 8}
	if got := Resample(in, 4); len(got) != 4 {
		t.Errorf("Resample to 4 gave %d", len(got))
	}
	if got := Resample(in, 20); len(got) != 20 {
		t.Errorf("Resample up to 20 gave %d", len(got))
	}
	if got := Resample(nil, 5); len(got) != 0 {
		t.Errorf("Resample of nothing gave %d", len(got))
	}
	if got := Resample(in, 0); len(got) != 0 {
		t.Errorf("Resample to nothing gave %d", len(got))
	}
}

// ---------------------------------------------------------------- cost

func BenchmarkScreenFlushStill(b *testing.B) {
	scr := NewScreen(discard{}, 170, 48, true)
	scr.SetBG(Themes["storm"].PanelBG)
	scr.Buf().Fill(Rect{0, 0, 170, 48}, '#', Style{FG: Themes["storm"].Accent})
	scr.Flush()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scr.Buf().Fill(Rect{0, 0, 170, 48}, '#', Style{FG: Themes["storm"].Accent})
		scr.Flush()
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
