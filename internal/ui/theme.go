package ui

// Theme system for the DeroStorm console.
//
// A theme is a table of RGB triples, not a table of escape sequences. That
// distinction is the whole reason this file can degrade: the same palette is
// rendered as 24-bit escapes on a terminal that has them, as the nearest entry
// in the xterm-256 cube on one that does not, and as one of the eight ANSI
// colours on the last resort. A theme author picks colours once; the terminal
// decides how they are spelled.
//
// Colour is switched off entirely -- every token becomes an empty string --
// when the user asks for it, when NO_COLOR is set, or when stdout is not a
// terminal. That last one matters: a miner run as a service or piped to a log
// file should write clean text, not escape sequences.

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// ---------------------------------------------------------------- colour depth

// Depth is how much colour the terminal on the other end can actually show.
type Depth int

const (
	// DepthNone is no colour at all: every token renders as the empty string.
	DepthNone Depth = iota
	// Depth16 is the eight ANSI colours and their bright forms. Every terminal
	// that understands escape sequences at all understands these.
	Depth16
	// Depth256 is the xterm palette: a 6x6x6 colour cube and a 24-step grey
	// ramp. Nearly universal since about 2010.
	Depth256
	// DepthTrue is 24-bit colour, which is what the theme is authored in.
	DepthTrue
)

func (d Depth) String() string {
	switch d {
	case DepthTrue:
		return "truecolor"
	case Depth256:
		return "256"
	case Depth16:
		return "16"
	}
	return "none"
}

// DetectDepth works out what the terminal can do from the environment.
//
// Guessing from $TERM alone is not enough any more: Windows Terminal, VS Code
// and most of the modern Unix terminals set COLORTERM instead and leave TERM
// at something conservative. So the check runs the other way round -- believe
// an explicit claim of truecolor, believe a known-good terminal, and only fall
// back to reading TERM when neither said anything.
//
// The bias is deliberately towards claiming less than the terminal has. A
// 256-colour approximation on a truecolor terminal is a slightly duller
// dashboard; 24-bit escapes on a terminal that cannot parse them is a screen
// of visible garbage.
func DetectDepth() Depth {
	if v := strings.ToLower(os.Getenv("DEROSTORM_COLOR")); v != "" {
		switch v {
		case "none", "off", "0":
			return DepthNone
		case "16", "ansi", "basic":
			return Depth16
		case "256":
			return Depth256
		case "true", "truecolor", "24bit", "24":
			return DepthTrue
		}
	}
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return DepthNone
	}

	switch strings.ToLower(os.Getenv("COLORTERM")) {
	case "truecolor", "24bit":
		return DepthTrue
	}
	// Windows Terminal, ConEmu and the VS Code integrated terminal all do
	// 24-bit colour and none of them advertise it in TERM.
	if os.Getenv("WT_SESSION") != "" || os.Getenv("ConEmuANSI") == "ON" {
		return DepthTrue
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app", "WezTerm", "vscode", "ghostty", "Hyper", "Apple_Terminal":
		// Apple_Terminal is the odd one out: it is 256 and has never been
		// more, so it is named here to be caught by the TERM check below
		// rather than promoted with the rest.
		if os.Getenv("TERM_PROGRAM") != "Apple_Terminal" {
			return DepthTrue
		}
	}

	term := os.Getenv("TERM")
	switch {
	case term == "":
		// No TERM at all. On Windows that is normal and the console has
		// already been put into virtual-terminal mode by the time anything
		// draws, so 24-bit is the right guess; anywhere else it means a very
		// plain environment.
		if os.PathSeparator == '\\' {
			return DepthTrue
		}
		return Depth16
	case strings.Contains(term, "truecolor"), strings.Contains(term, "direct"):
		return DepthTrue
	case strings.Contains(term, "kitty"), strings.Contains(term, "alacritty"),
		strings.Contains(term, "wezterm"), strings.Contains(term, "ghostty"):
		return DepthTrue
	case strings.Contains(term, "256"):
		return Depth256
	case term == "dumb":
		return DepthNone
	}
	return Depth16
}

