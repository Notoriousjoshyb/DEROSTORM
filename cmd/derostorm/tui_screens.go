package main

// The screens behind the number keys.
//
// Each one is the dashboard's treatment of a single subject, given the whole
// window instead of a twentieth of it. They exist because the dashboard has to
// choose: it can show ten network figures or the four that matter, and the
// right answer for a screen someone glances at is four. The other six live
// here, one keystroke away, where there is room to lay them out properly.
//
// They reuse the dashboard's own panels wherever the content is the same. A
// second implementation of the thread grid would be a second thing to keep
// correct and a second thing to get subtly out of step.

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/notoriousjoshyb/derostorm/internal/ui"
)

// screenTitle draws the "you are here" line above a screen's content and
// returns what is left.
func screenTitle(cv *ui.Canvas, r ui.Rect, title, sub string, t *Theme) ui.Rect {
	if r.H < 3 {
		return r
	}
	cv.Text(r.X+2, r.Y, ui.TrackedCaps(title), ui.Style{FG: t.Accent, Bold: true})
	if sub != "" {
		cv.TextRight(r.Right()-2, r.Y, sub, ui.Style{FG: t.Muted})
	}
	cv.HLine(r.X+1, r.Y+1, r.W-2, '─', ui.Style{FG: t.Border})
	return ui.Rect{X: r.X, Y: r.Y + 2, W: r.W, H: r.H - 2}
}

// ---------------------------------------------------------------- mining

func drawMiningScreen(cv *ui.Canvas, body ui.Rect, s Snapshot, t *Theme, frame int) {
	body = screenTitle(cv, body.Inset2(1, 0), "MINING", "AstroBWTv3", t)
	if body.H < 6 {
		return
	}
	rows := body.Rows(body.H/2, -1)

	top := rows[0].Cols(38, 34, 28)
	panelMining(cv, top[0], s, t, frame)
	panelHistory(cv, top[1], s, t)
	panelStatus(cv, top[2], s, t, frame)

	bottom := rows[1].Cols(50, 50)
	panelDevices(cv, bottom[0], s, t)
	panelThreads(cv, bottom[1], s, t)
}

// panelDevices is the per-source table: the CPU and then a row per GPU, with
// its share of the total, its temperature and whatever detail the card
// reported.
//
// This is the panel that answers "is everything still working?", which a
// single combined hashrate cannot. A source that stops contributing shows up
// here immediately rather than as a headline figure that is quietly a third
// lower than it was an hour ago.
func panelDevices(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme) {
	in := ui.Panel(cv, r, t, ui.PanelOpts{Title: "HASHING DEVICES", Mark: "◈"})
	if in.Empty() {
		return
	}
	devices := s.deviceRows()
	peak, total := 0.0, 0.0
	for _, d := range devices {
		if d.Rate > peak {
			peak = d.Rate
		}
		total += d.Rate
	}
	if peak <= 0 {
		peak = 1
	}
	if total <= 0 {
		total = 1
	}

	y := in.Y
	const rateW, shareW, tempW, noteW = 11, 6, 6, 7
	noteAt := in.Right() - noteW
	tempAt := noteAt - tempW
	shareAt := tempAt - shareW
	rateAt := shareAt - rateW

	if in.W > 44 {
		cv.TextRight(rateAt+rateW-1, y, "RATE", ui.Style{FG: t.Muted})
		cv.TextRight(shareAt+shareW-1, y, "SHARE", ui.Style{FG: t.Muted})
		cv.TextRight(tempAt+tempW-1, y, "TEMP", ui.Style{FG: t.Muted})
		cv.TextRight(in.Right(), y, "POWER", ui.Style{FG: t.Muted})
		y++
	}

	for _, d := range devices {
		if y >= in.Bottom() {
			break
		}
		fg := t.Accent
		if d.IsGPU {
			fg = t.Accent2
		}
		if d.Ailing {
			fg = t.Err
		}
		cv.TextIn(in.X, y, 7, d.Label, ui.Style{FG: t.Muted})
		if barW := rateAt - in.X - 9; barW > 4 {
			ui.DrawBar(cv, in.X+8, y, barW, d.Rate/peak, fg, t.Dim)
		}
		cv.TextRight(rateAt+rateW-1, y, ui.HumanRate(d.Rate), ui.Style{FG: t.Text})
		cv.TextRight(shareAt+shareW-1, y, ui.Pct(d.Rate, total), ui.Style{FG: t.Dim})
		cv.TextRight(tempAt+tempW-1, y, tempOr(d.TempC), ui.Style{FG: ui.TempColour(t, d.TempC)})
		note := d.Note
		if note == "" {
			note = ui.NA
		}
		cv.TextRight(in.Right(), y, note, ui.Style{FG: t.Dim})
		y++
	}
}

