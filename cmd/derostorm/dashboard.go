package main

// The live console. Everything is drawn as a fixed-height frame and redrawn in
// place, so the panel stays put and only the numbers move. Nothing here knows
// about mining; it renders a Snapshot.
//
// Width arithmetic is done on visible runes, never on the byte length of a
// string, because every value carries colour escapes. The lineBuf builder below
// is the only place that appends text, and it tracks visible width as it goes,
// so a padded line is always exactly the width it claims.

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ---------------------------------------------------------------- glyph set

const (
	boxTL, boxTR, boxBL, boxBR = "╭", "╮", "╰", "╯"
	boxH, boxV                 = "─", "│"
	boxTeeL, boxTeeR           = "├", "┤"
	bullet                     = "◆"
	logMark                    = "▸"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
var sparkLevels = []rune("▁▂▃▄▅▆▇█")

// Bar glyphs for the CPU/GPU split. Eighths let a bar carry a fraction of a
// column, so a small difference between two sources is still visible.
var barFull = '█'
var barParts = []rune("▏▎▍▌▋▊▉")
var barEmpty = '░'

// wordmark is drawn once at startup, above the live area.
var wordmark = []string{
	"╺┳┓┏━╸┏━┓┏━┓┏━┓╺┳╸┏━┓┏━┓┏┳┓",
	" ┃┃┣╸ ┣┳┛┃ ┃┗━┓ ┃ ┃ ┃┣┳┛┃┃┃",
	"╺┻┛┗━╸┛┗╸┗━┛┗━┛ ╹ ┗━┛┛┗╸╹ ╹",
}

// ---------------------------------------------------------------- line buffer

type lineBuf struct {
	sb strings.Builder
	w  int // visible width written so far
}

func (l *lineBuf) add(colour, s string) {
	if colour != "" {
		l.sb.WriteString(colour)
		l.sb.WriteString(s)
		l.sb.WriteString("\x1b[0m")
	} else {
		l.sb.WriteString(s)
	}
	l.w += utf8.RuneCountInString(s)
}

func (l *lineBuf) padTo(col int) {
	if col > l.w {
		l.sb.WriteString(strings.Repeat(" ", col-l.w))
		l.w = col
	}
}

func (l *lineBuf) String() string { return l.sb.String() }

// ---------------------------------------------------------------- state

// MinerState is the connection/mining state shown in the headline row.
type MinerState int

const (
	StateConnecting MinerState = iota
	StateMining
	StateReconnecting
	StateStopping
)

// LogLevel picks the colour of an event line.
type LogLevel int

const (
	LogInfo LogLevel = iota
	LogGood
	LogWarn
	LogError
)

type LogEntry struct {
	At    time.Time
	Level LogLevel
	Tag   string
	Text  string
}

// Snapshot is everything the console draws in one frame.
type Snapshot struct {
	State    MinerState
	Hashrate float64 // hashes/sec, rolling, CPU and GPU together
	Threads  int     // CPU mining threads
	GPUs     int     // GPU workers running

	// Split of Hashrate by source. Only drawn when GPUs > 0, where knowing
	// which half is which is the difference between "the GPU is helping" and
	// "the GPU died twenty minutes ago and I did not notice".
	CPURate float64
	GPURate float64

	History    []float64 // recent hashrate samples, oldest first
	Height     int64
	Difficulty uint64
	NetHashes  uint64
	Blocks     uint64
	MiniBlocks uint64
	Rejected   uint64
	Uptime     time.Duration
	Node       string
	Testnet    bool
	Log        []LogEntry

	// GPUTuning is set while a device is still measuring its block count, so the
	// panel can say the number is not settled rather than let someone read a
	// mid-sweep hashrate as the machine's real one.
	GPUTuning bool

	// Input is the command line the user is typing. It is drawn as the last row
	// of the frame so the terminal never has to echo it into the panel.
	Input     string
	ShowInput bool

	Frame int
}

// ---------------------------------------------------------------- console

// Console owns the terminal. Feed it snapshots; it handles redrawing.
type Console struct {
	mu       sync.Mutex
	out      io.Writer
	theme    *Theme
	width    int
	logLines int
	// maxLogLines is what logLines started as, so a window that grows can have
	// its event rows given back rather than staying at whatever a briefly small
	// window trimmed it to.
	maxLogLines int
	lastLines   int
	live        bool // false => plain scrolling output (mono / not a TTY)
	// colour records whether escape sequences may be emitted at all. It is
	// decided once, from the environment, and a runtime theme switch cannot
	// override it -- otherwise "theme copper" would start writing escapes into
	// a log file.
	colour bool
}

func NewConsole(out io.Writer, theme *Theme, live bool, logLines int) *Console {
	return &Console{
		out: out, theme: theme, width: panelWidth(TerminalWidth()),
		logLines: logLines, maxLogLines: logLines, live: live,
		colour: theme.Name != "mono",
	}
}

// minPanelWidth is the narrowest the two-column layout still works at. Below
// this the stat values have nowhere to go and the box loses its shape.
const minPanelWidth = 60

// maxPanelWidth stops the panel sprawling across an ultrawide window, where a
// hashrate a metre from its label is harder to read, not easier.
const maxPanelWidth = 96

// panelWidth turns a terminal width into a panel width: two columns of margin,
// clamped at both ends.
//
// The two ends are not the same case, which the previous version of this
// conflated. A terminal width of zero means it could not be read -- output
// redirected, or not a console -- and a default is the only option. A terminal
// that genuinely is 40 columns wide is a different thing, and answering it with
// a 76-column panel is the worst of the options: every line wraps, one row
// becomes two, and the redraw then moves the cursor up by half the rows it
// actually wrote. Better to return the narrowest working layout and let Adapt
// decide the panel cannot be shown at all.
func panelWidth(termCols int) int {
	if termCols <= 0 {
		return 76
	}
	w := termCols - 2
	if w > maxPanelWidth {
		return maxPanelWidth
	}
	if w < minPanelWidth {
		return minPanelWidth
	}
	return w
}

// Adapt re-reads the terminal size and fits the panel to it, growing the event
// log back if there is room again. Reports whether the panel fits at all.
//
// Called every render tick, because a window can be resized at any moment and
// the consequences are not cosmetic: the panel is redrawn by moving the cursor
// up over its own height, so a panel taller than its window loses its top row
// to the scrollback on every frame and walks down the screen. It is also how the
// start-up resize is picked up -- terminals honour that asynchronously, so the
// size right after asking is not the size a moment later.
func (c *Console) Adapt() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fitTo(TerminalWidth(), TerminalHeight())
}

