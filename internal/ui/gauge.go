package ui

// Gauges: bars, ring gauges, arcs, thermometers and activity spectra.
//
// The ring and arc gauges are drawn by rasterising a circle rather than by
// stamping a fixed piece of art. That costs a little arithmetic per frame and
// buys the thing a fixed drawing cannot have: the same gauge is legible at
// nine cells wide in a cramped window and at nineteen on a wide one, and the
// filled arc is genuinely proportional at every size instead of snapping to
// whichever eight positions the art happened to have.

import (
	"math"
	"strings"
)

// Bar renders frac of width as a horizontal bar, to the eighth of a column.
//
// Eighths matter more than they look. Whole cells give a 20-cell bar twenty
// distinct values, so two GPUs three percent apart draw identically and the
// bar stops being able to answer the question it is there for.
func Bar(frac float64, width int) string {
	if width < 1 {
		return ""
	}
	frac = clamp01(frac)
	eighths := int(frac*float64(width)*8 + 0.5)
	full, rest := eighths/8, eighths%8

	var sb strings.Builder
	sb.Grow(width * 3)
	n := 0
	for ; n < full && n < width; n++ {
		sb.WriteRune(BlockFull)
	}
	if rest > 0 && n < width {
		sb.WriteRune(BarParts[rest-1])
		n++
	}
	for ; n < width; n++ {
		sb.WriteRune(BlockLight)
	}
	return sb.String()
}

// DrawBar puts a Bar on the canvas with the filled part and the track in
// different colours, which a single-colour string cannot do: ░ in the accent
// colour reads as a dim version of the value rather than as empty track.
func DrawBar(cv *Canvas, x, y, w int, frac float64, fg, track string) {
	if w < 1 {
		return
	}
	frac = clamp01(frac)
	eighths := int(frac*float64(w)*8 + 0.5)
	full, rest := eighths/8, eighths%8
	i := 0
	for ; i < full && i < w; i++ {
		cv.Set(x+i, y, BlockFull, Style{FG: fg})
	}
	if rest > 0 && i < w {
		cv.Set(x+i, y, BarParts[rest-1], Style{FG: fg})
		i++
	}
	for ; i < w; i++ {
		cv.Set(x+i, y, BlockLight, Style{FG: track})
	}
}

// MeterRow is the labelled bar used for the CPU/GPU split and the thread list:
// name, bar, and a right-aligned figure. Returns nothing; it owns its row.
//
// The right-hand columns are laid out from the right edge inwards at fixed
// widths and the bar absorbs the difference. Laying out from the left instead
// produces a table whose last column shifts as the numbers change width, which
// is the single most common way a terminal dashboard looks amateur.
func MeterRow(cv *Canvas, r Rect, y int, name string, frac float64, value string, t *Theme, fg string) {
	if y < r.Y || y >= r.Bottom() || r.W < 8 {
		return
	}
	nameW := Width(name)
	if nameW > r.W/3 {
		nameW = r.W / 3
	}
	valW := Width(value)
	barW := r.W - nameW - valW - 2
	if barW < 1 {
		cv.TextIn(r.X, y, r.W, name, Style{FG: t.Muted})
		return
	}
	cv.TextIn(r.X, y, nameW, name, Style{FG: t.Muted})
	DrawBar(cv, r.X+nameW+1, y, barW, frac, fg, t.Dim)
	cv.TextRight(r.Right(), y, value, Style{FG: t.Text})
}

// ---------------------------------------------------------------- ring gauge

// RingOpts describes one ring or arc gauge.
type RingOpts struct {
	Frac float64 // 0..1 of the sweep that is filled

	// Start and Sweep are degrees, clockwise, zero at the top. A full donut is
	// Start 0, Sweep 360. The open-bottomed arc used for a load meter is
	// Start 225, Sweep 270.
	Start, Sweep float64

	// Inner is the hole radius as a fraction of the outer radius. Below about
	// 0.5 the ring stops reading as a ring and starts reading as a pie, and
	// there is then no room left for the number in the middle.
	Inner float64

	FG    string // the filled arc
	Track string // the unfilled remainder of the arc
	// Lines are centred inside the hole, the value first.
	Lines []string
	// LineFG parallels Lines; a missing or empty entry falls back to FG.
	LineFG []string
}

