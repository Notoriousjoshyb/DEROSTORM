package ui

// Artwork: the wordmark, the large digits, and the small pieces that sit in
// the corner of a panel.
//
// Two kinds of art live here and they are built differently on purpose.
//
// Fixed art -- the cloud, the cube, the mast -- is a table of strings with a
// palette that maps each glyph to a colour token. Using the block-density
// characters as the brightness ramp means the art carries its own shading, so
// a piece can be redrawn in any theme without a per-piece colour map.
//
// Generated art -- the globe and the storm -- is rasterised from geometry each
// frame. That is what lets it turn. A rotating globe drawn as twelve frames of
// fixed art would be twelve times the source and would still only have twelve
// positions; six lines of trigonometry have all of them.

import (
	"math"
	"strings"
)

// ArtPalette maps a glyph in a piece of art to a colour token.
type ArtPalette map[rune]string

// DrawArt stamps art at (x, y). Spaces are transparent, so a piece can be laid
// over a panel without punching a rectangular hole in it.
func DrawArt(cv *Canvas, x, y int, art []string, pal ArtPalette, def string) {
	for dy, line := range art {
		dx := 0
		for _, r := range line {
			if r == ' ' {
				dx++
				continue
			}
			fg := def
			if c, ok := pal[r]; ok {
				fg = c
			}
			cv.Set(x+dx, y+dy, r, Style{FG: fg})
			dx += RuneWidth(r)
		}
	}
}

// ArtWidth is the widest line of a piece.
func ArtWidth(art []string) int {
	w := 0
	for _, l := range art {
		if n := Width(l); n > w {
			w = n
		}
	}
	return w
}

// Shade is the palette shared by every fixed piece: the block-density
// characters run from the faintest under-layer to the full accent.
func Shade(t *Theme) ArtPalette {
	return ArtPalette{
		'░': t.Glow,
		'▒': t.Border,
		'▓': t.Accent2,
		'█': t.Accent,
	}
}

// StormShade is Shade with the bolt flashing. The cloud is on screen for the
// whole session, so the flash is slow -- four frames of fourteen -- enough to
// read as weather and not as a cursor.
func StormShade(t *Theme, frame int) ArtPalette {
	pal := Shade(t)
	if frame%14 < 4 {
		pal['█'] = t.Accent2
		pal['▓'] = t.Accent
	}
	return pal
}

// ---------------------------------------------------------------- wordmark

// WordmarkLarge is DEROSTORM at five rows, for a window wide enough to carry
// it. Block capitals rather than an outline font: at this size an outline is
// mostly holes, and the holes are what a terminal's line spacing ruins.
var WordmarkLarge = []string{
	"██████  ███████ ██████   ██████  ███████ ████████  ██████  ██████  ███    ███",
	"██   ██ ██      ██   ██ ██    ██ ██         ██    ██    ██ ██   ██ ████  ████",
	"██   ██ █████   ██████  ██    ██ ███████    ██    ██    ██ ██████  ██ ████ ██",
	"██   ██ ██      ██   ██ ██    ██      ██    ██    ██    ██ ██   ██ ██  ██  ██",
	"██████  ███████ ██   ██  ██████  ███████    ██     ██████  ██   ██ ██      ██",
}

// WordmarkSmall is the same name at three rows, in the heavy box characters
// the rest of this program uses. It is what a 100-column window gets.
var WordmarkSmall = []string{
	"╺┳┓┏━╸┏━┓┏━┓┏━┓╺┳╸┏━┓┏━┓┏┳┓",
	" ┃┃┣╸ ┣┳┛┃ ┃┗━┓ ┃ ┃ ┃┣┳┛┃┃┃",
	"╺┻┛┗━╸┛┗╸┗━┛┗━┛ ╹ ┗━┛┛┗╸╹ ╹",
}

// ---------------------------------------------------------------- big digits

// bigDigits is a three-row numeral set in heavy box characters, for the
// headline hashrate. Three rows is the largest a number can be and still leave
// room for anything else in a panel eleven rows tall.
var bigDigits = map[rune][3]string{
	'0': {"┏━┓", "┃ ┃", "┗━┛"},
	'1': {" ╻ ", " ┃ ", " ╹ "},
	'2': {"╺━┓", "┏━┛", "┗━╸"},
	'3': {"╺━┓", " ━┫", "╺━┛"},
	'4': {"╻ ╻", "┗━┫", "  ╹"},
	'5': {"┏━╸", "┗━┓", "╺━┛"},
	'6': {"┏━╸", "┣━┓", "┗━┛"},
	'7': {"╺━┓", "  ┃", "  ╹"},
	'8': {"┏━┓", "┣━┫", "┗━┛"},
	'9': {"┏━┓", "┗━┫", "╺━┛"},
	'.': {"  ", "  ", "▄ "},
	',': {"  ", "  ", "▖ "},
	':': {"  ", "▪ ", "▪ "},
	'-': {"   ", "╺━╸", "   "},
	' ': {"  ", "  ", "  "},
	'%': {"▘ ▗", " ╱ ", "▖ ▝"},
}

