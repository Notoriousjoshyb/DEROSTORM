package main

// The full-screen console: the terminal side of it.
//
// This file owns the terminal and the keyboard and nothing else. It knows how
// to get into the alternate screen buffer and reliably back out of it, how to
// turn a stream of bytes from a raw-mode stdin into key events, which screen is
// showing, and when to draw. What any of those screens contain is in
// tui_dashboard.go and tui_screens.go, and what any of the numbers mean is in
// the engine, which this cannot reach.
//
// The separation is not tidiness. The mining threads and the getwork socket
// run whether or not this exists: the console reads a Snapshot that has
// already been assembled, and every path in here either draws or returns. If
// the renderer panicked on every frame the machine would keep mining, and
// Render is written so that it does not take the process with it if it ever
// does.

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/notoriousjoshyb/derostorm/internal/ui"
)

// ---------------------------------------------------------------- screens

type screenID int

const (
	screenDashboard screenID = iota
	screenMining
	screenStats
	screenNetwork
	screenThreads
	screenConfig
	screenLogs
	screenPools
	screenHelp
)

// navItem is one entry in the footer. Order here is the order on screen.
type navItem struct {
	key   rune
	label string
	id    screenID
}

var navItems = []navItem{
	{'M', "MINING", screenMining},
	{'S', "STATISTICS", screenStats},
	{'N', "NETWORK", screenNetwork},
	{'T', "THREADS", screenThreads},
	{'C', "CONFIG", screenConfig},
	{'L', "LOGS", screenLogs},
	{'P', "POOLS", screenPools},
	{'H', "HELP", screenHelp},
}

func (s screenID) title() string {
	switch s {
	case screenMining:
		return "MINING"
	case screenStats:
		return "STATISTICS"
	case screenNetwork:
		return "NETWORK"
	case screenThreads:
		return "THREADS"
	case screenConfig:
		return "CONFIG"
	case screenLogs:
		return "LOGS"
	case screenPools:
		return "POOLS"
	case screenHelp:
		return "HELP"
	}
	return "DASHBOARD"
}

// ---------------------------------------------------------------- keys

type keyCode int

const (
	keyRune keyCode = iota
	keyEsc
	keyEnter
	keyBackspace
	keyTab
	keyUp
	keyDown
	keyLeft
	keyRight
	keyPgUp
	keyPgDn
	keyHome
	keyEnd
	keyCtrlC
)

type keyEvent struct {
	code keyCode
	r    rune
}

// decodeKeys turns a chunk of raw stdin into key events.
//
// A chunk rather than a byte at a time, because that is how an escape sequence
// arrives: pressing the up arrow puts ESC [ A into the terminal's buffer in one
// go, and a reader taking one byte at a time cannot tell that from someone
// pressing Escape and then typing "[A". Reading what is available and parsing
// the whole of it is both simpler and correct for the case that matters.
//
// Sequences this does not recognise are dropped rather than delivered as their
// individual bytes, so an unhandled function key cannot type a stray letter
// into the command line.
func decodeKeys(b []byte) []keyEvent {
	var out []keyEvent
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0x1b && i+2 < len(b) && (b[i+1] == '[' || b[i+1] == 'O'):
			final := b[i+2]
			n := 3
			// CSI parameters, e.g. ESC [ 5 ~ for Page Up.
			if final >= '0' && final <= '9' {
				j := i + 2
				for j < len(b) && b[j] >= '0' && b[j] <= '9' {
					j++
				}
				if j < len(b) {
					switch string(b[i+2 : j]) {
					case "5":
						out = append(out, keyEvent{code: keyPgUp})
					case "6":
						out = append(out, keyEvent{code: keyPgDn})
					case "1", "7":
						out = append(out, keyEvent{code: keyHome})
					case "4", "8":
						out = append(out, keyEvent{code: keyEnd})
					}
					n = j - i + 1
				} else {
					n = len(b) - i
				}
				i += n
				continue
			}
			switch final {
			case 'A':
				out = append(out, keyEvent{code: keyUp})
			case 'B':
				out = append(out, keyEvent{code: keyDown})
			case 'C':
				out = append(out, keyEvent{code: keyRight})
			case 'D':
				out = append(out, keyEvent{code: keyLeft})
			case 'H':
				out = append(out, keyEvent{code: keyHome})
			case 'F':
				out = append(out, keyEvent{code: keyEnd})
			}
			i += n
		case c == 0x1b:
			out = append(out, keyEvent{code: keyEsc})
			i++
		case c == '\r' || c == '\n':
			out = append(out, keyEvent{code: keyEnter})
			i++
		case c == 0x7f || c == 0x08:
			out = append(out, keyEvent{code: keyBackspace})
			i++
		case c == '\t':
			out = append(out, keyEvent{code: keyTab})
			i++
		case c == 0x03:
			out = append(out, keyEvent{code: keyCtrlC})
			i++
		case c == 0x15: // ctrl-u
			out = append(out, keyEvent{code: keyRune, r: rune(0x15)})
			i++
		case c >= 0x20 && c < 0x7f:
			out = append(out, keyEvent{code: keyRune, r: rune(c)})
			i++
		default:
			i++ // control byte with no meaning here
		}
	}
	return out
}