// Ring rasterises a ring gauge into r.
//
// Half-block characters double the vertical resolution, which is what makes a
// circle in a grid of cells that are twice as tall as they are wide come out
// round. Where the two halves of a cell differ, the cell is drawn as an upper
// half-block with the lower half carried as a background colour -- the only
// place in this program that paints a background, and it never extends beyond
// the ring itself.
func Ring(cv *Canvas, r Rect, t *Theme, o RingOpts) {
	if r.W < 5 || r.H < 3 {
		return
	}
	if o.Sweep == 0 {
		o.Sweep = 360
	}
	if o.Inner <= 0 {
		o.Inner = 0.66
	}
	if o.FG == "" {
		o.FG = t.Accent
	}
	if o.Track == "" {
		o.Track = t.Dim
	}
	frac := clamp01(o.Frac)

	// Sub-rows: two per cell. The ellipse is fitted to whichever axis runs out
	// first, so the gauge stays circular in a rectangle of any shape rather
	// than stretching to fill it.
	subH := r.H * 2
	cx, cy := float64(r.W)/2, float64(subH)/2
	rad := math.Min(cx, cy)
	if rad < 2 {
		return
	}
	inner := rad * o.Inner
	// Half a cell of tolerance, so the ring is a solid line rather than a
	// dotted one where the annulus falls between sample points.
	outer := rad - 0.15

	filled := func(sx, sy int) (on bool, isFill bool) {
		dx := float64(sx) + 0.5 - cx
		dy := float64(sy) + 0.5 - cy
		d := math.Hypot(dx, dy)
		if d < inner || d > outer {
			return false, false
		}
		// Degrees clockwise from straight up.
		deg := math.Atan2(dx, -dy) * 180 / math.Pi
		if deg < 0 {
			deg += 360
		}
		rel := deg - o.Start
		for rel < 0 {
			rel += 360
		}
		if rel > o.Sweep {
			return false, false
		}
		return true, rel <= o.Sweep*frac
	}

	for cyi := 0; cyi < r.H; cyi++ {
		for cxi := 0; cxi < r.W; cxi++ {
			topOn, topFill := filled(cxi, cyi*2)
			botOn, botFill := filled(cxi, cyi*2+1)
			x, y := r.X+cxi, r.Y+cyi
			colour := func(fill bool) string {
				if fill {
					return o.FG
				}
				return o.Track
			}
			switch {
			case !topOn && !botOn:
				// leave the terminal's own background showing through
			case topOn && botOn && topFill == botFill:
				cv.Set(x, y, BlockFull, Style{FG: colour(topFill)})
			case topOn && botOn:
				cv.Set(x, y, HalfTop, Style{FG: colour(topFill), BG: bgOf(colour(botFill))})
			case topOn:
				cv.Set(x, y, HalfTop, Style{FG: colour(topFill)})
			default:
				cv.Set(x, y, HalfBottom, Style{FG: colour(botFill)})
			}
		}
	}

	// Text in the hole, vertically centred.
	if len(o.Lines) == 0 {
		return
	}
	n := len(o.Lines)
	top := r.Y + (r.H-n)/2
	for i, s := range o.Lines {
		fg := o.FG
		if i < len(o.LineFG) && o.LineFG[i] != "" {
			fg = o.LineFG[i]
		}
		bold := i == 0
		cv.TextCenter(r.X, top+i, r.W, Clip(s, r.W-2), Style{FG: fg, Bold: bold})
	}
}