// depth is the depth every theme is currently rendered at. It is read on every
// theme build and nowhere else, so changing it is cheap and only affects
// themes built afterwards -- which is why SetDepth rebuilds the table.
var (
	depthMu sync.Mutex
	depth   = DetectDepth()
)

// CurrentDepth is the depth themes are being rendered at.
func CurrentDepth() Depth {
	depthMu.Lock()
	defer depthMu.Unlock()
	return depth
}

// SetDepth forces a colour depth and rebuilds every theme at it. Used by the
// --color flag and by the tests, which have to be able to check all four
// spellings without an environment.
func SetDepth(d Depth) {
	depthMu.Lock()
	depth = d
	depthMu.Unlock()
	buildThemes()
}

// ---------------------------------------------------------------- colours

// RGB is one authored colour.
type RGB struct{ R, G, B uint8 }

func hex(v uint32) RGB { return RGB{uint8(v >> 16), uint8(v >> 8), uint8(v)} }

// Mix is c blended towards d by t in [0,1]. Used to build the gradient ramps
// and to derive a track colour from an accent, so a theme only has to name the
// colours a reader can actually distinguish.
func (c RGB) Mix(d RGB, t float64) RGB {
	if t <= 0 {
		return c
	}
	if t >= 1 {
		return d
	}
	f := func(a, b uint8) uint8 { return uint8(float64(a) + (float64(b)-float64(a))*t + 0.5) }
	return RGB{f(c.R, d.R), f(c.G, d.G), f(c.B, d.B)}
}

// FG is the foreground escape for this colour at the current depth.
func (c RGB) FG(d Depth) string { return c.esc(d, 38, 30, 90) }

// BG is the background escape for this colour at the current depth.
func (c RGB) BG(d Depth) string { return c.esc(d, 48, 40, 100) }

func (c RGB) esc(d Depth, sgr, base16, bright16 int) string {
	switch d {
	case DepthTrue:
		return "\x1b[" + strconv.Itoa(sgr) + ";2;" +
			strconv.Itoa(int(c.R)) + ";" + strconv.Itoa(int(c.G)) + ";" + strconv.Itoa(int(c.B)) + "m"
	case Depth256:
		return "\x1b[" + strconv.Itoa(sgr) + ";5;" + strconv.Itoa(c.xterm256()) + "m"
	case Depth16:
		idx, bright := c.ansi16()
		if bright {
			return "\x1b[" + strconv.Itoa(bright16+idx) + "m"
		}
		return "\x1b[" + strconv.Itoa(base16+idx) + "m"
	}
	return ""
}

// xterm256 is the nearest entry in the xterm palette.
//
// The grey ramp is checked separately from the colour cube rather than being
// left to a single nearest-neighbour search over all 240 entries. Without it a
// near-grey like the muted label colour lands on a cube entry that is visibly
// tinted, and a dashboard whose labels are faintly green is worse than one
// whose labels are a shade off.
func (c RGB) xterm256() int {
	q := func(v uint8) int {
		switch {
		case v < 48:
			return 0
		case v < 115:
			return 1
		default:
			return (int(v) - 35) / 40
		}
	}
	lv := [6]int{0, 95, 135, 175, 215, 255}
	ri, gi, bi := q(c.R), q(c.G), q(c.B)
	cubeErr := sq(lv[ri]-int(c.R)) + sq(lv[gi]-int(c.G)) + sq(lv[bi]-int(c.B))

	// The 24-step grey ramp, entries 232..255.
	grey := (int(c.R)*299 + int(c.G)*587 + int(c.B)*114) / 1000
	gidx := (grey - 3) / 10
	if gidx < 0 {
		gidx = 0
	}
	if gidx > 23 {
		gidx = 23
	}
	gv := 8 + gidx*10
	greyErr := sq(gv-int(c.R)) + sq(gv-int(c.G)) + sq(gv-int(c.B))

	if greyErr < cubeErr {
		return 232 + gidx
	}
	return 16 + 36*ri + 6*gi + bi
}

func sq(v int) int { return v * v }

