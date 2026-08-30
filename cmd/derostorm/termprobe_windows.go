//go:build windows

package main

// Asking the terminal how big it is, rather than asking Windows.
//
// GetConsoleScreenBufferInfo reports the console's own model of its window,
// and that model can disagree with what is on the screen: a resize we asked
// for is recorded before -- or instead of -- the window actually taking it,
// and the console has no way to know the difference. Drawing to a width that
// is not there is what puts the right-hand panels off the edge of the display.
//
// The cursor position report has no such gap. Park the cursor past any
// plausible edge, and the terminal clamps it to the real bottom-right corner
// and says where that is. Whatever it answers is on screen by definition,
// because the terminal just put the cursor there.

import (
	"bytes"
	"os"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

const (
	enableVirtualTerminalInput = 0x0200
	keyEventRecord             = 0x0001
	waitObject0                = 0x00000000
)

var (
	procReadConsoleInput  = kernel32.NewProc("ReadConsoleInputW")
	procWaitForSingle     = kernel32.NewProc("WaitForSingleObject")
	procFlushConsoleInput = kernel32.NewProc("FlushConsoleInputBuffer")
)

// inputRecord is INPUT_RECORD with the KEY_EVENT_RECORD arm of its union
// spelled out. The other arms are all smaller, so a read that lands on one
// still fills the struct and is discarded by the event type.
type inputRecord struct {
	eventType uint16
	_         uint16
	keyDown   int32
	repeat    uint16
	vkCode    uint16
	scanCode  uint16
	char      uint16
	ctrlState uint32
}

// probeTerminalSize returns the size the terminal itself reports, in columns
// and rows.
//
// The reply arrives through stdin, so this has to run before the key reader is
// started or it will race it for the answer. It restores the console mode it
// found and never leaves a read outstanding: a blocked read on stdin would go
// on to eat the user's first keystroke.
func probeTerminalSize() (cols, rows int, ok bool) {
	if !StdoutIsTTY() {
		return 0, 0, false
	}
	hIn := syscall.Handle(os.Stdin.Fd())
	var mode uint32
	if r, _, _ := procGetConsoleMode.Call(uintptr(hIn), uintptr(unsafe.Pointer(&mode))); r == 0 {
		return 0, 0, false // stdin is not a console; nothing will answer
	}
	probe := (mode &^ (enableLineInput | enableEchoInput | enableProcessedInput)) | enableVirtualTerminalInput
	if r, _, _ := procSetConsoleMode.Call(uintptr(hIn), uintptr(probe)); r == 0 {
		return 0, 0, false
	}
	defer procSetConsoleMode.Call(uintptr(hIn), uintptr(mode))

	// Anything already queued would be read as if it were the reply.
	procFlushConsoleInput.Call(uintptr(hIn))

	// DECSC and DECRC around the move, so the cursor ends up where it started.
	if _, err := os.Stdout.WriteString("\x1b7\x1b[9999;9999H\x1b[6n\x1b8"); err != nil {
		return 0, 0, false
	}

	deadline := time.Now().Add(300 * time.Millisecond)
	var reply []byte
	for len(reply) < 32 {
		left := time.Until(deadline)
		if left <= 0 {
			break
		}
		w, _, _ := procWaitForSingle.Call(uintptr(hIn), uintptr(uint32(left/time.Millisecond)+1))
		if w != waitObject0 {
			break // timed out, or the handle went away
		}
		var rec inputRecord
		var got uint32
		r, _, _ := procReadConsoleInput.Call(uintptr(hIn),
			uintptr(unsafe.Pointer(&rec)), 1, uintptr(unsafe.Pointer(&got)))
		if r == 0 || got == 0 {
			break
		}
		// Focus, mouse and key-up records all arrive here too.
		if rec.eventType != keyEventRecord || rec.keyDown == 0 || rec.char == 0 {
			continue
		}
		reply = append(reply, byte(rec.char))
		if rec.char == 'R' {
			break
		}
	}
	return parseCPR(reply)
}

// parseCPR reads ESC [ rows ; cols R.
func parseCPR(b []byte) (cols, rows int, ok bool) {
	i := bytes.LastIndex(b, []byte{0x1b, '['})
	if i < 0 {
		return 0, 0, false
	}
	body := b[i+2:]
	end := bytes.IndexByte(body, 'R')
	if end < 0 {
		return 0, 0, false
	}
	half := bytes.SplitN(body[:end], []byte{';'}, 2)
	if len(half) != 2 {
		return 0, 0, false
	}
	r, err1 := strconv.Atoi(string(half[0]))
	c, err2 := strconv.Atoi(string(half[1]))
	if err1 != nil || err2 != nil || r <= 0 || c <= 0 {
		return 0, 0, false
	}
	return c, r, true
}

// SyncConsoleToProbe asks the terminal for its real size and shrinks the
// console to match when the console is claiming more.
//
// Only ever shrinks. A probe that comes back larger than the console thinks it
// is means there is more room than we are using, which is harmless -- the
// frame is simply drawn smaller than it could be, and the next frame will pick
// the new size up anyway.
func SyncConsoleToProbe() (cols, rows int, ok bool) {
	c, r, ok := probeTerminalSize()
	if !ok {
		return 0, 0, false
	}
	wc, wr := TerminalWidth(), TerminalHeight()
	if wc > 0 && wr > 0 && (c < wc || r < wr) {
		h := uintptr(syscall.Handle(os.Stdout.Fd()))
		applyWin32Size(h, min(c, wc), min(r, wr))
	}
	return c, r, true
}