// fitTo is Adapt against a given size rather than the real terminal, which is
// what makes the behaviour testable: there is no terminal under `go test`, and
// the interesting cases are windows this machine may not have.
//
// Caller holds the lock.
func (c *Console) fitTo(cols, rows int) bool {
	if cols <= 0 || rows <= 0 {
		return true // size unknown; leave the layout alone
	}
	c.width = panelWidth(cols)

	// Grow first, then trim: a window that got taller should get its log rows
	// back, and starting from the maximum is what lets that happen. A trim-only
	// version leaves a two-line log forever after one brief small window.
	c.logLines = c.maxLogLines
	for c.logLines > minLogLines && c.frameHeight() > rows {
		c.logLines--
	}

	// Too narrow counts as not fitting, and there is nothing to trim for it. A
	// panel wider than its window wraps, and a wrapped row is two rows as far as
	// the terminal is concerned, so Draw's cursor arithmetic would then be wrong
	// by the number of rows that wrapped.
	if c.width+1 > cols {
		return false
	}
	return c.frameHeight() <= rows
}

// SetTheme switches theme. It reports false when colour is not available in
// this environment, in which case the request is ignored and mono is kept.
func (c *Console) SetTheme(t *Theme) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.colour && t.Name != "mono" {
		return false
	}
	c.theme = t
	return true
}

