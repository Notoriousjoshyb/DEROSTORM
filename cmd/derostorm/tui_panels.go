package main

// The dashboard's panels.
//
// One function per panel, each handed a rectangle and a snapshot and allowed
// to draw inside that rectangle and nowhere else. None of them knows what is
// next to it, which is what lets the layout in tui_layout.go move them around
// freely at different window sizes.
//
// Two rules run through all of them.
//
// Every panel degrades downwards rather than overflowing. A panel drops its
// least important row first and its artwork before its numbers, so a short
// window loses detail instead of losing its shape.
//
// Nothing is invented. Where a figure is not available on this platform or is
// not something getwork reports, the panel says so with the same "--" mark
// everywhere. A dashboard that fills a gap with a plausible number is worse
// than one with a gap in it, because only one of the two can be trusted.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/notoriousjoshyb/derostorm/internal/ui"
)

// Panel marks. One glyph per panel, all from the geometric-shapes block so
// every one of them is a single cell in every terminal this runs in.
//
// A distinct mark per panel rather than the same diamond twelve times. On a
// screen with a dozen boxes on it the marks are what the eye navigates by:
// somebody looking for the GPU panel finds its mark without reading a title.
const (
	markMining  = "◈"
	markHistory = "▨"
	markNetwork = "◉"
	markCPU     = "▣"
	markGPU     = "▤"
	markShares  = "◍"
	markChain   = "▩"
	markSystem  = "▦"
	markLog     = "▥"
	markThreads = "▧"
	markStorm   = "◆"
	markQuick   = "◧"
	markConn    = "◨"
)

// sensorStaleAfter is how old a hardware poll has to be before the console
// stops presenting it as current.
//
// Twelve seconds, against a poll interval of about two. A sensor source can
// stop answering without failing -- a monitoring program is closed, a driver
// call starts blocking -- and the last value it gave simply sits there looking
// like a live reading. A dashboard that shows a stale temperature as a current
// one is worse than one that shows nothing, because the whole point of the
// number is to notice when it moves.
const sensorStaleAfter = 12 * time.Second

// staleMark is the suffix put on a reading that has stopped being updated, or
// the empty string when it is current.
func staleMark(at time.Time) string {
	if at.IsZero() || time.Since(at) < sensorStaleAfter {
		return ""
	}
	return " ·stale"
}

// stale reports whether a poll timestamp is too old to present as current.
func stale(at time.Time) bool {
	return !at.IsZero() && time.Since(at) >= sensorStaleAfter
}

// ringWidth is how wide to make a round gauge that is h rows tall.
//
// A terminal cell is about twice as tall as it is wide and the gauges are
// drawn at half-cell vertical resolution, so a circle h rows tall is h*2 cells
// across. Anything wider than that is empty margin the panel could have given
// to its numbers.
func ringWidth(h, max int) int {
	w := h * 2
	if w > max {
		w = max
	}
	return w
}

// kvRows draws label/value rows down r from y and returns the row after the
// last one drawn.
func kvRows(cv *ui.Canvas, r ui.Rect, y int, rows [][2]string, t *Theme, fg func(i int) string) int {
	for i, kv := range rows {
		if y >= r.Bottom() {
			break
		}
		colour := ""
		if fg != nil {
			colour = fg(i)
		}
		ui.Row(cv, r, y, kv[0], kv[1], t, colour)
		y++
	}
	return y
}

// ---------------------------------------------------------------- mining

func panelMining(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme, frame int) {
	in := ui.Panel(cv, r, t, ui.PanelOpts{Title: "MINING PERFORMANCE", Mark: markMining})
	if in.Empty() {
		return
	}
	y := in.Y

	mark, colour, label := stateLabel(s, t, frame)
	if !s.LastShare.IsZero() && time.Since(s.LastShare) < 700*time.Millisecond {
		colour = t.Good
	}
	num, unit := ui.SplitRate(s.Hashrate)

	// How many hashing sources the split has to show, decided before the
	// headline is drawn because it is what decides whether there is room for
	// the headline at all.
	//
	// The big numerals cost four rows. Spending them and then running out
	// before the GPU row is the worst of both: a panel that looks impressive
	// and has silently dropped the one figure that says the card is alive.
	// So on a short panel the headline gives way and the sources are drawn.
	nSources := 1
	if s.GPUs > 0 || s.GPURate > 0 {
		nSources = 2
	}

	if in.H >= 4+nSources && in.W >= 30 {
		// The headline carries the gradient, and it is the only number on the
		// screen that does. That is the point of it: one figure answers "is
		// this machine working", and it should be findable without reading.
		w := ui.BigTextGradient(cv, in.X, y, num, t)
		cv.Text(in.X+w+2, y+2, unit, ui.Style{FG: t.Accent})
		// The state sits at the top right of the headline, where it is read in
		// the same glance as the number it qualifies.
		lw := ui.Width(label) + 2
		if in.W > w+lw+8 {
			cv.Set(in.Right()-lw, y, []rune(mark)[0], ui.Style{FG: colour})
			cv.Text(in.Right()-lw+2, y, label, ui.Style{FG: colour, Bold: true})
			if in.H >= 6 {
				sw := in.W - w - lw - 3
				if sw < 8 {
					sw = 8
				}
				if sw > in.W-2 {
					sw = 12
				}
				ui.Sparkline(cv, in.Right()-sw, y+2, sw, s.History, t)
			}
		}
		y += 3
		cv.Text(in.X, y, "TOTAL HASHRATE", ui.Style{FG: t.Muted})
		y++
		// The breathing room under the headline is the first thing to go. A
		// blank row is worth less than the row it was costing the GPU.
		if in.Bottom()-y > 2 {
			y++
		}
	} else {
		cv.Text(in.X, y, ui.HumanRate(s.Hashrate), ui.Style{FG: t.Accent, Bold: true})
		cv.TextRight(in.Right(), y, label, ui.Style{FG: colour, Bold: true})
		y += 2
	}

	// The split. Proportion of the total, not of the peak: the question is how
	// much of the headline each source is responsible for.
	total := s.CPURate + s.GPURate
	if total <= 0 {
		total = 1
	}
	sources := []struct {
		name string
		rate float64
		fg   string
	}{
		{"CPU", s.CPURate, t.Accent},
	}
	if s.GPUs > 0 || s.GPURate > 0 {
		sources = append(sources, struct {
			name string
			rate float64
			fg   string
		}{"GPU", s.GPURate, t.Accent2})
	}

	// The bar gets a row of its own where there is one for every source, which
	// is the arrangement the reference uses and the reason it works: a bar
	// squeezed into the gap between a label and a percentage is eight cells
	// long on a 100-column window, and eight cells cannot show the difference
	// between 60% and 70%.
	//
	// Every source or none. Giving the CPU a bar row and then running out
	// before the GPU drops the comparison the split exists to make.
	ownRow := in.W >= 16 && in.Bottom()-y >= 2*len(sources)

	if in.W >= 16 && in.Bottom()-y > len(sources)+1 && (s.GPUs > 0 || s.GPURate > 0) {
		ui.DrawSplitBar(cv, in.X, y, in.W, s.CPURate/total, s.GPURate/total, t.Accent, t.Accent2, t.Dim)
		y++
	}

	for _, row := range sources {
		if y >= in.Bottom() {
			break
		}
		pct := row.rate / total
		head := fmt.Sprintf("%s %s", row.name, ui.HumanRate(row.rate))
		share := fmt.Sprintf("%.0f%%", pct*100)
		// The share is set in a filled chip rather than as bare text. It is
		// the only number on the row that is a proportion rather than a rate,
		// and a chip says that faster than a per-cent sign does.
		shareW := ui.Width(share) + 2
		if hw := in.W - shareW - 1; hw > 0 {
			cv.TextIn(in.X, y, hw, head, ui.Style{FG: t.Muted})
		}
		chip := ui.Style{FG: row.fg, BG: t.SelBG, Bold: true}
		for i := 0; i < shareW; i++ {
			cv.Set(in.Right()-shareW+i, y, ' ', chip)
		}
		cv.Text(in.Right()-shareW+1, y, share, chip)

		inline := in.W - ui.Width(head) - shareW - 4
		switch {
		case ownRow:
			y++
			ui.DrawBar(cv, in.X, y, in.W, pct, row.fg, t.Dim)
		case inline > 6:
			ui.DrawBar(cv, in.X+ui.Width(head)+2, y, inline, pct, row.fg, t.Dim)
		}
		y++
	}

	// A blank row before the averages, but only when losing it would not cost
	// a row of numbers.
	if in.Bottom()-y > 4 {
		y++
	}
	pairs := [][2]string{
		{"AVG 1M", ui.HumanRate(s.Avg1m)},
		{"AVG 5M", ui.HumanRate(s.Avg5m)},
		{"AVG 15M", ui.HumanRate(s.Avg15m)},
		{"PEAK", ui.HumanRate(s.PeakRate)},
		{"SESSION", ui.HumanRate(s.AvgRate)},
		{"ALGO", "AstroBWTv3"},
	}
	// Two columns when there is width for them, which is what turns seven rows
	// into four and keeps the panel from needing a window nobody has.
	if in.W >= 42 {
		cols := ui.Rect{X: in.X, Y: y, W: in.W, H: in.Bottom() - y}.Cols(1, 1)
		half := (len(pairs) + 1) / 2
		kvRows(cv, cols[0].Inset2(0, 0), y, pairs[:half], t, nil)
		right := ui.Rect{X: cols[1].X + 2, Y: y, W: cols[1].W - 2, H: cols[1].H}
		kvRows(cv, right, y, pairs[half:], t, nil)
	} else {
		kvRows(cv, ui.Rect{X: in.X, Y: y, W: in.W, H: in.Bottom() - y}, y, pairs, t, nil)
	}
}