// ---------------------------------------------------------------- statistics

func drawStatsScreen(cv *ui.Canvas, body ui.Rect, s Snapshot, t *Theme) {
	body = screenTitle(cv, body.Inset2(1, 0), "STATISTICS", "session totals and hashrate history", t)
	if body.H < 6 {
		return
	}

	// Four windows, drawn only where there is data. A 24-hour chart on a miner
	// that has been up for eleven minutes is twenty-three hours of blank with
	// a smudge at the right-hand end, which says less than not drawing it.
	type win struct {
		title  string
		labels []string
		vals   []float64
	}
	wins := []win{
		{"5 MIN", []string{"5m", "4m", "3m", "2m", "1m", "Now"}, s.History},
		{"15 MIN", []string{"15m", "10m", "5m", "Now"}, s.Hist15m},
		{"1 HOUR", []string{"1h", "45m", "30m", "15m", "Now"}, s.Hist1h},
		{"24 HOUR", []string{"24h", "18h", "12h", "6h", "Now"}, s.Hist24h},
	}
	var live []win
	for _, w := range wins {
		if len(w.vals) >= 2 {
			live = append(live, w)
		}
	}
	if len(live) == 0 {
		live = wins[:1]
	}

	rows := body.Rows(body.H*2/3, -1)
	chartArea := rows[0]

	perRow := 2
	if chartArea.W < 90 {
		perRow = 1
	}
	nRows := (len(live) + perRow - 1) / perRow
	if nRows < 1 {
		nRows = 1
	}
	heights := make([]int, nRows)
	for i := range heights {
		heights[i] = chartArea.H / nRows
	}
	strips := chartArea.Rows(heights...)
	for i, w := range live {
		ri, ci := i/perRow, i%perRow
		if ri >= len(strips) || strips[ri].H < 5 {
			break
		}
		weights := make([]int, perRow)
		for j := range weights {
			weights[j] = 1
		}
		cell := strips[ri].Cols(weights...)[ci]
		in := ui.Panel(cv, cell, t, ui.PanelOpts{Title: "HASHRATE", Mark: "◈", Right: w.title})
		if !in.Empty() {
			ui.Chart(cv, in, t, ui.ChartOpts{Values: w.vals, XLabels: w.labels, Fill: in.H >= 6})
		}
	}

	cols := rows[1].Cols(34, 33, 33)
	panelSessionTotals(cv, cols[0], s, t)
	panelShares(cv, cols[1], s, t)
	panelRateSummary(cv, cols[2], s, t)
}

func panelSessionTotals(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme) {
	in := ui.Panel(cv, r, t, ui.PanelOpts{Title: "SESSION", Mark: "◈"})
	if in.Empty() {
		return
	}
	kvRows(cv, in, in.Y, [][2]string{
		{"UPTIME", ui.HMS(s.Uptime)},
		{"TOTAL HASHES", ui.Commas(s.TotalHashes)},
		{"SHARES SUBMITTED", ui.Commas(s.Submitted)},
		{"MINIBLOCKS", ui.Commas(s.MiniBlocks)},
		{"BLOCKS", ui.Commas(s.Blocks)},
		{"REJECTED", ui.Commas(s.Rejected)},
		{"BEST SHARE", shareDiffText(s)},
		{"LAST SHARE", ui.Ago(s.LastShare)},
	}, t, nil)
}

