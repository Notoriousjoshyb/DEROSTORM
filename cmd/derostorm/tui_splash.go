package main

// The start-up transition.
//
// The rule this file exists to obey is that the splash must never delay a
// single hash. It is not a stage of start-up with a duration; it is a picture
// drawn *between* the stages that were happening anyway.
//
// The run loop starts the getwork socket, sets the thread count, hands the GPU
// its device list and starts the sensors, in that order, and each of those
// takes as long as it takes. After each one, one frame is drawn -- a few
// hundred microseconds into the alternate buffer -- and then the next stage
// begins immediately. Nothing sleeps, nothing waits for an animation, and if
// the whole start-up happens in five milliseconds then the splash is on screen
// for five milliseconds and the dashboard takes over. That is the correct
// behaviour: the splash is there to fill a wait, not to create one.

import (
	"github.com/notoriousjoshyb/derostorm/internal/ui"
)

// splashSteps are the subsystems the transition reports on, in the order the
// run loop brings them up.
//
// A tick means "started", not "connected": the getwork socket is still dialling
// when its tick appears, and waiting for the dial before drawing the tick would
// mean the splash was waiting for the network -- which is the one thing it must
// never do. What the node is actually doing is on the dashboard a fraction of a
// second later, and it is the honest place for it.
var splashSteps = []string{"NODE", "CPU", "GPU", "TELEMETRY"}

// splash draws the start-up screen and remembers which steps are done.
type splash struct {
	tui  *TUI
	done map[string]bool
	// skipped is a step that was never going to happen on this run -- a
	// machine mining without a GPU, for instance. It is drawn as a dash
	// rather than a tick, because a tick against a subsystem that is not
	// running is the sort of small lie that makes a whole dashboard
	// untrustworthy.
	off map[string]bool
}

func newSplash(t *TUI) *splash {
	s := &splash{tui: t, done: map[string]bool{}, off: map[string]bool{}}
	s.paint()
	return s
}

// step marks a subsystem as up and redraws.
func (s *splash) step(name string) {
	if s == nil {
		return
	}
	s.done[name] = true
	s.paint()
}

// skip marks a subsystem as not part of this run.
func (s *splash) skip(name string) {
	if s == nil {
		return
	}
	s.off[name] = true
	s.paint()
}

// done finishes the transition. The next frame the run loop paints is the
// dashboard, and because the screen is diffed rather than cleared, the
// changeover costs only the cells that differ.
func (s *splash) finish() {}

func (s *splash) paint() {
	if s == nil || s.tui == nil {
		return
	}
	s.tui.Splash(s.done, s.off)
}

// Splash draws one frame of the start-up screen.
//
// It goes through the same Screen and the same diffing flush as every other
// frame, so entering and leaving it cannot leave the terminal in a state the
// dashboard does not expect.
func (t *TUI) Splash(done, off map[string]bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.entered {
		return
	}
	defer func() {
		// The same guarantee Render makes: a slip in the drawing code must not
		// take a mining machine down with it.
		if r := recover(); r != nil {
			t.scr.Invalidate()
		}
	}()

	cols, rows := TerminalWidth(), TerminalHeight()
	if cols <= 0 || rows <= 0 {
		cols, rows = t.scr.Size()
	}
	t.scr.Resize(cols, rows)
	drawSplash(t.scr.Buf(), t.theme, done, off)
	t.scr.Flush()
}

func drawSplash(cv *ui.Canvas, th *Theme, done, off map[string]bool) {
	r := cv.Bounds()
	if r.W < 24 || r.H < 8 {
		return
	}

	// Everything is stacked around the middle of the window: the wordmark, a
	// line saying what is starting, the four ticks, and the tagline.
	list := len(splashSteps)
	markH := len(ui.WordmarkLarge)
	if r.W < ui.ArtWidth(ui.WordmarkLarge)+4 || r.H < markH+list+8 {
		markH = len(ui.WordmarkSmall)
	}
	block := markH + 2 + list + 3
	top := r.Y + (r.H-block)/2
	if top < r.Y {
		top = r.Y
	}

	y := top
	name := ui.Rect{X: r.X, Y: y, W: r.W, H: markH}
	y += drawWordmark(cv, name, th)
	y++

	cv.TextCenter(r.X, y, r.W, "INITIALISING ASTROBWTv3 ENGINE", ui.Style{FG: th.Muted})
	y += 2

	// The list is centred as a block, not per row, so the ticks line up under
	// each other instead of drifting with the length of each label.
	const labelW = 14
	x := r.X + (r.W-labelW-4)/2
	for _, name := range splashSteps {
		if y >= r.Bottom()-2 {
			break
		}
		mark, fg := "·", th.Dim
		switch {
		case off[name]:
			mark, fg = "–", th.Dim
		case done[name]:
			mark, fg = "✓", th.Good
		}
		cv.Set(x, y, '[', ui.Style{FG: th.Dim})
		cv.Text(x+1, y, mark, ui.Style{FG: fg, Bold: true})
		cv.Set(x+2, y, ']', ui.Style{FG: th.Dim})
		lfg := th.Muted
		if done[name] {
			lfg = th.Text
		}
		cv.Text(x+4, y, name, ui.Style{FG: lfg})
		y++
	}

	y++
	if y < r.Bottom() {
		cv.TextCenter(r.X, y, r.W, ui.TrackedCaps("MINING IN THE STORM"),
			ui.Style{FG: th.Accent2, Bold: true})
	}
	// A rule the width of the block, in the wordmark's own gradient, so the
	// splash and the header are visibly the same design.
	if ruleY := r.Y + r.H - 2; ruleY > y && r.W > 40 {
		w := r.W / 3
		for i := 0; i < w; i++ {
			cv.Set(r.X+(r.W-w)/2+i, ruleY, '─', ui.Style{FG: th.Ramp(float64(i) / float64(w-1))})
		}
	}
}