// ---------------------------------------------------------------- history

func panelHistory(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme, sel int) {
	sel = clampRange(sel)
	win := histRanges[sel]

	// The range strip is drawn over the top border after the panel is built,
	// because it is wider than a PanelOpts.Right label and it has a selected
	// state a plain string cannot carry. Where there is no room for the strip
	// the panel falls back to naming the window it is showing, which is the
	// one thing the strip has to communicate.
	tabsW := 0
	for _, l := range rangeLabels {
		tabsW += ui.Width(l) + 2
	}
	right := ""
	if r.W < tabsW+ui.Width("HASHRATE HISTORY")+10 {
		tabsW, right = 0, win.label
	}
	in := ui.Panel(cv, r, t, ui.PanelOpts{Title: "HASHRATE HISTORY", Mark: markHistory, Right: right})
	if in.Empty() {
		return
	}
	if tabsW > 0 {
		ui.Tabs(cv, r.Right()-2-tabsW, r.Y, rangeLabels, sel, t)
	}
	// Chart wants four rows: an axis, and a plot with something in it. Handed
	// three it draws nothing, and an empty bordered box reads as a panel that
	// has failed rather than one that was given three rows -- so below that
	// the panel changes shape instead of going blank.
	vals := win.pick(s)
	if in.H < 4 || in.W < 10 {
		y := in.Y
		if in.W >= 24 {
			cv.Text(in.X, y, ui.HumanRate(s.Hashrate), ui.Style{FG: t.Accent, Bold: true})
			cv.TextRight(in.Right(), y, "PEAK "+ui.HumanRate(s.PeakRate), ui.Style{FG: t.Muted})
			y++
		}
		if y < in.Bottom() {
			ui.Spectrum(cv, ui.Rect{X: in.X, Y: y, W: in.W, H: in.Bottom() - y}, vals, t, t.Accent)
		}
		return
	}

	ui.Chart(cv, in, t, ui.ChartOpts{
		Values:  vals,
		XLabels: win.xaxis,
		Fill:    in.H >= 6,
		Empty:   "collecting " + win.label + " samples…",
	})
}

// ---------------------------------------------------------------- network