// ansi16 is the nearest of the sixteen basic colours, as an index 0..7 and
// whether it is the bright form.
//
// Hue first, brightness second. Matching on distance in RGB puts the theme's
// violet on blue and its cyan on cyan, which is right, but it also puts the
// mid-grey border on black -- invisible. Deciding the index from the dominant
// channels and the bright bit from luminance keeps every token distinguishable
// from the background, which is the only thing that matters at this depth.
func (c RGB) ansi16() (idx int, bright bool) {
	r, g, b := int(c.R), int(c.G), int(c.B)
	maxc := r
	if g > maxc {
		maxc = g
	}
	if b > maxc {
		maxc = b
	}
	minc := r
	if g < minc {
		minc = g
	}
	if b < minc {
		minc = b
	}
	lum := (r*299 + g*587 + b*114) / 1000
	bright = lum >= 128

	// Near-grey: black, white, or one of the two greys between them.
	//
	// The floor for plain black is deliberately very low. Several themes have
	// a border or a dim token that is a dark blue-grey, and mapping those to
	// colour 0 puts them at exactly the background colour of most terminals --
	// so the panel borders simply disappear and the dashboard looks like a
	// field of floating numbers. Anything with any light in it at all becomes
	// bright black, which every terminal renders as a visible grey.
	if maxc-minc < 40 {
		switch {
		case lum < 24:
			return 0, false // black
		case lum < 128:
			return 0, true // bright black == grey
		case lum < 200:
			return 7, false // white
		default:
			return 7, true // bright white
		}
	}
	// A channel counts as "on" when it is within a third of the strongest.
	on := func(v int) int {
		if v*3 >= maxc*2 {
			return 1
		}
		return 0
	}
	// ANSI index bits: 1 red, 2 green, 4 blue.
	return on(r) | on(g)<<1 | on(b)<<2, bright
}

// ---------------------------------------------------------------- theme

type Theme struct {
	Name string
	Desc string

	Accent  string // headline numbers, the product name
	Accent2 string // secondary highlights, GPU figures, the spinner
	Accent3 string // the third note: magenta in the default theme, used sparingly
	Text    string // primary values
	Muted   string // field labels
	Dim     string // separators, timestamps, inactive text
	Border  string // box chrome
	Good    string // accepted shares, healthy state
	Warn    string // stale / reconnecting
	Err     string // rejected / errors

	// Glow is the faint under-layer used behind large artwork and the outer
	// halo of a gauge. It is a third accent rather than Dim because it has to
	// read as "the accent, quieter" and not as "inactive".
	Glow string

	// BorderHi is the border of a panel the user is looking at: the focused
	// one, or one carrying a state that has just changed. A single brighter
	// rule is the cheapest way to say "here" on a screen of twelve boxes.
	BorderHi string

	// Track is the unfilled part of a bar or ring: dark enough to read as
	// empty, light enough to show where the bar ends.
	Track string

	// Fill is the area under a chart trace: the secondary accent taken most
	// of the way down to the panel ground. It is derived rather than authored
	// because it has exactly one job -- be the same hue as the line and far
	// enough back that the line still reads as the subject.
	Fill string

	// PanelBG is the background painted inside a panel. Empty means "leave the
	// terminal's own background alone", which is what every theme but the
	// default does -- the default paints, because the reference look is a dark
	// instrument panel and a console with a pale background would show the
	// artwork as dark shapes on white.
	PanelBG string
	// HeaderBG is the header strip's background, a shade deeper than PanelBG.
	HeaderBG string
	// SelBG is the background of a selected tab or key.
	SelBG string

	Reset string
	Bold  string

	// ramp is the accent-to-accent2-to-accent3 gradient, precomputed. The
	// wordmark and the chart fills index into it rather than building an
	// escape per cell per frame: at 8 frames a second across a 76-cell
	// wordmark that would be six hundred allocations a second for a picture
	// that never changes.
	ramp []string
	// heat is the cool-to-hot ramp used by the temperature gauges.
	heat []string
	// pal is what the theme was authored as, kept so the table can be rebuilt
	// at a different depth.
	pal palette
}

