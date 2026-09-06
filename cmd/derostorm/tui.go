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

// name is the screen as --screen and the saved preference spell it: the same
// vocabulary screenByName parses, so a preference written by one run is
// understood by the next.
func (s screenID) name() string {
	if s == screenDashboard {
		return "dashboard"
	}
	return strings.ToLower(s.title())
}

// ---------------------------------------------------------------- keys

type keyCode int

const (
	keyRune keyCode = iota
	keyEsc
	keyEnter
	keyBackspace
	keyTab
	keyBackTab
	keyUp
	keyDown
	keyLeft
	keyRight
	keyPgUp
	keyPgDn
	keyHome
	keyEnd
	keyCtrlC
	// keyFocusIn and keyFocusOut are not keys. They are what a terminal that
	// understands focus reporting sends when its window gains or loses focus,
	// and they arrive on stdin mixed in with the key presses, so this is where
	// they have to be decoded.
	keyFocusIn
	keyFocusOut
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
			case 'I':
				out = append(out, keyEvent{code: keyFocusIn})
			case 'O':
				out = append(out, keyEvent{code: keyFocusOut})
			case 'Z':
				out = append(out, keyEvent{code: keyBackTab})
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
	// log is the event view's own state: where it is scrolled, whether it is
	// following, and what it is filtered to. The scroll returns to the bottom
	// whenever the user changes screen -- a log frozen at the position it
	// happened to be left at three screens ago is a trap -- but the filter
	// does not, because a filter is a decision and a scroll position is not.
	log logView

	// histRange is which window the hashrate chart is showing, and gpuSel
	// which card the GPU panel is on. Both are indices into tables that
	// cannot be empty, and both are wrapped rather than clamped so holding
	// the key down cycles instead of sticking at an end.
	histRange int
	gpuSel    int

	// anim is whether decorative animation runs, and focused whether the
	// terminal window currently has focus. Animation needs both: there is no
	// point spending frames turning a globe in a window nobody is looking at.
	//
	// focused starts true, because a terminal that does not support focus
	// reporting will never say either way and "assume it is being watched" is
	// the only answer that cannot leave a visible console frozen.
	anim    bool
	focused bool

	// title, titleAt and lastTitle drive the window title. See setTitle.
	title     bool
	titleAt   time.Time
	lastTitle string

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

// TUIOpts is what a run's flags and saved preferences say the console should
// start as. Everything in it is a display decision; none of it reaches mining.
type TUIOpts struct {
	Anim      bool   // decorative animation
	Title     bool   // rewrite the terminal's title bar
	Screen    string // the screen to open on
	HistRange string // the hashrate window to open on
}

func NewTUI(out *os.File, theme *Theme, w, h int, o TUIOpts) *TUI {
	t := &TUI{
		out:     out,
		theme:   theme,
		scr:     ui.NewScreen(out, w, h, theme.Name != "mono"),
		anim:    o.Anim,
		title:   o.Title,
		focused: true,
	}
	t.screen = screenByName(o.Screen)
	for i, r := range histRanges {
		if strings.EqualFold(r.label, o.HistRange) {
			t.histRange = i
		}
	}
	t.scr.SetBG(theme.PanelBG)
	return t
}

// Prefs is the console state worth remembering between runs. Read by the
// "save" command; nothing here can change how the miner mines.
func (t *TUI) Prefs() (screen, histRange string, anim bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.screen.name(), histRanges[t.histRange].label, t.anim
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
	t.scr.SetBG(th.PanelBG)
	t.scr.Invalidate()
}

func (t *TUI) Screen() screenID {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.screen
}

// SetLogFilter is the "filter" runtime command: show only lines containing
// this text. An empty string clears it.
func (t *TUI) SetLogFilter(text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.log.text = text
	t.log.min = LogInfo
	t.log.scroll = 0
}

// SetRange is the "range" runtime command: pick the chart window by name.
func (t *TUI) SetRange(label string) bool {
	for i, r := range histRanges {
		if strings.EqualFold(r.label, label) {
			t.mu.Lock()
			t.histRange = i
			t.mu.Unlock()
			return true
		}
	}
	return false
}

// SetAnimation is the "animation on|off" runtime command.
func (t *TUI) SetAnimation(on bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.anim = on
	t.scr.Invalidate()
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

	// Focus is not a key press and must not be treated as one: it arrives
	// while the user is typing in another window, and letting it through the
	// switch below would mean alt-tabbing away scrolled the log.
	switch k.code {
	case keyFocusIn:
		t.focused = true
		t.scr.Invalidate()
		return "", false
	case keyFocusOut:
		t.focused = false
		return "", false
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

	// Scrolling back pauses the follow, and returning to the bottom resumes
	// it. Anything else is a trap: a user who scrolls up to read a line and
	// then watches it get pushed off the top has been given a scrollbar that
	// does not work.
	scrollBy := func(n int) {
		t.log.scroll += n
		if t.log.scroll < 0 {
			t.log.scroll = 0
		}
		t.log.paused = t.log.scroll > 0
	}

	switch k.code {
	case keyEsc:
		t.goTo(screenDashboard)
		return "", false
	case keyUp:
		scrollBy(1)
		return "", false
	case keyDown:
		scrollBy(-1)
		return "", false
	case keyPgUp:
		scrollBy(10)
		return "", false
	case keyPgDn:
		scrollBy(-10)
		return "", false
	case keyHome:
		scrollBy(1 << 20) // clamped against the real length when drawn
		return "", false
	case keyEnd:
		t.log.scroll, t.log.paused = 0, false
		return "", false
	case keyLeft:
		t.histRange = clampRange(t.histRange - 1)
		return "", false
	case keyRight:
		t.histRange = clampRange(t.histRange + 1)
		return "", false
	case keyTab:
		t.goTo(screenID((int(t.screen) + 1) % (int(screenHelp) + 1)))
		return "", false
	case keyBackTab:
		t.goTo(screenID((int(t.screen) + int(screenHelp)) % (int(screenHelp) + 1)))
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
			t.goTo(screenDashboard)
			return "", false
		case 'j':
			scrollBy(1)
			return "", false
		case 'k':
			scrollBy(-1)
			return "", false
		case '[', '<', ',':
			t.histRange = clampRange(t.histRange - 1)
			return "", false
		case ']', '>', '.':
			t.histRange = clampRange(t.histRange + 1)
			return "", false
		case 'g':
			t.gpuSel++
			return "", false
		case 'a', 'A':
			t.anim = !t.anim
			t.scr.Invalidate()
			return "", false
		case 'f', 'F':
			// All -> shares and better -> warnings and better -> errors only.
			t.log.text = ""
			switch t.log.min {
			case LogInfo:
				t.log.min = LogGood
			case LogGood:
				t.log.min = LogWarn
			case LogWarn:
				t.log.min = LogError
			default:
				t.log.min = LogInfo
			}
			t.log.scroll = 0
			return "", false
		case 'x', 'X':
			t.log.clearedAt = time.Now()
			t.log.scroll = 0
			return "", false
		case ' ':
			t.log.paused = !t.log.paused
			if !t.log.paused {
				t.log.scroll = 0
			}
			return "", false
		case '?':
			t.goTo(screenHelp)
			return "", false
		}
		for _, n := range navItems {
			if k.r == n.key || k.r == n.key+32 { // upper or lower case
				if t.screen == n.id {
					t.goTo(screenDashboard) // pressing it again comes back
				} else {
					t.goTo(n.id)
				}
				return "", false
			}
		}
	}
	return "", false
}

// goTo changes screen, unpins the log and forces a repaint. Every navigation
// path goes through it so none of them can forget one of the three.
func (t *TUI) goTo(id screenID) {
	t.screen = id
	t.log.scroll, t.log.paused = 0, false
	t.scr.Invalidate()
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
		cmd:       string(t.cmd),
		cmdOpen:   t.cmdOpen,
		anim:      t.anim && t.focused,
		histRange: t.histRange,
		gpuSel:    t.gpuSel,
		log:       t.log,
	})

	if time.Now().Before(t.blockUntil) {
		drawBlockBanner(cv, cv.Bounds(), s, t.theme, t.frame)
	}
	t.updateTitle(s)

	t.scr.Flush()
	return nil
}

