//go:build windows

package main

// Windows console setup. conhost does not process ANSI escapes unless
// ENABLE_VIRTUAL_TERMINAL_PROCESSING is turned on for the handle, so without
// this the dashboard would print raw escape sequences. Windows Terminal enables
// it already; older consoles do not.

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	enableVirtualTerminalProcessing = 0x0004
	fileTypeChar                    = 0x0002
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
	procGetFileType    = kernel32.NewProc("GetFileType")
	procGetScreenInfo  = kernel32.NewProc("GetConsoleScreenBufferInfo")
)

// EnableVirtualTerminal turns on ANSI processing for stdout and reports whether
// it is now available.
func EnableVirtualTerminal() bool {
	h := syscall.Handle(os.Stdout.Fd())
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(uintptr(h), uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		return false // not a console
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return true
	}
	r, _, _ = procSetConsoleMode.Call(uintptr(h), uintptr(mode|enableVirtualTerminalProcessing))
	return r != 0
}

// StdoutIsTTY reports whether stdout is a character device (a console) rather
// than a pipe or a file.
func StdoutIsTTY() bool {
	r, _, _ := procGetFileType.Call(uintptr(syscall.Handle(os.Stdout.Fd())))
	return uint32(r) == fileTypeChar
}

type consoleScreenBufferInfo struct {
	size              struct{ x, y int16 }
	cursorPosition    struct{ x, y int16 }
	attributes        uint16
	window            struct{ left, top, right, bottom int16 }
	maximumWindowSize struct{ x, y int16 }
}

// TerminalWidth returns the console width in columns, or 0 if unknown.
func TerminalWidth() int {
	var info consoleScreenBufferInfo
	r, _, _ := procGetScreenInfo.Call(uintptr(syscall.Handle(os.Stdout.Fd())), uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0
	}
	if w := int(info.window.right - info.window.left + 1); w > 0 {
		return w
	}
	return 0
}

// TerminalHeight returns the console height in rows, or 0 if unknown.
//
// The window rectangle, not the screen buffer: the buffer is usually thousands
// of rows of scrollback, and what matters here is how many rows are visible at
// once. A panel taller than that scrolls, and a panel that scrolls is redrawn
// in the wrong place.
func TerminalHeight() int {
	var info consoleScreenBufferInfo
	r, _, _ := procGetScreenInfo.Call(uintptr(syscall.Handle(os.Stdout.Fd())), uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0
	}
	if h := int(info.window.bottom - info.window.top + 1); h > 0 {
		return h
	}
	return 0
}

// ---------------------------------------------------------------- window size

const (
	procSetBufferName = "SetConsoleScreenBufferSize"
	procSetWindowName = "SetConsoleWindowInfo"
	procLargestName   = "GetLargestConsoleWindowSize"

	monitorDefaultToNearest = 2
)

var (
	procSetBufferSize = kernel32.NewProc(procSetBufferName)
	procSetWindowInfo = kernel32.NewProc(procSetWindowName)
	procLargestWindow = kernel32.NewProc(procLargestName)
	procConsoleWindow = kernel32.NewProc("GetConsoleWindow")

	user32             = syscall.NewLazyDLL("user32.dll")
	procGetWindowRect  = user32.NewProc("GetWindowRect")
	procMonitorFromWin = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfo = user32.NewProc("GetMonitorInfoW")
)

type smallRect struct{ left, top, right, bottom int16 }

type winRect struct{ left, top, right, bottom int32 }

type monitorInfo struct {
	cbSize    uint32
	rcMonitor winRect
	rcWork    winRect
	dwFlags   uint32
}

// coord packs a COORD into the single argument word the console API expects:
// x in the low half, y in the high half.
func coord(x, y int) uintptr {
	return uintptr(uint32(uint16(y))<<16 | uint32(uint16(x)))
}

// applyWin32Size sets the console to cols x rows and returns the window size it
// actually ended up with, or 0, 0 if that could not be read.
//
// The buffer and the window are resized in the order that keeps them from
// crossing: the window shrinks first when the buffer is about to, and grows
// last when the buffer just has. The API rejects a window larger than its
// buffer outright, and half a resize leaves a console worse than the one it
// started from.
//
// The buffer width is set to exactly the window width, never wider. A buffer
// wider than its window is a horizontal scrollbar, and a horizontal scrollbar
// is a frame whose right-hand panels are simply not on screen.
func applyWin32Size(h uintptr, cols, rows int) (int, int) {
	var info consoleScreenBufferInfo
	if r, _, _ := procGetScreenInfo.Call(h, uintptr(unsafe.Pointer(&info))); r == 0 {
		return 0, 0
	}
	shrink := smallRect{0, 0, int16(min(cols, int(info.size.x)) - 1), int16(min(rows, int(info.size.y)) - 1)}
	procSetWindowInfo.Call(h, 1, uintptr(unsafe.Pointer(&shrink)))

	// Buffer height keeps whatever scrollback is already there; only the width
	// is pinned to the window.
	procSetBufferSize.Call(h, coord(cols, max(rows, int(info.size.y))))

	want := smallRect{0, 0, int16(cols - 1), int16(rows - 1)}
	procSetWindowInfo.Call(h, 1, uintptr(unsafe.Pointer(&want)))

	if r, _, _ := procGetScreenInfo.Call(h, uintptr(unsafe.Pointer(&info))); r == 0 {
		return 0, 0
	}
	return int(info.window.right - info.window.left + 1), int(info.window.bottom - info.window.top + 1)
}