// palette is a theme as authored: RGB, no escapes.
type palette struct {
	Desc                        string
	Accent, Accent2, Accent3    RGB
	Text, Muted, Dim, Border    RGB
	Good, Warn, Err             RGB
	Glow, Track                 RGB
	PanelBG, HeaderBG, SelBG    RGB
	Paint                       bool // whether the BG tokens are used at all
	RampA, RampB, RampC         RGB  // the wordmark gradient's three stops
	HeatCool, HeatWarm, HeatHot RGB
}

// palettes are the themes as authored. --theme picks one by name.
var palettes = map[string]palette{
	// storm is the reference look and the default: neon cyan into electric
	// violet on a deep navy-black, with magenta kept for the few things that
	// have to be louder than the violet.
	"storm": {
		Desc:   "neon cyan into violet on deep navy — the DEROSTORM look",
		Accent: hex(0x35E6F2), Accent2: hex(0xA96BFF), Accent3: hex(0xF25CC1),
		Text: hex(0xD3E4F0), Muted: hex(0x6F8CA6), Dim: hex(0x3E5670), Border: hex(0x1E4E6B),
		Good: hex(0x33E88E), Warn: hex(0xFFC23D), Err: hex(0xFF5B72),
		Glow: hex(0x123B57), Track: hex(0x17293D),
		PanelBG: hex(0x070B16), HeaderBG: hex(0x04060F), SelBG: hex(0x1B2A4A), Paint: true,
		RampA: hex(0x2ED9F2), RampB: hex(0x8A6BFF), RampC: hex(0xF25CC1),
		HeatCool: hex(0x35E6F2), HeatWarm: hex(0xFFC23D), HeatHot: hex(0xFF5B72),
	},
	// cyber is the previous default, kept by name so a saved config that asks
	// for it still gets what it asked for.
	"cyber": {
		Desc:   "neon cyan + violet on black — the control-centre look",
		Accent: hex(0x2EE6F6), Accent2: hex(0xBB77FB), Accent3: hex(0xF25CC1),
		Text: hex(0xCFDCE8), Muted: hex(0x74889A), Dim: hex(0x3F5260), Border: hex(0x1C5C66),
		Good: hex(0x3DE07B), Warn: hex(0xFAC02E), Err: hex(0xF45B5B),
		Glow: hex(0x143F4A), Track: hex(0x16262C),
		PanelBG: hex(0x00040A), HeaderBG: hex(0x000308), SelBG: hex(0x0E3742),
		RampA: hex(0x2EE6F6), RampB: hex(0xBB77FB), RampC: hex(0xF25CC1),
		HeatCool: hex(0x2EE6F6), HeatWarm: hex(0xFAC02E), HeatHot: hex(0xF45B5B),
	},
	"default": {
		Desc:   "cyan + violet on near-black",
		Accent: hex(0x56D4DD), Accent2: hex(0xA98BFF), Accent3: hex(0xE87AC8),
		Text: hex(0xC9D4E0), Muted: hex(0x7A8798), Dim: hex(0x4A5666), Border: hex(0x3A4450),
		Good: hex(0x4ADE80), Warn: hex(0xFBBF24), Err: hex(0xF87171),
		Glow: hex(0x253742), Track: hex(0x1E2733),
		PanelBG: hex(0x0A0E14), HeaderBG: hex(0x070A0F), SelBG: hex(0x1E2A38),
		RampA: hex(0x56D4DD), RampB: hex(0xA98BFF), RampC: hex(0xE87AC8),
		HeatCool: hex(0x56D4DD), HeatWarm: hex(0xFBBF24), HeatHot: hex(0xF87171),
	},
	"copper": {
		Desc:   "burnt copper + slate on charcoal",
		Accent: hex(0xE4813F), Accent2: hex(0x6FA0BA), Accent3: hex(0xE8C15C),
		Text: hex(0xD6E0E4), Muted: hex(0x8298A0), Dim: hex(0x55666E), Border: hex(0x2A383E),
		Good: hex(0x4FB98A), Warn: hex(0xD6A63C), Err: hex(0xE06C6C),
		Glow: hex(0x4A301E), Track: hex(0x241911),
		PanelBG: hex(0x0D0A08), HeaderBG: hex(0x080605), SelBG: hex(0x3A2517),
		RampA: hex(0xE4813F), RampB: hex(0xE8C15C), RampC: hex(0x6FA0BA),
		HeatCool: hex(0x6FA0BA), HeatWarm: hex(0xD6A63C), HeatHot: hex(0xE06C6C),
	},
	"aurora": {
		Desc:   "emerald + ice on deep green-black",
		Accent: hex(0x5EEAD4), Accent2: hex(0x86EFAC), Accent3: hex(0x7DD3FC),
		Text: hex(0xCBE3DC), Muted: hex(0x74948C), Dim: hex(0x465F59), Border: hex(0x2C433E),
		Good: hex(0x4ADE80), Warn: hex(0xFCD34D), Err: hex(0xFB7185),
		Glow: hex(0x1E3D38), Track: hex(0x152825),
		PanelBG: hex(0x05100D), HeaderBG: hex(0x030B09), SelBG: hex(0x16403A),
		RampA: hex(0x5EEAD4), RampB: hex(0x86EFAC), RampC: hex(0x7DD3FC),
		HeatCool: hex(0x5EEAD4), HeatWarm: hex(0xFCD34D), HeatHot: hex(0xFB7185),
	},
	"ember": {
		Desc:   "amber + rose on warm black",
		Accent: hex(0xFBBF24), Accent2: hex(0xFB7E5C), Accent3: hex(0xF472B6),
		Text: hex(0xE7DACF), Muted: hex(0x9C8677), Dim: hex(0x6B574C), Border: hex(0x45332B),
		Good: hex(0xA3E635), Warn: hex(0xFACC15), Err: hex(0xF87171),
		Glow: hex(0x533814), Track: hex(0x2A1D14),
		PanelBG: hex(0x100A06), HeaderBG: hex(0x0A0604), SelBG: hex(0x3D2812),
		RampA: hex(0xFBBF24), RampB: hex(0xFB7E5C), RampC: hex(0xF472B6),
		HeatCool: hex(0xFBBF24), HeatWarm: hex(0xFB7E5C), HeatHot: hex(0xF87171),
	},
}