func panelNetwork(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme, frame int) {
	in := ui.Panel(cv, r, t, ui.PanelOpts{Title: "NETWORK STATUS", Mark: markNetwork})
	if in.Empty() {
		return
	}

	// The globe is the first thing to go when the panel narrows: it is the
	// only part of this panel that is not a fact.
	text := in
	if in.W >= 52 && in.H >= 6 {
		gw := ringWidth(in.H, 20)
		// A caption under the globe, at whatever length the rows allow. A
		// turning wireframe sphere with no words beside it is decoration; with
		// them it is a statement about what the network is.
		//
		// The rows come out of the globe, so the caption is only taken as far
		// as the sphere can afford: four lines on a tall panel, one on a
		// short one, none at all when the globe would be left too small to
		// turn convincingly.
		caption := []string(nil)
		switch {
		case in.H >= 11 && gw >= 14:
			caption = []string{"DERO", "GLOBAL", "PRIVATE", "DECENTRALISED"}
		case in.H >= 8 && gw >= 14:
			caption = []string{"DERO · PRIVATE"}
		}
		globe := ui.Rect{X: in.Right() - gw, Y: in.Y, W: gw, H: in.H - len(caption)}
		ui.Globe(cv, globe, t, float64(frame)*0.05)
		for i, l := range caption {
			st := ui.Style{FG: t.Muted}
			if i == 0 && len(caption) > 1 {
				st = ui.Style{FG: t.Accent2, Bold: true}
			}
			cv.TextCenter(in.Right()-gw, globe.Bottom()+i, gw, ui.Clip(l, gw), st)
		}
		text = ui.Rect{X: in.X, Y: in.Y, W: in.W - gw - 2, H: in.H}
	}

	colour, label := connLabel(s, t)
	y := text.Y
	dot := string(ui.Bullet) + " " + label
	if lw := text.W - ui.Width(dot) - 1; lw >= ui.MinLabel {
		cv.TextIn(text.X, y, lw, "STATUS", ui.Style{FG: t.Muted})
	}
	cv.TextRight(text.Right(), y, ui.Clip(dot, text.W), ui.Style{FG: colour, Bold: true})
	y++

	netHash := ui.NA
	if v, ok := s.Info.NetHashrate(); ok {
		netHash = ui.HumanRate(v)
	}
	peers := ui.NA
	if s.Info.OK {
		peers = fmt.Sprintf("%d / %d", s.Info.PeersOut, s.Info.Peers)
	}
	latency := ui.NA
	if s.Info.OK {
		latency = fmt.Sprintf("%d ms", s.Info.Latency.Milliseconds())
	}
	netDiff := ui.NA
	if s.Info.OK && s.Info.Difficulty > 0 {
		netDiff = ui.Commas(s.Info.Difficulty)
	}

	// "DIFFICULTY", not "NETWORK DIFFICULTY": the panel is titled NETWORK
	// STATUS and eighteen cells of label leaves nothing for the figure in a
	// panel narrow enough to be on a 100-column screen.
	rows := [][2]string{
		{"NETWORK HASHRATE", netHash},
		{"DIFFICULTY", netDiff},
		{"HEIGHT", ui.Commas(uint64(maxi64(s.Height, 0)))},
		{"PEERS", peers},
		{"LATENCY", latency},
		{"NODE", s.Node},
		{"JOB HEIGHT", ui.Commas(uint64(maxi64(s.Height, 0)))},
		{"JOB DIFFICULTY", ui.Commas(s.Difficulty)},
		{"BEST SHARE DIFF", shareDiffText(s)},
		{"LAST JOB", naDur(s.LastJob)},
	}
	kvRows(cv, ui.Rect{X: text.X, Y: y, W: text.W, H: text.Bottom() - y}, y, rows, t, nil)
}

func shareDiffText(s Snapshot) string {
	if s.BestShare == 0 {
		return ui.NA
	}
	return ui.ShortNum(float64(s.BestShare))
}

// ---------------------------------------------------------------- cpu

func panelCPU(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme) {
	in := ui.Panel(cv, r, t, ui.PanelOpts{Title: "CPU PERFORMANCE", Mark: markCPU,
		Right: ui.HumanRate(cpuRow(s).Rate)})
	if in.Empty() {
		return
	}
	cpu := cpuRow(s)

	// The activity spectrum is drawn last and only if there are rows to spare.
	body := in
	specH := 0
	if in.H >= 7 {
		specH = 2
		body = ui.Rect{X: in.X, Y: in.Y, W: in.W, H: in.H - specH - 1}
	}

	load := s.Sys.LoadPct / 100
	loadText := ui.NA
	if s.Sys.HaveLoad {
		loadText = fmt.Sprintf("%.0f%%", s.Sys.LoadPct)
	}
	if stale(s.Sys.At) {
		// The gauge is left where it was and the reading says why. Zeroing it
		// would be inventing a measurement; hiding it would lose the last one
		// that was real.
		loadText = ui.NA
		load = 0
	}

	text := body
	if body.W >= 30 && body.H >= 4 {
		gw := ringWidth(body.H, 15)
		ring := ui.Rect{X: body.X, Y: body.Y, W: gw, H: body.H}
		ui.Ring(cv, ring, t, ui.RingOpts{
			Frac: load, FG: t.Accent, Track: t.Glow,
			Lines: []string{loadText, "LOAD"}, LineFG: []string{t.Accent, t.Muted},
		})
		text = ui.Rect{X: body.X + gw + 2, Y: body.Y, W: body.W - gw - 2, H: body.H}
	}

	y := text.Y
	if text.W >= 20 {
		cv.Text(text.X, y, ui.HumanRate(cpu.Rate), ui.Style{FG: t.Accent, Bold: true})
		cv.TextRight(text.Right(), y, "HASHRATE", ui.Style{FG: t.Muted})
		y += 2
	}

	freq := ui.NA
	if s.Sys.HaveFreq {
		freq = fmt.Sprintf("%.2f GHz", float64(s.Sys.FreqMHz)/1000)
	}
	rows := [][2]string{
		{"THREADS", fmt.Sprint(s.Threads)},
		{"TEMP", tempOr(cpu.TempC) + staleMark(s.Sensors.At)},
		{"FREQ", freq},
		// No platform this runs on exposes a package power figure without a
		// kernel driver, so this is honest rather than absent: the row is worth
		// keeping so the layout matches the GPU panel beside it.
		{"POWER", ui.NA},
	}
	colours := func(i int) string {
		if i == 1 {
			return ui.TempColour(t, cpu.TempC)
		}
		return ""
	}
	kvRows(cv, ui.Rect{X: text.X, Y: y, W: text.W, H: text.Bottom() - y}, y, rows, t, colours)

	if specH > 0 {
		ui.Spectrum(cv, ui.Rect{X: in.X, Y: in.Bottom() - specH, W: in.W, H: specH}, s.CPUHist, t, t.Accent)
	}
}

// ---------------------------------------------------------------- gpu