func panelRateSummary(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme) {
	in := ui.Panel(cv, r, t, ui.PanelOpts{Title: "HASHRATE", Mark: "◈"})
	if in.Empty() {
		return
	}
	eff := ui.NA
	if v, ok := gpuEfficiency(s); ok {
		eff = fmt.Sprintf("%.0f H/W", v)
	}
	kvRows(cv, in, in.Y, [][2]string{
		{"CURRENT", ui.HumanRate(s.Hashrate)},
		{"1 MINUTE", ui.HumanRate(s.Avg1m)},
		{"5 MINUTE", ui.HumanRate(s.Avg5m)},
		{"15 MINUTE", ui.HumanRate(s.Avg15m)},
		{"SESSION AVG", ui.HumanRate(s.AvgRate)},
		{"PEAK", ui.HumanRate(s.PeakRate)},
		{"CPU", ui.HumanRate(s.CPURate)},
		{"GPU", ui.HumanRate(s.GPURate)},
		// GPU-only, because the cards report their own power and the CPU does
		// not on any platform this runs on. A whole-machine figure would be a
		// real number divided by a made-up one, and it is exactly the number
		// someone would use to choose a power limit.
		{"GPU EFFICIENCY", eff},
	}, t, nil)
}

// ---------------------------------------------------------------- network

func drawNetworkScreen(cv *ui.Canvas, body ui.Rect, s Snapshot, t *Theme, frame int) {
	body = screenTitle(cv, body.Inset2(1, 0), "NETWORK", s.Node, t)
	if body.H < 6 {
		return
	}
	rows := body.Rows(body.H/2, -1)

	top := rows[0].Cols(40, 30, 30)
	panelNetwork(cv, top[0], s, t, frame)
	panelNodeDetail(cv, top[1], s, t)
	panelConnection(cv, top[2], s, t, frame)

	bottom := rows[1].Cols(45, 55)
	panelBlockchain(cv, bottom[0], s, t)
	panelJob(cv, bottom[1], s, t)
}

func panelNodeDetail(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme) {
	in := ui.Panel(cv, r, t, ui.PanelOpts{Title: "NODE", Mark: "◈"})
	if in.Empty() {
		return
	}
	if !s.Info.OK {
		y := in.Y
		cv.TextIn(in.X, y, in.W, "chain detail unavailable", ui.Style{FG: t.Warn})
		y += 2
		for _, line := range wrapText(
			"derod serves getwork and JSON-RPC on different ports, and this node "+
				"answered on neither of the addresses tried. Peer count, network "+
				"hashrate and block interval need that endpoint.", in.W) {
			if y >= in.Bottom() {
				break
			}
			cv.TextIn(in.X, y, in.W, line, ui.Style{FG: t.Dim})
			y++
		}
		if y < in.Bottom()-1 {
			cv.TextIn(in.X, in.Bottom()-1, in.W, "start with --rpc-address=host:port", ui.Style{FG: t.Muted})
		}
		return
	}
	kvRows(cv, in, in.Y, [][2]string{
		{"VERSION", s.Info.Version},
		{"NETWORK", s.Info.Network},
		{"HEIGHT", ui.Commas(uint64(maxi64(s.Info.Height, 0)))},
		{"TOPO HEIGHT", ui.Commas(uint64(maxi64(s.Info.TopoHeight, 0)))},
		{"PEERS IN", fmt.Sprint(s.Info.PeersIn)},
		{"PEERS OUT", fmt.Sprint(s.Info.PeersOut)},
		{"MINERS", fmt.Sprint(s.Info.Miners)},
		{"MINIS IN MEMORY", fmt.Sprint(s.Info.MiniInMemory)},
		{"TX POOL", ui.Commas(s.Info.TxPool)},
		{"RPC", s.Info.Endpoint},
	}, t, nil)
}

func panelJob(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme) {
	in := ui.Panel(cv, r, t, ui.PanelOpts{Title: "CURRENT JOB", Mark: "◈"})
	if in.Empty() {
		return
	}
	eta := ui.NA
	if s.Hashrate > 0 && s.Difficulty > 0 {
		// The mean gap between shares at this rate and this difficulty. It is
		// a mean over a memoryless process, so a run three times that long is
		// unremarkable; the number is for comparing settings, not for
		// predicting the next minute.
		eta = "~" + ui.ShortDur(time.Duration(float64(s.Difficulty)/s.Hashrate*float64(time.Second)))
	}
	share := ui.NA
	if v, ok := s.Info.NetHashrate(); ok && v > 0 && s.Hashrate > 0 {
		share = fmt.Sprintf("%.4f%%", s.Hashrate/v*100)
	}
	kvRows(cv, in, in.Y, [][2]string{
		{"JOB HEIGHT", ui.Commas(uint64(maxi64(s.Height, 0)))},
		{"JOB DIFFICULTY", ui.Commas(s.Difficulty)},
		{"JOB AGE", naDur(s.LastJob)},
		{"CONNECTED FOR", naDur(s.ConnectedAt)},
		{"EXPECTED SHARE", eta},
		{"SHARE OF NETWORK", share},
		{"BEST SHARE FOUND", shareDiffText(s)},
	}, t, nil)
}

