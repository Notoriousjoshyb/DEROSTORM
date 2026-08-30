//go:build !windows

package main

// Away from Windows the terminal size comes from an ioctl on the tty, which
// the kernel answers from the same number the terminal emulator set. There is
// no second opinion to reconcile it with, so there is nothing to probe.

func probeTerminalSize() (cols, rows int, ok bool) { return 0, 0, false }

// SyncConsoleToProbe reports the terminal size, which here is just the size.
func SyncConsoleToProbe() (cols, rows int, ok bool) { return 0, 0, false }
