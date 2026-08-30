package ui

// The live line chart.
//
// Braille, because it is the only way a terminal gets a curve. A braille cell
// carries a 2x4 grid of independently settable dots, so a chart drawn this way
// has eight times the vertical resolution and twice the horizontal resolution
// of one drawn with block characters -- which is the difference between a
// hashrate trace that shows a two-percent dip and one that shows a flat line
// with an occasional step in it.
//
// The cost of braille is that a cell has one colour for all eight of its dots.
// That is handled below by deciding the colour per cell rather than per dot:
// any cell the line passes through is drawn in the line colour, and cells that
// only contain area fill are drawn in the quieter one.

import (
	"math"
)

// brailleBase is U+2800, the empty braille pattern. Dots are set by adding a
// bit; see brailleBit for which bit is which.
const brailleBase = 0x2800

// brailleBit maps a sub-cell to its bit. The braille block numbers its dots in
// two columns of four, but the low six bits are the historic 2x3 cell and the
// bottom row was bolted on afterwards as bits 6 and 7 -- so the row-4 case
// cannot be folded into the arithmetic for the others.
func brailleBit(col, row int) byte {
	if row < 3 {
		return 1 << uint(col*3+row)
	}
	return 1 << uint(6+col)
}

// brailleGrid is a sub-pixel bitmap that renders as braille cells.
type brailleGrid struct {
	w, h  int    // in cells
	dots  []byte // one byte of dot bits per cell
	line  []bool // true where the cell contains part of the line itself
	subW  int
	subH  int
	haveD bool
}

func newBrailleGrid(w, h int) *brailleGrid {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return &brailleGrid{w: w, h: h, dots: make([]byte, w*h), line: make([]bool, w*h), subW: w * 2, subH: h * 4}
}

// set turns on one sub-pixel. isLine marks the cell as carrying the trace, so
// the renderer can colour it differently from a cell holding only fill.
func (g *brailleGrid) set(sx, sy int, isLine bool) {
	if sx < 0 || sy < 0 || sx >= g.subW || sy >= g.subH {
		return
	}
	cx, cy := sx/2, sy/4
	i := cy*g.w + cx
	g.dots[i] |= brailleBit(sx%2, sy%4)
	if isLine {
		g.line[i] = true
	}
	g.haveD = true
}

func (g *brailleGrid) draw(cv *Canvas, r Rect, lineFG, fillFG string) {
	for cy := 0; cy < g.h; cy++ {
		for cx := 0; cx < g.w; cx++ {
			i := cy*g.w + cx
			if g.dots[i] == 0 {
				continue
			}
			fg := fillFG
			if g.line[i] {
				fg = lineFG
			}
			cv.Set(r.X+cx, r.Y+cy, rune(brailleBase+int(g.dots[i])), Style{FG: fg})
		}
	}
}

// ChartOpts describes one line chart.
type ChartOpts struct {
	Values []float64 // oldest first
	// XLabels are drawn along the bottom, spread evenly across the plot. The
	// last one is right-aligned against the plot's right edge, because it
	// names "now" and "now" is the point a reader looks for first.
	XLabels []string
	// Format turns an axis value into its label. Defaults to ShortNum.
	Format func(float64) string
	// Fill shades the area under the trace. Off for a chart small enough that
	// the fill would be most of it.
	Fill bool
	// Baseline forces the Y axis to start at zero. On by default for a
	// hashrate: an auto-scaled floor makes normal jitter look like a collapse,
	// which is exactly the wrong thing for the number that says whether the
	// machine is healthy.
	NoBaseline bool
	LineFG     string
	FillFG     string
	// Empty is what to say when there is not enough data yet.
	Empty string
}