// BigTextWidth is how many cells BigText will occupy.
func BigTextWidth(s string) int {
	w := 0
	for _, r := range s {
		g, ok := bigDigits[r]
		if !ok {
			g = bigDigits[' ']
		}
		w += Width(g[0]) + 1
	}
	if w > 0 {
		w-- // no gap after the last glyph
	}
	return w
}

// BigText draws s in the three-row numeral set and returns its width. Any
// character with no glyph is drawn as a space, which keeps a caller from
// having to know what the font covers.
func BigText(cv *Canvas, x, y int, s string, st Style) int {
	dx := 0
	for _, r := range s {
		g, ok := bigDigits[r]
		if !ok {
			g = bigDigits[' ']
		}
		for row := 0; row < 3; row++ {
			cv.Text(x+dx, y+row, g[row], st)
		}
		dx += Width(g[0]) + 1
	}
	if dx > 0 {
		dx--
	}
	return dx
}

// ---------------------------------------------------------------- fixed art

// StormCloud is the header mark: a cloud with a bolt breaking out of it. The
// density ramp does the shading, so it reads as lit from inside in every theme.
//
// Six rows, because that is what is left of an eight-row header once the run
// header and the closing rule have taken theirs. Artwork that is one row
// taller than its slot does not overflow -- it is clipped -- but a clipped
// cloud has a flat bottom, which looks like a bug rather than a cloud.
var StormCloud = []string{
	"    ░▒▒▒▒▒░    ",
	"  ░▒▓█████▓▒░  ",
	" ░▓█████████▓▒ ",
	" ▒███████████▒ ",
	"  ░▒▓▓█▟▙█▓▒░  ",
	"      ▝█▛      ",
}

// CubeArt is the blockchain mark: a block with its inner faces shaded, which
// is what makes it read as a solid rather than as a hexagon.
var CubeArt = []string{
	"  ▄▄▄▄▄▄▄  ",
	"▄▀░░░░░░░▀▄",
	"█░▒▒▒▒▒▒▒░█",
	"█░▒▓▓▓▓▓▒░█",
	"▀▄░▒▒▒▒▒░▄▀",
	"  ▀▀▀▀▀▀▀  ",
}

// MastArt is the connection mark. The signal arcs above it are drawn by Mast
// so they can light up in sequence.
var MastArt = []string{
	"  ▟▙  ",
	" ▟██▙ ",
	"▟█▓▓█▙",
	"▀▀▀▀▀▀",
}

// ChipArt is the memory mark.
var ChipArt = []string{
	" ╷╷╷╷ ",
	"┌────┐",
	"┤▒▓▓▒├",
	"┤▒▓▓▒├",
	"└────┘",
	" ╵╵╵╵ ",
}

// ---------------------------------------------------------------- generated

// Globe draws a rotating wireframe sphere in braille. phase advances it.
//
// The curves are traced parametrically -- walk each meridian in latitude and
// each parallel in longitude, project, plot -- rather than found by testing
// every pixel against a grid equation. Tracing gives continuous lines at any
// size; testing gives a scatter of dots that only resolves into a globe at one
// particular radius, because near the limb a whole line falls between two
// sample points.
//
// Meridians and parallels rather than a coastline: a landmass at this size is
// a smear, whereas a turning grid reads immediately as a globe and as a live
// one. Points on the far side are drawn as the quieter layer, which is what
// gives the sphere depth instead of looking like a flat disc.
func Globe(cv *Canvas, r Rect, t *Theme, phase float64) {
	if r.W < 6 || r.H < 3 {
		return
	}
	g := newBrailleGrid(r.W, r.H)
	cx, cy := float64(g.subW)/2, float64(g.subH)/2
	rad := math.Min(cx, cy) - 0.5
	if rad < 2 {
		return
	}

	plot := func(lat, lon float64) {
		// A right-handed sphere seen from +z. nz > 0 is the near hemisphere.
		cl := math.Cos(lat)
		nx := cl * math.Sin(lon)
		ny := math.Sin(lat)
		nz := cl * math.Cos(lon)
		g.set(int(cx+nx*rad), int(cy+ny*rad), nz > 0)
	}

	const meridians, parallels = 8, 5
	steps := int(rad * 3)
	if steps < 24 {
		steps = 24
	}
	for m := 0; m < meridians; m++ {
		lon := phase + float64(m)*math.Pi/meridians
		for i := 0; i <= steps; i++ {
			lat := -math.Pi/2 + math.Pi*float64(i)/float64(steps)
			plot(lat, lon)
		}
	}
	for p := 1; p < parallels; p++ {
		lat := -math.Pi/2 + math.Pi*float64(p)/float64(parallels)
		for i := 0; i <= steps*2; i++ {
			lon := phase + 2*math.Pi*float64(i)/float64(steps*2)
			plot(lat, lon)
		}
	}
	g.draw(cv, r, t.Accent, t.Glow)
}