// ---------------------------------------------------------------- threads

func drawThreadsScreen(cv *ui.Canvas, body ui.Rect, s Snapshot, t *Theme) {
	body = screenTitle(cv, body.Inset2(1, 0), "THREADS",
		fmt.Sprintf("%d of %d logical CPUs", s.Threads, runtime.NumCPU()), t)
	if body.H < 5 {
		return
	}
	rows := body.Rows(-1, 7)

	in := ui.Panel(cv, rows[0], t, ui.PanelOpts{
		Title: "PER-THREAD UTILISATION", Mark: "◈", Right: "share of the fastest thread"})
	if !in.Empty() {
		drawThreadGrid(cv, in, s, t)
	}

	cols := rows[1].Cols(34, 33, 33)
	panelThreadSummary(cv, cols[0], s, t)
	panelCPU(cv, cols[1], s, t)
	panelHint(cv, cols[2], t, []string{
		"press : to open the command line",
		"threads 12   set the thread count",
		"threads +2   add two threads",
		"threads -1   give one back",
	})
}

func panelThreadSummary(cv *ui.Canvas, r ui.Rect, s Snapshot, t *Theme) {
	in := ui.Panel(cv, r, t, ui.PanelOpts{Title: "THREAD SUMMARY", Mark: "◈"})
	if in.Empty() {
		return
	}
	n := s.Threads
	if n > len(s.ThreadRates) {
		n = len(s.ThreadRates)
	}
	best, worst, sum := 0.0, 0.0, 0.0
	if n > 0 {
		worst = s.ThreadRates[0]
		for _, v := range s.ThreadRates[:n] {
			if v > best {
				best = v
			}
			if v < worst {
				worst = v
			}
			sum += v
		}
	}
	mean := 0.0
	if n > 0 {
		mean = sum / float64(n)
	}
	spread := ui.NA
	if best > 0 {
		// The gap between the fastest and slowest worker as a fraction. Small
		// is healthy; large means something is not sharing the machine evenly,
		// which is usually another process or a core that is parked.
		spread = fmt.Sprintf("%.0f%%", (best-worst)/best*100)
	}
	kvRows(cv, in, in.Y, [][2]string{
		{"RUNNING", fmt.Sprint(s.Threads)},
		{"LOGICAL CPUS", fmt.Sprint(runtime.NumCPU())},
		{"FASTEST", ui.HumanRate(best)},
		{"SLOWEST", ui.HumanRate(worst)},
		{"MEAN", ui.HumanRate(mean)},
		{"SPREAD", spread},
	}, t, nil)
}

// ---------------------------------------------------------------- config

func drawConfigScreen(cv *ui.Canvas, body ui.Rect, s Snapshot, t *Theme) {
	body = screenTitle(cv, body.Inset2(1, 0), "CONFIG", s.ConfigPath, t)
	if body.H < 5 {
		return
	}
	rows := body.Rows(body.H/2, -1)
	top := rows[0].Cols(50, 50)

	in := ui.Panel(cv, top[0], t, ui.PanelOpts{Title: "SETTINGS", Mark: "◈"})
	if !in.Empty() {
		gpus := "off"
		if len(s.GPUList) > 0 {
			parts := make([]string, len(s.GPUList))
			for i, d := range s.GPUList {
				parts[i] = fmt.Sprint(d)
			}
			gpus = strings.Join(parts, ", ")
		}
		batch, blocks := "auto", "measured"
		if s.GPUBatch > 0 {
			batch = ui.Commas(uint64(s.GPUBatch))
		}
		if s.GPUBlocks > 0 {
			blocks = fmt.Sprint(s.GPUBlocks)
		}
		net := "mainnet"
		if s.Testnet {
			net = "testnet"
		}
		// The wallet address is deliberately not here, and not anywhere else in
		// this console. It is not an operational number and it is the one
		// string on the machine worth keeping off a screen that gets
		// photographed and posted when something goes wrong.
		kvRows(cv, in, in.Y, [][2]string{
			{"NODE", s.Node},
			{"NETWORK", net},
			{"CPU THREADS", fmt.Sprint(s.Threads)},
			{"GPU DEVICES", gpus},
			{"GPU BATCH", batch},
			{"GPU BLOCKS", blocks},
			{"THEME", s.ThemeName},
			{"CONFIG FILE", s.ConfigPath},
			{"LOG FILE", s.LogFile},
		}, t, nil)
	}

	panelHint(cv, top[1], t, append([]string{
		"press : to open the command line",
		"",
	}, configCommandHelp...))

	in = ui.Panel(cv, rows[1], t, ui.PanelOpts{Title: "START-UP NOTES", Mark: "◈"})
	if !in.Empty() {
		notes := append([]string{}, s.SensorNote...)
		if s.SANote != "" {
			notes = append([]string{s.SANote}, notes...)
		}
		if s.NodeNote != "" {
			notes = append(notes, s.NodeNote)
		}
		y := in.Y
		for _, n := range notes {
			for _, line := range wrapText(n, in.W-2) {
				if y >= in.Bottom() {
					break
				}
				cv.Set(in.X, y, ui.Caret, ui.Style{FG: t.Dim})
				cv.TextIn(in.X+2, y, in.W-2, line, ui.Style{FG: t.Muted})
				y++
			}
		}
		if y == in.Y {
			cv.TextIn(in.X, in.Y, in.W, "nothing to report", ui.Style{FG: t.Dim})
		}
	}
}

