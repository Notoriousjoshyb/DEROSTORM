//go:build windows

package main

// Raw-ish stdin on Windows.
//
// The live panel redraws in place, so the terminal must not echo typed keys
// itself -- its cursor is wherever our last redraw left it, and echoed text
// would land in the middle of the frame. Instead we turn off line buffering and
// echo, read keys one at a time, and draw the input line ourselves as part of
// the frame.
//
// ENABLE_PROCESSED_INPUT stays on so Ctrl-C still raises an interrupt rather
// than arriving as a byte we would have to handle.

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	enableProcessedInput = 0x0001
	enableLineInput      = 0x0002
	enableEchoInput      = 0x0004
)

// EnableRawInput switches stdin to unbuffered, unechoed input. It returns a
// restore function and whether it worked; when it does not (stdin is a pipe,
// say), the caller falls back to plain line reading.
func EnableRawInput() (restore func(), ok bool) {
	h := syscall.Handle(os.Stdin.Fd())
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(uintptr(h), uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		return func() {}, false
	}
	raw := (mode &^ (enableLineInput | enableEchoInput)) | enableProcessedInput
	if r, _, _ = procSetConsoleMode.Call(uintptr(h), uintptr(raw)); r == 0 {
		return func() {}, false
	}
	return func() { procSetConsoleMode.Call(uintptr(h), uintptr(mode)) }, true
}