// Chart draws a line chart with a labelled Y axis, an axis rule and X labels.
// It takes the whole rectangle it is given, including the space for the axes.
func Chart(cv *Canvas, r Rect, t *Theme, o ChartOpts) {
	if r.W < 10 || r.H < 4 {
		return
	}
	if o.Format == nil {
		o.Format = ShortNum
	}
	if o.LineFG == "" {
		o.LineFG = t.Accent
	}
	if o.FillFG == "" {
		o.FillFG = t.Glow
	}
	if o.Empty == "" {
		o.Empty = "collecting samples…"
	}
	// The area fill is a colour, not a shape: it is the trace drawn in the
	// quiet accent. With no colour to draw it in there is nothing to tell it
	// apart from the trace, and it turns the plot into noise -- so on a mono
	// terminal the chart is the line alone.
	if t.Mono() {
		o.Fill = false
	}

	// The X labels get a row of their own where there is room for one. Sharing
	// the rule's row works, but the rule then runs through the gaps between the
	// labels and the whole axis reads as a dashed line with words in it.
	xAxisH := 0
	switch {
	case len(o.XLabels) == 0:
	case r.H >= 8:
		xAxisH = 2
	case r.H >= 6:
		xAxisH = 1
	}

	// Scale first, because the Y labels decide how wide the axis gutter is and
	// therefore how wide the plot is.
	lo, hi := 0.0, 0.0
	for _, v := range o.Values {
		if v > hi {
			hi = v
		}
	}
	if !o.NoBaseline {
		lo = 0
	} else {
		lo = hi
		for _, v := range o.Values {
			if v < lo {
				lo = v
			}
		}
	}
	if hi <= lo {
		hi = lo + 1
	}

	maxTicks := (r.H - xAxisH - 1) / 2
	if maxTicks < 2 {
		maxTicks = 2
	}
	if maxTicks > 6 {
		maxTicks = 6
	}
	nTicks, hi := niceAxis(lo, hi, maxTicks)

	labelW := 0
	labels := make([]string, nTicks+1)
	for i := range labels {
		v := lo + (hi-lo)*float64(nTicks-i)/float64(nTicks)
		labels[i] = o.Format(v)
		if w := Width(labels[i]); w > labelW {
			labelW = w
		}
	}
	if labelW > r.W/4 {
		labelW = 0 // no room for an axis; the trace is worth more than its labels
	}

	gutter := 0
	if labelW > 0 {
		gutter = labelW + 1 // labels plus the tick column
	}
	plot := Rect{r.X + gutter, r.Y, r.W - gutter, r.H - xAxisH}
	if plot.W < 4 {
		return
	}

	// Y axis: labels and the rule they attach to.
	if gutter > 0 {
		axis := Style{FG: t.Border}
		for y := plot.Y; y < plot.Bottom(); y++ {
			cv.Set(plot.X-1, y, lineV, axis)
		}
		for i, s := range labels {
			y := plot.Y + i*(plot.H-1)/nTicks
			cv.TextRight(plot.X-2, y, s, Style{FG: t.Muted})
			cv.Set(plot.X-1, y, lineTeeR, axis)
		}
	}

	// X axis rule and labels.
	if xAxisH > 0 {
		axis := Style{FG: t.Border}
		y := plot.Bottom()
		cv.HLine(plot.X, y, plot.W, lineH, axis)
		if gutter > 0 {
			cv.Set(plot.X-1, y, '└', axis)
		}
		drawXLabels(cv, Rect{plot.X, y + xAxisH - 1, plot.W, 1}, o.XLabels, t)
	}

	if len(o.Values) < 2 {
		cv.TextCenter(plot.X, plot.Y+plot.H/2, plot.W, o.Empty, Style{FG: t.Dim})
		return
	}

	// The trace. One sub-column per sub-pixel column, sampling the series so a
	// long history compresses rather than being cropped: a five-minute chart
	// should show five minutes, not the most recent forty seconds of it.
	g := newBrailleGrid(plot.W, plot.H)
	span := hi - lo
	yOf := func(v float64) int {
		f := (v - lo) / span
		y := int(math.Round((1 - clamp01(f)) * float64(g.subH-1)))
		if y < 0 {
			y = 0
		}
		if y >= g.subH {
			y = g.subH - 1
		}
		return y
	}

	prevY := -1
	for sx := 0; sx < g.subW; sx++ {
		// Nearest sample for this sub-column.
		idx := 0
		if g.subW > 1 {
			idx = int(math.Round(float64(sx) * float64(len(o.Values)-1) / float64(g.subW-1)))
		}
		y := yOf(o.Values[idx])

		if prevY >= 0 && absInt(y-prevY) > 1 {
			// Join to the previous column so a steep move is a line rather
			// than two disconnected dots.
			step := 1
			if y < prevY {
				step = -1
			}
			for yy := prevY + step; yy != y; yy += step {
				g.set(sx, yy, true)
			}
		}
		g.set(sx, y, true)
		prevY = y

		if o.Fill {
			// Every third sub-row, not every one. A solid fill under a trace
			// that sits near the top of the plot is a large block with a line
			// on it, and the shape -- the thing the chart is for -- stops being
			// what the eye lands on. A dither reads as shading instead.
			for yy := y + 2; yy < g.subH; yy += 3 {
				g.set(sx, yy, false)
			}
		}
	}
	g.draw(cv, plot, o.LineFG, o.FillFG)
}

