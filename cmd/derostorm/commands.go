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

	// TUI is the full-screen console, or nil when this run is using the
	// compact panel or plain output. Every command that touches it checks,
	// because exactly which console a run has is decided by the terminal and
	// not by the command being typed.
	TUI *TUI
	// SetRefresh changes the console frame rate. Separate from TUI because the
	// ticker it drives lives in the run loop, not in the console.
	SetRefresh func(fps int)
}

const commandHelp = "threads <n> · theme <name> · filter <text> · range <5m..24h> · " +
	"refresh <fps> · animation on|off · save · config · help · quit"

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
		if n == 0 {
			if ctx.Engine.GPUs() == 0 {
				ctx.Log(LogWarn, "threads", "now 0 with no GPU workers running — nothing is mining")
			} else {
				ctx.Log(LogGood, "threads", "CPU off — GPU only")
			}
		} else if cpus := DefaultThreads(); n > cpus {
			ctx.Log(LogWarn, "threads", "now %d — more than the %d CPUs available, expect this to be slower", n, cpus)
		} else {
			ctx.Log(LogGood, "threads", "now %d", n)
		}

	case "theme":
		if len(args) == 0 {
			// Console is nil on a run using the full-screen console, which is
			// most of them. Reaching through it for the current theme name is
			// how "theme" used to take the whole miner down with a nil
			// dereference on the one console where it is easiest to type.
			ctx.Log(LogInfo, "theme", "%s — available: %s",
				ctx.Config.Theme, strings.Join(themeNames(), ", "))
			return
		}
		name := strings.ToLower(args[0])
		th, ok := themes[name]
		if !ok {
			ctx.Log(LogError, "theme", "unknown theme %q — available: %s", args[0], strings.Join(themeNames(), ", "))
			return
		}
		ctx.Config.Theme = name // remember the preference either way
		switch {
		case ctx.TUI != nil:
			ctx.TUI.SetTheme(th)
		case ctx.Console != nil && !ctx.Console.SetTheme(th):
			ctx.Log(LogWarn, "theme", "saved %s as your preference, but this output has no colour (not a terminal, or NO_COLOR)", name)
			return
		}
		ctx.Log(LogGood, "theme", "switched to %s", name)

	case "filter":
		if ctx.TUI == nil {
			ctx.Log(LogWarn, "filter", "only the full-screen console has an event view to filter")
			return
		}
		text := strings.Join(args, " ")
		if strings.EqualFold(text, "off") || strings.EqualFold(text, "none") {
			text = ""
		}
		ctx.TUI.SetLogFilter(text)
		if text == "" {
			ctx.Log(LogGood, "filter", "cleared — showing every event")
		} else {
			ctx.Log(LogGood, "filter", "showing events matching %q", text)
		}

	case "range":
		if ctx.TUI == nil {
			ctx.Log(LogWarn, "range", "only the full-screen console has a chart")
			return
		}
		if len(args) == 0 {
			ctx.Log(LogInfo, "range", "available: %s", strings.Join(rangeLabels, ", "))
			return
		}
		if !ctx.TUI.SetRange(args[0]) {
			ctx.Log(LogError, "range", "unknown window %q — available: %s",
				args[0], strings.Join(rangeLabels, ", "))
			return
		}
		ctx.Config.UI.Range = strings.ToUpper(args[0])
		ctx.Log(LogGood, "range", "chart window is now %s", strings.ToUpper(args[0]))

	case "animation", "anim":
		on := true
		if len(args) > 0 {
			switch strings.ToLower(args[0]) {
			case "off", "no", "0", "false":
				on = false
			case "on", "yes", "1", "true":
			default:
				ctx.Log(LogError, "animation", "expected on or off")
				return
			}
		}
		ctx.Config.UI.Animate = boolPtr(on)
		if ctx.TUI != nil {
			ctx.TUI.SetAnimation(on)
		}
		ctx.Log(LogGood, "animation", "%s", onOff(on))

	case "refresh":
		if len(args) == 0 || ctx.SetRefresh == nil {
			ctx.Log(LogInfo, "refresh", "%d frames per second — use \"refresh <%d-%d>\"",
				ctx.Config.UI.Refresh, minRefreshFPS, maxRefreshFPS)
			return
		}
		n, err := strconv.Atoi(args[0])
		if err != nil || n < minRefreshFPS || n > maxRefreshFPS {
			ctx.Log(LogError, "refresh", "expected a number between %d and %d", minRefreshFPS, maxRefreshFPS)
			return
		}
		ctx.Config.UI.Refresh = n
		ctx.SetRefresh(n)
		ctx.Log(LogGood, "refresh", "console now redraws %d times a second", n)

	case "save":
		// The console's own state is folded in at the moment of saving rather
		// than written back on every key press, so a session spent flicking
		// between screens does not keep rewriting a file on disk.
		if ctx.TUI != nil {
			screen, rng, anim := ctx.TUI.Prefs()
			ctx.Config.UI.Screen, ctx.Config.UI.Range = screen, rng
			ctx.Config.UI.Animate = boolPtr(anim)
		}
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

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
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
