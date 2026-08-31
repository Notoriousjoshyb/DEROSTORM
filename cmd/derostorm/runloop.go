package main

// The run loop: everything that has to happen between "start mining" and
// "stop", and the assembly of the Snapshot the console draws.
//
// The shape is deliberately one-directional. The engine, the sensors, the
// machine metrics and the node watcher each run on their own goroutine and
// each publish a value. This loop reads those values on a timer, builds one
// immutable Snapshot, and hands it to whichever console is in use. Nothing in
// the console can reach back: there is no path by which a slow terminal, a
// resized window or a panicking widget can make a mining thread wait.
//
// That is the whole reason the render tick can be as fast as it likes. It is
// eight frames a second here because that is where a hashrate stops looking
// jerky, not because anything is racing it.

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/deroproject/derohe/astrobwt/astrobwtv3"
	"github.com/notoriousjoshyb/derostorm/internal/ui"
)

// consoleMode is which of the three output styles a run uses.
type consoleMode int

const (
	// consoleFull is the full-screen console: the alternate buffer, panels,
	// single-key navigation. The default on an interactive terminal.
	consoleFull consoleMode = iota
	// consoleClassic is the compact in-place panel. It scrolls with the shell,
	// leaves its output in the scrollback, and is the right choice inside tmux
	// panes, over a slow link, or anywhere the alternate buffer is a nuisance.
	consoleClassic
	// consolePlain is one line per event and nothing else: for services, for
	// log files, and for terminals that cannot do better.
	consolePlain
)

type runOpts struct {
	cfg        *Config
	cfgPath    string
	theme      *Theme
	themeNote  string
	isTTY      bool
	mode       consoleMode
	testnet    bool
	runFor     int
	rpcAddress string
	logFile    string
	statsFile  string
}

// renderTick is how often a frame is built. Eight a second: fast enough that
// the animated pieces move smoothly and a changing hashrate does not step,
// slow enough that the cost is invisible next to mining. Every frame writes
// only the cells that changed, so a still screen costs almost nothing.
const renderTick = 125 * time.Millisecond

