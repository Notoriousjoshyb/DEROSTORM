//go:build !windows

package main

// Console setup on everything that is not Windows. ANSI needs no enabling here;
// only the TTY test and the width query are platform work.

import (
	"os"
	"syscall"
	"unsafe"
)

// EnableVirtualTerminal is a no-op outside Windows.
func EnableVirtualTerminal() bool { return true }

// StdoutIsTTY reports whether stdout is a terminal rather than a pipe or file.
func StdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

type winsize struct {
	rows, cols, xpixel, ypixel uint16
}

// TerminalWidth returns the terminal width in columns, or 0 if unknown.
func TerminalWidth() int {
	if ws, ok := winSize(); ok {
		return int(ws.cols)
	}
	return 0
}

// TerminalHeight returns the terminal height in rows, or 0 if unknown.
func TerminalHeight() int {
	if ws, ok := winSize(); ok {
		return int(ws.rows)
	}
	return 0
}

func winSize() (winsize, bool) {
	var ws winsize
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL,
		os.Stdout.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	return ws, errno == 0
}
