package ui

// The storm identity: the DERO mark, the ridgeline, the moon, the gradient
// wordmark and the wireframe block.
//
// Everything here is drawn from a table or from a handful of sine terms. There
// is no image protocol, no sixel, no half-block photograph -- a terminal that
// cannot show those is not a terminal running a degraded version of this
// dashboard, it is the only kind of terminal this dashboard is for.
//
// The ridgelines are the one piece that could have been expensive and is not.
// A mountain profile is a fixed function of x, so it is evaluated once per
// column and the result is a column height; nothing is sampled, nothing is
// searched, and the whole range across a 170-column header costs a hundred and
// seventy calls to a four-term sine.

import (
	"math"
	"strings"
)

// DeroMark is the corner mark: a blocky glyph in the shape of the DERO badge.
//
// Pixel-art rather than an outline, because at six rows an outline is two
// rows of curve and four rows of nothing, and the block characters are the
// only ones whose shape a terminal cannot get wrong.
var DeroMark = []string{
	"▟█▙     ▟█▙",
	"███▄▄▄▄▄███",
	"██▀▀▀▀▀▀▀██",
	"██ ▒▒▒▒▒ ██",
	"███▄▄▄▄▄███",
	"▜█▛ ▀▀▀ ▜█▛",
}

// DeroMarkSmall is the same badge at three rows, for a header that has given
// its height to the panels.
var DeroMarkSmall = []string{
	"▟█▙   ▟█▙",
	"███▄▄▄███",
	"▜█▛ ▀ ▜█▛",
}

// CubeWire is the blockchain mark: an isometric block drawn as an outline, so
// it reads as a wire model of a block rather than as a solid lump.
var CubeWire = []string{
	"   ╱▔▔▔▔▔╲   ",
	" ╱▔       ▔╲ ",
	"▕  ╲     ╱  ▏",
	"▕    ▚ ▞    ▏",
	"▕    ▞ ▚    ▏",
	"▕  ╱     ╲  ▏",
	" ╲▁       ▁╱ ",
	"   ╲▁▁▁▁▁╱   ",
}

// ---------------------------------------------------------------- gradient wordmark

// GradientArt stamps a piece of art with a horizontal colour ramp across its
// width, so the wordmark runs cyan on the left into magenta on the right.
//
// The ramp is indexed, not computed: Theme.Ramp hands back a precomputed
// escape string, so a 76-cell wordmark costs 76 map-free lookups and no
// allocation at all. That is what makes it affordable to redraw whenever the
// header is invalidated rather than caching a rendered picture.
func GradientArt(cv *Canvas, x, y int, art []string, t *Theme, bold bool) {
	w := ArtWidth(art)
	if w < 1 {
		return
	}
	for dy, line := range art {
		dx := 0
		for _, r := range line {
			if r != ' ' {
				cv.Set(x+dx, y+dy, r, Style{FG: t.Ramp(float64(dx) / float64(w-1)), Bold: bold})
			}
			dx += RuneWidth(r)
		}
	}
}

// ---------------------------------------------------------------- ridgeline

// ridge is the height profile of the mountains, in [0,1], at position u across
// the range.
//
// Four sine terms at incommensurate frequencies, which is the cheapest way to
// get a profile that never repeats across any width a terminal can be and
// still looks like rock rather than like a wave. The seed shifts the phase, so
// two ranges drawn on the same screen are not the same range twice.
func ridge(u float64, seed float64) float64 {
	v := 0.50*math.Sin(u*3.1+seed) +
		0.26*math.Sin(u*7.7+seed*2.3+1.1) +
		0.14*math.Sin(u*17.3+seed*0.7+2.7) +
		0.10*math.Sin(u*31.1+seed*1.9+0.4)
	// Fold to [0,1] and bias downwards: a skyline whose average is halfway up
	// the box reads as a wall, not as a horizon.
	return clamp01(0.42 + v*0.34)
}