func (c *Console) Theme() *Theme {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.theme
}

// bannerRows is how many lines Banner prints: one blank, the three-row
// wordmark, the subtitle, a blank, four key/value rows, and a closing blank.
// A theme note adds one more.
func bannerRows(themeNote string) int {
	n := 1 + len(wordmark) + 1 + 1 + 4 + 1
	if themeNote != "" {
		n++
	}
	return n
}

// minLogLines is the smallest event log worth drawing. Below two, a connect
// followed by a job message would already have scrolled the interesting one
// away, and the panel stops earning the rows it is taking.
const minLogLines = 2

// FrameHeight is how many rows the live panel occupies at its tallest, which is
// with a GPU running: the split rows only exist then, and this is used to size
// the terminal window before anything is drawn. Measured by building a frame
// rather than counted by hand, so it cannot drift when a row is added.
func (c *Console) FrameHeight() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.frameHeight()
}

func (c *Console) frameHeight() int {
	return len(c.frame(Snapshot{ShowInput: true, GPUs: 1}))
}

// LogLines is the size of the event log area, after any trimming.
func (c *Console) LogLines() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.logLines
}

// Width is the panel width in columns.
func (c *Console) Width() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.width
}

// Banner prints the wordmark and the run header. Called once, before the live
// area exists.
func (c *Console) Banner(version, node, wallet string, testnet bool, themeNote string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.theme

	fmt.Fprintln(c.out)
	for i, row := range wordmark {
		colour := t.Accent
		if i == 2 {
			colour = t.Accent2
		}
		fmt.Fprintf(c.out, "  %s\n", t.c(colour, row))
	}
	fmt.Fprintf(c.out, "  %s\n\n",
		t.c(t.Muted, "AstroBWTv3 miner for DERO  ·  "+version))

	kv := func(k, v string) {
		fmt.Fprintf(c.out, "  %s  %s\n", t.c(t.Muted, pad(k, 10)), t.c(t.Text, v))
	}
	net := "mainnet"
	if testnet {
		net = "testnet"
	}
	kv("node", node)
	kv("network", net)
	kv("wallet", wallet)
	kv("theme", t.Name)
	// The worker count is deliberately not here. This header is printed once,
	// above the live panel, and never redrawn, so a count here would still say
	// 8 after "threads 12". The panel carries it instead, where it updates.
	if themeNote != "" {
		fmt.Fprintf(c.out, "  %s  %s\n", pad("", 10), t.c(t.Dim, themeNote))
	}
	fmt.Fprintln(c.out)
}

// Draw renders one frame, redrawing in place when live.
func (c *Console) Draw(s Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()

	lines := c.frame(s)

	if !c.live {
		return // plain mode prints events as they happen instead
	}

	var b strings.Builder
	if c.lastLines > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", c.lastLines)
	}
	for _, ln := range lines {
		b.WriteString("\r\x1b[2K")
		b.WriteString(ln)
		b.WriteString("\n")
	}
	// The frame is allowed to change height -- the CPU/GPU split rows appear
	// when a GPU starts -- so anything the last frame left below this one has to
	// be wiped, and the cursor left where the shorter frame ends. Without this a
	// panel that shrinks leaves a copy of its old bottom rows on the screen.
	for i := len(lines); i < c.lastLines; i++ {
		b.WriteString("\r\x1b[2K\n")
	}
	if len(lines) < c.lastLines {
		fmt.Fprintf(&b, "\x1b[%dA", c.lastLines-len(lines))
	}
	c.lastLines = len(lines)
	io.WriteString(c.out, b.String())
}