// screenFit returns how much of the monitor's usable area the console window
// takes up, per axis. A value below 1 means the window hangs off the screen by
// that much.
//
// Ratios rather than pixel counts, because both measurements come through the
// same DPI virtualisation and a ratio does not care what scale it was taken at.
// That is what lets this work on a 4K monitor at 150% without the process
// having to declare a DPI awareness it does not otherwise need.
func screenFit() (fw, fh float64, ok bool) {
	hwnd, _, _ := procConsoleWindow.Call()
	if hwnd == 0 {
		return 1, 1, false
	}
	var win winRect
	if r, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&win))); r == 0 {
		return 1, 1, false
	}
	mon, _, _ := procMonitorFromWin.Call(hwnd, monitorDefaultToNearest)
	if mon == 0 {
		return 1, 1, false
	}
	var mi monitorInfo
	mi.cbSize = uint32(unsafe.Sizeof(mi))
	if r, _, _ := procGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi))); r == 0 {
		return 1, 1, false
	}
	winW, winH := float64(win.right-win.left), float64(win.bottom-win.top)
	workW := float64(mi.rcWork.right - mi.rcWork.left)
	workH := float64(mi.rcWork.bottom - mi.rcWork.top)
	if winW <= 0 || winH <= 0 || workW <= 0 || workH <= 0 {
		return 1, 1, false
	}
	return workW / winW, workH / winH, true
}

// EnsureTerminalSize grows the console so a window of cols x rows fits without
// scrolling. It only ever grows, so a user who has deliberately made their
// window huge keeps it.
//
// Two mechanisms, because neither covers both consoles. Classic conhost honours
// SetConsoleScreenBufferSize and SetConsoleWindowInfo but ignores the ANSI
// resize; Windows Terminal is the other way round, since its window belongs to
// the tab rather than to the console. Trying the Win32 path first and falling
// back to the escape sequence covers both, and a console that honours neither
// is left exactly as it was.
func EnsureTerminalSize(cols, rows int) {
	if !StdoutIsTTY() {
		return
	}
	h := uintptr(syscall.Handle(os.Stdout.Fd()))

	var info consoleScreenBufferInfo
	if r, _, _ := procGetScreenInfo.Call(h, uintptr(unsafe.Pointer(&info))); r == 0 {
		return
	}
	curCols := int(info.window.right - info.window.left + 1)
	curRows := int(info.window.bottom - info.window.top + 1)
	if curCols >= cols && curRows >= rows {
		return
	}

	wantCols, wantRows := max(curCols, cols), max(curRows, rows)

	// Never ask for more than the screen can show, or the call fails outright
	// and the window is left at its original size.
	//
	// And not quite as much as it says it can show. GetLargestConsoleWindowSize
	// measures the display, not the space a window actually gets: it does not
	// take off the title bar or the taskbar. Asking for exactly that produces a
	// console whose bottom rows are under the taskbar or off the screen, which
	// the console does not know about -- it reports the full height, the frame
	// is drawn to it, and the rows nobody can see push the visible ones off the
	// top. That is a console that scrolls until the window is maximised, which
	// is the one thing this function exists to prevent.
	const marginCols, marginRows = 1, 2
	if r, _, _ := procLargestWindow.Call(h); r != 0 {
		if maxCols := int(int16(uint32(r)&0xffff)) - marginCols; maxCols > 0 && wantCols > maxCols {
			wantCols = maxCols
		}
		if maxRows := int(int16(uint32(r)>>16)) - marginRows; maxRows > 0 && wantRows > maxRows {
			wantRows = maxRows
		}
	}

	gotCols, gotRows := applyWin32Size(h, wantCols, wantRows)

	// Now measure the window against the monitor it landed on, and shrink until
	// it fits.
	//
	// This is the 4K case. On a scaled display conhost sizes its window from a
	// font measured before the scale factor is applied, so the window it builds
	// for 164 columns can be wider than the screen -- and it does not know. It
	// reports 164 columns, the frame is drawn to 164 columns, and the right-hand
	// panels are off the edge of the monitor. GetLargestConsoleWindowSize is
	// computed the same wrong way, so it does not catch this either. Only the
	// window's own pixels do.
	//
	// Three passes: a ratio correction is slightly optimistic, so the first pass
	// gets close, the second lands it, and the third is there so that the loop
	// is not load-bearing.
	const floorCols, floorRows = 80, 24
	for i := 0; i < 3 && gotCols > 0 && gotRows > 0; i++ {
		fw, fh, ok := screenFit()
		if !ok || (fw >= 1 && fh >= 1) {
			break
		}
		nextCols, nextRows := gotCols, gotRows
		if fw < 1 {
			nextCols = max(int(float64(gotCols)*fw)-1, floorCols)
		}
		if fh < 1 {
			nextRows = max(int(float64(gotRows)*fh)-1, floorRows)
		}
		if nextCols >= gotCols && nextRows >= gotRows {
			break // already as small as this is prepared to go
		}
		gotCols, gotRows = applyWin32Size(h, nextCols, nextRows)
	}

	// Did any of that take? Windows Terminal will have ignored all of it.
	if gotCols >= cols && gotRows >= rows {
		return
	}
	if gotCols > 0 && gotCols < cols {
		// It took, and the screen is genuinely too small for the ask. Asking
		// again over ANSI would only undo the fitting done above.
		return
	}
	resizeANSI(cols, rows)
}