var configCommandHelp = []string{
	"threads <n>    change the thread count live",
	"theme <name>   switch colour theme",
	"save           write settings to the config file",
	"config         print the active settings to the log",
	"quit           stop mining and exit",
	"",
	"themes: " + strings.Join(ui.ThemeNames(), ", "),
}

func panelHint(cv *ui.Canvas, r ui.Rect, t *Theme, lines []string) {
	in := ui.Panel(cv, r, t, ui.PanelOpts{Title: "COMMANDS", Mark: "◈"})
	if in.Empty() {
		return
	}
	y := in.Y
	for _, l := range lines {
		if y >= in.Bottom() {
			break
		}
		fg := t.Muted
		if strings.HasPrefix(l, "press ") || strings.HasPrefix(l, "themes:") {
			fg = t.Dim
		}
		cv.TextIn(in.X, y, in.W, l, ui.Style{FG: fg})
		y++
	}
}

// ---------------------------------------------------------------- logs

func drawLogScreen(cv *ui.Canvas, body ui.Rect, s Snapshot, t *Theme, scroll int) {
	sub := "↑ ↓ PgUp PgDn to scroll · End for live"
	if scroll > 0 {
		sub = fmt.Sprintf("scrolled back %d · End for live", scroll)
	}
	body = screenTitle(cv, body.Inset2(1, 0), "LOGS", sub, t)
	if body.H < 3 {
		return
	}
	in := ui.Panel(cv, body, t, ui.PanelOpts{
		Title: "EVENT LOG", Mark: "◈", Right: fmt.Sprintf("%d events held", len(s.Log))})
	if in.Empty() {
		return
	}
	drawLogLines(cv, in, s.Log, t, scroll)
}

// ---------------------------------------------------------------- pools

