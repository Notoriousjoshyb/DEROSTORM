package main

// The dashboard's header, its responsive grid, and the block-found banner.
//
// The grid is the part worth reading. A terminal dashboard has to work at 80
// columns and at 220, in a 24-row window and a 60-row one, and the usual way
// that is attempted -- a fixed layout per breakpoint -- produces four layouts
// that each break at some size between the breakpoints.
//
// Instead every panel declares the narrowest width it can still say something
// useful in, and every band declares the shortest it can be. The layout then:
//
//  1. packs each band's panels into as many rows as the window's width needs,
//  2. keeps bands from the top until the height runs out,
//  3. hands the leftover rows back to the bands that benefit most.
//
// Which means there are no breakpoints at all. The layout is continuous, a
// panel is either drawn at a size it can work at or not drawn, and nothing ever
// overlaps because every panel is handed a rectangle and cannot write outside
// it.

import (
	"fmt"
	"time"

	"github.com/notoriousjoshyb/derostorm/internal/ui"
)

// ---------------------------------------------------------------- header

// headerHeight is how many rows the header takes at this window size.
//
// The header is the first thing to give up space, because it is the only part
// of the screen that carries no live data. On a short window it collapses to a
// single line of text and the rows go to the panels, which is the right trade:
// nobody is watching the wordmark.
func headerHeight(w, h int) int {
	switch {
	case h >= 40 && w >= 100:
		return 8 // artwork, the large wordmark, subtitle, run header, rule
	case h >= 28 && w >= 76:
		return 6 // the compact wordmark
	case h >= 18:
		return 3 // one line of name, the run header, a rule
	default:
		return 2
	}
}

func drawHeader(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme, frame int) {
	if r.Empty() {
		return
	}
	// The closing rule belongs to the header, so the body below it starts on a
	// clean row whatever height the header ended up with.
	ruleY := r.Bottom() - 1
	cv.HLine(r.X, ruleY, r.W, '─', ui.Style{FG: t.Border})

	inner := ui.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H - 1}
	if inner.H <= 0 {
		return
	}

	// The run header is always the last row of the interior: it is the only
	// part of the header anyone reads twice.
	runY := inner.Bottom() - 1
	drawRunHeader(cv, ui.Rect{X: inner.X + 1, Y: runY, W: inner.W - 2, H: 1}, s, t)

	// The mark is only drawn when there are whole rows for it above the run
	// header. Clipping it instead would give the cloud a flat bottom, which
	// looks like a rendering fault rather than like a cloud.
	markW := 0
	if avail := runY - inner.Y; avail >= len(ui.StormCloud) && inner.W >= 96 {
		ui.DrawArt(cv, inner.X+2, inner.Y, ui.StormCloud, ui.Shade(t), t.Accent)
		markW = ui.ArtWidth(ui.StormCloud) + 4
	}

	// The version badge, boxed, top right -- the one piece of chrome the
	// reference puts a box around, and it earns it: a version number is what
	// someone is asked for first when something is wrong.
	//
	// badgeW stays zero when the badge is not drawn. Reserving its width
	// unconditionally, as this used to, took ten columns off the wordmark on
	// exactly the windows too small to show a badge in the first place.
	badge := "v" + version
	badgeW := 0
	if inner.W > markW+40 && inner.H >= 3 {
		badgeW = ui.Width(badge) + 4
		bx := inner.Right() - badgeW - 2
		drawBadge(cv, bx, inner.Y+1, badge, t)
	}

	nameArea := ui.Rect{X: inner.X + markW, Y: inner.Y, W: inner.W - markW - badgeW - 2, H: runY - inner.Y}
	if nameArea.W < 20 {
		nameArea = ui.Rect{X: inner.X, Y: inner.Y, W: inner.W, H: runY - inner.Y}
	}
	if nameArea.H <= 0 {
		return
	}

	used := ui.Banner(cv, nameArea.X, nameArea.Y, nameArea.W, nameArea.H, t)
	if sub := nameArea.Y + used; sub < nameArea.Bottom() {
		cv.TextCenter(nameArea.X, sub, nameArea.W,
			ui.Clip("ASTROBWTv3 MINER FOR DERO", nameArea.W), ui.Style{FG: t.Muted})
	}
	_ = frame
}