// bgOf turns a foreground escape into the matching background escape. The two
// differ only in the leading 38 versus 48, so this is a substitution rather
// than a colour conversion, and an empty (mono) token stays empty.
func bgOf(fg string) string {
	if len(fg) < 6 || !strings.HasPrefix(fg, "\x1b[38;") {
		return ""
	}
	return "\x1b[48;" + fg[5:]
}

// ---------------------------------------------------------------- spectrum

// Spectrum is the activity histogram drawn under the CPU and GPU panels: one
// eighth-height block per sample, most recent on the right.
//
// It is deliberately not a line chart. At four rows and forty columns a line
// would be three distinguishable heights; a bar per sample reads as texture,
// which is the honest thing to show for a signal whose shape matters and whose
// exact values are already printed above it.
func Spectrum(cv *Canvas, r Rect, vals []float64, t *Theme, fg string) {
	if r.Empty() {
		return
	}
	if fg == "" {
		fg = t.Accent
	}
	if len(vals) > r.W {
		vals = vals[len(vals)-r.W:]
	}
	max, min := 0.0, math.Inf(1)
	for _, v := range vals {
		if v > max {
			max = v
		}
		if v < min {
			min = v
		}
	}
	if max <= 0 {
		return
	}

	// The floor of the strip, which is not zero.
	//
	// A rig holding a steady rate produces samples within a few per cent of
	// each other. Measured against zero every one of them rounds to the same
	// number of whole cells, and a two-row spectrum comes out as a solid block
	// with a ripple along its top -- which is the one thing a display whose
	// entire job is to show variation must not do.
	//
	// So the strip is scaled to the range actually present, with a floor under
	// how far it will stretch: below a spread of 15% the scale stops opening
	// up, so a genuinely flat line stays visibly flat instead of being
	// amplified into a mountain range.
	span := max - min
	if quiet := max * 0.15; span < quiet {
		span = quiet
	}
	// A little more range than the data uses, so the tallest bar stops short
	// of the ceiling and a new peak has somewhere to go.
	base := max - span/0.85
	if base < 0 {
		base = 0
	}

	// Right-aligned, so a partly-filled history grows leftwards from "now"
	// instead of sitting at the left edge and appearing to be old data.
	x0 := r.Right() - len(vals)
	steps := r.H * 8
	for i, v := range vals {
		h := int((v - base) / (max - base) * float64(steps))
		if h < 1 && v > 0 {
			h = 1
		}
		if h > steps {
			h = steps
		}
		x := x0 + i
		for row := 0; row < r.H; row++ {
			// Rows are filled bottom-up.
			y := r.Bottom() - 1 - row
			left := h - row*8
			switch {
			case left <= 0:
				// nothing in this cell
			case left >= 8:
				cv.Set(x, y, BlockFull, Style{FG: fg})
			default:
				cv.Set(x, y, Spark[left-1], Style{FG: fg})
			}
		}
	}
}

// Sparkline is one row of eighth-height blocks, shaded in three bands so a dip
// is legible without the row becoming a rainbow.
func Sparkline(cv *Canvas, x, y, w int, vals []float64, t *Theme) {
	if w < 1 {
		return
	}
	if len(vals) > w {
		vals = vals[len(vals)-w:]
	}
	max := 0.0
	for _, v := range vals {
		if v > max {
			max = v
		}
	}
	x0 := x + w - len(vals)
	for i, v := range vals {
		idx := 0
		if max > 0 {
			idx = int(v / max * float64(len(Spark)-1))
		}
		if idx < 0 {
			idx = 0
		}
		if idx >= len(Spark) {
			idx = len(Spark) - 1
		}
		fg := t.Dim
		switch {
		case idx >= 6:
			fg = t.Accent
		case idx >= 3:
			fg = t.Accent2
		}
		cv.Set(x0+i, y, Spark[idx], Style{FG: fg})
	}
}

// ---------------------------------------------------------------- thermometer

