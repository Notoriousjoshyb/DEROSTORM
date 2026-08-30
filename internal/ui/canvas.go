package ui

// The cell grid and the code that puts it on a terminal.
//
// Every screen in this program is drawn the same way: clear a grid of cells,
// let each panel write into its own rectangle, then hand the grid to a Screen
// which works out the smallest set of escape sequences that turn the last
// frame into this one.
//
// A grid rather than a list of lines, for three reasons that each cost real
// bugs the other way round:
//
//   - Overlap becomes impossible to hide. Two panels that both write cell
//     (40, 12) produce one visible character, not a line that is silently two
//     cells too wide and pushes every border after it sideways.
//   - Placement is absolute, so a widget can be drawn without knowing what is
//     to the left of it. That is what lets the same donut appear in three
//     panels at three different widths.
//   - Redrawing can be diffed. At eight frames a second on a 200x50 window,
//     repainting everything would be 10,000 cells and several kilobytes of
//     escapes per frame; only a handful of those cells change between frames,
//     and this writes only those.

import (
	"io"
	"strings"
)

// Style is how one cell is coloured. FG and BG are complete ANSI escape
// sequences rather than colour values, so a theme can hand over whatever it
// likes -- 24-bit, 256-colour, or the empty string for no colour at all.
type Style struct {
	FG   string
	BG   string
	Bold bool
}

// FGOf is this style with a different foreground, for the very common case of
// "the same thing, in the warning colour".
func (s Style) FGOf(fg string) Style { s.FG = fg; return s }

// B is this style in bold.
func (s Style) B() Style { s.Bold = true; return s }

// Cell is one character position.
type Cell struct {
	R rune
	S Style
	// cont marks the right half of a double-width character. It carries no
	// glyph of its own; the renderer skips it, because the wide rune to its
	// left has already painted this column.
	cont bool
}

// Canvas is a grid of cells that widgets draw into.
type Canvas struct {
	W, H  int
	cells []Cell
}

func NewCanvas(w, h int) *Canvas {
	c := &Canvas{}
	c.Resize(w, h)
	return c
}

// Resize reshapes the canvas, keeping the allocation when it is already big
// enough. Terminal resizes arrive as a stream of intermediate sizes while a
// window is being dragged, so this runs often enough to be worth not
// reallocating on every one of them.
func (c *Canvas) Resize(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if w == c.W && h == c.H && c.cells != nil {
		return
	}
	if n := w * h; n > cap(c.cells) {
		c.cells = make([]Cell, n)
	} else {
		c.cells = c.cells[:n]
	}
	c.W, c.H = w, h
	c.Clear()
}

func (c *Canvas) Clear() {
	blank := Cell{R: ' '}
	for i := range c.cells {
		c.cells[i] = blank
	}
}

// Bounds is the whole canvas as a rectangle.
func (c *Canvas) Bounds() Rect { return Rect{0, 0, c.W, c.H} }

func (c *Canvas) in(x, y int) bool { return x >= 0 && y >= 0 && x < c.W && y < c.H }

// Set writes one rune. Out-of-range writes are dropped rather than clamped: a
// widget handed a rectangle too small for it should lose its overflow, not
// stack it against the edge where it would look like real content.
func (c *Canvas) Set(x, y int, r rune, s Style) {
	if !c.in(x, y) {
		return
	}
	w := RuneWidth(r)
	if w == 0 {
		return
	}
	i := y*c.W + x
	// Writing over half of an existing wide rune would leave its orphan on
	// screen, so blank the partner cell in whichever direction it lies.
	if c.cells[i].cont && x > 0 {
		c.cells[i-1] = Cell{R: ' ', S: c.cells[i-1].S}
	}
	if x+1 < c.W && c.cells[i+1].cont {
		c.cells[i+1] = Cell{R: ' ', S: c.cells[i+1].S}
	}
	if w == 2 && x+1 >= c.W {
		// A wide rune with one column left would spill onto the next row,
		// which the terminal would treat as a line of a different length.
		c.cells[i] = Cell{R: ' ', S: s}
		return
	}
	c.cells[i] = Cell{R: r, S: s}
	if w == 2 {
		c.cells[i+1] = Cell{R: 0, S: s, cont: true}
	}
}

// Text writes s starting at (x, y) and returns the number of cells written.
func (c *Canvas) Text(x, y int, s string, st Style) int {
	w := 0
	for _, r := range s {
		rw := RuneWidth(r)
		if rw == 0 {
			continue
		}
		if x+w >= c.W {
			break
		}
		c.Set(x+w, y, r, st)
		w += rw
	}
	return w
}

// TextIn writes s clipped to max cells, so a caller can hand over a value of
// unknown length and still know exactly what it costs.
func (c *Canvas) TextIn(x, y, max int, s string, st Style) int {
	return c.Text(x, y, Clip(s, max), st)
}

// TextRight right-aligns s so that it ends at column right-1.
func (c *Canvas) TextRight(right, y int, s string, st Style) {
	s = Clip(s, right)
	c.Text(right-Width(s), y, s, st)
}