func drawBadge(cv *ui.Canvas, x, y int, s string, t *Theme) {
	w := ui.Width(s) + 4
	st := ui.Style{FG: t.Border}
	cv.Set(x, y, '┌', st)
	cv.Set(x+w-1, y, '┐', st)
	cv.Set(x, y+2, '└', st)
	cv.Set(x+w-1, y+2, '┘', st)
	cv.HLine(x+1, y, w-2, '─', st)
	cv.HLine(x+1, y+2, w-2, '─', st)
	cv.Set(x, y+1, '│', st)
	cv.Set(x+w-1, y+1, '│', st)
	cv.TextCenter(x+1, y+1, w-2, s, ui.Style{FG: t.Accent, Bold: true})
}

// drawRunHeader is the one-line "what am I looking at" row: which node, which
// network, how long it has been up.
func drawRunHeader(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme) {
	if r.W < 20 {
		return
	}
	net := "mainnet"
	if s.Testnet {
		net = "testnet"
	}
	// Ordered by how often they are read, because the row is truncated from
	// the right: on a narrow window the fields that survive should be the ones
	// worth keeping.
	fields := [][2]string{
		{"NODE", s.Node},
		{"NETWORK", net},
		{"UPTIME", ui.HMS(s.Uptime)},
		{"THEME", t.Name},
		{"MINER", "AstroBWTv3"},
	}
	tag := "DERO MINING CONTROL CENTRE"
	tagW := 0
	if r.W >= 100 {
		tagW = ui.Width(tag) + 3
		cv.TextRight(r.Right(), r.Y, tag, ui.Style{FG: t.Accent2, Bold: true})
	}

	x := r.X
	limit := r.Right() - tagW
	for _, f := range fields {
		item := f[0] + ": " + f[1]
		if x+ui.Width(item)+3 > limit {
			break
		}
		x += cv.Text(x, r.Y, f[0]+":", ui.Style{FG: t.Muted})
		x++
		x += cv.Text(x, r.Y, f[1], ui.Style{FG: t.Text})
		x += 3
	}
}

// ---------------------------------------------------------------- grid

// panelDef is one panel's contribution to the layout.
type panelDef struct {
	// minW is the narrowest this panel is worth drawing at. Below it the panel
	// is moved to a row of its own, or dropped.
	minW int
	// weight is its share of a row's width once the row is settled.
	weight int
	draw   func(cv *ui.Canvas, r ui.Rect)
}

// bandFloorH is the shortest a band will ever be drawn at: a border, three
// rows of contents and a border.
//
// It is not a band's minH, which is the height a band would like. This is the
// height below which its panels stop being able to say anything at all, and it
// is the difference between a small window showing every panel in less space
// and a small window showing half of them in the space they asked for. Three
// interior rows is a headline figure and two stats, which is enough for every
// panel here to be worth looking at.
const bandFloorH = 5

// bandDef is a horizontal band of the dashboard.
type bandDef struct {
	// minH is the shortest the band is worth drawing at; every panel in it is
	// written to say something useful at that height.
	minH int
	// grow is its share of whatever height is left over. The event log has the
	// largest, because it is the only panel whose usefulness scales with rows:
	// every other one shows the same numbers in more or less space.
	grow   int
	panels []panelDef
}

// ---------------------------------------------------------------- dashboard

func drawDashboard(cv *ui.Canvas, body ui.Rect, s Snapshot, t *Theme, frame int) {
	if body.Empty() {
		return
	}
	body = body.Inset2(1, 0)

	bands := []bandDef{
		{minH: 8, grow: 3, panels: []panelDef{
			{minW: 32, weight: 30, draw: func(c *ui.Canvas, r ui.Rect) { panelMining(c, r, s, t, frame) }},
			{minW: 30, weight: 34, draw: func(c *ui.Canvas, r ui.Rect) { panelHistory(c, r, s, t) }},
			{minW: 32, weight: 36, draw: func(c *ui.Canvas, r ui.Rect) { panelNetwork(c, r, s, t, frame) }},
		}},
		{minH: 8, grow: 4, panels: []panelDef{
			{minW: 28, weight: 30, draw: func(c *ui.Canvas, r ui.Rect) { panelCPU(c, r, s, t) }},
			{minW: 28, weight: 34, draw: func(c *ui.Canvas, r ui.Rect) { panelGPU(c, r, s, t) }},
			{minW: 30, weight: 36, draw: func(c *ui.Canvas, r ui.Rect) { panelShares(c, r, s, t) }},
		}},
		{minH: 8, grow: 3, panels: []panelDef{
			{minW: 26, weight: 26, draw: func(c *ui.Canvas, r ui.Rect) { panelBlockchain(c, r, s, t) }},
			{minW: 26, weight: 27, draw: func(c *ui.Canvas, r ui.Rect) { panelSystem(c, r, s, t) }},
			{minW: 36, weight: 47, draw: func(c *ui.Canvas, r ui.Rect) { panelLog(c, r, s, t, 0) }},
		}},
		{minH: 8, grow: 2, panels: []panelDef{
			{minW: 24, weight: 28, draw: func(c *ui.Canvas, r ui.Rect) { panelThreads(c, r, s, t) }},
			{minW: 26, weight: 33, draw: func(c *ui.Canvas, r ui.Rect) { panelStatus(c, r, s, t, frame) }},
			{minW: 22, weight: 24, draw: func(c *ui.Canvas, r ui.Rect) { panelQuick(c, r, s, t) }},
			{minW: 16, weight: 15, draw: func(c *ui.Canvas, r ui.Rect) { panelConnection(c, r, s, t, frame) }},
		}},
	}

	for _, cell := range planBands(body, bands) {
		cell.p.draw(cv, cell.r)
	}
}