// Thermometer draws a small bulb thermometer with a column filled in
// proportion to the reading, and colours it by band.
//
// scaleMin and scaleMax bound the column. They are not the sensor's range,
// they are the range a person cares about: a CPU between 30 and 100 degrees.
// A thermometer scaled from absolute zero would never move.
func Thermometer(cv *Canvas, x, y, h int, tempC float64, have bool, t *Theme) {
	if h < 4 {
		h = 4
	}
	frame := Style{FG: t.Dim}
	stemH := h - 1

	// stem
	for i := 0; i < stemH; i++ {
		cv.Set(x, y+i, lineV, frame)
		cv.Set(x+3, y+i, lineV, frame)
	}
	cv.Set(x, y, '╭', frame)
	cv.Set(x+1, y, lineH, frame)
	cv.Set(x+2, y, lineH, frame)
	cv.Set(x+3, y, '╮', frame)
	// bulb
	cv.Set(x, y+stemH, '╰', frame)
	cv.Set(x+3, y+stemH, '╯', frame)
	cv.Set(x+1, y+stemH, lineH, frame)
	cv.Set(x+2, y+stemH, lineH, frame)

	if !have {
		cv.TextCenter(x, y+stemH/2, 4, NA, Style{FG: t.Dim})
		return
	}

	const scaleMin, scaleMax = 30.0, 100.0
	frac := clamp01((tempC - scaleMin) / (scaleMax - scaleMin))
	fg := TempColour(t, tempC)
	fillRows := int(frac*float64(stemH-1) + 0.5)
	for i := 0; i < fillRows; i++ {
		yy := y + stemH - 1 - i
		cv.Set(x+1, yy, BlockFull, Style{FG: fg})
		cv.Set(x+2, yy, BlockFull, Style{FG: fg})
	}
	// The bulb is always full: a thermometer with an empty bulb reads as
	// broken rather than as cold.
	cv.Set(x+1, y+stemH, BlockFull, Style{FG: fg})
	cv.Set(x+2, y+stemH, BlockFull, Style{FG: fg})
}

// Tank is a vertical fill gauge in a box: the same visual language as the
// thermometer, for a quantity that is a proportion of a capacity rather than a
// position on a scale. Memory is the case it exists for.
func Tank(cv *Canvas, x, y, w, h int, frac float64, t *Theme, fg string) {
	if w < 3 || h < 3 {
		return
	}
	frame := Style{FG: t.Dim}
	cv.Set(x, y, '┌', frame)
	cv.Set(x+w-1, y, '┐', frame)
	cv.Set(x, y+h-1, '└', frame)
	cv.Set(x+w-1, y+h-1, '┘', frame)
	cv.HLine(x+1, y, w-2, lineH, frame)
	cv.HLine(x+1, y+h-1, w-2, lineH, frame)
	cv.VLine(x, y+1, h-2, lineV, frame)
	cv.VLine(x+w-1, y+1, h-2, lineV, frame)

	inner := h - 2
	if inner < 1 {
		return
	}
	filled := int(clamp01(frac)*float64(inner) + 0.5)
	for i := 0; i < inner; i++ {
		row := y + h - 2 - i
		glyph, st := BlockLight, Style{FG: t.Dim}
		if i < filled {
			glyph, st = BlockFull, Style{FG: fg}
		}
		for c := 1; c < w-1; c++ {
			cv.Set(x+c, row, glyph, st)
		}
	}
}

// TempColour bands a temperature so a glance is enough. The boundaries are
// deliberately conservative: 65 is where a card starts trading clocks for
// safety, and 80 is where most of them are throttling in earnest.
func TempColour(t *Theme, c float64) string {
	switch {
	case c <= -273:
		return t.Dim
	case c < 65:
		return t.Good
	case c < 80:
		return t.Warn
	default:
		return t.Err
	}
}

func clamp01(v float64) float64 {
	if v < 0 || math.IsNaN(v) {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