// Ridgeline draws a mountain silhouette across r, filled from the profile
// down. Two ranges are drawn, the far one in the faint layer, which is what
// gives a six-row strip any sense of depth at all.
//
// Used behind the header and inside the storm panel. It is the only decorative
// element that spans a whole panel, so it is drawn in Glow: present when
// looked for, invisible when read past.
func Ridgeline(cv *Canvas, r Rect, t *Theme, seed float64, near, far string) {
	if r.W < 4 || r.H < 2 {
		return
	}
	if near == "" {
		near = t.Glow
	}
	if far == "" {
		far = t.Glow
	}
	// Half-cell vertical resolution: a peak can land on the top half of a cell
	// and be drawn with ▄ below it, which doubles the apparent height
	// resolution for nothing.
	rows := r.H
	for i := 0; i < r.W; i++ {
		u := float64(i) / float64(maxi(r.W-1, 1))

		drawCol := func(h float64, fg string, solid bool) {
			// h is the fraction of the strip the peak reaches.
			half := int(h*float64(rows)*2 + 0.5)
			if half < 1 {
				return
			}
			topCell := rows - (half+1)/2
			if topCell < 0 {
				topCell = 0
			}
			// The topmost cell is a half block when the peak lands mid-cell.
			glyph := BlockFull
			if half%2 == 1 {
				glyph = HalfBottom
			}
			if !solid {
				glyph = '▄'
				if half%2 == 0 {
					glyph = '█'
				}
			}
			cv.Set(r.X+i, r.Y+topCell, glyph, Style{FG: fg})
			for y := topCell + 1; y < rows; y++ {
				cv.Set(r.X+i, r.Y+y, BlockFull, Style{FG: fg})
			}
		}

		drawCol(ridge(u, seed+4.2)*0.72, far, true)
		drawCol(ridge(u, seed), near, true)
	}
}

// RidgeWire is the ridgeline as a braille outline rather than a silhouette:
// two crests and a light lattice under them, which is the shape the reference
// artwork uses inside the storm panel.
//
// A wireframe rather than a fill, because this one sits behind a caption and a
// filled mountain would either swallow the text or have to be drawn so faintly
// that it stopped being a mountain.
func RidgeWire(cv *Canvas, r Rect, t *Theme, seed float64) {
	if r.W < 8 || r.H < 3 {
		return
	}
	g := newBrailleGrid(r.W, r.H)
	sw, sh := g.subW, g.subH

	// Two crests and nothing else. A lattice was tried under them and is
	// wrong here: this sits behind a caption, and any mesh dense enough to
	// read as a wireframe is dense enough to make the words on top of it
	// unreadable. Two ridges at different depths give the same sense of
	// distance for a twentieth of the ink.
	for x := 0; x < sw; x++ {
		u := float64(x) / float64(maxi(sw-1, 1))
		far := int(float64(sh-1) * (1 - ridge(u, seed+4.2)*0.55))
		near := int(float64(sh-1) * (1 - ridge(u, seed)*0.86))
		g.set(x, far, false)
		g.set(x, near, true)
	}
	g.draw(cv, r, t.Accent2, t.Glow)
}

// ---------------------------------------------------------------- moon

// MoonArt is the header's moon: a disc with a couple of maria in it.
//
// The craters are what stop it reading as a full stop. A plain filled circle
// six rows tall is a dot; two darker patches placed off-centre are a moon, and
// they cost two characters.
var MoonArt = []string{
	"  ▄███▄  ",
	" ███████ ",
	"███▒█████",
	"█████▒███",
	" ██▒████ ",
	"  ▀███▀  ",
}

// MoonPalette shades the moon: the body in the quiet layer, the maria darker
// still, so it sits behind everything rather than competing with the wordmark.
func MoonPalette(t *Theme) ArtPalette {
	return ArtPalette{'█': t.Dim, '▒': t.Glow, '▄': t.Dim, '▀': t.Dim}
}

// ---------------------------------------------------------------- wire cube