func run(o runOpts) {
	cfg := o.cfg
	live := o.isTTY && o.theme.Name != "mono" && o.mode != consolePlain
	if !live {
		o.mode = consolePlain
	}

	// ---- shared plumbing, the same whichever console is used
	var (
		logMu   sync.Mutex
		logRing []LogEntry
	)
	// A console that is not the plain one prints nothing itself; every event
	// goes into this ring and is drawn as part of a frame. Keeping 400 is what
	// makes the log screen worth having: scrolling back to the connect message
	// after an hour needs the connect message still to be there.
	const logRingSize = 400

	var plainConsole *Console
	if o.mode != consoleFull {
		plainConsole = NewConsole(os.Stdout, o.theme, o.mode == consoleClassic, 6)
		plainConsole.PlanDevices(len(cfg.GPUs))
	}

	addLog := func(e LogEntry) {
		logMu.Lock()
		logRing = append(logRing, e)
		if len(logRing) > logRingSize {
			logRing = logRing[len(logRing)-logRingSize:]
		}
		logMu.Unlock()
		if o.mode == consolePlain && plainConsole != nil {
			plainConsole.PlainLog(e)
		}
	}
	logf := func(l LogLevel, tag, format string, args ...interface{}) {
		addLog(LogEntry{At: time.Now(), Level: l, Tag: tag, Text: fmt.Sprintf(format, args...)})
	}

	// Point the hash at the faster suffix sort before any thread starts, so no
	// two threads disagree about which sort they are using. Reported either
	// way: a 35% difference in hashrate should never be a silent one.
	saNote, saFast := InstallFastSuffixSort()

	// ---- the console
	var tui *TUI
	switch o.mode {
	case consoleFull:
		cols, rows := prepareWindow(o.theme, o.themeNote)
		tui = NewTUI(os.Stdout, o.theme, cols, rows)
	case consoleClassic:
		if !fitClassicPanel(plainConsole, o.theme, o.themeNote, cfg) {
			o.mode = consolePlain
			plainConsole = NewConsole(os.Stdout, o.theme, false, 6)
			plainConsole.PlanDevices(len(cfg.GPUs))
		}
	}
	if plainConsole != nil {
		plainConsole.Banner(version, cfg.Node, cfg.Wallet, o.testnet, o.themeNote)
	}

	engine := NewEngine(cfg.Node, cfg.Wallet)

	stopOnce := sync.Once{}
	done := make(chan struct{})
	quit := func() {
		stopOnce.Do(func() {
			engine.setState(StateStopping)
			engine.Stop()
			close(done)
		})
	}

	// The terminal has to be given back whatever happens next -- a normal
	// quit, an interrupt, a SIGTERM, or a panic on the way out of this
	// function. A deferred restore covers the first and the last; the signal
	// handler below covers the middle two, and Close is safe to call twice.
	if tui != nil {
		defer tui.Close()
		// Into the alternate buffer now, before the engine, the sensors and
		// the node watcher are started. That work takes about a second, and
		// entering afterwards meant a second of the user's own terminal --
		// their prompt, their scrollback, whatever the resize request had just
		// done to it -- sitting there looking like the program had hung or
		// broken something.
		tui.Enter()
	}

	// ---- input
	rawRestore := func() {}
	rawOK := false
	var editor *Editor
	if live {
		rawRestore, rawOK = EnableRawInput()
		defer rawRestore()
		editor = &Editor{}
	}

	cmdCtx := CommandContext{
		Engine: engine, Console: plainConsole, Config: cfg, Path: o.cfgPath,
		Quit: quit, Log: logf,
	}
	// The full-screen console has its own theme switch to make, because it
	// holds a second copy of the pointer for its own drawing.
	dispatch := func(line string) {
		if plainConsole == nil {
			runTUICommand(cmdCtx, tui, line)
			return
		}
		RunCommand(cmdCtx, line)
	}

	keys := make(chan keyEvent, 64)
	switch {
	case tui != nil && rawOK:
		go ReadKeys(keys, done)
	case plainConsole != nil:
		go ReadCommands(rawOK, editor, dispatch)
	}

	sigc := make(chan os.Signal, 1)
	// SIGTERM as well as interrupt: a miner is usually started by something
	// that stops it with SIGTERM -- systemd, docker, a task scheduler -- and
	// exiting on that without restoring the terminal is how a session is left
	// in the alternate buffer with no cursor.
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigc:
			if tui != nil {
				tui.Close()
			}
			rawRestore()
			logf(LogInfo, "quit", "shutting down")
			quit()
		case <-done:
		}
	}()

	if o.runFor > 0 {
		go func() {
			select {
			case <-time.After(time.Duration(o.runFor) * time.Second):
				logf(LogInfo, "quit", "--run-for elapsed")
				quit()
			case <-done:
			}
		}()
	}

	go func() {
		for {
			select {
			case e := <-engine.Events():
				addLog(e)
			case <-done:
				return
			}
		}
	}()

	// ---- start mining
	go engine.RunGetWork()
	if err := engine.SetThreads(cfg.Threads); err != nil {
		logf(LogError, "threads", "%v", err)
	}
	if saFast {
		logf(LogGood, "start", "%s", saNote)
	} else {
		logf(LogWarn, "start", "%s", saNote)
	}
	if len(cfg.GPUs) > 0 {
		engine.SetGPUs(cfg.GPUs, cfg.GPUBatch, cfg.GPUBlocks)
		logf(LogInfo, "start", "mining with %d threads and %d GPU(s)", cfg.Threads, len(cfg.GPUs))
	} else {
		logf(LogInfo, "start", "mining with %d threads", cfg.Threads)
	}
	if o.mode == consoleFull {
		logf(LogInfo, "start", "press H for help, : for the command line, Q to quit")
	} else {
		logf(LogInfo, "start", "commands: %s", commandHelp)
	}

	// Sensors start after the workers so the first poll sees the machine under
	// load rather than idle, and their failures are reported once here rather
	// than every two seconds for the life of the run.
	sensors := NewSensors(cfg.GPUs)
	defer sensors.Close()
	sensorNotes := sensors.Notes()
	for _, note := range sensorNotes {
		level := LogWarn
		if strings.HasPrefix(note, "CPU temperature from") {
			level = LogInfo
		}
		logf(level, "sensors", "%s", note)
	}

	sysmon := NewSysMonitor()
	defer sysmon.Close()

	nodes := NewNodeWatcher(cfg.Node, o.rpcAddress)
	defer nodes.Close()
	nodeNoteSaid := false

	if plainConsole != nil {
		plainConsole.HideCursor()
	}

	// ---- the sampler
	start := time.Now()
	st := newSampler(start, cfg.Threads)

	frame := 0
	var lastHeight int64 = -1
	heightAt := time.Time{}
	var lastRenderErr string

	// The machine-readable half of the console. nil unless --stats-file asked
	// for one, and every method tolerates that.
	stats := newStatsWriter(o.statsFile)
	defer stats.Remove()

	ticker := time.NewTicker(renderTick)
	defer ticker.Stop()

	build := func(now time.Time) Snapshot {
		job := engine.Job()

		if h := int64(job.Height); h != lastHeight {
			// The chain moved. This is the only honest measure of "time since
			// the last block" a miner has: getwork announces the new height and
			// says nothing about when the previous block was found.
			if lastHeight >= 0 {
				heightAt = now
			}
			lastHeight = h
		}

		logMu.Lock()
		logCopy := append([]LogEntry(nil), logRing...)
		logMu.Unlock()

		sensed := sensors.Sample()
		s := Snapshot{
			State:       engine.State(),
			Hashrate:    st.rate(),
			Threads:     engine.Threads(),
			GPUs:        engine.GPUs(),
			GPUTuning:   engine.GPUTuning(),
			CPURate:     st.cpu(),
			GPURate:     st.gpu(),
			Devices:     buildDeviceRows(engine, st.perGPU, st.cpu(), sensed),
			PeakRate:    st.peak,
			AvgRate:     st.sessionAvg(now),
			Avg1m:       meanTail(st.oneSec.vals, 60),
			Avg5m:       meanTail(st.oneSec.vals, 300),
			Avg15m:      meanTail(st.fiveSec.vals, 180),
			History:     st.oneSec.vals,
			Hist15m:     st.fiveSec.vals,
			Hist1h:      st.twentySec.vals,
			Hist6h:      st.ninetySec.vals,
			Hist24h:     st.sixMin.vals,
			CPUHist:     st.cpuSpectrum.vals,
			GPUHist:     st.gpuSpectrum.vals,
			ThreadRates: st.threadRates(engine.Threads()),
			TotalHashes: st.lastTotal,
			Sensors:     sensed,
			Sys:         sysmon.Sample(),
			Info:        nodes.Info(),
			Height:      int64(job.Height),
			Difficulty:  parseUint(job.Difficulty),
			// DERO difficulty is already in hashes per second, so the job's
			// difficulty is the network hashrate. See NodeInfo.NetHashrate.
			NetHashes:   job.Difficultyuint64,
			Blocks:      job.Blocks,
			MiniBlocks:  job.MiniBlocks,
			Rejected:    job.Rejected,
			Submitted:   engine.Submitted(),
			BestShare:   engine.BestShare(),
			LastShare:   engine.LastShare(),
			ConnectedAt: engine.ConnectedAt(),
			LastJob:     engine.LastJob(),
			BlockEvent:  engine.BlockEvent(),
			HeightAt:    heightAt,
			Uptime:      now.Sub(start),
			Node:        cfg.Node,
			Testnet:     o.testnet,
			Log:         logCopy,
			Frame:       frame,

			ConfigPath: o.cfgPath,
			LogFile:    o.logFile,
			GPUList:    cfg.GPUs,
			GPUBatch:   cfg.GPUBatch,
			GPUBlocks:  cfg.GPUBlocks,
			ThemeName:  o.theme.Name,
			SANote:     saNote,
			NodeNote:   nodes.Note(),
			SensorNote: sensorNotes,
		}
		if plainConsole != nil && editor != nil && rawOK {
			s.Input = editor.Buffer()
			s.ShowInput = true
		}
		return s
	}

	for {
		select {
		case <-done:
			if tui != nil {
				tui.Close()
			} else if plainConsole != nil {
				plainConsole.Finish()
			}
			rawRestore()
			summary(plainConsole, o.theme, engine, time.Since(start))
			return

		case k := <-keys:
			line, wantQuit := tui.Key(k)
			if wantQuit {
				logf(LogInfo, "quit", "shutting down")
				quit()
				continue
			}
			if line != "" {
				dispatch(line)
			}

		case now := <-ticker.C:
			frame++
			st.tick(now, engine)

			// The node watcher's verdict is worth exactly one log line, once,
			// whenever it settles -- and it settles a second or two after
			// start-up rather than before it.
			if !nodeNoteSaid {
				if note := nodes.Note(); note != "" {
					nodeNoteSaid = true
					level := LogInfo
					if strings.HasPrefix(note, "no derod") {
						level = LogWarn
					}
					logf(level, "node", "%s", note)
				}
			}

			s := build(now)
			stats.Write(s, version, false)
			switch {
			case tui != nil:
				if err := tui.Render(s); err != nil && err.Error() != lastRenderErr {
					// Report it once and carry on. The machine is still
					// mining, and a console that dies quietly is worse than
					// one that says what happened.
					lastRenderErr = err.Error()
					logf(LogError, "console", "%v", err)
				}
			case plainConsole != nil:
				plainConsole.Adapt()
				plainConsole.Draw(s)
			}
		}
	}
}