func panelGPU(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme, sel int) {
	gpus := gpuRows(s)
	right := ui.HumanRate(s.GPURate)
	if n := len(gpus); n > 1 {
		// The header carries the selected card's own detail, because on a rig
		// the rows below are a comparison and the header is the only place
		// with room to say anything about one device in particular. It is
		// also what makes the G key visibly do something.
		right = fmt.Sprintf("%d DEVICES · %s%s", n, right, gpuDetail(s, ((sel%n)+n)%n))
	}
	in := ui.Panel(cv, r, t, ui.PanelOpts{Title: "GPU PERFORMANCE", Mark: markGPU, Right: right})
	if in.Empty() {
		return
	}

	if len(gpus) == 0 {
		cv.TextCenter(in.X, in.Y+in.H/2-1, in.W, "NO GPU IN USE", ui.Style{FG: t.Dim})
		if in.H >= 4 {
			cv.TextCenter(in.X, in.Y+in.H/2+1, in.W, "start with --gpu=all", ui.Style{FG: t.Dim})
		}
		return
	}

	// More than one card gets a row each. A single donut for a rig is worse
	// than useless: it averages away the one card that has stopped, which is
	// the entire reason to look at this panel.
	if len(gpus) > 1 {
		panelGPUList(cv, in, gpus, s, t, sel)
		return
	}

	g := gpus[0]
	var sensor *GPUSensor
	if len(s.Sensors.GPUs) > 0 {
		sensor = &s.Sensors.GPUs[0]
	}

	body := in
	specH := 0
	if in.H >= 7 {
		specH = 2
		body = ui.Rect{X: in.X, Y: in.Y, W: in.W, H: in.H - specH - 1}
	}

	util, utilText := 0.0, ui.NA
	if sensor != nil && sensor.HaveUtil {
		util = float64(sensor.UtilPct) / 100
		utilText = fmt.Sprintf("%d%%", sensor.UtilPct)
	}

	text := body
	if body.W >= 30 && body.H >= 4 {
		gw := ringWidth(body.H, 15)
		ui.Ring(cv, ui.Rect{X: body.X, Y: body.Y, W: gw, H: body.H}, t, ui.RingOpts{
			Frac: util, FG: t.Accent2, Track: t.Glow,
			Lines: []string{utilText, "LOAD"}, LineFG: []string{t.Accent2, t.Muted},
		})
		text = ui.Rect{X: body.X + gw + 2, Y: body.Y, W: body.W - gw - 2, H: body.H}
	}

	y := text.Y
	if text.W >= 20 {
		cv.Text(text.X, y, ui.HumanRate(g.Rate), ui.Style{FG: t.Accent2, Bold: true})
		cv.TextRight(text.Right(), y, "HASHRATE", ui.Style{FG: t.Muted})
		y += 2
	}

	vram, clock, power := ui.NA, ui.NA, ui.NA
	if sensor != nil {
		if sensor.HaveMem && sensor.MemTotalMB > 0 {
			vram = fmt.Sprintf("%.1f / %.1f GB",
				float64(sensor.MemUsedMB)/1024, float64(sensor.MemTotalMB)/1024)
		}
		if sensor.HaveClock {
			clock = fmt.Sprintf("%d MHz", sensor.ClockMHz)
		}
		if sensor.HavePower {
			power = fmt.Sprintf("%.0f W", sensor.PowerW)
		}
	}
	rows := [][2]string{
		{"TEMP", tempOr(g.TempC) + staleMark(s.Sensors.At)},
		{"VRAM", vram},
		{"CLOCK", clock},
		{"POWER", power},
	}
	// The card's own name, where the panel is wide enough for it to be worth
	// more than the row it costs. On a single-card machine it is the one thing
	// on this panel that says which machine you are looking at.
	if sensor != nil && sensor.Name != "" && text.W >= 30 && text.Bottom()-y > len(rows) {
		rows = append(rows, [2]string{"DEVICE", sensor.Name})
	}
	colours := func(i int) string {
		if i == 0 {
			return ui.TempColour(t, g.TempC)
		}
		return ""
	}
	kvRows(cv, ui.Rect{X: text.X, Y: y, W: text.W, H: text.Bottom() - y}, y, rows, t, colours)

	if specH > 0 {
		ui.Spectrum(cv, ui.Rect{X: in.X, Y: in.Bottom() - specH, W: in.W, H: specH}, s.GPUHist, t, t.Accent2)
	}
}

// panelGPUList is the multi-card view: a row per device, sized so a six-card
// rig still fits.
func panelGPUList(cv *ui.Canvas, in ui.Rect, gpus []DeviceStat, s Snapshot, t *Theme, sel int) {
	if len(gpus) > 0 {
		sel = ((sel % len(gpus)) + len(gpus)) % len(gpus)
	}
	peak := 0.0
	for _, g := range gpus {
		if g.Rate > peak {
			peak = g.Rate
		}
	}
	if peak <= 0 {
		peak = 1
	}
	y := in.Y
	for i, g := range gpus {
		if y >= in.Bottom() {
			cv.TextRight(in.Right(), in.Bottom()-1,
				fmt.Sprintf("+%d more", len(gpus)-i), ui.Style{FG: t.Dim})
			break
		}
		fg := t.Accent2
		if g.Ailing {
			fg = t.Err
		}
		// Fixed columns from the right edge inwards, so the numbers line up
		// between rows whatever width they happen to be.
		const rateW, tempW, noteW = 11, 6, 6
		noteAt := in.Right() - noteW
		tempAt := noteAt - tempW
		rateAt := tempAt - rateW
		label := ui.Style{FG: t.Muted}
		if i == sel {
			// The selected card is the one the G key steps through. On a rig
			// the rows are otherwise interchangeable, and a caret costs one
			// cell to say which one a per-device figure elsewhere refers to.
			label = ui.Style{FG: t.Accent, Bold: true}
			cv.Set(in.X-1, y, ui.Caret, ui.Style{FG: t.Accent})
		}
		cv.TextIn(in.X, y, 6, g.Label, label)
		if barW := rateAt - in.X - 8; barW > 4 {
			ui.DrawBar(cv, in.X+7, y, barW, g.Rate/peak, fg, t.Track)
		}
		cv.TextRight(rateAt+rateW-1, y, ui.HumanRate(g.Rate), ui.Style{FG: t.Text})
		cv.TextRight(tempAt+tempW-1, y, tempOr(g.TempC), ui.Style{FG: ui.TempColour(t, g.TempC)})
		cv.TextRight(in.Right(), y, g.Note, ui.Style{FG: t.Dim})
		y++
	}
	if y+1 < in.Bottom() {
		ui.Row(cv, in, in.Bottom()-1, "TOTAL", ui.HumanRate(s.GPURate), t, t.Accent2)
	}
}

// ---------------------------------------------------------------- shares