// monoTheme is not a palette: it is the absence of one, and it has to be a
// literal because every token in it is the empty string rather than a colour
// rendered at depth zero.
var monoTheme = &Theme{Name: "mono", Desc: "no colour, for logs, CI and dumb terminals"}

// Themes is the built table, one *Theme per palette plus mono. Rebuilt by
// SetDepth; read by name from PickTheme and from the runtime "theme" command.
var Themes = map[string]*Theme{}

// rampSteps is how many stops the wordmark gradient is quantised to.
//
// Thirty-two is not a compromise between smoothness and cost -- at 24-bit the
// escape strings are built once and never again, so more steps would be free.
// It is the point past which a terminal's own colour rendering stops resolving
// the difference, and it keeps the ramp usefully short at 256 colours, where
// several neighbouring steps collapse onto the same palette entry anyway.
const rampSteps = 32

func init() { buildThemes() }

func buildThemes() {
	depthMu.Lock()
	d := depth
	depthMu.Unlock()

	built := make(map[string]*Theme, len(palettes)+1)
	for name, p := range palettes {
		built[name] = buildTheme(name, p, d)
	}
	built["mono"] = monoTheme
	Themes = built
}

func buildTheme(name string, p palette, d Depth) *Theme {
	t := &Theme{
		Name: name, Desc: p.Desc, pal: p,
		Accent: p.Accent.FG(d), Accent2: p.Accent2.FG(d), Accent3: p.Accent3.FG(d),
		Text: p.Text.FG(d), Muted: p.Muted.FG(d), Dim: p.Dim.FG(d), Border: p.Border.FG(d),
		Good: p.Good.FG(d), Warn: p.Warn.FG(d), Err: p.Err.FG(d),
		Glow: p.Glow.FG(d), Track: p.Track.FG(d),
		Fill:     p.Accent2.Mix(p.PanelBG, 0.66).FG(d),
		BorderHi: p.Accent.FG(d),
		Reset:    "\x1b[0m", Bold: "\x1b[1m",
	}
	// Backgrounds are only painted where the terminal can render them without
	// swamping the foreground. At sixteen colours a filled panel background is
	// one of eight flat colours behind text drawn in another of the same
	// eight, and the result is unreadable often enough that it is not worth
	// having at all.
	if p.Paint && d >= Depth256 {
		t.PanelBG = p.PanelBG.BG(d)
		t.HeaderBG = p.HeaderBG.BG(d)
		t.SelBG = p.SelBG.BG(d)
	} else if d >= Depth256 {
		t.SelBG = p.SelBG.BG(d)
	}

	t.ramp = make([]string, rampSteps)
	for i := range t.ramp {
		f := float64(i) / float64(rampSteps-1)
		var c RGB
		if f < 0.5 {
			c = p.RampA.Mix(p.RampB, f*2)
		} else {
			c = p.RampB.Mix(p.RampC, (f-0.5)*2)
		}
		t.ramp[i] = c.FG(d)
	}
	t.heat = make([]string, rampSteps)
	for i := range t.heat {
		f := float64(i) / float64(rampSteps-1)
		var c RGB
		if f < 0.5 {
			c = p.HeatCool.Mix(p.HeatWarm, f*2)
		} else {
			c = p.HeatWarm.Mix(p.HeatHot, (f-0.5)*2)
		}
		t.heat[i] = c.FG(d)
	}
	return t
}

