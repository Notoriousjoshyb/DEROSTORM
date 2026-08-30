package ui

// Text measurement and trimming.
//
// Everything drawn by this package is placed on a cell grid, so the only
// question that ever matters about a string is how many cells it occupies.
// That is not its byte length and it is not always its rune count: a CJK
// character or an emoji takes two cells, and a combining mark takes none.
//
// Getting this wrong is not cosmetic. A panel is drawn by writing into fixed
// cells; a string measured as narrower than it draws pushes the right-hand
// border one cell along and the whole frame loses its shape.

import "strings"

// RuneWidth is how many terminal cells r occupies.
//
// The table is deliberately short. This is a mining console: the only
// non-Latin text it can ever be handed is a node hostname, and the only wide
// glyphs it draws itself are ones it chose. A full Unicode width table would
// be several thousand lines to make the same decision about characters that
// never arrive.
func RuneWidth(r rune) int {
	switch {
	case r == 0:
		return 0
	case r < 0x20, r == 0x7f:
		return 0 // control characters are never drawn
	case r < 0x300:
		return 1 // the common case: ASCII and Latin-1, first
	}
	switch {
	// Combining marks sit on the previous cell.
	case r >= 0x0300 && r <= 0x036f,
		r >= 0x200b && r <= 0x200f,
		r == 0xfeff:
		return 0
	// Wide: CJK, Hangul, fullwidth forms, and the emoji planes.
	case r >= 0x1100 && r <= 0x115f,
		r >= 0x2e80 && r <= 0xa4cf,
		r >= 0xa960 && r <= 0xa97f,
		r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff,
		r >= 0xfe30 && r <= 0xfe6f,
		r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6,
		r >= 0x1f300 && r <= 0x1f9ff,
		r >= 0x20000 && r <= 0x3fffd:
		return 2
	}
	return 1
}

// Width is how many cells s occupies.
func Width(s string) int {
	w := 0
	for _, r := range s {
		w += RuneWidth(r)
	}
	return w
}

// Clip trims s to w cells, marking a trim with an ellipsis. A hostname cut off
// without a mark reads as a different hostname, which is the one thing a node
// field must never do.
func Clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if Width(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	out, used := make([]rune, 0, w), 0
	for _, r := range s {
		rw := RuneWidth(r)
		if used+rw > w-1 {
			break
		}
		out = append(out, r)
		used += rw
	}
	return string(out) + "…"
}

// Pad left-aligns s in w cells.
func Pad(s string, w int) string {
	if n := Width(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return Clip(s, w)
}

// LPad right-aligns s in w cells, which is what a column of numbers wants.
func LPad(s string, w int) string {
	if n := Width(s); n < w {
		return strings.Repeat(" ", w-n) + s
	}
	return Clip(s, w)
}

// Center centres s in w cells, biasing the odd cell to the left so a value
// does not appear to drift right as it gains a digit.
func Center(s string, w int) string {
	n := Width(s)
	if n >= w {
		return Clip(s, w)
	}
	left := (w - n) / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", w-n-left)
}

// Repeat is strings.Repeat for a rune, guarding a negative count so a layout
// that computed a width of -1 draws nothing rather than panicking.
func Repeat(r rune, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(string(r), n)
}