// Storm draws the swirling particle field behind the mining-status panel: two
// counter-rotating arms of points on an expanding spiral.
//
// It is the one purely decorative thing on the screen, and it earns its place
// by being the fastest possible answer to "is this program alive?". A frozen
// swirl says the render loop has stopped long before any number would.
func Storm(cv *Canvas, r Rect, t *Theme, phase float64, active bool) {
	if r.W < 10 || r.H < 4 {
		return
	}
	g := newBrailleGrid(r.W, r.H)
	cx, cy := float64(g.subW)/2, float64(g.subH)/2
	rx, ry := cx-1, cy-1

	const arms = 3
	const perArm = 42
	for a := 0; a < arms; a++ {
		base := phase + float64(a)*2*math.Pi/arms
		for i := 0; i < perArm; i++ {
			f := float64(i) / perArm
			// A logarithmic spiral: the angle grows with the radius, which is
			// what makes the arms trail rather than radiate.
			ang := base + f*3.4
			rr := 0.18 + f*0.82
			x := cx + math.Cos(ang)*rr*rx
			y := cy + math.Sin(ang)*rr*ry
			g.set(int(x), int(y), f > 0.55)
		}
	}
	line, fill := t.Accent, t.Glow
	if !active {
		line, fill = t.Dim, t.Glow
	}
	g.draw(cv, r, line, fill)
}

// Mast draws the connection mark with its signal arcs, the arcs lighting up in
// sequence when there is a link and staying dark when there is not.
func Mast(cv *Canvas, r Rect, t *Theme, bars int, frame int, ok bool) {
	if r.W < 8 || r.H < len(MastArt)+1 {
		return
	}
	arcs := []string{"╭─╮", "╭───╮", "╭─────╮"}
	nArcs := r.H - len(MastArt)
	if nArcs > len(arcs) {
		nArcs = len(arcs)
	}
	if nArcs < 0 {
		nArcs = 0
	}

	top := r.Y + (r.H-len(MastArt)-nArcs)/2
	DrawArt(cv, r.X+(r.W-ArtWidth(MastArt))/2, top+nArcs, MastArt, Shade(t), t.Accent2)

	// The brightest arc steps outwards each frame, so a healthy link visibly
	// transmits rather than just being a static drawing of an aerial.
	live := 0
	if nArcs > 0 {
		live = (frame / 3) % nArcs
	}
	for i := 0; i < nArcs; i++ {
		a := arcs[i]
		w := Width(a)
		if w > r.W {
			break
		}
		y := top + nArcs - 1 - i
		fg := t.Dim
		if ok && i < bars {
			fg = t.Accent2
			if i == live {
				fg = t.Accent
			}
		}
		cv.Text(r.X+(r.W-w)/2, y, a, Style{FG: fg})
	}
}

// SignalBars is a five-segment strength meter, the same shape as a phone's.
func SignalBars(cv *Canvas, x, y int, lit int, t *Theme) int {
	const n = 5
	for i := 0; i < n; i++ {
		fg := t.Dim
		if i < lit {
			switch {
			case lit >= 4:
				fg = t.Good
			case lit >= 2:
				fg = t.Warn
			default:
				fg = t.Err
			}
		}
		cv.Set(x+i, y, Spark[(i+1)*len(Spark)/n-1], Style{FG: fg})
	}
	return n
}

// CenterArt stamps a fixed piece centred in r.
func CenterArt(cv *Canvas, r Rect, art []string, t *Theme, def string) {
	w, h := ArtWidth(art), len(art)
	DrawArt(cv, r.X+(r.W-w)/2, r.Y+(r.H-h)/2, art, Shade(t), def)
}

// Banner renders the wordmark at the largest size that fits in w columns and h
// rows, and reports how many rows it used.
//
// Both dimensions, because they run out at different window sizes: a 100x30
// window has the width for the large wordmark and not the height, and a banner
// that checked only the width would draw five rows into four and write its
// last row over the run header.
func Banner(cv *Canvas, x, y, w, h int, t *Theme) int {
	draw := func(art []string, second int) int {
		cx := x + (w-ArtWidth(art))/2
		for i, line := range art {
			// A vertical gradient from cyan into violet, one row at a time. It
			// is the cheapest thing in this file and it is what makes the
			// wordmark look lit rather than printed.
			fg := t.Accent
			if i >= second {
				fg = t.Accent2
			}
			cv.Text(cx, y+i, line, Style{FG: fg, Bold: true})
		}
		return len(art)
	}
	switch {
	case h >= len(WordmarkLarge) && w >= ArtWidth(WordmarkLarge):
		return draw(WordmarkLarge, 3)
	case h >= len(WordmarkSmall) && w >= ArtWidth(WordmarkSmall):
		return draw(WordmarkSmall, 2)
	default:
		cv.TextCenter(x, y, w, "DEROSTORM", Style{FG: t.Accent, Bold: true})
		return 1
	}
}

// TrackedCaps spaces out a short string, the way the reference sets its
// headings. Only worth it on a heading: on a value it would break the eye's
// ability to read a number as one thing.
func TrackedCaps(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}