// placed is one panel and the rectangle the layout gave it.
type placed struct {
	p panelDef
	r ui.Rect
}

// planBands is layoutBands without the drawing: it returns what goes where and
// leaves the caller to draw, which is what makes the layout testable without a
// terminal.
func planBands(body ui.Rect, bands []bandDef) []placed {
	type row struct {
		band int
		// ord is this row's position inside its band: 0 for the first row, 1
		// for the panels that would not fit beside them, and so on.
		ord    int
		panels []panelDef
	}

	var rows []row
	for bi, b := range bands {
		var cur []panelDef
		curW, ord := 0, 0
		for _, p := range b.panels {
			if len(cur) > 0 && curW+p.minW > body.W {
				rows = append(rows, row{bi, ord, cur})
				cur, curW, ord = nil, 0, ord+1
			}
			cur = append(cur, p)
			curW += p.minW
		}
		if len(cur) > 0 {
			rows = append(rows, row{bi, ord, cur})
		}
	}

	// Which rows fit, measured against the floor rather than against what the
	// bands asked for. A band squeezed to five rows still shows its panels'
	// headline figures; a band that was dropped shows nothing, and a dashboard
	// with the GPU missing is not a smaller dashboard, it is a different one.
	//
	// So every row is kept that can be given bandFloorH, and the heights are
	// settled afterwards. On a default console window this is the difference
	// between two bands and all four: everything is on screen, tight, and it
	// opens out as the window is made bigger.
	//
	// Taken in passes rather than straight down the list, for the case where
	// even the floor will not fit them all: a band that had to wrap gets its
	// first row in pass zero and its overflow in pass one, so a short window
	// spends its rows on one row from every band before it spends any on a
	// second row of the first band.
	keep := make([]bool, len(rows))
	used, fit := 0, 0
	for pass := 0; ; pass++ {
		took := false
		for i, rw := range rows {
			if rw.ord != pass {
				continue
			}
			took = true
			if used+bandFloorH > body.H {
				continue
			}
			used += bandFloorH
			keep[i] = true
			fit++
		}
		if !took {
			break
		}
	}
	if fit == 0 {
		return nil
	}
	kept := rows[:0]
	for i, rw := range rows {
		if keep[i] {
			kept = append(kept, rw)
		}
	}
	rows = kept

	heights := make([]int, fit)
	for i := range heights {
		heights[i] = bandFloorH
	}
	spare := body.H - used

	// Back up to each band's stated minimum first, a row at a time round the
	// list. Round-robin rather than in proportion, because the shortfall is
	// usually one or two rows in total and proportional shares of two rows
	// round to nothing for everybody.
	for spare > 0 {
		moved := false
		for i, rw := range rows {
			if spare == 0 {
				break
			}
			if heights[i] < bands[rw.band].minH {
				heights[i]++
				spare--
				moved = true
			}
		}
		if !moved {
			break
		}
	}

	// Whatever is still going spare is shared by grow, which is where the log
	// gets its extra rows on a tall window.
	totalGrow := 0
	for _, rw := range rows {
		totalGrow += bands[rw.band].grow
	}
	if spare > 0 && totalGrow > 0 {
		given := 0
		for i, rw := range rows {
			add := spare * bands[rw.band].grow / totalGrow
			heights[i] += add
			given += add
		}
		for i := 0; given < spare; i, given = i+1, given+1 {
			heights[i%fit]++
		}
	}

	var out []placed
	y := body.Y
	for i, rw := range rows {
		strip := ui.Rect{X: body.X, Y: y, W: body.W, H: heights[i]}
		for j, r := range splitRow(strip, rw.panels) {
			if !r.Empty() {
				out = append(out, placed{rw.panels[j], r})
			}
		}
		y += heights[i]
	}
	return out
}