// runTUICommand is RunCommand with the console's own theme kept in step. The
// full-screen console holds its own pointer to the theme for drawing, so a
// "theme copper" that only updated the config would change the saved setting
// and nothing on screen.
func runTUICommand(ctx CommandContext, t *TUI, line string) {
	before := ctx.Config.Theme
	RunCommand(ctx, line)
	if t == nil || ctx.Config.Theme == before {
		return
	}
	if th, ok := ui.Themes[ctx.Config.Theme]; ok && th.Name != "mono" {
		t.SetTheme(th)
	}
}

// prepareWindow asks the terminal for a window the console fits in and returns
// the size it actually got.
//
// Only ever grows: a window somebody has deliberately made large is left
// alone. The request is an ANSI escape that a terminal is free to ignore,
// which is why the answer is measured rather than assumed -- and why the
// console works at whatever size comes back rather than requiring this to have
// succeeded.
func prepareWindow(theme *Theme, themeNote string) (int, int) {
	const wantCols, wantRows = 164, 48
	cols, rows := TerminalWidth(), TerminalHeight()
	if cols > 0 && rows > 0 && (cols < wantCols || rows < wantRows) {
		EnsureTerminalSize(maxInt(cols, wantCols), maxInt(rows, wantRows))
		cols, rows = WaitForTerminalSize(wantCols, wantRows, 750*time.Millisecond)
	}

	// Now check the answer against the terminal itself.
	//
	// A console can report a window it has not actually got: the resize is
	// recorded in its own model before -- or instead of -- the window taking
	// it, and nothing in the console API can tell the two apart. Drawing to a
	// width that is not there is what puts the right-hand panels off the edge
	// of the display. Parking the cursor past the corner and reading back
	// where it landed cannot be wrong that way, because the terminal is the
	// one that put it there. This runs before the key reader starts, since the
	// reply arrives on stdin.
	if _, _, ok := SyncConsoleToProbe(); ok {
		if c, r := TerminalWidth(), TerminalHeight(); c > 0 && r > 0 {
			cols, rows = c, r
		}
	}
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 40
	}
	return cols, rows
}

