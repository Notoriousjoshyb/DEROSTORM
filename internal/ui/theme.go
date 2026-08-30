package ui

// Theme system for the DeroStorm console.
//
// Colours are 24-bit ANSI escapes chosen per token rather than per widget, so a
// new theme is a table of values and nothing else has to change. The token
// names follow the oh-my-pi theme vocabulary (accent / border / muted / dim /
// success / warning / error), which is what every layout here is built around.
//
// Colour is switched off entirely -- every token becomes an empty string --
// when the user asks for it, when NO_COLOR is set, or when stdout is not a
// terminal. That last one matters: a miner run as a service or piped to a log
// file should write clean text, not escape sequences.

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

type Theme struct {
	Name string
	Desc string

	Accent  string // headline numbers, the product name
	Accent2 string // secondary highlights, GPU figures, the spinner
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

	Reset string
	Bold  string
}

func rgb(r, g, b int) string { return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b) }

// BG is the background-colour escape for an RGB triple. Used sparingly: the
// panels deliberately keep the terminal's own background so the console sits
// inside whatever the user has set up, and only the footer bar and a selected
// tab paint over it.
func BG(r, g, b int) string { return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b) }

// Themes are registered here; --theme picks one by name.
var Themes = map[string]*Theme{
	// cyber is the reference look: neon cyan and violet on black, the palette
	// the dashboard artwork was drawn for.
	"cyber": {
		Name: "cyber", Desc: "neon cyan + violet on black — the control-centre look",
		Accent:   rgb(0x2E, 0xE6, 0xF6),
		Accent2:  rgb(0xBB, 0x77, 0xFB),
		Text:     rgb(0xCF, 0xDC, 0xE8),
		Muted:    rgb(0x74, 0x88, 0x9A),
		Dim:      rgb(0x3F, 0x52, 0x60),
		Border:   rgb(0x1C, 0x5C, 0x66),
		Good:     rgb(0x3D, 0xE0, 0x7B),
		Warn:     rgb(0xFA, 0xC0, 0x2E),
		Err:      rgb(0xF4, 0x5B, 0x5B),
		Glow:     rgb(0x14, 0x3F, 0x4A),
		BorderHi: rgb(0x2E, 0xE6, 0xF6),
		Reset:    "\x1b[0m", Bold: "\x1b[1m",
	},
	"default": {
		Name: "default", Desc: "cyan + violet on near-black",
		Accent:   rgb(0x56, 0xD4, 0xDD),
		Accent2:  rgb(0xA9, 0x8B, 0xFF),
		Text:     rgb(0xC9, 0xD4, 0xE0),
		Muted:    rgb(0x7A, 0x87, 0x98),
		Dim:      rgb(0x4A, 0x56, 0x66),
		Border:   rgb(0x3A, 0x44, 0x50),
		Good:     rgb(0x4A, 0xDE, 0x80),
		Warn:     rgb(0xFB, 0xBF, 0x24),
		Err:      rgb(0xF8, 0x71, 0x71),
		Glow:     rgb(0x25, 0x37, 0x42),
		BorderHi: rgb(0x56, 0xD4, 0xDD),
		Reset:    "\x1b[0m", Bold: "\x1b[1m",
	},
	"copper": {
		Name: "copper", Desc: "burnt copper + slate on charcoal",
		Accent:   rgb(0xE4, 0x81, 0x3F),
		Accent2:  rgb(0x6F, 0xA0, 0xBA),
		Text:     rgb(0xD6, 0xE0, 0xE4),
		Muted:    rgb(0x82, 0x98, 0xA0),
		Dim:      rgb(0x55, 0x66, 0x6E),
		Border:   rgb(0x2A, 0x38, 0x3E),
		Good:     rgb(0x4F, 0xB9, 0x8A),
		Warn:     rgb(0xD6, 0xA6, 0x3C),
		Err:      rgb(0xE0, 0x6C, 0x6C),
		Glow:     rgb(0x4A, 0x30, 0x1E),
		BorderHi: rgb(0xE4, 0x81, 0x3F),
		Reset:    "\x1b[0m", Bold: "\x1b[1m",
	},
	"aurora": {
		Name: "aurora", Desc: "emerald + ice on deep green-black",
		Accent:   rgb(0x5E, 0xEA, 0xD4),
		Accent2:  rgb(0x86, 0xEF, 0xAC),
		Text:     rgb(0xCB, 0xE3, 0xDC),
		Muted:    rgb(0x74, 0x94, 0x8C),
		Dim:      rgb(0x46, 0x5F, 0x59),
		Border:   rgb(0x2C, 0x43, 0x3E),
		Good:     rgb(0x4A, 0xDE, 0x80),
		Warn:     rgb(0xFC, 0xD3, 0x4D),
		Err:      rgb(0xFB, 0x71, 0x85),
		Glow:     rgb(0x1E, 0x3D, 0x38),
		BorderHi: rgb(0x5E, 0xEA, 0xD4),
		Reset:    "\x1b[0m", Bold: "\x1b[1m",
	},
	"ember": {
		Name: "ember", Desc: "amber + rose on warm black",
		Accent:   rgb(0xFB, 0xBF, 0x24),
		Accent2:  rgb(0xFB, 0x7E, 0x5C),
		Text:     rgb(0xE7, 0xDA, 0xCF),
		Muted:    rgb(0x9C, 0x86, 0x77),
		Dim:      rgb(0x6B, 0x57, 0x4C),
		Border:   rgb(0x45, 0x33, 0x2B),
		Good:     rgb(0xA3, 0xE6, 0x35),
		Warn:     rgb(0xFA, 0xCC, 0x15),
		Err:      rgb(0xF8, 0x71, 0x71),
		Glow:     rgb(0x53, 0x38, 0x14),
		BorderHi: rgb(0xFB, 0xBF, 0x24),
		Reset:    "\x1b[0m", Bold: "\x1b[1m",
	},
	"mono": {
		Name: "mono", Desc: "no colour, for logs, CI and dumb terminals",
	},
}

// DefaultTheme is the theme a run gets when nothing asks for another. It is
// the palette the dashboard artwork was drawn against, so it is also the one
// the layout is tuned for.
const DefaultTheme = "cyber"

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