// splitRow divides a strip between the panels packed into it, giving each one
// the width it said it needed and sharing only the surplus by weight.
//
// Splitting by weight alone -- which is what this did, and what Rect.Cols
// still does for callers whose parts have no minimum -- quietly broke the
// contract the rest of this file is built on. Three panels declaring 32, 30
// and 32 columns, packed into a body of 98 because 94 of it was spoken for,
// were then handed 29, 33 and 35 by weights of 30/34/36. The first panel was
// three columns under the width it had just been checked against, so it fell
// back to its cramped form on a window wide enough for the full one, and there
// was nothing on screen to explain why.
func splitRow(strip ui.Rect, panels []panelDef) []ui.Rect {
	if len(panels) == 0 {
		return nil
	}
	base, grow := 0, 0
	for _, p := range panels {
		base += p.minW
		grow += p.weight
	}

	widths := make([]int, len(panels))
	if base > strip.W {
		// The minimums cannot all be met, which only happens for a single
		// panel wider than the window. Share what there is in proportion to
		// them, so the shortfall is spread rather than landing on one panel.
		for i, p := range panels {
			widths[i] = strip.W * p.minW / base
		}
	} else {
		spare := strip.W - base
		for i, p := range panels {
			widths[i] = p.minW
			if grow > 0 {
				widths[i] += spare * p.weight / grow
			}
		}
	}

	out := make([]ui.Rect, len(panels))
	x, given := strip.X, 0
	for i, w := range widths {
		out[i] = ui.Rect{X: x, Y: strip.Y, W: w, H: strip.H}
		x += w
		given += w
	}
	// The rounding remainder, one cell at a time from the left, so the row
	// always reaches the right edge instead of leaving a ragged margin that
	// moves as the window is resized.
	for i := 0; given < strip.W; i, given = i+1, given+1 {
		j := i % len(out)
		out[j].W++
		for k := j + 1; k < len(out); k++ {
			out[k].X++
		}
	}
	return out
}

// ---------------------------------------------------------------- events

// drawBlockBanner is the found-a-block announcement: a boxed overlay in the
// middle of whatever screen is showing.
//
// It is an overlay rather than a screen of its own because it must never take
// the dashboard away. Finding a block is the moment someone most wants to see
// that everything else is still running, and a full-screen takeover would hide
// exactly that. It clears itself after a few seconds; nothing has to be
// dismissed.
//
// No reward figure appears here, and none can: getwork reports that a block was
// found and says nothing about what it paid.
func drawBlockBanner(cv *ui.Canvas, full ui.Rect, s Snapshot, t *Theme, frame int) {
	lines := []string{
		"BLOCK FOUND",
		"",
		"HEIGHT " + ui.Commas(uint64(maxi64(s.Height, 0))),
		"BLOCK #" + ui.Commas(s.Blocks),
	}
	w := 0
	for _, l := range lines {
		if n := ui.Width(l); n > w {
			w = n
		}
	}
	w += 12
	h := len(lines) + 4
	if w > full.W-4 {
		w = full.W - 4
	}
	if w < 20 || h > full.H-2 {
		return
	}
	x := full.X + (full.W-w)/2
	y := full.Y + (full.H-h)/2

	// The border pulses between the two accents. A static box would be missed
	// by anyone glancing at the screen; two frames of movement will not be.
	fg := t.Good
	if frame%8 < 4 {
		fg = t.Accent
	}
	st := ui.Style{FG: fg, Bold: true}

	// Blank the area first so the panel underneath cannot show through.
	cv.Fill(ui.Rect{X: x, Y: y, W: w, H: h}, ' ', ui.Style{})
	cv.Set(x, y, '┏', st)
	cv.Set(x+w-1, y, '┓', st)
	cv.Set(x, y+h-1, '┗', st)
	cv.Set(x+w-1, y+h-1, '┛', st)
	cv.HLine(x+1, y, w-2, '━', st)
	cv.HLine(x+1, y+h-1, w-2, '━', st)
	cv.VLine(x, y+1, h-2, '┃', st)
	cv.VLine(x+w-1, y+1, h-2, '┃', st)

	for i, l := range lines {
		if l == "" {
			continue
		}
		s := ui.Style{FG: t.Text}
		if i == 0 {
			s = ui.Style{FG: t.Good, Bold: true}
			l = ui.TrackedCaps(l)
		}
		cv.TextCenter(x+1, y+2+i, w-2, ui.Clip(l, w-2), s)
	}
}