// fitClassicPanel sizes the window for the compact panel, reporting whether it
// fits at all. Lifted unchanged in behaviour from the original run loop.
func fitClassicPanel(c *Console, theme *Theme, themeNote string, cfg *Config) bool {
	const spare = 2
	wantRows := bannerRows(themeNote) + c.FrameHeight(len(cfg.GPUs)) + spare
	wantCols := c.Width() + spare

	EnsureTerminalSize(wantCols, wantRows)
	gotCols, gotRows := WaitForTerminalSize(wantCols, wantRows, 750*time.Millisecond)

	if c.Adapt() {
		return true
	}
	fmt.Fprintf(os.Stderr,
		"terminal is %d x %d and the compact panel needs %d x %d — using plain output for this run\n",
		gotCols, gotRows, wantCols, wantRows)
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---------------------------------------------------------------- sampling

// series is a fixed-length history of one value at one resolution.
//
// Samples arriving faster than the resolution are averaged into the next slot
// rather than dropped. That is the difference between a one-hour chart that
// shows the shape of the hour and one that shows whichever instant each
// twenty-second boundary happened to land on -- and with a GPU, whose batches
// land about once a second, the second of those has a periodic wobble in it
// that the machine does not.
type series struct {
	every time.Duration
	next  time.Time
	n     int
	vals  []float64
	acc   float64
	cnt   int
}

func newSeries(start time.Time, every time.Duration, n int) *series {
	return &series{every: every, next: start.Add(every), n: n, vals: make([]float64, 0, n)}
}

func (s *series) add(now time.Time, v float64) {
	s.acc += v
	s.cnt++
	if now.Before(s.next) {
		return
	}
	s.next = now.Add(s.every)
	mean := 0.0
	if s.cnt > 0 {
		mean = s.acc / float64(s.cnt)
	}
	s.acc, s.cnt = 0, 0
	s.vals = append(s.vals, mean)
	if len(s.vals) > s.n {
		// Copy down rather than reslice: reslicing walks the slice header
		// forward through an ever-growing backing array, so a miner left up for
		// a week would keep reallocating a day's worth of samples.
		copy(s.vals, s.vals[len(s.vals)-s.n:])
		s.vals = s.vals[:s.n]
	}
}

// sampler turns the engine's monotonic counters into rates and histories.
type sampler struct {
	lastAt    time.Time
	lastTotal uint64
	lastGPU   uint64
	start     time.Time

	// recent and recentGPU are the smoothed headline rates. Ten seconds,
	// because a GPU returns a whole batch of thousands of hashes at once,
	// roughly once a second: averaged over one second the displayed rate
	// alternates between "a batch landed" and "none did" and swings by more
	// than half either way. That is an artefact of the sampling, not of the
	// mining.
	recent    *rateWindow
	recentGPU *rateWindow
	// inst is the one-second view, which is what the histories are built from.
	// Its job is to show shape, and a ten-second average would hide the very
	// dips the headline is smoothing away.
	inst    *rateWindow
	instGPU *rateWindow

	perGPU      map[int]*rateWindow
	lastGPUEach map[int]uint64

	threadLast []uint64
	threadRate []float64

	oneSec      *series
	fiveSec     *series
	twentySec   *series
	ninetySec   *series
	sixMin      *series
	cpuSpectrum *series
	gpuSpectrum *series

	peak float64
}

func newSampler(start time.Time, threads int) *sampler {
	const hz = int(time.Second / renderTick)
	return &sampler{
		start: start, lastAt: start,
		recent:      newRateWindow(10 * hz),
		recentGPU:   newRateWindow(10 * hz),
		inst:        newRateWindow(hz),
		instGPU:     newRateWindow(hz),
		perGPU:      map[int]*rateWindow{},
		lastGPUEach: map[int]uint64{},
		threadLast:  make([]uint64, maxThreads),
		threadRate:  make([]float64, maxThreads),

		// 5 minutes at 1s, 15 at 5s, 1 hour at 20s, 6 hours at 90s, 24 at 6m.
		// Every one of them is a few hundred float64s; the whole set is under
		// 10 kB and is the entire memory cost of the history screens.
		oneSec:      newSeries(start, time.Second, 300),
		fiveSec:     newSeries(start, 5*time.Second, 180),
		twentySec:   newSeries(start, 20*time.Second, 180),
		ninetySec:   newSeries(start, 90*time.Second, 240),
		sixMin:      newSeries(start, 6*time.Minute, 240),
		cpuSpectrum: newSeries(start, time.Second, 120),
		gpuSpectrum: newSeries(start, time.Second, 120),
	}
}

func (s *sampler) tick(now time.Time, e *Engine) {
	total := e.TotalHashes()
	gpu := e.GPUHashes()
	dt := now.Sub(s.lastAt).Seconds()
	if dt <= 0 {
		return
	}

	instTotal := float64(total-s.lastTotal) / dt
	instGPU := float64(gpu-s.lastGPU) / dt
	instCPU := instTotal - instGPU
	if instCPU < 0 {
		instCPU = 0
	}

	s.recent.add(instTotal)
	s.recentGPU.add(instGPU)
	s.inst.add(instTotal)
	s.instGPU.add(instGPU)

	for _, d := range e.GPUDeviceList() {
		w := s.perGPU[d]
		if w == nil {
			w = newRateWindow(10 * int(time.Second/renderTick))
			s.perGPU[d] = w
			s.lastGPUEach[d] = e.GPUHashesFor(d)
		}
		n := e.GPUHashesFor(d)
		w.add(float64(n-s.lastGPUEach[d]) / dt)
		s.lastGPUEach[d] = n
	}

	// Per-thread rates as an exponential average. A ring per thread would be
	// sixty-four rings on a big machine for a number that only has to be
	// steady enough to compare with its neighbours.
	const alpha = 0.06
	for i := 0; i < maxThreads; i++ {
		n := e.ThreadHashes(i)
		if n == 0 && s.threadLast[i] == 0 {
			continue
		}
		r := float64(n-s.threadLast[i]) / dt
		s.threadLast[i] = n
		s.threadRate[i] += (r - s.threadRate[i]) * alpha
	}

	s.lastTotal, s.lastGPU, s.lastAt = total, gpu, now

	if r := s.rate(); r > s.peak {
		s.peak = r
	}

	one := s.inst.mean()
	s.oneSec.add(now, one)
	s.fiveSec.add(now, one)
	s.twentySec.add(now, one)
	s.ninetySec.add(now, one)
	s.sixMin.add(now, one)
	s.cpuSpectrum.add(now, s.inst.mean()-s.instGPU.mean())
	s.gpuSpectrum.add(now, s.instGPU.mean())
}

func (s *sampler) rate() float64 { return s.recent.mean() }
func (s *sampler) gpu() float64  { return s.recentGPU.mean() }

func (s *sampler) cpu() float64 {
	if v := s.rate() - s.gpu(); v > 0 {
		return v
	}
	return 0
}

func (s *sampler) sessionAvg(now time.Time) float64 {
	if up := now.Sub(s.start).Seconds(); up > 0 {
		return float64(s.lastTotal) / up
	}
	return 0
}

func (s *sampler) threadRates(n int) []float64 {
	if n < 0 {
		n = 0
	}
	if n > maxThreads {
		n = maxThreads
	}
	return s.threadRate[:n]
}

// meanTail is the mean of the last n values, or of all of them when there are
// fewer than n. Fewer rather than nothing: a one-minute average is useful
// forty seconds in, and refusing to show one until the window is full means
// the panel is blank for exactly the period someone is watching it most.
func meanTail(vals []float64, n int) float64 {
	if len(vals) == 0 {
		return 0
	}
	if n > len(vals) {
		n = len(vals)
	}
	sum := 0.0
	for _, v := range vals[len(vals)-n:] {
		sum += v
	}
	return sum / float64(n)
}

// summary prints the session totals after the console has been taken down.
func summary(c *Console, t *Theme, e *Engine, up time.Duration) {
	job := e.Job()
	total := e.TotalHashes()
	avg := 0.0
	if up.Seconds() > 0 {
		avg = float64(total) / up.Seconds()
	}
	out := os.Stdout
	fmt.Fprintf(out, "\n  %s\n", t.C(t.Accent+t.Bold, "session summary"))
	line := func(k, v string) {
		fmt.Fprintf(out, "    %s  %s\n", t.C(t.Muted, pad(k, 12)), t.C(t.Text, v))
	}
	line("uptime", ui.HMS(up))
	line("hashes", ui.Commas(total))
	line("average", ui.HumanRate(avg))
	line("shares sent", ui.Commas(e.Submitted()))
	line("miniblocks", ui.Commas(job.MiniBlocks))
	line("blocks", ui.Commas(job.Blocks))
	line("rejected", ui.Commas(job.Rejected))
	if e.BestShare() > 0 {
		line("best share", ui.Commas(e.BestShare()))
	}
	if p := astrobwtv3.RecoveredPanics; p != 0 {
		fmt.Fprintf(out, "    %s  %s\n", t.C(t.Err, pad("WARNING", 12)),
			t.C(t.Err, fmt.Sprintf("%d hash(es) aborted internally and returned a falsified result", p)))
	}
	fmt.Fprintln(out)
}