// ReadKeys pumps raw stdin into a key channel. It returns when stdin closes,
// which is what happens on shutdown once the terminal mode is restored.
func ReadKeys(ch chan<- keyEvent, done <-chan struct{}) {
	buf := make([]byte, 32)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			if err == io.EOF || err != nil {
				return
			}
			continue
		}
		for _, k := range decodeKeys(buf[:n]) {
			select {
			case ch <- k:
			case <-done:
				return
			}
		}
	}
}

// ---------------------------------------------------------------- the console

// TUI is the full-screen console.
type TUI struct {
	mu    sync.Mutex
	out   *os.File
	scr   *ui.Screen
	theme *Theme

	screen screenID
	// logScroll is how many lines back from the newest the log view is
	// scrolled. Zero means pinned to the bottom, which is where it returns
	// whenever the user changes screen -- a log frozen at the position it
	// happened to be left at three screens ago is a trap.
	logScroll int

	// cmd is the runtime command line, shown only while it is open. Keeping it
	// behind a key rather than always on screen buys back a row for the
	// dashboard and stops single-key navigation from typing into it.
	cmdOpen bool
	cmd     []rune
	cmdHint string

	frame int
	// blockUntil is when the found-a-block banner should stop being drawn.
	blockUntil  time.Time
	lastBlockAt time.Time

	// entered records whether the alternate buffer is currently in use, so
	// Close is safe to call from a signal handler and from a defer without the
	// terminal being reset twice.
	entered bool
}

func NewTUI(out *os.File, theme *Theme, w, h int) *TUI {
	return &TUI{
		out:   out,
		theme: theme,
		scr:   ui.NewScreen(out, w, h, theme.Name != "mono"),
	}
}

// Enter switches to the alternate screen buffer.
func (t *TUI) Enter() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.entered {
		return
	}
	t.scr.Enter()
	t.entered = true
}

// Close puts the terminal back exactly as it was found: normal buffer, cursor
// visible, default colours.
//
// Safe to call more than once and from more than one goroutine, because it is
// called from a defer, from the interrupt handler and from the quit command,
// and the one thing that must never happen is the program exiting with the
// terminal still in the alternate buffer.
func (t *TUI) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.entered {
		return
	}
	t.scr.Leave()
	t.entered = false
}

func (t *TUI) SetTheme(th *Theme) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.theme = th
	t.scr.Invalidate()
}

func (t *TUI) Screen() screenID {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.screen
}

// CommandOpen reports whether the command line has the keyboard.
func (t *TUI) CommandOpen() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cmdOpen
}

// minCols and minRows are the smallest window the console will draw in. Below
// either, it says so and waits rather than drawing a broken frame: a dashboard
// with its borders overlapping its values is worse than an honest message,
// because it looks like the program is malfunctioning rather than the window
// being too small.
const (
	minCols = 56
	minRows = 14
)

