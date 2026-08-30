//go:build windows

package main

// Every measurement of the terminal, side by side.
//
// The console reports its size four different ways and they do not have to
// agree. When the frame comes out wider than the window, which of them is
// lying is the whole question, and no amount of reasoning about it settles
// what one run on the affected machine settles in a second.

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

func consoleDiag() []string {
	out := []string{}
	add := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)) }

	if !StdoutIsTTY() {
		return []string{"stdout is not a console (piped or redirected)"}
	}
	h := uintptr(syscall.Handle(os.Stdout.Fd()))

	var info consoleScreenBufferInfo
	if r, _, _ := procGetScreenInfo.Call(h, uintptr(unsafe.Pointer(&info))); r == 0 {
		add("GetConsoleScreenBufferInfo failed")
	} else {
		add("window rect        %d x %d   (left %d top %d right %d bottom %d)",
			info.window.right-info.window.left+1, info.window.bottom-info.window.top+1,
			info.window.left, info.window.top, info.window.right, info.window.bottom)
		add("screen buffer      %d x %d", info.size.x, info.size.y)
		add("max window size    %d x %d", info.maximumWindowSize.x, info.maximumWindowSize.y)
	}
	if r, _, _ := procLargestWindow.Call(h); r != 0 {
		add("largest window     %d x %d", int16(uint32(r)&0xffff), int16(uint32(r)>>16))
	} else {
		add("largest window     unavailable")
	}

	// The pixels. In Windows Terminal GetConsoleWindow returns a hidden helper
	// window rather than the tab, so a zero or absurd rectangle here is itself
	// the answer: it says which console this is.
	if hwnd, _, _ := procConsoleWindow.Call(); hwnd == 0 {
		add("console hwnd       none (Windows Terminal or a pseudoconsole)")
	} else {
		var win winRect
		if r, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&win))); r == 0 {
			add("console hwnd       %#x, GetWindowRect failed", hwnd)
		} else {
			add("console hwnd       %#x", hwnd)
			add("window pixels      %d x %d   at (%d, %d)",
				win.right-win.left, win.bottom-win.top, win.left, win.top)
		}
		if mon, _, _ := procMonitorFromWin.Call(hwnd, monitorDefaultToNearest); mon != 0 {
			var mi monitorInfo
			mi.cbSize = uint32(unsafe.Sizeof(mi))
			if r, _, _ := procGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi))); r != 0 {
				add("monitor pixels     %d x %d   work area %d x %d",
					mi.rcMonitor.right-mi.rcMonitor.left, mi.rcMonitor.bottom-mi.rcMonitor.top,
					mi.rcWork.right-mi.rcWork.left, mi.rcWork.bottom-mi.rcWork.top)
			}
		}
	}

	// The one that cannot be wrong: the terminal put the cursor there itself.
	if c, r, ok := probeTerminalSize(); ok {
		add("cursor probe       %d x %d   <- what is actually on screen", c, r)
	} else {
		add("cursor probe       no reply")
	}

	for _, k := range []string{"WT_SESSION", "WT_PROFILE_ID", "TERM", "TERM_PROGRAM", "ConEmuANSI", "ANSICON", "SESSIONNAME"} {
		if v := os.Getenv(k); v != "" {
			add("env %-14s %s", k, v)
		}
	}
	return out
}
