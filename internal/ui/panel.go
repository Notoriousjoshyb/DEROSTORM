package ui

// Panels, dividers, labelled rows and the other chrome every screen is built
// from. Everything in this file draws into a Rect and returns the Rect that is
// left for content, so a caller never has to know how thick a border is.

// Box drawing. Thin single lines throughout: the reference look is a technical
// instrument panel, and a heavy or doubled border reads as a warning rather
// than as structure. Heavy lines are kept for the one place that is a warning.
const (
	lineH, lineV       = '─', '│'
	lineTL, lineTR     = '┌', '┐'
	lineBL, lineBR     = '└', '┘'
	lineTeeL, lineTeeR = '├', '┤'
	lineTeeU, lineTeeD = '┴', '┬'
	lineCross          = '┼'

	heavyH, heavyV   = '━', '┃'
	heavyTL, heavyTR = '┏', '┓'
	heavyBL, heavyBR = '┗', '┛'
)

// Glyphs used by more than one widget.
const (
	BlockFull  = '█'
	BlockLight = '░'
	BlockMed   = '▒'
	BlockDark  = '▓'
	HalfTop    = '▀'
	HalfBottom = '▄'
	Dot        = '·'
	Bullet     = '●'
	Diamond    = '◆'
	Chevron    = '›'
	Caret      = '▸'
)

// BarParts are the eighth-of-a-cell partial blocks, so a bar can carry a
// fraction of a column. Without them a 20-cell bar has 20 possible values and
// two devices a few percent apart draw identically.
var BarParts = []rune("▏▎▍▌▋▊▉")

// Spark are the eighth-height blocks used by sparklines and spectra.
var Spark = []rune("▁▂▃▄▅▆▇█")

// Spinner frames for anything that is working but has nothing to report yet.
var Spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Pulse frames for the mining heartbeat: a dot that swells and fades. Slower
// and calmer than the spinner, because it is on screen permanently and a fast
// animation in the corner of a panel someone stares at for hours is an
// irritation, not information.
var Pulse = []rune{'·', '∙', '●', '∙'}

// PanelOpts is everything a panel header can carry.
type PanelOpts struct {
	Title string // drawn top-left, in the accent colour
	Mark  string // a one-cell glyph before the title
	Right string // a quieter label at the top-right: units, a scale, a count
	// Accent overrides the border colour, which is how a panel says its
	// contents need attention without moving or growing.
	Accent string
	// Bold draws the border in the highlight colour. Used for the focused
	// panel on screens that have one.
	Focus bool
}

// Panel draws a bordered box with a title and returns the interior rectangle,
// already inset by one cell of padding on the left and right.
//
// The one-cell horizontal padding is not decoration. Values are right-aligned
// against the interior edge, and a number that touches a vertical rule is
// noticeably harder to read than one with a space before it -- which, over a
// screen with a hundred numbers on it, is the difference between a panel that
// scans and one that has to be studied.
func Panel(cv *Canvas, r Rect, t *Theme, o PanelOpts) Rect {
	if r.W < 4 || r.H < 3 {
		return Rect{}
	}
	border := Style{FG: t.Border}
	if o.Accent != "" {
		border.FG = o.Accent
	}
	if o.Focus {
		border.FG = t.BorderHi
	}

	// corners and rules
	cv.Set(r.X, r.Y, lineTL, border)
	cv.Set(r.Right()-1, r.Y, lineTR, border)
	cv.Set(r.X, r.Bottom()-1, lineBL, border)
	cv.Set(r.Right()-1, r.Bottom()-1, lineBR, border)
	cv.HLine(r.X+1, r.Y, r.W-2, lineH, border)
	cv.HLine(r.X+1, r.Bottom()-1, r.W-2, lineH, border)
	cv.VLine(r.X, r.Y+1, r.H-2, lineV, border)
	cv.VLine(r.Right()-1, r.Y+1, r.H-2, lineV, border)

	// title, sitting on the top rule
	x := r.X + 2
	if o.Mark != "" && r.W > 12 {
		x += cv.Text(x, r.Y, o.Mark, Style{FG: t.Accent2})
		x++
	}
	if o.Title != "" {
		avail := r.Right() - 2 - x
		if o.Right != "" {
			avail -= Width(o.Right) + 3
		}
		if avail > 3 {
			x += cv.TextIn(x, r.Y, avail, o.Title, Style{FG: t.Accent, Bold: true})
		}
	}
	if o.Right != "" && r.W > 24 {
		rx := r.Right() - 2 - Width(o.Right)
		if rx > x+1 {
			cv.Text(rx, r.Y, o.Right, Style{FG: t.Muted})
		}
	}

	return r.Inset2(2, 1)
}

