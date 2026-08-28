package main

// The ANSI window-resize request, shared by every platform.
//
// CSI 8 ; rows ; cols t is the XTWINOPS resize. xterm defined it, most modern
// terminals implement it, and Windows Terminal is one of them -- which matters
// because Windows Terminal ignores the Win32 console resize calls, its window
// belonging to the tab rather than to the console.
//
// A terminal that does not implement it discards the sequence silently, so this
// is safe to send blind. It is only ever sent when stdout is a terminal with
// escape processing on, so it can never end up in a log file.

import (
	"fmt"
	"os"
	"time"
)

func resizeANSI(cols, rows int) {
	fmt.Fprintf(os.Stdout, "\x1b[8;%d;%dt", rows, cols)
}

// WaitForTerminalSize polls until the terminal is at least cols x rows, or the
// deadline passes, and returns the size it ended up with.
//
// This matters because the ANSI resize is a *request*, handled asynchronously.
// Windows Terminal's window belongs to the tab, and the resize goes through its
// UI thread; asking and then immediately measuring reads the size from before
// the request. Acting on that reading is worse than not measuring at all -- it
// looks like the terminal refused, so the panel gets trimmed to fit a window
// that is about to be big enough.
//
// Polling rather than sleeping a fixed time, so the common case where it is
// already the right size costs one query and returns.
func WaitForTerminalSize(cols, rows int, within time.Duration) (int, int) {
	deadline := time.Now().Add(within)
	for {
		c, r := TerminalWidth(), TerminalHeight()
		if c <= 0 || r <= 0 {
			return c, r // size unknown; nothing to wait for
		}
		if c >= cols && r >= rows {
			return c, r
		}
		if !time.Now().Before(deadline) {
			return c, r
		}
		time.Sleep(15 * time.Millisecond)
	}
}