// ---------------------------------------------------------------- helpers

// stateLabel is the run state as a mark, a colour and a word.
func stateLabel(s Snapshot, t *Theme, frame int) (mark, colour, label string) {
	switch s.State {
	case StateConnecting:
		return ui.Spinner[frame%len(ui.Spinner)], t.Accent2, "CONNECTING"
	case StateReconnecting:
		return ui.Spinner[frame%len(ui.Spinner)], t.Warn, "RECONNECTING"
	case StateStopping:
		return string(ui.Bullet), t.Dim, "STOPPING"
	}
	if s.GPUTuning {
		return ui.Spinner[frame%len(ui.Spinner)], t.Accent2, "TUNING GPU"
	}
	return string(ui.Bullet), t.Good, "MINING"
}

// connLabel is the connection state for the network panel, which is a
// different question from the mining state: the miner can be hashing happily
// on a job whose socket died a minute ago.
func connLabel(s Snapshot, t *Theme) (colour, label string) {
	switch {
	case s.State == StateConnecting:
		return t.Warn, "CONNECTING"
	case s.State == StateReconnecting:
		return t.Err, "DISCONNECTED"
	case s.ConnectedAt.IsZero():
		return t.Warn, "CONNECTING"
	case !s.LastJob.IsZero() && time.Since(s.LastJob) > 2*time.Minute:
		// The socket is open and the node has said nothing for two minutes.
		// That is not a healthy connection, and calling it one is how a rig
		// spends a night mining a stale job.
		return t.Warn, "STALLED"
	}
	return t.Good, "CONNECTED"
}

// signalBars turns latency and job freshness into a 0-5 strength.
//
// Latency alone is not connection health -- a 5ms link that has sent no work
// for five minutes is worse than a 200ms one that is keeping up -- so the job
// gap caps the result.
func signalBars(s Snapshot) int {
	if s.State == StateReconnecting || s.State == StateConnecting {
		return 0
	}
	bars := 3 // an open socket with nothing else known
	if s.Info.OK {
		switch l := s.Info.Latency; {
		case l < 30*time.Millisecond:
			bars = 5
		case l < 80*time.Millisecond:
			bars = 4
		case l < 200*time.Millisecond:
			bars = 3
		case l < 500*time.Millisecond:
			bars = 2
		default:
			bars = 1
		}
	}
	if !s.LastJob.IsZero() {
		switch gap := time.Since(s.LastJob); {
		case gap > 2*time.Minute:
			bars = 1
		case gap > 45*time.Second && bars > 2:
			bars = 2
		}
	}
	return bars
}

func signalWord(bars int) string {
	switch {
	case bars >= 5:
		return "EXCELLENT"
	case bars == 4:
		return "GOOD"
	case bars == 3:
		return "FAIR"
	case bars == 2:
		return "WEAK"
	case bars == 1:
		return "POOR"
	}
	return "NO SIGNAL"
}

// naDur is a duration since a timestamp, or "--" when it never happened.
func naDur(at time.Time) string {
	if at.IsZero() {
		return ui.NA
	}
	return ui.ShortDur(time.Since(at))
}

// gpuRows is the per-device view, built once so several panels agree.
func gpuRows(s Snapshot) []DeviceStat {
	var out []DeviceStat
	for _, d := range s.deviceRows() {
		if d.IsGPU {
			out = append(out, d)
		}
	}
	return out
}

func cpuRow(s Snapshot) DeviceStat {
	for _, d := range s.deviceRows() {
		if !d.IsGPU {
			return d
		}
	}
	return DeviceStat{Label: "CPU", Rate: s.CPURate, TempC: tempUnknown}
}

// tempOr is a temperature or the standard "not available" mark.
func tempOr(c float64) string {
	if c <= tempUnknown {
		return ui.NA
	}
	return fmt.Sprintf("%d°C", int(c+0.5))
}

func sectionTitle(cv *ui.Canvas, r ui.Rect, y int, s string, t *Theme) {
	cv.Text(r.X, y, s, ui.Style{FG: t.Accent2})
	if n := ui.Width(s); r.W > n+2 {
		cv.HLine(r.X+n+1, y, r.W-n-1, '─', ui.Style{FG: t.Border})
	}
}