// WireCube draws a rotating wireframe cube in braille.
//
// Twelve edges, projected and walked. It turns on one axis and tilts on
// another by a fixed amount, because a cube rotating about a single axis
// through its centre presents a face straight on twice a turn and momentarily
// stops looking like a cube at all.
//
// The whole thing is about four hundred braille sets per frame at panel size,
// which is why the caller is expected to advance the phase a few times a
// second rather than every frame: the cost is not the arithmetic, it is that
// a cube spinning at eight frames a second is a distraction on a screen
// somebody is trying to read numbers off.
func WireCube(cv *Canvas, r Rect, t *Theme, phase float64) {
	if r.W < 6 || r.H < 4 {
		return
	}
	g := newBrailleGrid(r.W, r.H)
	cx, cy := float64(g.subW)/2, float64(g.subH)/2
	rad := math.Min(cx, cy) * 0.78
	if rad < 2 {
		return
	}

	const tilt = 0.42
	sinA, cosA := math.Sin(phase), math.Cos(phase)
	sinB, cosB := math.Sin(tilt), math.Cos(tilt)

	type pt struct{ x, y, z float64 }
	var v [8]pt
	for i := 0; i < 8; i++ {
		x := float64(i&1)*2 - 1
		y := float64((i>>1)&1)*2 - 1
		z := float64((i>>2)&1)*2 - 1
		// Yaw, then pitch.
		x, z = x*cosA-z*sinA, x*sinA+z*cosA
		y, z = y*cosB-z*sinB, y*sinB+z*cosB
		v[i] = pt{x, y, z}
	}
	// The projection is normalised against the widest the eight corners
	// actually get, rather than against the cube's own radius. A unit cube
	// seen corner-on is 1.73 times as wide as face-on, so a fixed scale is
	// either too small for most of the turn or spills off the panel for part
	// of it -- and a wireframe clipped by the panel edge stops reading as a
	// solid at all.
	persp := func(z float64) float64 { return 1.0 / (2.8 - z*0.5) }
	span := 0.0
	for _, q := range v {
		s := persp(q.z)
		if a := math.Abs(q.x * s); a > span {
			span = a
		}
		if a := math.Abs(q.y * s); a > span {
			span = a
		}
	}
	if span <= 0 {
		return
	}
	// One scale for both axes. A braille cell is two sub-columns wide and
	// four sub-rows tall, and a terminal cell is about twice as tall as it is
	// wide, so a braille sub-pixel is very nearly square and a cube drawn with
	// equal scales comes out square on screen.
	scale := math.Min(cx-1, cy-1) / span
	proj := func(p pt) (int, int, bool) {
		s := persp(p.z)
		return int(cx + p.x*s*scale), int(cy + p.y*s*scale), p.z > 0
	}

	edges := [12][2]int{
		{0, 1}, {1, 3}, {3, 2}, {2, 0},
		{4, 5}, {5, 7}, {7, 6}, {6, 4},
		{0, 4}, {1, 5}, {2, 6}, {3, 7},
	}
	for _, e := range edges {
		x0, y0, n0 := proj(v[e[0]])
		x1, y1, n1 := proj(v[e[1]])
		near := n0 && n1
		steps := maxi(absInt(x1-x0), absInt(y1-y0))
		if steps < 1 {
			steps = 1
		}
		for i := 0; i <= steps; i++ {
			f := float64(i) / float64(steps)
			g.set(int(float64(x0)+(float64(x1-x0))*f), int(float64(y0)+(float64(y1-y0))*f), near)
		}
	}
	g.draw(cv, r, t.Accent2, t.Glow)
}

// ---------------------------------------------------------------- chrome

// Chevrons is the run of leaning strokes the reference puts after a heading.
// Purely typographic, and the one place a decoration is allowed to mean
// nothing at all.
func Chevrons(cv *Canvas, x, y, n int, t *Theme) int {
	for i := 0; i < n; i++ {
		cv.Set(x+i, y, '╱', Style{FG: t.Ramp(float64(i) / float64(maxi(n-1, 1)))})
	}
	return n
}

// Tagline is a stacked block of small capitals, right- or left-aligned. The
// header uses two of them: the DERO values on the left and what this program
// is on the right.
func Tagline(cv *Canvas, x, y, w int, lines []string, t *Theme, right bool) {
	for i, l := range lines {
		st := Style{FG: t.Muted}
		if i == 0 {
			st = Style{FG: t.Accent2, Bold: true}
		}
		if right {
			cv.TextRight(x+w, y+i, Clip(l, w), st)
		} else {
			cv.TextIn(x, y+i, w, l, st)
		}
	}
}

// Swatch is the small filled square that stands in front of a legend entry.
// Two cells, because one is a sliver and three is a bar.
func Swatch(cv *Canvas, x, y int, fg string) int {
	cv.Set(x, y, BlockFull, Style{FG: fg})
	cv.Set(x+1, y, BlockFull, Style{FG: fg})
	return 2
}

// Tabs draws a row of small labels with one of them selected, the way the
// history panel offers its time ranges.
//
// The selected tab is painted rather than merely brightened. On a screen with
// this many bright things on it a colour change alone does not read as "this
// one is chosen"; a filled block does, and it survives the drop to sixteen
// colours where several of the accents collapse onto each other.
func Tabs(cv *Canvas, x, y int, labels []string, sel int, t *Theme) int {
	w := 0
	for i, l := range labels {
		lw := Width(l) + 2
		st := Style{FG: t.Muted}
		if i == sel {
			st = Style{FG: t.Accent, BG: t.SelBG, Bold: true}
		}
		for j := 0; j < lw; j++ {
			cv.Set(x+w+j, y, ' ', st)
		}
		cv.Text(x+w+1, y, l, st)
		w += lw
	}
	return w
}

// TrackedCapsN is TrackedCaps with the gap chosen by the caller, for a header
// that has room to breathe.
func TrackedCapsN(s string, gap int) string {
	if gap < 1 {
		return s
	}
	var b strings.Builder
	pad := strings.Repeat(" ", gap)
	for i, r := range s {
		if i > 0 {
			b.WriteString(pad)
		}
		b.WriteRune(r)
	}
	return b.String()
}

func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}