// Key handles one key press. It returns the command line to run, if the user
// just submitted one, and whether the program should quit.
func (t *TUI) Key(k keyEvent) (cmd string, quit bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if k.code == keyCtrlC {
		return "", true
	}

	if t.cmdOpen {
		switch k.code {
		case keyEsc:
			t.cmdOpen, t.cmd = false, t.cmd[:0]
		case keyEnter:
			line := strings.TrimSpace(string(t.cmd))
			t.cmdOpen, t.cmd = false, t.cmd[:0]
			return line, false
		case keyBackspace:
			if n := len(t.cmd); n > 0 {
				t.cmd = t.cmd[:n-1]
			}
		case keyRune:
			if k.r == 0x15 { // ctrl-u
				t.cmd = t.cmd[:0]
			} else if len(t.cmd) < 120 {
				t.cmd = append(t.cmd, k.r)
			}
		}
		return "", false
	}

	switch k.code {
	case keyEsc:
		t.screen, t.logScroll = screenDashboard, 0
		t.scr.Invalidate()
		return "", false
	case keyUp:
		t.logScroll++
		return "", false
	case keyDown:
		if t.logScroll > 0 {
			t.logScroll--
		}
		return "", false
	case keyPgUp:
		t.logScroll += 10
		return "", false
	case keyPgDn:
		t.logScroll -= 10
		if t.logScroll < 0 {
			t.logScroll = 0
		}
		return "", false
	case keyHome:
		t.logScroll = 1 << 20 // clamped against the real length when drawn
		return "", false
	case keyEnd:
		t.logScroll = 0
		return "", false
	case keyTab:
		t.screen = screenID((int(t.screen) + 1) % (int(screenHelp) + 1))
		t.logScroll = 0
		t.scr.Invalidate()
		return "", false
	case keyRune:
		switch k.r {
		case ':', '/':
			t.cmdOpen = true
			t.cmd = t.cmd[:0]
			return "", false
		case 'q', 'Q':
			return "", true
		case 'd', 'D':
			t.screen, t.logScroll = screenDashboard, 0
			t.scr.Invalidate()
			return "", false
		case 'j':
			t.logScroll++
			return "", false
		case 'k':
			if t.logScroll > 0 {
				t.logScroll--
			}
			return "", false
		}
		for _, n := range navItems {
			if k.r == n.key || k.r == n.key+32 { // upper or lower case
				if t.screen == n.id {
					t.screen = screenDashboard // pressing it again comes back
				} else {
					t.screen = n.id
				}
				t.logScroll = 0
				t.scr.Invalidate()
				return "", false
			}
		}
	}
	return "", false
}

// Render draws one frame.
//
// It recovers from a panic and keeps going. That is not a licence for the
// drawing code to be careless -- everything in it is written to survive a
// zero-width rectangle -- but a miner that has been running for three weeks
// should not be taken down by an arithmetic slip in a panel, and a caller
// looking at the recovered error can tell that is what happened.
func (t *TUI) Render(s Snapshot) (err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.entered {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("console frame failed: %v", r)
			t.scr.Invalidate()
		}
	}()

	cols, rows := TerminalWidth(), TerminalHeight()
	if cols <= 0 || rows <= 0 {
		cols, rows = t.scr.Size()
	}
	t.scr.Resize(cols, rows)

	t.frame = s.Frame
	cv := t.scr.Buf()

	// The block banner is latched rather than driven straight off the
	// snapshot: a block is found in an instant, and the news should stay on
	// screen long enough to be read by someone who was looking away.
	if !s.BlockEvent.IsZero() && s.BlockEvent.After(t.lastBlockAt) {
		t.lastBlockAt = s.BlockEvent
		t.blockUntil = time.Now().Add(8 * time.Second)
	}

	renderFrame(cv, s, t.theme, frameOpts{
		screen:    t.screen,
		frame:     t.frame,
		logScroll: t.logScroll,
		cmd:       string(t.cmd),
		cmdOpen:   t.cmdOpen,
	})

	if time.Now().Before(t.blockUntil) {
		drawBlockBanner(cv, cv.Bounds(), s, t.theme, t.frame)
	}

	t.scr.Flush()
	return nil
}

// frameOpts is the console's own state, separated from the snapshot so that a
// frame can be rendered without a console at all -- which is what --preview
// does, and what the layout tests do.
type frameOpts struct {
	screen    screenID
	frame     int
	logScroll int
	cmd       string
	cmdOpen   bool
}

