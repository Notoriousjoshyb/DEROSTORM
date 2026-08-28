//go:build linux || darwin || freebsd || netbsd || openbsd

package main

// Raw-ish stdin on unix. See rawmode_windows.go for why the terminal must not
// echo. ISIG is left on so Ctrl-C still raises an interrupt.

import (
	"os"

	"golang.org/x/sys/unix"
)

func EnableRawInput() (restore func(), ok bool) {
	fd := int(os.Stdin.Fd())
	prev, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return func() {}, false
	}
	raw := *prev
	raw.Lflag &^= unix.ICANON | unix.ECHO
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, &raw); err != nil {
		return func() {}, false
	}
	return func() { unix.IoctlSetTermios(fd, ioctlWriteTermios, prev) }, true
}