// frameOpts is the console's own state, separated from the snapshot so that a
// frame can be rendered without a console at all -- which is what --preview
// does, and what the layout tests do.
type frameOpts struct {
	screen  screenID
	frame   int
	cmd     string
	cmdOpen bool

	// anim is whether decorative animation runs. When it is off, every widget
	// that would have cycled a frame is handed a frozen counter instead of
	// being given a flag of its own -- one place decides, and no widget has to
	// remember to check.
	anim bool

	// histRange is the hashrate window on show, gpuSel the card the GPU panel
	// is on, and log the event view's filter and follow state.
	histRange int
	gpuSel    int
	log       logView
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
	// af is the animation clock. It is the frame counter when decoration is
	// running and a constant when it is not, so --no-animation costs one
	// assignment here instead of a conditional inside every widget that moves.
	af := 0
	if o.anim {
		af = o.frame
	}
	rows := full.Rows(headerHeight(full.W, full.H), -1, footH)
	drawHeader(cv, rows[0], s, th, af)
	drawFooter(cv, rows[2], s, th, o)

	body := rows[1]
	switch o.screen {
	case screenDashboard:
		drawDashboard(cv, body, s, th, af, o)
	case screenMining:
		drawMiningScreen(cv, body, s, th, af, o)
	case screenStats:
		drawStatsScreen(cv, body, s, th)
	case screenNetwork:
		drawNetworkScreen(cv, body, s, th, af)
	case screenThreads:
		drawThreadsScreen(cv, body, s, th)
	case screenConfig:
		drawConfigScreen(cv, body, s, th)
	case screenLogs:
		drawLogScreen(cv, body, s, th, o.log)
	case screenPools:
		drawPoolsScreen(cv, body, s, th)
	case screenHelp:
		drawHelpScreen(cv, body, th)
	}
}

