package main

// The runtime command line: a minimal editor whose buffer is rendered as part
// of the live frame, plus the commands themselves.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
)

// ---------------------------------------------------------------- editor

// Editor holds the line being typed. The console reads Buffer() each frame and
// draws it; nothing is ever echoed by the terminal itself.
type Editor struct {
	mu  sync.Mutex
	buf []rune
}

func (e *Editor) Buffer() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return string(e.buf)
}

// Feed consumes one input byte. It returns a completed line when Enter is
// pressed.
func (e *Editor) Feed(b byte) (line string, complete bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	switch {
	case b == '\r' || b == '\n':
		line = strings.TrimSpace(string(e.buf))
		e.buf = e.buf[:0]
		return line, true
	case b == 0x7f || b == 0x08: // backspace / delete
		if n := len(e.buf); n > 0 {
			e.buf = e.buf[:n-1]
		}
	case b == 0x15: // ctrl-u, clear the line
		e.buf = e.buf[:0]
	case b == 0x1b: // escape, discard
		e.buf = e.buf[:0]
	case b >= 0x20 && b < 0x7f:
		if len(e.buf) < 120 {
			e.buf = append(e.buf, rune(b))
		}
	}
	return "", false
}

// ---------------------------------------------------------------- dispatch

// CommandContext is everything a command is allowed to touch.
type CommandContext struct {
	Engine  *Engine
	Console *Console
	Config  *Config
	Path    string
	Quit    func()
	Log     func(LogLevel, string, string, ...interface{})
}

const commandHelp = "threads <n> · theme <name> · save · config · help · quit"

// RunCommand executes one typed line.
func RunCommand(ctx CommandContext, line string) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return
	}
	cmd := strings.ToLower(fields[0])
	args := fields[1:]

	switch cmd {

	case "t", "threads":
		if len(args) == 0 {
			ctx.Log(LogInfo, "threads", "%d running — use \"threads <n>\" to change", ctx.Engine.Threads())
			return
		}
		n, err := parseThreadArg(args[0], ctx.Engine.Threads())
		if err != nil {
			ctx.Log(LogError, "threads", "%v", err)
			return
		}
		if err := ctx.Engine.SetThreads(n); err != nil {
			ctx.Log(LogError, "threads", "%v", err)
			return
		}
		ctx.Config.Threads = n
		if cpus := DefaultThreads(); n > cpus {
			ctx.Log(LogWarn, "threads", "now %d — more than the %d CPUs available, expect this to be slower", n, cpus)
		} else {
			ctx.Log(LogGood, "threads", "now %d", n)
		}

	case "theme":
		if len(args) == 0 {
			ctx.Log(LogInfo, "theme", "%s — available: %s", ctx.Console.Theme().Name, strings.Join(themeNames(), ", "))
			return
		}
		name := strings.ToLower(args[0])
		th, ok := themes[name]
		if !ok {
			ctx.Log(LogError, "theme", "unknown theme %q — available: %s", args[0], strings.Join(themeNames(), ", "))
			return
		}
		ctx.Config.Theme = name // remember the preference either way
		if !ctx.Console.SetTheme(th) {
			ctx.Log(LogWarn, "theme", "saved %s as your preference, but this output has no colour (not a terminal, or NO_COLOR)", name)
			return
		}
		ctx.Log(LogGood, "theme", "switched to %s", name)

	case "save":
		if err := ctx.Config.Save(ctx.Path); err != nil {
			ctx.Log(LogError, "save", "%v", err)
			return
		}
		ctx.Log(LogGood, "save", "written to %s", ctx.Path)

	case "config", "c":
		ctx.Log(LogInfo, "config", "node %s · threads %d · theme %s · %s",
			ctx.Config.Node, ctx.Config.Threads, ctx.Config.Theme, netName(ctx.Config.Testnet))
		ctx.Log(LogInfo, "config", "wallet %s", ctx.Config.Wallet)
		ctx.Log(LogInfo, "config", "file %s", ctx.Path)

	case "h", "help", "?":
		ctx.Log(LogInfo, "help", "%s", commandHelp)
		ctx.Log(LogInfo, "help", "threads accepts +2, -4 or an absolute number")

	case "q", "quit", "exit", "bye":
		ctx.Log(LogInfo, "quit", "shutting down")
		ctx.Quit()

	default:
		ctx.Log(LogError, "command", "unknown command %q — %s", cmd, commandHelp)
	}
}

// parseThreadArg accepts an absolute count or a relative adjustment (+2, -4).
func parseThreadArg(s string, current int) (int, error) {
	rel := 0
	switch {
	case strings.HasPrefix(s, "+"):
		rel = 1
		s = s[1:]
	case strings.HasPrefix(s, "-"):
		rel = -1
		s = s[1:]
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("expected a number, +n or -n, got %q", s)
	}
	if rel != 0 {
		n = current + rel*n
	}
	if n < 0 || n > maxThreads {
		return 0, fmt.Errorf("threads must be between 0 and %d", maxThreads)
	}
	return n, nil
}

// ---------------------------------------------------------------- reader

// ReadCommands pumps stdin into the dispatcher. In raw mode it feeds the editor
// one byte at a time so the console can draw the line; otherwise it falls back
// to whole lines from the terminal's own buffer.
func ReadCommands(raw bool, ed *Editor, dispatch func(string)) {
	if !raw {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			dispatch(strings.TrimSpace(sc.Text()))
		}
		return
	}

	var b [1]byte
	for {
		n, err := os.Stdin.Read(b[:])
		if err == io.EOF {
			return
		}
		if err != nil || n == 0 {
			return
		}
		if line, done := ed.Feed(b[0]); done && line != "" {
			dispatch(line)
		}
	}
}