// drawXLabels spreads labels across a row: the first left-aligned, the last
// right-aligned, the rest centred on their share of the width. Anchoring the
// ends rather than centring all of them is what stops "Now" from floating a
// few cells short of the edge it is supposed to name.
func drawXLabels(cv *Canvas, r Rect, labels []string, t *Theme) {
	n := len(labels)
	if n == 0 || r.W < 6 {
		return
	}
	st := Style{FG: t.Dim}
	for i, s := range labels {
		if s == "" {
			continue
		}
		w := Width(s)
		var x int
		switch {
		case i == 0:
			x = r.X
		case i == n-1:
			x = r.Right() - w
		default:
			x = r.X + i*(r.W-1)/(n-1) - w/2
		}
		if x < r.X {
			x = r.X
		}
		if x+w > r.Right() {
			x = r.Right() - w
		}
		cv.Text(x, r.Y, s, st)
	}
}

// niceAxis chooses how many ticks to draw and where the top of the scale goes.
//
// The naive version -- round the maximum up to the next power of ten and
// divide -- produces a hashrate chart labelled 0, 66.7K, 133.3K, 200K with the
// trace squashed into the bottom quarter. Both halves of that are wrong: the
// labels are not numbers anyone reads, and half the plot is empty.
//
// So the step is chosen first, from the set of intervals a person reads, and
// the tick count follows from it. Every candidate step that yields a legal
// number of ticks is tried and the one whose top sits closest above the data
// wins. For a machine at 111 KH/s in a panel with room for four ticks that is
// four steps of 30K: every label round, and the trace filling most of the plot.
//
// Choosing the tick count first and rounding the step up afterwards -- the
// obvious way round -- cannot do this. Rounding 28.4K up lands on 40K, and the
// axis tops out at 160K for data that reaches 111K.
func niceAxis(lo, max float64, maxTicks int) (int, float64) {
	span := max - lo
	if span <= 0 || maxTicks < 2 {
		return 2, lo + 1
	}
	// A little headroom, so the peak is not welded to the top rule.
	want := span * 1.03

	steps := []float64{1, 1.5, 2, 2.5, 3, 4, 5, 6, 8}
	base := math.Floor(math.Log10(want / float64(maxTicks)))

	bestN, bestHi := 0, math.Inf(1)
	for k := base; k <= base+2; k++ {
		mag := math.Pow(10, k)
		for _, c := range steps {
			step := c * mag
			n := int(math.Ceil(want/step - 1e-9))
			if n < 2 || n > maxTicks {
				continue
			}
			if hi := lo + step*float64(n); hi < bestHi {
				bestN, bestHi = n, hi
			}
		}
	}
	if bestN == 0 {
		return maxTicks, lo + span*1.1
	}
	return bestN, bestHi
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Resample reduces or stretches a series to exactly n points by averaging the
// samples that fall in each output bucket.
//
// Averaging rather than picking every k-th sample: a hashrate series decimated
// by picking will show whichever phase of the GPU's batch cycle it happened to
// land on, and the chart then has a periodic wobble that the machine does not.
func Resample(vals []float64, n int) []float64 {
	if n < 1 || len(vals) == 0 {
		return nil
	}
	if len(vals) == n {
		return vals
	}
	out := make([]float64, n)
	for i := range out {
		lo := i * len(vals) / n
		hi := (i + 1) * len(vals) / n
		if hi <= lo {
			hi = lo + 1
		}
		if hi > len(vals) {
			hi = len(vals)
		}
		s := 0.0
		for _, v := range vals[lo:hi] {
			s += v
		}
		out[i] = s / float64(hi-lo)
	}
	return out
}