// footerBrands are the run's signature, longest first. The footer takes the
// longest one that still leaves the key bar whole.
//
// Longest-that-fits rather than a width breakpoint. A breakpoint has to be
// guessed against a key bar whose width depends on how many screens there are,
// and the guess was wrong: at 170 columns the full signature fitted the test
// and then pushed HELP and QUIT off the end of the row. Measuring the keys
// first and spending what is left cannot be wrong that way.
var footerBrands = []string{
	" │ ASTROBWTv3 │ DERO │ PRIVACY · FREEDOM · DECENTRALISATION",
	" │ ASTROBWTv3 │ DERO",
	" │ DERO",
	"",
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

	// How wide the key bar would like to be, measured before anything is
	// drawn. The keys are the only thing on this row anyone acts on, so they
	// are laid out first and the signature gets the remainder.
	keysW := 1
	for _, n := range navItems {
		keysW += ui.Width(n.label) + 7
	}
	keysW += 8 // [Q] QUIT

	base := "DEROSTORM v" + version
	if s.Testnet {
		base += " · testnet"
	}
	right := ""
	for _, brand := range footerBrands {
		if keysW+ui.Width(base)+ui.Width(brand)+3 <= r.W {
			right = base + brand
			break
		}
	}
	if right == "" && r.W > keysW+ui.Width(base)+3 {
		right = base
	}

	limit := r.Right() - 1
	if right != "" {
		limit -= ui.Width(right) + 2
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
		st := ui.Style{}
		if active {
			// The selected screen is painted rather than only brightened. On a
			// screen with this much colour on it a hue change alone does not
			// read as "you are here", and the fill survives the drop to
			// sixteen colours where several accents collapse together.
			lfg = th.Accent
			st = ui.Style{BG: th.SelBG}
			for i := -1; i < ui.Width(item)+1; i++ {
				cv.Set(x+i, y, ' ', st)
			}
		}
		cv.Set(x, y, '[', ui.Style{FG: th.Dim, BG: st.BG})
		cv.Set(x+1, y, n.key, ui.Style{FG: kfg, Bold: true, BG: st.BG})
		cv.Set(x+2, y, ']', ui.Style{FG: th.Dim, BG: st.BG})
		cv.Text(x+4, y, n.label, ui.Style{FG: lfg, Bold: active, BG: st.BG})
		x += ui.Width(item) + 3
	}
	if x+8 <= limit {
		cv.Set(x, y, '[', ui.Style{FG: th.Dim})
		cv.Set(x+1, y, 'Q', ui.Style{FG: th.Err, Bold: true})
		cv.Set(x+2, y, ']', ui.Style{FG: th.Dim})
		cv.Text(x+4, y, "QUIT", ui.Style{FG: th.Muted})
	}
	if right != "" {
		drawBrand(cv, r.Right()-1, y, right, th)
	}
}

// drawBrand writes the signature with the product name in the wordmark
// gradient and the rest of it quiet, so the footer carries the identity
// without shouting a version number at anyone.
func drawBrand(cv *ui.Canvas, right, y int, s string, th *Theme) {
	x := right - ui.Width(s)
	name := "DEROSTORM"
	for i, ch := range name {
		cv.Set(x+i, y, ch, ui.Style{FG: th.Ramp(float64(i) / float64(len(name)-1)), Bold: true})
	}
	cv.Text(x+ui.Width(name), y, s[len(name):], ui.Style{FG: th.Dim})
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