func panelShares(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme) {
	in := ui.Panel(cv, r, t, ui.PanelOpts{Title: "SHARE STATS", Mark: markShares, Right: "ALL TIME"})
	if in.Empty() {
		return
	}

	acc, judged := s.ShareAcceptance()
	accText := ui.NA
	if judged {
		accText = fmt.Sprintf("%.1f%%", acc*100)
	}

	text := in
	if in.W >= 34 && in.H >= 5 {
		gw := ringWidth(in.H, 19)
		ui.Ring(cv, ui.Rect{X: in.X, Y: in.Y, W: gw, H: in.H}, t, ui.RingOpts{
			Frac: acc, Inner: 0.66,
			FG: t.Good, Track: t.Err,
			Lines: []string{accText, "ACCEPTED"}, LineFG: []string{t.Good, t.Muted},
		})
		text = ui.Rect{X: in.X + gw + 2, Y: in.Y, W: in.W - gw - 2, H: in.H}
	}

	judgedN := s.MiniBlocks + s.Rejected
	pctOf := func(v uint64) string {
		// The share of the total is the first thing to go when the column
		// narrows. It costs nine cells, it is the difference between a row
		// that fits its label and one that does not, and the ring beside it is
		// already showing the same proportion.
		if judgedN == 0 || text.W < 26 {
			return ""
		}
		return fmt.Sprintf("  (%.1f%%)", float64(v)/float64(judgedN)*100)
	}

	y := text.Y
	lines := []struct {
		label, value, fg string
	}{
		{"ACCEPTED", ui.Commas(s.MiniBlocks) + pctOf(s.MiniBlocks), t.Good},
		{"REJECTED", ui.Commas(s.Rejected) + pctOf(s.Rejected), t.Err},
		// getwork reports accepted and rejected and nothing else. A stale or
		// invalid share is not a category the protocol has, so inventing a
		// count for one would be inventing a number outright.
		{"STALE", ui.NA, t.Warn},
		{"INVALID", ui.NA, t.Muted},
	}
	for _, l := range lines {
		if y >= text.Bottom() {
			break
		}
		fg := l.fg
		if l.value == ui.NA {
			fg = t.Dim
		}
		// A colour swatch in front of each label, matching the ring beside it.
		// Four counts in four colours is a legend whether or not it is drawn
		// as one, and drawing it as one is what makes the ring readable.
		x := text.X
		swW := 0
		if text.W >= 22 {
			swW = ui.Swatch(cv, x, y, fg) + 1
			x += swW
		}
		// The same rule ui.Row applies, by hand because these labels carry a
		// colour of their own. Writing the label unbounded and then painting
		// the value over it, which is what this did, produced rows reading
		// "ACCEP1,245  (99.0%)".
		if lw := text.W - swW - ui.Width(l.value) - 1; lw >= ui.MinLabel {
			cv.TextIn(x, y, lw, l.label, ui.Style{FG: fg})
		}
		cv.TextRight(text.Right(), y, ui.Clip(l.value, text.W), ui.Style{FG: fg})
		y++
	}
	if y < text.Bottom() {
		y++
	}
	kvRows(cv, ui.Rect{X: text.X, Y: y, W: text.W, H: text.Bottom() - y}, y, [][2]string{
		{"SUBMITTED", ui.Commas(s.Submitted)},
		{"BEST SHARE", shareDiffText(s)},
		{"SHARES / MIN", sharesPerMin(s)},
		{"LAST SHARE", ui.Ago(s.LastShare)},
	}, t, nil)
}

// gpuDetail is the selected card's clock and power, for the multi-device
// header. Empty when nothing is known about it, rather than a row of dashes:
// the header is not a table and a dash there reads as a broken value.
func gpuDetail(s Snapshot, sel int) string {
	rows := gpuRows(s)
	if sel < 0 || sel >= len(rows) {
		return ""
	}
	out := "  ·  " + rows[sel].Label
	var g *GPUSensor
	for i := range s.Sensors.GPUs {
		if "GPU "+strconv.Itoa(s.Sensors.GPUs[i].Index) == rows[sel].Label {
			g = &s.Sensors.GPUs[i]
			break
		}
	}
	if g == nil {
		return out
	}
	if g.HaveClock {
		out += fmt.Sprintf(" · %d MHz", g.ClockMHz)
	}
	if g.HavePower {
		out += fmt.Sprintf(" · %.0f W", g.PowerW)
	}
	if g.HaveMem && g.MemTotalMB > 0 {
		out += fmt.Sprintf(" · %.1f/%.1f GB", float64(g.MemUsedMB)/1024, float64(g.MemTotalMB)/1024)
	}
	return out
}

func sharesPerMin(s Snapshot) string {
	if s.Uptime < 10*time.Second || s.Submitted == 0 {
		return ui.NA
	}
	return fmt.Sprintf("%.2f", float64(s.Submitted)/s.Uptime.Minutes())
}

// ---------------------------------------------------------------- blockchain

func panelBlockchain(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme, frame int) {
	in := ui.Panel(cv, r, t, ui.PanelOpts{Title: "BLOCKCHAIN STATUS", Mark: markChain})
	if in.Empty() {
		return
	}

	text := in
	// A turning wire block rather than the old shaded lump. It is the one
	// piece of artwork on the screen that is also a statement of what the
	// panel is about, and at twelve cells wide the wireframe reads as a block
	// where a solid reads as a smudge.
	//
	// It turns at a fifth of the frame rate. A cube spinning eight times a
	// second beside a column of figures is a distraction; one that moves
	// visibly but slowly is a sign of life.
	if cw := 13; in.W >= cw+24 && in.H >= 5 {
		ui.WireCube(cv, ui.Rect{X: in.X, Y: in.Y, W: cw, H: in.H}, t, float64(frame)*0.035)
		text = ui.Rect{X: in.X + cw + 2, Y: in.Y, W: in.W - cw - 2, H: in.H}
	}

	netDiff, interval := ui.NA, ui.NA
	if s.Info.OK {
		if s.Info.Difficulty > 0 {
			netDiff = ui.Commas(s.Info.Difficulty)
		}
		if s.Info.AvgBlockTime50 > 0 {
			interval = fmt.Sprintf("%.1fs", s.Info.AvgBlockTime50)
		} else if s.Info.BlockTime > 0 {
			interval = fmt.Sprintf("%ds", s.Info.BlockTime)
		}
	}

	rows := [][2]string{
		{"HEIGHT", ui.Commas(uint64(maxi64(s.Height, 0)))},
		{"MINI BLOCKS", ui.Commas(s.MiniBlocks)},
		{"BLOCKS FOUND", ui.Commas(s.Blocks)},
		// getwork has no orphan counter. The node knows; the miner is not told.
		{"ORPHANED", ui.NA},
		{"DIFFICULTY", ui.Commas(s.Difficulty)},
		{"NET DIFF", netDiff},
		{"LAST BLOCK", naDur(s.HeightAt)},
		{"INTERVAL", interval},
	}
	colours := func(i int) string {
		if i == 2 && s.Blocks > 0 {
			return t.Good
		}
		return ""
	}
	kvRows(cv, text, text.Y, rows, t, colours)
}

// ---------------------------------------------------------------- system