// drawPoolsScreen is honest about what this miner is.
//
// DeroStorm speaks derod's getwork, which is solo mining: the work comes from
// a node and a found block is this machine's. There is no pool, no pool
// account and no pool share accounting, and a screen that laid out empty
// fields called "pool fee" and "pool balance" would imply there was.
func drawPoolsScreen(cv *ui.Canvas, body ui.Rect, s Snapshot, t *Theme) {
	body = screenTitle(cv, body.Inset2(1, 0), "POOLS", "solo mining via derod getwork", t)
	if body.H < 5 {
		return
	}
	rows := body.Rows(body.H/2, -1)
	cols := rows[0].Cols(55, 45)

	in := ui.Panel(cv, cols[0], t, ui.PanelOpts{Title: "WORK SOURCE", Mark: "◈"})
	if !in.Empty() {
		colour, label := connLabel(s, t)
		y := in.Y
		cv.Text(in.X, y, "STATUS", ui.Style{FG: t.Muted})
		cv.TextRight(in.Right(), y, string(ui.Bullet)+" "+label, ui.Style{FG: colour, Bold: true})
		y += 2
		latency := ui.NA
		if s.Info.OK {
			latency = fmt.Sprintf("%d ms", s.Info.Latency.Milliseconds())
		}
		rpc := ui.NA
		if s.Info.OK {
			rpc = s.Info.Endpoint
		}
		kvRows(cv, ui.Rect{X: in.X, Y: y, W: in.W, H: in.Bottom() - y}, y, [][2]string{
			{"TYPE", "solo · derod getwork"},
			{"GETWORK", "wss://" + s.Node + "/ws/"},
			{"RPC", rpc},
			{"CONNECTED FOR", naDur(s.ConnectedAt)},
			{"LAST JOB", naDur(s.LastJob)},
			{"LATENCY", latency},
			{"SHARES SENT", ui.Commas(s.Submitted)},
			{"ACCEPTED", ui.Commas(s.MiniBlocks)},
		}, t, nil)
	}

	in = ui.Panel(cv, cols[1], t, ui.PanelOpts{Title: "ABOUT SOLO MINING", Mark: "◈"})
	if !in.Empty() {
		y := in.Y
		for _, line := range wrapText(
			"This miner talks getwork to a DERO node. Work comes from that node "+
				"and a block it finds belongs to the address the node was given. "+
				"There is no pool between the two, so there is no pool fee, no pool "+
				"share accounting and no pool payout schedule to show here.", in.W) {
			if y >= in.Bottom() {
				break
			}
			cv.TextIn(in.X, y, in.W, line, ui.Style{FG: t.Muted})
			y++
		}
		if y+1 < in.Bottom() {
			cv.TextIn(in.X, y+1, in.W, "change node: --daemon-rpc-address=host:port",
				ui.Style{FG: t.Dim})
		}
	}

	panelLog(cv, rows[1], s, t, 0)
}

// ---------------------------------------------------------------- help

func drawHelpScreen(cv *ui.Canvas, body ui.Rect, t *Theme) {
	body = screenTitle(cv, body.Inset2(1, 0), "HELP", "DEROSTORM v"+version, t)
	if body.H < 5 {
		return
	}
	cols := body.Cols(34, 33, 33)

	in := ui.Panel(cv, cols[0], t, ui.PanelOpts{Title: "SCREENS", Mark: "◈"})
	if !in.Empty() {
		rows := make([][2]string, 0, len(navItems)+3)
		rows = append(rows, [2]string{"D  or  Esc", "dashboard"})
		for _, n := range navItems {
			rows = append(rows, [2]string{string(n.key), strings.ToLower(n.label)})
		}
		rows = append(rows,
			[2]string{"Tab", "next screen"},
			[2]string{"Q  or  Ctrl-C", "quit"})
		kvRows(cv, in, in.Y, rows, t, nil)
	}

	in = ui.Panel(cv, cols[1], t, ui.PanelOpts{Title: "KEYS", Mark: "◈"})
	if !in.Empty() {
		kvRows(cv, in, in.Y, [][2]string{
			{"↑ ↓  or  j k", "scroll the log"},
			{"PgUp PgDn", "scroll a page"},
			{"Home", "oldest event"},
			{"End", "back to live"},
			{":  or  /", "command line"},
			{"Esc", "cancel / dashboard"},
			{"Ctrl-U", "clear the command"},
		}, t, nil)
	}

	in = ui.Panel(cv, cols[2], t, ui.PanelOpts{Title: "COMMANDS", Mark: "◈"})
	if !in.Empty() {
		y := in.Y
		for _, l := range configCommandHelp {
			if y >= in.Bottom() {
				break
			}
			cv.TextIn(in.X, y, in.W, l, ui.Style{FG: t.Muted})
			y++
		}
		if y+1 < in.Bottom() {
			cv.TextIn(in.X, y+1, in.W, "start with --no-tui for plain output", ui.Style{FG: t.Dim})
		}
	}
}

// ---------------------------------------------------------------- helpers

// wrapText breaks a paragraph at word boundaries to fit w columns.
func wrapText(s string, w int) []string {
	if w < 8 {
		return nil
	}
	var out []string
	var line strings.Builder
	for _, word := range strings.Fields(s) {
		add := ui.Width(word)
		if line.Len() > 0 && ui.Width(line.String())+1+add > w {
			out = append(out, line.String())
			line.Reset()
		}
		if line.Len() > 0 {
			line.WriteByte(' ')
		}
		line.WriteString(word)
	}
	if line.Len() > 0 {
		out = append(out, line.String())
	}
	return out
}