// TextCenter centres s within [x, x+w).
func (c *Canvas) TextCenter(x, y, w int, s string, st Style) {
	s = Clip(s, w)
	c.Text(x+(w-Width(s))/2, y, s, st)
}

func (c *Canvas) HLine(x, y, w int, r rune, st Style) {
	for i := 0; i < w; i++ {
		c.Set(x+i, y, r, st)
	}
}

func (c *Canvas) VLine(x, y, h int, r rune, st Style) {
	for i := 0; i < h; i++ {
		c.Set(x, y+i, r, st)
	}
}

func (c *Canvas) Fill(r Rect, ru rune, st Style) {
	for y := r.Y; y < r.Bottom(); y++ {
		c.HLine(r.X, y, r.W, ru, st)
	}
}

// ---------------------------------------------------------------- rectangles

// Rect is a region of the canvas. A widget is handed one and may draw anywhere
// inside it and nowhere else.
type Rect struct{ X, Y, W, H int }

func (r Rect) Right() int  { return r.X + r.W }
func (r Rect) Bottom() int { return r.Y + r.H }
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Inset shrinks a rectangle on all sides. A rectangle inset past nothing
// becomes empty rather than inverted, so a widget only has to check Empty
// rather than guard against a negative width at every use.
func (r Rect) Inset(n int) Rect { return r.Inset2(n, n) }

func (r Rect) Inset2(x, y int) Rect {
	out := Rect{r.X + x, r.Y + y, r.W - 2*x, r.H - 2*y}
	if out.W < 0 {
		out.W = 0
	}
	if out.H < 0 {
		out.H = 0
	}
	return out
}

// Rows cuts a rectangle into horizontal strips of the given heights, top down.
// A height of -1 means "whatever is left"; at most one entry may use it.
func (r Rect) Rows(heights ...int) []Rect {
	fixed, flex := 0, -1
	for i, h := range heights {
		if h < 0 {
			flex = i
			continue
		}
		fixed += h
	}
	out := make([]Rect, len(heights))
	y := r.Y
	for i, h := range heights {
		if i == flex {
			h = r.H - fixed
		}
		if h < 0 {
			h = 0
		}
		if y+h > r.Bottom() {
			h = r.Bottom() - y
		}
		if h < 0 {
			h = 0
		}
		out[i] = Rect{r.X, y, r.W, h}
		y += h
	}
	return out
}

// Cols cuts a rectangle into columns sized by weight, handing the rounding
// remainder out one cell at a time from the left.
//
// Weights rather than widths, because the whole point of the layout is that
// the same three panels fill a 100-column window and a 220-column one. The
// remainder loop matters more than it looks: without it the columns can add up
// to two cells less than the parent, and the dashboard grows a ragged right
// edge that moves as the window is resized.
func (r Rect) Cols(weights ...int) []Rect {
	total := 0
	for _, w := range weights {
		if w > 0 {
			total += w
		}
	}
	if total <= 0 || len(weights) == 0 {
		return []Rect{r}
	}
	out := make([]Rect, len(weights))
	x, given := r.X, 0
	for i, wt := range weights {
		w := 0
		if wt > 0 {
			w = r.W * wt / total
		}
		out[i] = Rect{x, r.Y, w, r.H}
		x += w
		given += w
	}
	for i := 0; given < r.W; i, given = i+1, given+1 {
		j := i % len(out)
		out[j].W++
		for k := j + 1; k < len(out); k++ {
			out[k].X++
		}
	}
	return out
}

// ---------------------------------------------------------------- screen

// Screen owns the terminal: the alternate buffer, the cursor, and the diff
// between the last frame and this one.
type Screen struct {
	out    io.Writer
	cur    *Canvas
	prev   *Canvas
	colour bool
	// full forces the next flush to repaint every cell. Set after a resize or
	// on entering the alternate buffer, where what is on the terminal is no
	// longer whatever the previous frame left there.
	full bool
	buf  strings.Builder
}

func NewScreen(out io.Writer, w, h int, colour bool) *Screen {
	return &Screen{
		out:    out,
		cur:    NewCanvas(w, h),
		prev:   NewCanvas(w, h),
		colour: colour,
		full:   true,
	}
}

// Buf is the canvas for the frame currently being built.
func (s *Screen) Buf() *Canvas { return s.cur }

func (s *Screen) Size() (int, int) { return s.cur.W, s.cur.H }

func (s *Screen) Resize(w, h int) {
	if w == s.cur.W && h == s.cur.H {
		return
	}
	s.cur.Resize(w, h)
	s.prev.Resize(w, h)
	s.full = true
}

// Invalidate forces a full repaint on the next flush.
func (s *Screen) Invalidate() { s.full = true }