// PlainLog writes a single event line, used when the live panel is off.
func (c *Console) PlainLog(e LogEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.theme
	fmt.Fprintf(c.out, "%s  %s  %s\n",
		e.At.Format("15:04:05"), pad(e.Tag, 10), t.c(c.levelColour(e.Level), e.Text))
}

func (c *Console) levelColour(l LogLevel) string {
	switch l {
	case LogGood:
		return c.theme.Good
	case LogWarn:
		return c.theme.Warn
	case LogError:
		return c.theme.Err
	default:
		return c.theme.Text
	}
}

// Finish leaves the cursor below the panel so the shell prompt lands cleanly.
func (c *Console) Finish() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.live {
		fmt.Fprint(c.out, "\x1b[?25h") // show cursor
	}
	fmt.Fprintln(c.out)
}

func (c *Console) HideCursor() {
	if c.live {
		fmt.Fprint(c.out, "\x1b[?25l")
	}
}

// ---------------------------------------------------------------- framing

func (c *Console) frame(s Snapshot) []string {
	t := c.theme
	W := c.width
	inner := W - 2 // between the two verticals

	out := make([]string, 0, 12+c.logLines)

	// ---- top rule with title on the left and build info on the right
	right := "AstroBWTv3 · v" + version
	if s.Testnet {
		right = "AstroBWTv3 · testnet"
	}
	out = append(out, c.ruleWithTitles("DEROSTORM", right))

	out = append(out, c.blank())

	// ---- headline: state, hashrate, threads
	{
		var l lineBuf
		l.add(t.Border, boxV)
		l.padTo(3)

		mark, markCol, label := bullet, t.Good, "MINING"
		switch s.State {
		case StateConnecting:
			mark, markCol, label = spinnerFrames[s.Frame%len(spinnerFrames)], t.Accent2, "CONNECTING"
		case StateReconnecting:
			mark, markCol, label = spinnerFrames[s.Frame%len(spinnerFrames)], t.Warn, "RECONNECTING"
		case StateStopping:
			mark, markCol, label = bullet, t.Dim, "STOPPING"
		}
		if s.State == StateMining && s.GPUTuning {
			// Mining, but the GPU is still choosing its block count, so the
			// headline number is going to move. Say so rather than let it be
			// read as the settled rate.
			mark, markCol, label = spinnerFrames[s.Frame%len(spinnerFrames)], t.Accent2, "TUNING GPU"
		}
		l.add(markCol, mark)
		l.add("", " ")
		l.add(markCol, label)

		rate := humanRate(s.Hashrate)
		l.padTo(3 + inner/2 - utf8.RuneCountInString(rate)/2 - 2)
		l.add(t.Accent+t.Bold, rate)

		thr := workerLabel(s)
		l.padTo(W - 2 - utf8.RuneCountInString(thr))
		l.add(t.Muted, thr)

		l.padTo(W - 1)
		l.add(t.Border, boxV)
		out = append(out, l.String())
	}

	// ---- sparkline
	{
		var l lineBuf
		l.add(t.Border, boxV)
		l.padTo(3)

		right := "60s"
		rw := utf8.RuneCountInString(right)

		sparkW := inner - 6 - rw
		if sparkW < 8 {
			sparkW = 8
		}
		addSparkline(&l, t, s.History, sparkW)
		l.padTo(W - 2 - rw)
		l.add(t.Dim, right)
		l.padTo(W - 1)
		l.add(t.Border, boxV)
		out = append(out, l.String())
	}

	// ---- where the hashrate is coming from, one bar per source.
	//
	// Only drawn when a GPU is running: with the CPU alone the bar would always
	// be full and the row would say nothing. Draw handles the frame changing
	// height when a GPU starts or stops.
	//
	// This is the row that answers "is the GPU still working?", which a single
	// combined number cannot. A GPU that stops contributing shows up here
	// immediately instead of as a headline figure that is quietly a third lower
	// than it was an hour ago.
	if s.GPUs > 0 {
		out = append(out, c.blank())

		peak := s.CPURate
		if s.GPURate > peak {
			peak = s.GPURate
		}
		total := s.CPURate + s.GPURate

		barW := inner - 27
		if barW < 8 {
			barW = 8
		}

		row := func(label string, rate float64, colour string) string {
			var l lineBuf
			l.add(t.Border, boxV)
			l.padTo(5)
			l.add(t.Muted, label)
			l.padTo(9)

			frac := 0.0
			if peak > 0 {
				frac = rate / peak
			}
			l.add(colour, bar(frac, barW))

			share := "--"
			if total > 0 {
				share = fmt.Sprintf("%.0f%%", rate/total*100)
			}
			l.padTo(W - 2 - 5 - 11)
			l.add(t.Text, lpad(humanRate(rate), 11))
			l.padTo(W - 2 - 5)
			l.add(t.Dim, lpad(share, 5))
			l.padTo(W - 1)
			l.add(t.Border, boxV)
			return l.String()
		}

		out = append(out, row("CPU", s.CPURate, t.Accent))
		gpuColour := t.Accent2
		if s.GPURate <= 0 {
			gpuColour = t.Err // running but contributing nothing
		}
		out = append(out, row("GPU", s.GPURate, gpuColour))
	}

	out = append(out, c.blank())
	out = append(out, c.divider())

	// ---- two-column stats
	//
	// SHARE is this miner's slice of the reported network hashrate, and next to
	// it the mean interval between shares at the current rate and difficulty.
	// Those two together are what says whether the machine is earning: a
	// hashrate on its own does not, because it means something different at
	// every difficulty.
	stats := [][4]string{
		{"HEIGHT", commas(uint64(maxi64(s.Height, 0))), "NETWORK", humanRate(float64(s.NetHashes))},
		{"BLOCKS", commas(s.Blocks), "DIFFICULTY", commas(s.Difficulty)},
		{"MINIBLOCKS", commas(s.MiniBlocks), "SHARE", shareText(s)},
		{"UPTIME", hms(s.Uptime), "REJECTED", commas(s.Rejected)},
	}
	colB := 3 + inner/2
	for _, row := range stats {
		var l lineBuf
		l.add(t.Border, boxV)
		l.padTo(3)
		l.add(t.Muted, row[0])
		l.padTo(3 + 12)
		l.add(t.Text, clip(row[1], colB-3-12-2))
		l.padTo(colB)
		l.add(t.Muted, row[2])
		l.padTo(colB + 12)

		val, colour := row[3], t.Text
		if row[2] == "REJECTED" && s.Rejected > 0 {
			colour = t.Warn
		}
		l.add(colour, clip(val, W-2-(colB+12))) // -2 keeps a space before the border
		l.padTo(W - 1)
		l.add(t.Border, boxV)
		out = append(out, l.String())
	}

	// The node gets a whole row: a hostname with a port is often wider than
	// half the panel, and a truncated one is no use for telling two nodes apart.
	{
		var l lineBuf
		l.add(t.Border, boxV)
		l.padTo(3)
		l.add(t.Muted, "NODE")
		l.padTo(3 + 12)
		l.add(t.Text, clip(s.Node, W-2-(3+12)))
		l.padTo(W - 1)
		l.add(t.Border, boxV)
		out = append(out, l.String())
	}

	out = append(out, c.bottom())

	// ---- event log, below the panel, fixed height so the frame never moves
	log := s.Log
	if len(log) > c.logLines {
		log = log[len(log)-c.logLines:]
	}
	for i := 0; i < c.logLines; i++ {
		if i >= len(log) {
			out = append(out, "")
			continue
		}
		e := log[i]
		var l lineBuf
		l.padTo(2)
		l.add(t.Dim, logMark)
		l.add("", " ")
		l.add(t.Dim, e.At.Format("15:04:05"))
		l.add("", "  ")
		l.add(c.levelColour(e.Level), pad(e.Tag, 10))
		l.add("", " ")
		l.add(t.Text, clip(e.Text, W-l.w))
		out = append(out, l.String())
	}

	// ---- command line, drawn by us because the terminal echo is switched off
	if s.ShowInput {
		var l lineBuf
		l.padTo(2)
		l.add(t.Accent, "›")
		l.add("", " ")
		if s.Input == "" {
			l.add(t.Dim, commandHelp)
		} else {
			l.add(t.Text, clip(s.Input, W-6))
			l.add(t.Accent2, "▏")
		}
		out = append(out, "")
		out = append(out, l.String())
	}

	return out
}