// Divider draws a horizontal rule across a panel interior, joining the panel's
// own vertical rules at each end so the box still reads as one shape.
func Divider(cv *Canvas, r Rect, y int, t *Theme) {
	if y < r.Y || y >= r.Bottom() {
		return
	}
	st := Style{FG: t.Border}
	cv.HLine(r.X-1, y, r.W+2, lineH, st)
	cv.Set(r.X-2, y, lineTeeL, st)
	cv.Set(r.Right()+1, y, lineTeeR, st)
}

// Label writes a muted field name at (x, y) and returns where the value should
// start. Every two-column stat row in the program goes through here, which is
// what keeps the value columns aligned between panels.
func Label(cv *Canvas, x, y, w int, s string, t *Theme) int {
	cv.TextIn(x, y, w, s, Style{FG: t.Muted})
	return x + w
}

// MinLabel is the fewest cells a field name is worth drawing in. Below it the
// name carries less than the ellipsis costs, so Row drops it instead.
//
// Exported because the two panels that colour their own labels cannot use Row
// and have to apply the same rule by hand.
const MinLabel = 6

// Row draws "LABEL            value" across the width of r at row y, with the
// value right-aligned against the far edge.
func Row(cv *Canvas, r Rect, y int, label, value string, t *Theme, valueFG string) {
	if y < r.Y || y >= r.Bottom() {
		return
	}
	if valueFG == "" {
		valueFG = t.Text
	}
	vw := Width(value)
	lw := r.W - vw - 1
	if lw < MinLabel {
		// No room for both. The value is the thing being looked up, so it wins
		// and the label is dropped whole.
		//
		// Dropped, not trimmed. Trimming used to leave rows reading
		// "NO… dero-node.mysrv.cloud:10100" and "NETWORK DIFFICUL… 100,580,000",
		// and a label cut to three cells is not a shorter label, it is a
		// smudge: it names nothing and it costs the row the clean right edge
		// that makes a column of figures scannable.
		cv.TextRight(r.Right(), y, Clip(value, r.W), Style{FG: valueFG})
		return
	}
	cv.TextIn(r.X, y, lw, label, Style{FG: t.Muted})
	cv.TextRight(r.Right(), y, value, Style{FG: valueFG})
}

// KeyVal is Row with a fixed label column, for a block of rows that must line
// up with each other rather than with the panel edge.
func KeyVal(cv *Canvas, x, y, labelW, valueRight int, label, value string, t *Theme, valueFG string) {
	if valueFG == "" {
		valueFG = t.Text
	}
	cv.TextIn(x, y, labelW, label, Style{FG: t.Muted})
	cv.TextRight(valueRight, y, Clip(value, valueRight-x-labelW), Style{FG: valueFG})
}

// StatusDot is the coloured ● and its label, the same shape everywhere a
// connection or a run state is reported.
func StatusDot(cv *Canvas, x, y int, mark string, colour, label string, t *Theme) int {
	w := cv.Text(x, y, mark, Style{FG: colour})
	w += cv.Text(x+w+1, y, label, Style{FG: colour, Bold: true}) + 1
	return w
}