// Ramp is the gradient colour at f in [0,1]: the wordmark's cyan at 0, the
// magenta accent at 1. Returns the empty string on the mono theme, so a caller
// can use it unconditionally.
func (t *Theme) Ramp(f float64) string {
	if len(t.ramp) == 0 {
		return ""
	}
	i := int(f*float64(rampSteps-1) + 0.5)
	if i < 0 {
		i = 0
	}
	if i >= rampSteps {
		i = rampSteps - 1
	}
	return t.ramp[i]
}

// Heat is the temperature gradient at f in [0,1]: cool at 0, hot at 1.
func (t *Theme) Heat(f float64) string {
	if len(t.heat) == 0 {
		return ""
	}
	i := int(f*float64(rampSteps-1) + 0.5)
	if i < 0 {
		i = 0
	}
	if i >= rampSteps {
		i = rampSteps - 1
	}
	return t.heat[i]
}

// DefaultTheme is the theme a run gets when nothing asks for another. It is
// the palette the dashboard artwork was drawn against, so it is also the one
// the layout is tuned for.
const DefaultTheme = "storm"

func ThemeNames() []string {
	out := make([]string, 0, len(Themes))
	for k := range Themes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// PickTheme resolves the --theme value against the environment. It returns the
// theme actually used and, when it had to override the request, why.
func PickTheme(requested string, stdoutIsTTY bool) (*Theme, string) {
	name := strings.ToLower(strings.TrimSpace(requested))
	if name == "" {
		name = DefaultTheme
	}

	t, ok := Themes[name]
	if !ok {
		return Themes[DefaultTheme], fmt.Sprintf("unknown theme %q, using %s (available: %s)", requested, DefaultTheme, strings.Join(ThemeNames(), ", "))
	}

	if t.Name == "mono" {
		return t, ""
	}
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return Themes["mono"], "NO_COLOR is set, using mono"
	}
	if !stdoutIsTTY {
		return Themes["mono"], "output is not a terminal, using mono"
	}
	if CurrentDepth() == DepthNone {
		return Themes["mono"], "this terminal reports no colour support, using mono"
	}
	return t, ""
}

// C wraps s in a colour token. With the mono theme every token is empty, so
// this returns s unchanged and no escape sequences reach the output at all.
func (t *Theme) C(token, s string) string {
	if token == "" {
		return s
	}
	return token + s + t.Reset
}

// c is the lower-case spelling the pre-existing console code uses.
func (t *Theme) c(token, s string) string { return t.C(token, s) }

// Mono reports whether this theme emits no escapes at all, which several
// widgets use to pick a plainer glyph set as well as a plainer palette.
func (t *Theme) Mono() bool { return t.Accent == "" }

// BG is the background-colour escape for an RGB triple, rendered at the
// current depth. Kept as a free function because a few call sites paint a
// colour that is not in any theme.
func BG(r, g, b int) string { return RGB{uint8(r), uint8(g), uint8(b)}.BG(CurrentDepth()) }