func (c *Console) ruleWithTitles(left, right string) string {
	t := c.theme
	W := c.width
	var l lineBuf
	l.add(t.Border, boxTL+boxH+" ")
	l.add(t.Accent+t.Bold, left)
	l.add("", " ")
	// Leave room for: fill, space, right title, space, one closing dash, corner.
	fillTo := W - 2 - utf8.RuneCountInString(right) - 2
	if fillTo > l.w {
		l.add(t.Border, strings.Repeat(boxH, fillTo-l.w))
	}
	l.add("", " ")
	l.add(t.Muted, right)
	l.add("", " ")
	if W-1 > l.w {
		l.add(t.Border, strings.Repeat(boxH, W-1-l.w))
	}
	l.add(t.Border, boxTR)
	return l.String()
}

func (c *Console) divider() string {
	t := c.theme
	return t.c(t.Border, boxTeeL+strings.Repeat(boxH, c.width-2)+boxTeeR)
}

func (c *Console) bottom() string {
	t := c.theme
	return t.c(t.Border, boxBL+strings.Repeat(boxH, c.width-2)+boxBR)
}

func (c *Console) blank() string {
	t := c.theme
	var l lineBuf
	l.add(t.Border, boxV)
	l.padTo(c.width - 1)
	l.add(t.Border, boxV)
	return l.String()
}

