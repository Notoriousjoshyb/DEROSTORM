//go:build !windows

package main

// Terminal sizing away from Windows. There is no console API to try, so the
// ANSI request is the whole implementation.

// EnsureTerminalSize asks the terminal for a window at least cols x rows.
// Terminals that do not honour the request are left as they are.
func EnsureTerminalSize(cols, rows int) {
	if !StdoutIsTTY() {
		return
	}
	if w := TerminalWidth(); w >= cols {
		// Already wide enough; assume the height is the user's choice too
		// rather than fighting a deliberately short window on every start.
		return
	}
	resizeANSI(cols, rows)
}