// renderFrame draws one complete frame -- header, screen, footer -- into cv.
//
// A free function rather than a method, so exactly the same code path produces
// the live console, the --preview dump and whatever a test asks for. A preview
// that went through a second rendering path would be a picture of a program
// that does not exist.
func renderFrame(cv *ui.Canvas, s Snapshot, th *Theme, o frameOpts) {
	full := cv.Bounds()
	if full.W < minCols || full.H < minRows {
		drawTooSmall(cv, full, th)
		return
	}

	footH := 1
	if o.cmdOpen {
		footH = 2
	}
	rows := full.Rows(headerHeight(full.W, full.H), -1, footH)
	drawHeader(cv, rows[0], s, th, o.frame)
	drawFooter(cv, rows[2], s, th, o)

	body := rows[1]
	switch o.screen {
	case screenDashboard:
		drawDashboard(cv, body, s, th, o.frame)
	case screenMining:
		drawMiningScreen(cv, body, s, th, o.frame)
	case screenStats:
		drawStatsScreen(cv, body, s, th)
	case screenNetwork:
		drawNetworkScreen(cv, body, s, th, o.frame)
	case screenThreads:
		drawThreadsScreen(cv, body, s, th)
	case screenConfig:
		drawConfigScreen(cv, body, s, th)
	case screenLogs:
		drawLogScreen(cv, body, s, th, o.logScroll)
	case screenPools:
		drawPoolsScreen(cv, body, s, th)
	case screenHelp:
		drawHelpScreen(cv, body, th)
	}
}

func drawFooter(cv *ui.Canvas, r ui.Rect, s Snapshot, th *Theme, o frameOpts) {
	if r.Empty() {
		return
	}
	y := r.Bottom() - 1

	if o.cmdOpen && r.H >= 2 {
		line := o.cmd
		cv.Text(r.X+1, r.Y, string(ui.Chevron), ui.Style{FG: th.Accent, Bold: true})
		if line == "" {
			cv.TextIn(r.X+3, r.Y, r.W-4, commandHelp, ui.Style{FG: th.Dim})
		} else {
			w := cv.TextIn(r.X+3, r.Y, r.W-5, line, ui.Style{FG: th.Text})
			cv.Set(r.X+3+w, r.Y, '▏', ui.Style{FG: th.Accent2})
		}
	}

	right := "DEROSTORM v" + version
	if s.Testnet {
		right = "DEROSTORM v" + version + " · testnet"
	}
	// Where the key bar has to stop. Measured from the wordmark rather than
	// set to a fixed margin -- the margin was fourteen columns and the
	// wordmark is sixteen, so on a narrow window the last key ran into it and
	// the footer read "[Q] QUITDEROSTORM v1.4.1". On a window too narrow for
	// both, the keys win: they are the only thing on the row anyone acts on.
	limit := r.Right() - 1
	if r.W > ui.Width(right)+22 {
		limit -= ui.Width(right) + 2
	} else {
		right = ""
	}

	// The key bar. Drawn from the left and simply stopped when the width runs
	// out, rather than compressed: half a legible key list beats a full one
	// that has been abbreviated into guesswork.
	x := r.X + 1
	for _, n := range navItems {
		item := fmt.Sprintf("[%c] %s", n.key, n.label)
		if x+ui.Width(item) > limit {
			break
		}
		active := o.screen == n.id
		kfg, lfg := th.Accent, th.Muted
		if active {
			kfg, lfg = th.Accent, th.Accent
		}
		cv.Set(x, y, '[', ui.Style{FG: th.Dim})
		cv.Set(x+1, y, n.key, ui.Style{FG: kfg, Bold: true})
		cv.Set(x+2, y, ']', ui.Style{FG: th.Dim})
		cv.Text(x+4, y, n.label, ui.Style{FG: lfg, Bold: active})
		x += ui.Width(item) + 3
	}
	if x+8 <= limit {
		cv.Set(x, y, '[', ui.Style{FG: th.Dim})
		cv.Set(x+1, y, 'Q', ui.Style{FG: th.Err, Bold: true})
		cv.Set(x+2, y, ']', ui.Style{FG: th.Dim})
		cv.Text(x+4, y, "QUIT", ui.Style{FG: th.Muted})
	}
	if right != "" {
		cv.TextRight(r.Right()-1, y, right, ui.Style{FG: th.Dim})
	}
}

func drawTooSmall(cv *ui.Canvas, r ui.Rect, th *Theme) {
	cols, rows := r.W, r.H
	msg := []string{
		"DEROSTORM",
		"",
		fmt.Sprintf("terminal is %d x %d", cols, rows),
		fmt.Sprintf("the console needs at least %d x %d", minCols, minRows),
		"",
		"mining continues — resize the window",
	}
	top := (rows - len(msg)) / 2
	for i, line := range msg {
		fg := th.Muted
		switch i {
		case 0:
			fg = th.Accent
		case 5:
			fg = th.Good
		}
		cv.TextCenter(0, top+i, cols, line, ui.Style{FG: fg, Bold: i == 0})
	}
}