// ---------------------------------------------------------------- formatting

// shareText is this miner's slice of the network, and the mean gap between
// shares at the current rate and difficulty.
//
// The interval is difficulty/hashrate because a share needs, on average,
// `difficulty` hashes. It is a mean over a memoryless process, so a run of
// three times that long is unremarkable; the number is for comparing settings,
// not for predicting the next minute.
func shareText(s Snapshot) string {
	if s.Hashrate <= 0 {
		return "--"
	}
	part := ""
	if s.NetHashes > 0 {
		part = fmt.Sprintf("%.2f%%", s.Hashrate/float64(s.NetHashes)*100)
	}
	if s.Difficulty > 0 {
		eta := time.Duration(float64(s.Difficulty) / s.Hashrate * float64(time.Second))
		if part != "" {
			return part + " · ~" + shortDur(eta)
		}
		return "~" + shortDur(eta)
	}
	if part == "" {
		return "--"
	}
	return part
}

// shortDur is a duration at one significant unit: long enough to compare, short
// enough for a table cell.
func shortDur(d time.Duration) string {
	switch {
	case d <= 0:
		return "--"
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%.1fd", d.Hours()/24)
	}
}

// bar renders frac of width as a horizontal bar, to the eighth of a column.
func bar(frac float64, width int) string {
	if width < 1 {
		return ""
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	eighths := int(frac*float64(width)*8 + 0.5)
	full := eighths / 8
	rest := eighths % 8

	var sb strings.Builder
	for i := 0; i < full && i < width; i++ {
		sb.WriteRune(barFull)
	}
	n := full
	if rest > 0 && n < width {
		sb.WriteRune(barParts[rest-1])
		n++
	}
	for ; n < width; n++ {
		sb.WriteRune(barEmpty)
	}
	return sb.String()
}

// addSparkline writes the sparkline into l, shaded by height rather than in one
// flat colour. Three bands is enough to make a dip legible at a glance without
// turning the row into a rainbow, and same-coloured runs are emitted as one
// span so a 60-column sparkline does not cost 60 escape sequences.
func addSparkline(l *lineBuf, t *Theme, vals []float64, width int) {
	cells, levels := sparkCells(vals, width)
	band := func(lv int) string {
		switch {
		case lv < 0:
			return "" // padding
		case lv < 3:
			return t.Dim
		case lv < 6:
			return t.Accent2
		default:
			return t.Accent
		}
	}
	run, colour := strings.Builder{}, band(levels[0])
	for i, r := range cells {
		if cb := band(levels[i]); cb != colour {
			l.add(colour, run.String())
			run.Reset()
			colour = cb
		}
		run.WriteRune(r)
	}
	l.add(colour, run.String())
}

// sparkCells is sparkline's glyphs plus the level of each, so a caller can
// colour them. A padding cell has level -1.
func sparkCells(vals []float64, width int) ([]rune, []int) {
	cells := make([]rune, 0, width)
	levels := make([]int, 0, width)
	if width < 1 {
		return []rune{' '}, []int{-1}
	}
	if len(vals) > width {
		vals = vals[len(vals)-width:]
	}
	for i := 0; i < width-len(vals); i++ {
		cells = append(cells, ' ')
		levels = append(levels, -1)
	}
	max := 0.0
	for _, v := range vals {
		if v > max {
			max = v
		}
	}
	for _, v := range vals {
		idx := 0
		if max > 0 {
			idx = int(v / max * float64(len(sparkLevels)-1))
		}
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparkLevels) {
			idx = len(sparkLevels) - 1
		}
		cells = append(cells, sparkLevels[idx])
		levels = append(levels, idx)
	}
	if len(cells) == 0 {
		return []rune{' '}, []int{-1}
	}
	return cells, levels
}

