package main

// --termdiag: print what every source says the terminal size is, at each stage
// of the startup sizing, and then draw a ruler exactly that wide.
//
// The ruler is the point of the whole thing. Every number above it is a claim;
// the ruler is a test. If the "END|" marker is not visible at the right-hand
// edge, the width the frame is drawn at is wider than the window, and that is
// the bug -- no further reasoning required.

import (
	"fmt"
	"strconv"
	"time"
)

func runTermDiag() {
	section := func(title string) {
		fmt.Printf("\n--- %s ---\n", title)
		for _, l := range consoleDiag() {
			fmt.Println("  " + l)
		}
	}

	fmt.Println("derostorm terminal diagnostic")
	section("as opened")

	const wantCols, wantRows = 164, 48
	EnsureTerminalSize(wantCols, wantRows)
	c, r := WaitForTerminalSize(wantCols, wantRows, 750*time.Millisecond)
	fmt.Printf("\n  asked for %d x %d, measured %d x %d\n", wantCols, wantRows, c, r)
	section("after the startup resize")

	if pc, pr, ok := SyncConsoleToProbe(); ok {
		fmt.Printf("\n  cursor probe said %d x %d\n", pc, pr)
	} else {
		fmt.Println("\n  cursor probe: no reply")
	}
	section("after syncing to the probe")

	w := TerminalWidth()
	fmt.Printf("\n--- ruler at the %d columns the frame would use ---\n", w)
	if w < 8 {
		fmt.Println("  too narrow to rule")
		return
	}
	line := make([]byte, w)
	for i := range line {
		line[i] = '-'
	}
	for i := 0; i < w; i += 10 {
		for j, ch := range []byte(strconv.Itoa(i)) {
			if i+j < w {
				line[i+j] = ch
			}
		}
	}
	copy(line[w-4:], "END|")
	fmt.Println(string(line))
	fmt.Println("\nIf you cannot see END| at the right edge, the console is")
	fmt.Println("reporting more columns than it is showing. That is the bug.")
}