// Enter switches to the alternate screen buffer, turns off automatic margin
// wrap and hides the cursor.
//
// The wrap is the one that looks like a bug rather than a setting. A frame
// that fills the window writes the bottom-right cell every time it repaints,
// and with wrap on that advances the cursor past the right margin on the last
// line -- which the terminal answers by scrolling everything up a row. The
// next frame is drawn a row higher than the one before it, and the console
// appears to crawl up the screen until the window is made big enough that the
// frame no longer reaches the last line. Terminals disagree about whether that
// scroll happens at once or is held until the next character, which is why it
// shows on some consoles and not others; with wrap off there is nothing left
// to disagree about.
//
// The alternate buffer is what makes a full-screen program polite: the user's
// scrollback is untouched while it runs and is exactly as they left it when it
// exits, instead of holding fifty thousand frames of dashboard.
func (s *Screen) Enter() {
	io.WriteString(s.out, "\x1b[?1049h\x1b[?7l\x1b[?25l\x1b[2J\x1b[H")
	s.full = true
}

// Leave restores the normal buffer, the cursor and the default colours. Called
// from a deferred function and from the signal handler, because a terminal
// left in the alternate buffer with a hidden cursor looks broken and the user
// has no obvious way to tell that it is not. Margin wrap goes back on here for
// the same reason it went off: every command run after this one would
// otherwise be in a terminal that silently truncates its long lines.
func (s *Screen) Leave() {
	io.WriteString(s.out, "\x1b[0m\x1b[?7h\x1b[?25h\x1b[?1049l")
}

// Flush writes the difference between the previous frame and the current one,
// then makes the current one the previous.
//
// Row at a time, and within a row only the span between the first and last
// changed cell. A hashrate that gains a decimal place costs the six cells it
// occupies, not the row, and not the screen.
func (s *Screen) Flush() {
	s.buf.Reset()
	s.buf.WriteString("\x1b[?25l")

	for y := 0; y < s.cur.H; y++ {
		row := y * s.cur.W
		first, last := -1, -1
		if s.full {
			first, last = 0, s.cur.W-1
		} else {
			for x := 0; x < s.cur.W; x++ {
				if s.cur.cells[row+x] != s.prev.cells[row+x] {
					if first < 0 {
						first = x
					}
					last = x
				}
			}
		}
		if first < 0 {
			continue
		}
		// A changed cell that is the right half of a wide rune has to be
		// repainted starting from the rune itself, or the terminal is asked to
		// draw the tail of a character whose head it never received.
		for first > 0 && s.cur.cells[row+first].cont {
			first--
		}
		if last+1 < s.cur.W && s.cur.cells[row+last+1].cont {
			last++
		}
		s.writeSpan(y, first, last)
	}

	s.cur, s.prev = s.prev, s.cur
	s.cur.Clear()
	s.full = false
	io.WriteString(s.out, s.buf.String())
}

func (s *Screen) writeSpan(y, first, last int) {
	cv := s.cur
	s.buf.WriteString("\x1b[")
	writeInt(&s.buf, y+1)
	s.buf.WriteByte(';')
	writeInt(&s.buf, first+1)
	s.buf.WriteByte('H')

	row := y * cv.W
	var cur Style
	started := false
	for x := first; x <= last && x < cv.W; x++ {
		c := cv.cells[row+x]
		if c.cont {
			continue // already painted by the wide rune to its left
		}
		if s.colour && (!started || c.S != cur) {
			s.buf.WriteString("\x1b[0m")
			if c.S.Bold {
				s.buf.WriteString("\x1b[1m")
			}
			s.buf.WriteString(c.S.BG)
			s.buf.WriteString(c.S.FG)
		}
		cur, started = c.S, true
		r := c.R
		if r == 0 {
			r = ' '
		}
		s.buf.WriteRune(r)
	}
	if s.colour && started {
		s.buf.WriteString("\x1b[0m")
	}
}

func writeInt(b *strings.Builder, n int) {
	if n <= 0 {
		b.WriteByte('0')
		return
	}
	var tmp [8]byte
	i := len(tmp)
	for n > 0 {
		i--
		tmp[i] = byte('0' + n%10)
		n /= 10
	}
	b.Write(tmp[i:])
}

// Lines renders the canvas as plain strings, one per row, with colour escapes
// inline and a reset at the end of every row.
//
// This is the one-shot form of Flush: no cursor movement, no diffing, nothing
// that assumes a terminal is on the other end. It exists so a frame can be
// dumped to stdout or captured by a test, and so that what a preview shows is
// produced by exactly the drawing code the live console uses.
func (c *Canvas) Lines(colour bool) []string {
	out := make([]string, 0, c.H)
	var b strings.Builder
	for y := 0; y < c.H; y++ {
		b.Reset()
		row := y * c.W
		var cur Style
		started := false
		for x := 0; x < c.W; x++ {
			cell := c.cells[row+x]
			if cell.cont {
				continue
			}
			if colour && (!started || cell.S != cur) {
				b.WriteString("\x1b[0m")
				if cell.S.Bold {
					b.WriteString("\x1b[1m")
				}
				b.WriteString(cell.S.BG)
				b.WriteString(cell.S.FG)
			}
			cur, started = cell.S, true
			r := cell.R
			if r == 0 {
				r = ' '
			}
			b.WriteRune(r)
		}
		if colour && started {
			b.WriteString("\x1b[0m")
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	return out
}
