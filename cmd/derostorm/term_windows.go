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
)

var (
	procSetBufferSize = kernel32.NewProc(procSetBufferName)
	procSetWindowInfo = kernel32.NewProc(procSetWindowName)
	procLargestWindow = kernel32.NewProc(procLargestName)
)

type smallRect struct{ left, top, right, bottom int16 }

// coord packs a COORD into the single argument word the console API expects:
// x in the low half, y in the high half.
func coord(x, y int) uintptr {
	return uintptr(uint32(uint16(y))<<16 | uint32(uint16(x)))
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
	if r, _, _ := procLargestWindow.Call(h); r != 0 {
		if maxCols := int(int16(uint32(r) & 0xffff)); maxCols > 0 && wantCols > maxCols {
			wantCols = maxCols
		}
		if maxRows := int(int16(uint32(r) >> 16)); maxRows > 0 && wantRows > maxRows {
			wantRows = maxRows
		}
	}

	// The buffer has to be at least as large as the window, so it grows first.
	// Its height is left alone when it is already bigger, which is what keeps
	// the scrollback the user already has.
	bufCols := max(int(info.size.x), wantCols)
	bufRows := max(int(info.size.y), wantRows)
	procSetBufferSize.Call(h, coord(bufCols, bufRows))

	rect := smallRect{0, 0, int16(wantCols - 1), int16(wantRows - 1)}
	procSetWindowInfo.Call(h, 1, uintptr(unsafe.Pointer(&rect)))

	// Did it take? Windows Terminal will have ignored the above.
	if r, _, _ := procGetScreenInfo.Call(h, uintptr(unsafe.Pointer(&info))); r != 0 {
		gotCols := int(info.window.right - info.window.left + 1)
		gotRows := int(info.window.bottom - info.window.top + 1)
		if gotCols >= cols && gotRows >= rows {
			return
		}
	}
	resizeANSI(cols, rows)
}