func panelSystem(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme) {
	in := ui.Panel(cv, r, t, ui.PanelOpts{Title: "SYSTEM OVERVIEW", Mark: markSystem})
	if in.Empty() {
		return
	}

	cpu := cpuRow(s)
	gpuTemp := tempUnknown
	if g := gpuRows(s); len(g) > 0 {
		gpuTemp = g[0].TempC
	}

	// Four tiles across when there is width, two when there is not. Below
	// that, a plain list -- a gauge two cells wide is decoration, not a gauge.
	// Four tiles in one row wherever the width allows, because that is the
	// arrangement that gives each gauge the whole height of the panel.
	// Stacking them two by two halves that, and a gauge one row tall is a
	// coloured label rather than a gauge.
	//
	// Two columns only when there is height for two rows of real tiles. Below
	// that the tiles are a label and a number with blank rows between them,
	// which the list says in half the space and without the holes.
	cols := 0
	switch {
	case in.W >= 36 && in.H >= 4:
		cols = 4
	case in.W >= 20 && in.H >= 6:
		cols = 2
	}

	if cols == 0 {
		mem := ui.NA
		if s.Sys.HaveMem {
			mem = fmt.Sprintf("%.1f / %.1f GB", float64(s.Sys.MemUsedMB)/1024, float64(s.Sys.MemTotalMB)/1024)
		}
		load := ui.NA
		if s.Sys.HaveLoad {
			load = fmt.Sprintf("%.0f%%", s.Sys.LoadPct)
		}
		kvRows(cv, in, in.Y, [][2]string{
			{"CPU TEMP", tempOr(cpu.TempC)},
			{"GPU TEMP", tempOr(gpuTemp)},
			{"CPU LOAD", load},
			{"MEMORY", mem},
		}, t, nil)
		return
	}

	tiles := []func(ui.Rect){
		func(c ui.Rect) { tileTemp(cv, c, "CPU TEMP", cpu.TempC, t) },
		func(c ui.Rect) { tileTemp(cv, c, "GPU TEMP", gpuTemp, t) },
		func(c ui.Rect) { tileLoad(cv, c, s, t) },
		func(c ui.Rect) { tileMemory(cv, c, s, t) },
	}
	nRows := (len(tiles) + cols - 1) / cols
	weights := make([]int, cols)
	for i := range weights {
		weights[i] = 1
	}
	heights := make([]int, nRows)
	for i := range heights {
		heights[i] = in.H / nRows
	}
	for ri, strip := range in.Rows(heights...) {
		if strip.H < 3 {
			break
		}
		for ci, cell := range strip.Cols(weights...) {
			if idx := ri*cols + ci; idx < len(tiles) {
				tiles[idx](cell)
			}
		}
	}
}

// Each tile is a title, a gauge and a reading. When there is no room for the
// gauge the reading moves up under the title rather than staying at the foot
// of the tile: a tile with two blank rows in the middle of it reads as a
// widget that failed to draw, which is exactly what it used to be.
func tileTemp(cv *ui.Canvas, r ui.Rect, label string, c float64, t *Theme) {
	if r.W < 6 || r.H < 2 {
		return
	}
	cv.TextCenter(r.X, r.Y, r.W, ui.Clip(label, r.W), ui.Style{FG: t.Muted})
	valueY := r.Y + 1
	// Four rows, not three. Thermometer draws four whatever it is handed, so
	// asked for three it put its bulb on the row the reading is written on.
	switch h := r.H - 2; {
	case h >= 4 && r.W >= 6:
		ui.Thermometer(cv, r.X+(r.W-4)/2, r.Y+1, h, c, c > tempUnknown, t)
		valueY = r.Bottom() - 1
	case r.H >= 3 && r.W >= 8 && c > tempUnknown:
		// Too short for a thermometer, wide enough for a bar. Same 30-100
		// scale, so the two forms of the tile read the same. The bar goes
		// under the reading rather than above it, which keeps the reading on
		// the row it is on in the tall form and stops the tile from opening a
		// gap between the two halves of one fact.
		ui.DrawBar(cv, r.X+1, r.Bottom()-1, r.W-2, (c-30)/70, ui.TempColour(t, c), t.Dim)
	}
	cv.TextCenter(r.X, valueY, r.W, tempOr(c), ui.Style{FG: ui.TempColour(t, c), Bold: true})
}

func tileLoad(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme) {
	if r.W < 6 || r.H < 2 {
		return
	}
	cv.TextCenter(r.X, r.Y, r.W, "CPU LOAD", ui.Style{FG: t.Muted})
	text := ui.NA
	frac := 0.0
	if s.Sys.HaveLoad {
		frac = s.Sys.LoadPct / 100
		text = fmt.Sprintf("%.0f%%", s.Sys.LoadPct)
	}
	valueY := r.Y + 1
	gaugeH := r.H - 2
	w := gaugeH * 2
	if w > r.W-2 {
		w = r.W - 2
	}
	// Ring's own floor is five columns by three rows; below it the call is a
	// no-op and the rows it was given stay empty.
	switch {
	case gaugeH >= 3 && w >= 5:
		// An open-bottomed arc rather than a full ring: it is the shape a dial
		// has, and at three rows a full ring has no room for a number inside.
		ui.Ring(cv, ui.Rect{X: r.X + (r.W-w)/2, Y: r.Y + 1, W: w, H: gaugeH}, t, ui.RingOpts{
			Frac: frac, Start: 225, Sweep: 270, Inner: 0.55,
			FG: t.Accent, Track: t.Glow,
		})
		valueY = r.Bottom() - 1
	case r.H >= 3 && r.W >= 8 && s.Sys.HaveLoad:
		ui.DrawBar(cv, r.X+1, r.Bottom()-1, r.W-2, frac, t.Accent, t.Dim)
	}
	cv.TextCenter(r.X, valueY, r.W, text, ui.Style{FG: t.Accent, Bold: true})
}

func tileMemory(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme) {
	if r.W < 6 || r.H < 2 {
		return
	}
	cv.TextCenter(r.X, r.Y, r.W, "MEMORY", ui.Style{FG: t.Muted})
	valueY := r.Y + 1
	// A tank rather than the chip drawing: at four rows the chip is an icon
	// with no reading in it, and this tile is a gauge, not a legend.
	w := 6
	if w > r.W-2 {
		w = r.W - 2
	}
	switch h := r.H - 2; {
	case h >= 3 && w >= 3:
		ui.Tank(cv, r.X+(r.W-w)/2, r.Y+1, w, h, s.Sys.MemUsedFrac(), t, t.Accent2)
		valueY = r.Bottom() - 1
	case r.W >= 8 && r.H >= 3 && s.Sys.HaveMem:
		ui.DrawBar(cv, r.X+1, r.Bottom()-1, r.W-2, s.Sys.MemUsedFrac(), t.Accent2, t.Dim)
	}
	text := ui.NA
	if s.Sys.HaveMem {
		text = fmt.Sprintf("%.1f/%.0fG", float64(s.Sys.MemUsedMB)/1024, float64(s.Sys.MemTotalMB)/1024)
	}
	cv.TextCenter(r.X, valueY, r.W, ui.Clip(text, r.W), ui.Style{FG: t.Accent2, Bold: true})
}

// ---------------------------------------------------------------- log

func panelLog(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme, v logView) {
	// The header says what the view is doing, and only when it is doing
	// something. A log that is simply following the newest line needs no
	// annotation; one that is paused, scrolled or filtered absolutely does,
	// because all three look identical to a log that has gone quiet.
	right := ""
	switch {
	case v.scroll > 0:
		right = fmt.Sprintf("↑ %d  PAUSED", v.scroll)
	case v.paused:
		right = "PAUSED"
	}
	if v.min != LogInfo || v.text != "" {
		if right != "" {
			right += "  ·  "
		}
		right += v.filterName()
	}
	in := ui.Panel(cv, r, t, ui.PanelOpts{Title: "LIVE MINING LOG", Mark: markLog, Right: right,
		Accent: logAccent(t, right)})
	if in.Empty() {
		return
	}
	drawLogLines(cv, in, v.apply(nil, s.Log), t, v.scroll)
}