// workerLabel says what is actually mining, and updates as the mix changes.
// This is the only place the worker count is drawn, so "threads 12" is visible
// immediately rather than being contradicted by a header printed at start-up.
func workerLabel(s Snapshot) string {
	switch {
	case s.Threads > 0 && s.GPUs == 1:
		return fmt.Sprintf("%d CPU · 1 GPU", s.Threads)
	case s.Threads > 0 && s.GPUs > 1:
		return fmt.Sprintf("%d CPU · %d GPU", s.Threads, s.GPUs)
	case s.Threads == 0 && s.GPUs == 1:
		return "1 GPU"
	case s.Threads == 0 && s.GPUs > 1:
		return fmt.Sprintf("%d GPU", s.GPUs)
	case s.Threads == 1:
		return "1 CPU thread"
	}
	return fmt.Sprintf("%d CPU threads", s.Threads)
}

func humanRate(h float64) string {
	switch {
	case h >= 1e12:
		return fmt.Sprintf("%.2f TH/s", h/1e12)
	case h >= 1e9:
		return fmt.Sprintf("%.2f GH/s", h/1e9)
	case h >= 1e6:
		return fmt.Sprintf("%.2f MH/s", h/1e6)
	case h >= 1e3:
		return fmt.Sprintf("%.2f KH/s", h/1e3)
	case h > 0:
		return fmt.Sprintf("%.0f H/s", h)
	}
	return "--"
}

func commas(v uint64) string {
	s := fmt.Sprintf("%d", v)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, d := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, d)
	}
	return string(out)
}

func hms(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	t := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", t/3600, (t%3600)/60, t%60)
}

func pad(s string, w int) string {
	n := utf8.RuneCountInString(s)
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
}

// lpad right-aligns s in w columns, for numbers in a table.
func lpad(s string, w int) string {
	n := utf8.RuneCountInString(s)
	if n >= w {
		return s
	}
	return strings.Repeat(" ", w-n) + s
}

func clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= w {
		return s
	}
	r := []rune(s)
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

func maxi64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
