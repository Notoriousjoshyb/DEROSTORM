//go:build !windows

package main

import (
	"fmt"
	"os"
)

func consoleDiag() []string {
	out := []string{fmt.Sprintf("terminal           %d x %d", TerminalWidth(), TerminalHeight())}
	for _, k := range []string{"TERM", "TERM_PROGRAM", "COLUMNS", "LINES"} {
		if v := os.Getenv(k); v != "" {
			out = append(out, fmt.Sprintf("env %-14s %s", k, v))
		}
	}
	return out
}