// logAccent brightens the log panel's border while it is not simply
// following. It is the cheapest possible answer to "why has the log stopped",
// which is otherwise a question someone asks for a minute before noticing
// they left it paused.
func logAccent(t *Theme, right string) string {
	if right == "" {
		return ""
	}
	return t.Warn
}

// drawLogLines renders the newest entries that fit, newest at the top.
//
// Newest first, which is the opposite of a terminal log and the right way
// round for a panel: the latest event is always on the same row, so it can be
// checked with a glance rather than by finding the end of a list whose length
// changes with the size of the window.
//
// Scroll is counted back from the newest rather than as an absolute position,
// so an entry arriving while someone is reading history does not shift the
// screen under them.
func drawLogLines(cv *ui.Canvas, in ui.Rect, log []LogEntry, t *Theme, scroll int) {
	n := in.H
	if n <= 0 || len(log) == 0 {
		if in.H > 0 {
			cv.TextCenter(in.X, in.Y+in.H/2, in.W, "no events yet", ui.Style{FG: t.Dim})
		}
		return
	}
	maxScroll := len(log) - n
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	end := len(log) - scroll
	start := end - n
	if start < 0 {
		start = 0
	}

	// A tag column wide enough for the longest tag in view, so the message
	// text starts on the same column in every row and the block can be read
	// down rather than across.
	tagW := 6
	for _, e := range log[start:end] {
		if w := len(e.Tag) + 3; w > tagW {
			tagW = w
		}
	}
	if tagW > 13 {
		tagW = 13
	}

	y := in.Y
	view := log[start:end]
	for i := len(view) - 1; i >= 0; i-- {
		e := view[i]
		if y >= in.Bottom() {
			break
		}
		x := in.X
		if in.W > 22 {
			x += cv.Text(x, y, e.At.Format("15:04:05"), ui.Style{FG: t.Dim}) + 1
		}
		fg := levelColour(t, e.Level)
		tag := "[" + strings.ToUpper(e.Tag) + "]"
		cv.TextIn(x, y, tagW, ui.Clip(tag, tagW), ui.Style{FG: fg})
		x += tagW
		if x < in.Right() {
			cv.TextIn(x, y, in.Right()-x, e.Text, ui.Style{FG: t.Text})
		}
		y++
	}
}

func levelColour(t *Theme, l LogLevel) string {
	switch l {
	case LogGood:
		return t.Good
	case LogWarn:
		return t.Warn
	case LogError:
		return t.Err
	}
	return t.Accent2
}

// ---------------------------------------------------------------- threads

func panelThreads(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme) {
	in := ui.Panel(cv, r, t, ui.PanelOpts{
		Title: "ACTIVE THREADS", Mark: markThreads,
		Right: fmt.Sprintf("%d CPU", s.Threads)})
	if in.Empty() {
		return
	}
	drawThreadGrid(cv, in, s, t)
}

// drawThreadGrid lays the per-thread bars out in as many columns as fit.
//
// The percentage is each thread's rate against the fastest thread, not against
// a nominal maximum: there is no such thing as a thread's rated hashrate, and
// what a reader wants to know is whether one worker is falling behind the
// others. All at 100% is the healthy picture.
func drawThreadGrid(cv *ui.Canvas, in ui.Rect, s Snapshot, t *Theme) {
	n := s.Threads
	if n > len(s.ThreadRates) {
		n = len(s.ThreadRates)
	}
	if n <= 0 {
		cv.TextCenter(in.X, in.Y+in.H/2, in.W, "no CPU threads running", ui.Style{FG: t.Dim})
		return
	}
	peak := 0.0
	for _, v := range s.ThreadRates[:n] {
		if v > peak {
			peak = v
		}
	}

	const colW = 18
	cols := in.W / colW
	if cols < 1 {
		cols = 1
	}
	rows := (n + cols - 1) / cols
	if rows > in.H {
		rows = in.H
	}
	if rows < 1 {
		return
	}
	cellW := in.W / cols

	// When there are more threads than cells, the last cell says how many are
	// not shown. Quietly drawing three quarters of a sixty-four-thread machine
	// looks exactly like a machine with sixteen threads, which is the one
	// reading this panel must not allow.
	cells := cols * rows
	shown := n
	if n > cells {
		shown = cells - 1
	}

	for i := 0; i < shown; i++ {
		col, row := i/rows, i%rows
		x := in.X + col*cellW
		y := in.Y + row
		frac := 0.0
		text := ui.NA
		if peak > 0 {
			frac = s.ThreadRates[i] / peak
			text = fmt.Sprintf("%3.0f%%", frac*100)
		}
		// The healthy case is coloured along the wordmark gradient by slot
		// number, which is what turns a column of identical bars into
		// something the eye can count along. A thread that has fallen behind
		// leaves the gradient for the warning colours, so the exception is
		// still the only thing on the panel that is not cyan-to-violet.
		fg := t.Ramp(float64(i) / float64(maxInt(n-1, 1)))
		if peak > 0 && frac < 0.8 {
			fg = t.Warn
		}
		if peak > 0 && frac < 0.4 {
			fg = t.Err
		}
		label := fmt.Sprintf("T%02d", i+1)
		cv.Text(x, y, label, ui.Style{FG: t.Muted})
		if barW := cellW - len(label) - 7; barW > 2 {
			ui.DrawBar(cv, x+len(label)+1, y, barW, frac, fg, t.Track)
		}
		cv.TextRight(x+cellW-1, y, text, ui.Style{FG: t.Text})
	}
	if shown < n {
		col, row := shown/rows, shown%rows
		cv.Text(in.X+col*cellW, in.Y+row,
			fmt.Sprintf("+%d more", n-shown), ui.Style{FG: t.Dim})
	}
}

// ---------------------------------------------------------------- status

func panelStatus(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme, frame int) {
	in := ui.Panel(cv, r, t, ui.PanelOpts{Title: "MINING STATUS", Mark: markStorm})
	if in.Empty() {
		return
	}
	_, colour, label := stateLabel(s, t, frame)
	active := s.State == StateMining

	if in.W >= 24 && in.H >= 4 {
		// A wire ridgeline rather than the spiral this used to draw. The
		// spiral said "something is turning"; the ridge says what the program
		// is called, and the caption over it lands on a horizon instead of in
		// the middle of a vortex.
		ui.RidgeWire(cv, in, t, 0.9)
		if in.W >= 40 && in.H >= 6 {
			mw := ui.ArtWidth(ui.DeroMarkSmall)
			ui.DrawArt(cv, in.X+(in.W-mw)/2, in.Y, ui.DeroMarkSmall,
				ui.ArtPalette{}, t.Accent)
		}
	}

	// Each line of text clears its own row before it is written. The swirl is
	// behind the words, and tracked capitals are more gap than letter -- so
	// without this the dots come up between the letters and the one piece of
	// plain English on the panel is the hardest thing on it to read.
	say := func(y int, text string, st ui.Style) {
		if y < in.Y || y >= in.Bottom() || text == "" {
			return
		}
		text = ui.Clip(text, in.W)
		w := ui.Width(text) + 8
		x := in.X + (in.W-w)/2
		if w > in.W {
			x, w = in.X, in.W
		}
		cv.Fill(ui.Rect{X: x, Y: y, W: w, H: 1}, ' ', ui.Style{})
		cv.TextCenter(in.X, y, in.W, text, st)
	}

	mid := in.Y + in.H/2
	if in.H >= 5 {
		mid = in.Y + (in.H-2)/2
	}
	say(mid, ui.TrackedCaps(label), ui.Style{FG: colour, Bold: true})
	if in.H >= 4 {
		sub := "IN THE STORM"
		if !active {
			sub = strings.ToUpper(s.Node)
		}
		say(mid+1, sub, ui.Style{FG: t.Accent2})
	}
	if in.H >= 6 {
		say(in.Bottom()-1, "ASTROBWTv3 ENGINE "+engineWord(active), ui.Style{FG: t.Muted})
	}
}

func engineWord(active bool) string {
	if active {
		return "ACTIVE"
	}
	return "WAITING"
}

// ---------------------------------------------------------------- quick

func panelQuick(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme) {
	in := ui.Panel(cv, r, t, ui.PanelOpts{Title: "QUICK STATS", Mark: markQuick})
	if in.Empty() {
		return
	}
	eff := ui.NA
	if acc, ok := s.ShareAcceptance(); ok {
		eff = fmt.Sprintf("%.1f%%", acc*100)
	}
	rows := [][2]string{
		{"UPTIME", ui.HMS(s.Uptime)},
		{"TOTAL HASHES", ui.Commas(s.TotalHashes)},
		{"ACCEPTED", ui.Commas(s.MiniBlocks)},
		{"REJECTED", ui.Commas(s.Rejected)},
		{"STALE", ui.NA},
		{"INVALID", ui.NA},
		{"EFFICIENCY", eff},
		{"JOB DIFFICULTY", ui.Commas(s.Difficulty)},
	}
	colours := func(i int) string {
		switch {
		case i == 2 && s.MiniBlocks > 0:
			return t.Good
		case i == 3 && s.Rejected > 0:
			return t.Warn
		case i == 4 || i == 5:
			return t.Dim
		}
		return ""
	}
	kvRows(cv, in, in.Y, rows, t, colours)
}

// ---------------------------------------------------------------- connection

func panelConnection(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme, frame int) {
	in := ui.Panel(cv, r, t, ui.PanelOpts{Title: "CONNECTION", Mark: markConn})
	if in.Empty() {
		return
	}
	bars := signalBars(s)
	ok := bars > 0
	colour, label := connLabel(s, t)
	ping := ui.NA
	if s.Info.OK {
		ping = fmt.Sprintf("%d ms", s.Info.Latency.Milliseconds())
	}

	// The mast needs five rows to be a mast. Below that the panel drops it and
	// spends the rows on the latency and peer count instead, which is more use
	// than a two-row drawing of an aerial.
	if in.W >= 10 && in.H >= len(ui.MastArt)+3 {
		// The aerial on the left and the strength meter on the right, which is
		// the arrangement that lets the two words underneath -- how good, how
		// far away -- sit under the thing each of them describes.
		// The aerial on the left and the strength meter on the right, so the
		// two words under them -- how strong, how far -- each sit under the
		// thing they describe. Both need five rows to be worth drawing, which
		// is what decides how many rows are left for the words.
		artH := in.H - 2
		if artH > len(ui.MastArt)+3 {
			artH = len(ui.MastArt) + 3
		}
		artW := in.W
		if in.W >= 22 {
			artW = in.W / 2
			ui.SignalRamp(cv, ui.Rect{X: in.X + artW, Y: in.Y, W: in.W - artW, H: artH}, bars, t, ok)
		}
		ui.Mast(cv, ui.Rect{X: in.X, Y: in.Y, W: artW, H: artH}, t, bars, frame, ok)

		y := in.Bottom() - 2
		if in.W >= 22 {
			cv.TextCenter(in.X, y, artW, "SIGNAL", ui.Style{FG: t.Muted})
			cv.TextCenter(in.X+artW, y, in.W-artW, signalWord(bars),
				ui.Style{FG: signalColour(t, bars), Bold: true})
			ui.Row(cv, in, in.Bottom()-1, "PING", ping, t, t.Text)
		} else {
			cv.Text(in.X, y, "SIGNAL", ui.Style{FG: t.Muted})
			ui.SignalBars(cv, in.Right()-5, y, bars, t)
			cv.TextCenter(in.X, in.Bottom()-1, in.W, signalWord(bars),
				ui.Style{FG: signalColour(t, bars), Bold: true})
		}
		return
	}

	y := in.Y
	cv.TextCenter(in.X, y, in.W, ui.Clip(string(ui.Bullet)+" "+label, in.W),
		ui.Style{FG: colour, Bold: true})
	y += 2
	if y < in.Bottom() && in.W >= 14 {
		cv.Text(in.X, y, "SIGNAL", ui.Style{FG: t.Muted})
		ui.SignalBars(cv, in.Right()-5, y, bars, t)
		y++
	}
	peers := ui.NA
	if s.Info.OK {
		peers = fmt.Sprint(s.Info.Peers)
	}
	// The strength word only gets its own row when there is one to spare.
	// Below that the rows go to the figures, because "GOOD" is a summary of
	// the ping and the ping is the thing worth reading.
	rows := ui.Rect{X: in.X, Y: y, W: in.W, H: in.Bottom() - y}
	if in.H >= 5 {
		rows.H--
		cv.TextCenter(in.X, in.Bottom()-1, in.W, signalWord(bars),
			ui.Style{FG: signalColour(t, bars), Bold: true})
	}
	kvRows(cv, rows, y, [][2]string{
		{"PING", ping},
		{"PEERS", peers},
		{"LAST JOB", naDur(s.LastJob)},
	}, t, nil)
}

func signalColour(t *Theme, bars int) string {
	switch {
	case bars >= 4:
		return t.Good
	case bars >= 2:
		return t.Warn
	}
	return t.Err
}
