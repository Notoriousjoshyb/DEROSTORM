package main

// DeroStorm -- an AstroBWTv3 miner for DERO.
//
// Startup order matters and is not obvious, so it is spelled out here:
//
//  1. parse flags
//  2. load the config file, if there is one
//  3. decide the network from (flag OR config), because globals.Initialize
//     needs it and address validation depends on it
//  4. run setup if anything essential is still missing
//  5. build the console, then the engine, then start mining
//
// Nothing writes to stdout between step 5 and shutdown except the console: the
// library logger is pointed at the log file so it cannot scribble over the
// live panel.

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/deroproject/derohe/astrobwt/astrobwtv3"
	"github.com/deroproject/derohe/globals"
	"github.com/docopt/docopt-go"
)

const version = "1.5.5"

const usage = `DeroStorm ` + version + `
AstroBWTv3 miner for DERO. Mines on the CPU, and on NVIDIA GPUs as well when
one is present. GPU support is CUDA, so AMD and Intel cards are not supported.

On first run DeroStorm asks a few questions and saves the answers next to the
executable, so afterwards you can just start it with no arguments.

Usage:
  derostorm [options]
  derostorm --setup [options]
  derostorm --bench [options]
  derostorm --preview [options]
  derostorm -h | --help
  derostorm --version

  The three lines above take the same options as the first. Naming one of them
  explicitly, as "--bench [--mining-threads=<n>]" once did, makes docopt treat
  that option as belonging only to that line, so "--mining-threads" outside
  "--bench" was rejected with this usage message rather than honoured.

Options:
  -h --help                       Show this screen.
  --version                       Show version.
  --setup                         Re-run the guided setup and exit to mining.
  --config=<path>                 Use this config file instead of the default.
  --wallet-address=<addr>         Mining rewards go to this address.
  --daemon-rpc-address=<host:port>  derod getwork address.
  --mining-threads=<n>            Number of mining threads.
  --gpu=<list>                    Also mine on these NVIDIA devices: 0, 0,1,
                                  all or off. Overrides the saved setting.
  --gpu-batch=<n>                 Nonces per GPU launch. Default: fill VRAM.
  --gpu-blocks=<n>                Resident blocks in the GPU suffix kernel.
                                  Default: four per SM (336 on a 5080).
  --theme=<name>                  cyber, default, copper, aurora, ember or
                                  mono.
  --tui                           Force the full-screen console. It is already
                                  the default on an interactive terminal.
  --no-tui                        Plain scrolling output, no console at all.
  --classic                       The compact in-place panel instead of the
                                  full-screen console.
  --no-dashboard                  Same as --no-tui.
  --rpc-address=<host:port>       derod JSON-RPC, for peer count, network
                                  hashrate and block interval. Default: the
                                  getwork host with the port two higher, which
                                  is where derod puts it.
  --testnet                       Use the DERO testnet.
  --debug                         Verbose logging to the log file.
  --bench                         Run the built-in benchmark and exit.
  --cpuprofile=<path>             Write a CPU profile. Works with --bench and
                                  with --run-for; this is how default.pgo is
                                  regenerated after a change to the hot path.
  --run-for=<sec>                 Mine for this many seconds, then print the
                                  session summary and exit. For measuring the
                                  real mining path rather than the benchmark.
  --preview                       Draw one console frame with sample data and
                                  exit. Use it to compare themes and to check
                                  a layout at a size you do not have.
  --size=<WxH>                    Size for --preview. Default: this terminal.
  --screen=<name>                 Screen for --preview: dashboard, mining,
                                  stats, network, threads, config, logs, pools
                                  or help.
  --preview-classic               Preview the compact panel in every theme.
  --termdiag                      Report what every source says the terminal
                                  size is, and rule a line that wide. For
                                  chasing a frame drawn wider than its window.

Console keys:
  M S N T C L P H  mining, statistics, network, threads, config, logs, pools,
                   help.  Esc or D returns to the dashboard, Tab cycles.
  arrows/PgUp/PgDn scroll the event log; End returns it to live
  :                open the command line
  Q or Ctrl-C      stop mining and exit

Runtime commands (press : first, then type and press Enter):
  threads <n>   change the thread count live, also accepts +2 or -4
  theme <name>  switch colour theme
  save          write current settings to the config file
  config        show the active settings
  quit          stop mining and exit
`

func main() {
	opts, err := docopt.Parse(usage, nil, true, "DeroStorm "+version, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot parse arguments: %v\n", err)
		os.Exit(2)
	}
	globals.Arguments = opts

	// CUDA numbers devices fastest-first by default, while NVML -- where the
	// temperature and power readings come from -- numbers them by PCI bus. On a
	// rig with unlike cards the two orderings differ, and "GPU 1" in the panel
	// would then be one card's hashrate beside another card's temperature.
	// Pinning CUDA to bus order makes the two the same numbering. Nothing here
	// wants fastest-first: every device that is going to be used is named
	// explicitly, so the order they are listed in decides nothing.
	if _, set := os.LookupEnv("CUDA_DEVICE_ORDER"); !set {
		os.Setenv("CUDA_DEVICE_ORDER", "PCI_BUS_ID")
	}

	// docopt only defines the keys present in the usage string; globals reads a
	// few others and expects them to be absent rather than nil-typed.
	if _, ok := globals.Arguments["--testnet"]; !ok {
		globals.Arguments["--testnet"] = false
	}

	// Profiling wraps everything below, so --bench and --run-for are both
	// covered by the same flag. Started before the console so the setup and
	// start-up cost is visible too; it is a rounding error next to mining, but
	// a profile that quietly excludes part of the program is worse than none.
	if v := optString(opts, "--cpuprofile"); v != "" {
		stop, err := startCPUProfile(v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "--cpuprofile: %v\n", err)
			os.Exit(1)
		}
		defer stop()
	}

	// ---- console basics, needed even for setup and errors
	vtOK := EnableVirtualTerminal()
	isTTY := StdoutIsTTY() && vtOK

	// The previews need no config, no node and no wallet: they exist so the
	// themes and the layout can be looked at before committing to either.
	if optBool(opts, "--preview") {
		runTUIPreview(optString(opts, "--theme"), optString(opts, "--size"),
			optString(opts, "--screen"), isTTY)
		return
	}
	if optBool(opts, "--preview-classic") {
		runPreview(optString(opts, "--theme"), isTTY)
		return
	}
	if optBool(opts, "--termdiag") {
		runTermDiag()
		return
	}

	// ---- config
	cfgPath := ConfigPath(optString(opts, "--config"))
	cfg, loadErr := LoadConfig(cfgPath)
	if loadErr != nil && !os.IsNotExist(loadErr) {
		fmt.Fprintf(os.Stderr, "%v\n", loadErr)
		os.Exit(1)
	}

	// Flags override the file, and the network has to be settled before
	// globals.Initialize runs.
	flagTestnet := optBool(opts, "--testnet")
	testnet := flagTestnet
	if cfg != nil && !flagTestnet {
		testnet = cfg.Testnet
	}
	globals.Arguments["--testnet"] = testnet

	themeName := optString(opts, "--theme")
	if themeName == "" && cfg != nil {
		themeName = cfg.Theme
	}
	theme, themeNote := PickTheme(themeName, isTTY)

	// --bench measures the hash function and nothing else: it needs no wallet,
	// no node and no config, so it must not trip the setup gate below.
	if optBool(opts, "--bench") {
		threads := DefaultThreads()
		if cfg != nil && cfg.Threads > 0 {
			threads = cfg.Threads
		}
		if v := optString(opts, "--mining-threads"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > maxThreads {
				fmt.Fprintf(os.Stderr, "--mining-threads must be between 1 and %d\n", maxThreads)
				os.Exit(2)
			}
			threads = n
		}
		devs := []int(nil)
		if cfg != nil {
			devs = cfg.GPUs
		}
		if v := optString(opts, "--gpu"); v != "" {
			d, err := parseGPUList(v)
			if err != nil {
				fmt.Fprintf(os.Stderr, "--gpu: %v\n", err)
				os.Exit(2)
			}
			devs = d
		}
		gpuBatch := 0
		if cfg != nil {
			gpuBatch = cfg.GPUBatch
		}
		if v := optString(opts, "--gpu-batch"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				gpuBatch = n
			}
		}
		// The benchmark should measure what mining will do, so the same suffix
		// sort is installed here as there.
		saNote, saOK := InstallFastSuffixSort()
		runBench(theme, threads, devs, gpuBatch, saNote, saOK)
		return
	}

	needSetup := optBool(opts, "--setup") || !cfg.Complete()
	// Explicit flags can supply what the config lacks, in which case no setup.
	if !optBool(opts, "--setup") && !cfg.Complete() {
		if optString(opts, "--wallet-address") != "" {
			needSetup = false
		}
	}

	if needSetup {
		newCfg, err := RunSetup(theme, themeName, cfgPath, cfg, flagTestnet)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nsetup cancelled: %v\n", err)
			os.Exit(1)
		}
		cfg = newCfg
		testnet = cfg.Testnet
		if optString(opts, "--theme") == "" {
			theme, themeNote = PickTheme(cfg.Theme, isTTY)
		}
	} else {
		globals.Initialize()
	}

	if cfg == nil {
		cfg = &Config{Threads: DefaultThreads(), Theme: theme.Name, Testnet: testnet}
	}

	// ---- flags win over the file for this run
	if v := optString(opts, "--wallet-address"); v != "" {
		cfg.Wallet = v
	}
	if v := optString(opts, "--daemon-rpc-address"); v != "" {
		cfg.Node = v
	}
	if v := optString(opts, "--mining-threads"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxThreads {
			fmt.Fprintf(os.Stderr, "--mining-threads must be between 1 and %d\n", maxThreads)
			os.Exit(2)
		}
		cfg.Threads = n
	}
	if v := optString(opts, "--gpu"); v != "" {
		devs, err := parseGPUList(v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "--gpu: %v\n", err)
			os.Exit(2)
		}
		cfg.GPUs = devs
	}
	if v := optString(opts, "--gpu-batch"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			fmt.Fprintln(os.Stderr, "--gpu-batch must be a non-negative number")
			os.Exit(2)
		}
		cfg.GPUBatch = n
	}
	if v := optString(opts, "--gpu-blocks"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			fmt.Fprintln(os.Stderr, "--gpu-blocks must be a non-negative number")
			os.Exit(2)
		}
		cfg.GPUBlocks = n
	}
	if len(cfg.GPUs) > 0 && !GPUAvailable {
		fmt.Fprintln(os.Stderr, "GPU mining is not built for this platform — drop --gpu")
		os.Exit(2)
	}
	if cfg.Node == "" {
		cfg.Node = DefaultNode(testnet)
	}
	if cfg.Threads < 1 {
		cfg.Threads = DefaultThreads()
	}

	// ---- validate the wallet against the live network
	addr, err := globals.ParseValidateAddress(cfg.Wallet)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wallet address is not valid for %s: %v\n", netName(testnet), err)
		fmt.Fprintf(os.Stderr, "run \"derostorm --setup\" to fix it\n")
		os.Exit(1)
	}
	cfg.Wallet = addr.String()

	// ---- logging goes to a file, never to the live panel
	logFile := openLogFile()
	defer logFile.Close()
	globals.InitializeLog(io.Discard, logFile)

	runFor := 0
	if v := optString(opts, "--run-for"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			fmt.Fprintln(os.Stderr, "--run-for must be a positive number of seconds")
			os.Exit(2)
		}
		runFor = n
	}

	mode := consoleFull
	switch {
	case optBool(opts, "--no-tui") || optBool(opts, "--no-dashboard"):
		mode = consolePlain
	case optBool(opts, "--classic"):
		mode = consoleClassic
	}

	run(runOpts{
		cfg: cfg, cfgPath: cfgPath, theme: theme, themeNote: themeNote,
		isTTY: isTTY, mode: mode, testnet: testnet, runFor: runFor,
		rpcAddress: optString(opts, "--rpc-address"),
		logFile:    logFile.Name(),
	})
}

// buildDeviceRows turns the engine's counters and the last sensor poll into the
// rows the panel draws: the CPU first, then one per running GPU.
//
// The GPU detail column is power draw where the card reports it, because that
// is the one number that changes what someone does next -- a card sitting at
// its power limit is a card that will not go faster whatever else is tuned.
func buildDeviceRows(e *Engine, perGPU map[int]*rateWindow, cpuRate float64, sensed SensorSample) []DeviceStat {
	rows := make([]DeviceStat, 0, 1+len(perGPU))
	rows = append(rows, DeviceStat{Label: "CPU", Rate: cpuRate, TempC: sensed.CPUTemp()})

	for _, d := range e.GPUDeviceList() {
		row := DeviceStat{
			Label: "GPU " + strconv.Itoa(d),
			IsGPU: true,
			TempC: tempUnknown,
		}
		if w := perGPU[d]; w != nil {
			row.Rate = w.mean()
		}
		// A device with a worker running and nothing coming back is the
		// failure worth shouting about; a device still filling its first
		// averaging window is not, so the flag waits for the window.
		row.Ailing = row.Rate <= 0 && perGPU[d] != nil && len(perGPU[d].buf) >= perGPU[d].n

		if g := sensed.gpuByIndex(d); g != nil {
			row.TempC = g.TempC
			if g.HavePower {
				row.Note = fmt.Sprintf("%.0fW", g.PowerW)
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// ---------------------------------------------------------------- benchmark

// runBench measures the hash function on the CPU across thread counts, and then
// -- when devices are asked for and present -- on each GPU across its block-count
// candidates. Both halves report the same unit, so the two can be added up to
// predict what the machine will do with both mining.
func runBench(t *Theme, maxThreadsWanted int, gpus []int, gpuBatch int,
	saNote string, saOK bool) {
	fmt.Printf("\n  %s\n", t.C(t.Accent+t.Bold, "DeroStorm benchmark · AstroBWTv3"))
	noteColour := t.Dim
	if !saOK {
		noteColour = t.Warn
	}
	fmt.Printf("  %s\n\n", t.C(noteColour, saNote))
	fmt.Printf("  %s\n", t.C(t.Muted, fmt.Sprintf("%8s %14s %16s %14s", "threads", "H/s", "time/hash", "H/s/thread")))

	if maxThreadsWanted < 1 {
		maxThreadsWanted = DefaultThreads()
	}

	const iterations = 1000
	warm := func(n int) { benchRound(n, 20) }
	warm(maxThreadsWanted)

	bestPerThreads := make([]float64, 0, maxThreadsWanted)
	for n := 1; n <= maxThreadsWanted; n++ {
		best := time.Duration(1 << 62)
		for rep := 0; rep < 3; rep++ {
			if d := benchRound(n, iterations); d < best {
				best = d
			}
		}
		hps := float64(n*iterations) / best.Seconds()
		bestPerThreads = append(bestPerThreads, hps)
		fmt.Printf("  %s\n", t.C(t.Text, fmt.Sprintf("%8d %14.1f %16s %14.1f",
			n, hps, (best/time.Duration(n*iterations)).Round(time.Microsecond).String(), hps/float64(n))))
	}

	cpuBest := 0.0
	if len(bestPerThreads) > 0 {
		cpuBest = bestPerThreads[len(bestPerThreads)-1]
		for _, v := range bestPerThreads {
			if v > cpuBest {
				cpuBest = v
			}
		}
	}

	if p := astrobwtv3.RecoveredPanics; p != 0 {
		fmt.Printf("\n  %s\n", t.C(t.Err, fmt.Sprintf("WARNING: %d hash(es) aborted internally", p)))
	}

	// ---- the two suffix sorts, against each other
	if saOK {
		runSABench(t, maxThreadsWanted, astrobwtv3.SuffixSort)
	}

	// ---- GPU
	gpuTotal := 0.0
	switch {
	case len(gpus) == 0:
		if GPUAvailable && GPUDeviceCount() > 0 {
			fmt.Printf("\n  %s\n", t.C(t.Dim, fmt.Sprintf(
				"%d %s device(s) present but not benchmarked — add --gpu=all",
				GPUDeviceCount(), GPUKind)))
		}
	case !GPUAvailable:
		fmt.Printf("\n  %s\n", t.C(t.Warn, "this build has no GPU support"))
	default:
		for _, d := range gpus {
			gpuTotal += runGPUBench(t, d, gpuBatch)
		}
	}

	if gpuTotal > 0 && cpuBest > 0 {
		fmt.Printf("  %s\n", t.C(t.Accent+t.Bold, fmt.Sprintf(
			"CPU %s + GPU %s = %s together",
			humanRate(cpuBest), humanRate(gpuTotal), humanRate(cpuBest+gpuTotal))))
	}
	fmt.Println()
}

func benchRound(threads, iterations int) time.Duration {
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			pinToCPU(slot)

			scratch := astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
			defer astrobwtv3.Pool.Put(scratch)

			// The benchmark has to hash the way mining hashes, or it measures
			// something the miner will not do. Where the paired SHA-256 is
			// available that means two nonces at a time; see engine.go.
			paired := PairedSHAAvailable()
			var scratchB *astrobwtv3.ScratchData
			if paired {
				scratchB = astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
				defer astrobwtv3.Pool.Put(scratchB)
			}

			var work, workB [48]byte
			work[0] = 1
			for k := 8; k < 43; k++ {
				work[k] = byte(k*7 + slot*13)
			}
			workB = work
			// Vary the nonce: AstroBWTv3's loop count and suffix-array length
			// depend on the input, so hashing one fixed value repeatedly would
			// measure a single point of the cost distribution.
			setNonce := func(w *[48]byte, it int) {
				w[43] = byte(it)
				w[44] = byte(it >> 8)
				w[45] = byte(it >> 16)
				w[46] = byte(slot)
			}
			it := 0
			if paired {
				for ; it+1 < iterations; it += 2 {
					setNonce(&work, it)
					setNonce(&workB, it+1)
					_, _ = astrobwtv3.AstroBWTv3_pair(work[:], workB[:], scratch, scratchB)
				}
			}
			// The odd one out when iterations is odd, and every hash when the
			// pairing is unavailable. Counting has to match what was done.
			for ; it < iterations; it++ {
				setNonce(&work, it)
				_ = astrobwtv3.AstroBWTv3_scratch(work[:], scratch)
			}
		}(i)
	}
	wg.Wait()
	return time.Since(start)
}

// ---------------------------------------------------------------- helpers

func openLogFile() *os.File {
	name := "derostorm.log"
	if exe, err := os.Executable(); err == nil {
		name = exe + ".log"
	}
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return os.NewFile(0, os.DevNull)
	}
	return f
}

func optString(o docopt.Opts, key string) string {
	v, ok := o[key]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func optBool(o docopt.Opts, key string) bool {
	v, ok := o[key]
	if !ok || v == nil {
		return false
	}
	b, _ := v.(bool)
	return b
}

func parseUint(s string) uint64 {
	n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// ---------------------------------------------------------------- preview

// runPreview draws the console once with representative data. With no --theme
// it shows every theme in turn, which is the quickest way to choose one.
func runPreview(themeName string, isTTY bool) {
	names := themeNames()
	if themeName != "" {
		if _, ok := themes[strings.ToLower(themeName)]; ok {
			names = []string{strings.ToLower(themeName)}
		}
	}

	now := time.Now()
	sample := Snapshot{
		State:      StateMining,
		Hashrate:   15500.0,
		Threads:    15,
		GPUs:       1,
		CPURate:    8120.0,
		GPURate:    7380.0,
		History:    previewHistory(),
		Height:     2481903,
		Difficulty: 132000,
		NetHashes:  120000,
		Blocks:     9,
		MiniBlocks: 89,
		Rejected:   1,
		Uptime:     12*time.Minute + 47*time.Second,
		Node:       "minernode1.dero.live:10100",
		PeakRate:   16240.0,
		AvgRate:    14980.0,
		Sensors: SensorSample{
			HaveCPU: true, CPUTempC: 71.5, CPUSource: "hardware monitor",
			GPUs: []GPUSensor{{
				Index: 0, Name: "NVIDIA GeForce RTX 5080", TempC: 66,
				FanPct: 54, HaveFan: true, UtilPct: 99, HaveUtil: true,
				PowerW: 214.7, PowerCapW: 360, HavePower: true,
			}},
		},
		Log: []LogEntry{
			{At: now.Add(-38 * time.Second), Level: LogGood, Tag: "connect", Text: "connected to minernode1.dero.live:10100"},
			{At: now.Add(-21 * time.Second), Level: LogInfo, Tag: "job", Text: "height 2481903 · difficulty 132000"},
			{At: now.Add(-14 * time.Second), Level: LogInfo, Tag: "submit", Text: "share at height 2481903"},
			{At: now.Add(-14 * time.Second), Level: LogGood, Tag: "accepted", Text: "miniblock 89 at height 2481903"},
			{At: now.Add(-6 * time.Second), Level: LogWarn, Tag: "rejected", Text: "share rejected (total 1)"},
			{At: now.Add(-2 * time.Second), Level: LogGood, Tag: "block", Text: "block 9 found at height 2481903"},
		},
		Input:     "threads 12",
		ShowInput: true,
	}

	for _, name := range names {
		// Two themes, not one: `shown` is the theme being described and `draw`
		// is the one actually used. They differ when colour is unavailable,
		// where every panel is drawn in mono -- and labelling all five "no
		// colour at all" because of that would describe the terminal rather
		// than the themes.
		shown := themes[name]
		draw := shown
		if !isTTY {
			draw = themes["mono"]
		}
		c := NewConsole(os.Stdout, draw, false, 6)
		fmt.Printf("\n  %s  %s\n\n",
			draw.C(draw.Accent+draw.Bold, "--theme="+name), draw.C(draw.Dim, shown.Desc))
		for _, ln := range c.frame(sample) {
			fmt.Println(ln)
		}
	}
	fmt.Println()
}

func previewHistory() []float64 {
	h := make([]float64, 60)
	for i := range h {
		// a plausible ramp-up then steady state with a little jitter
		switch {
		case i < 6:
			h[i] = float64(i) * 1200
		default:
			h[i] = 7200 + float64((i*37)%600)
		}
	}
	return h
}

// rateWindow is a fixed-length ring of per-tick rates, averaged to smooth them.
type rateWindow struct {
	buf []float64
	n   int
}

func newRateWindow(n int) *rateWindow { return &rateWindow{buf: make([]float64, 0, n), n: n} }

func (w *rateWindow) add(v float64) {
	w.buf = append(w.buf, v)
	if len(w.buf) > w.n {
		w.buf = w.buf[1:]
	}
}

func (w *rateWindow) mean() float64 {
	if len(w.buf) == 0 {
		return 0
	}
	s := 0.0
	for _, v := range w.buf {
		s += v
	}
	return s / float64(len(w.buf))
}

// parseGPUList turns the --gpu argument into device indices. "all" takes every
// device the driver reports, "off" and "none" disable the GPU, and anything
// else is a comma-separated list of indices.
func parseGPUList(v string) ([]int, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "off", "none", "no", "0000-off":
		return nil, nil
	case "all":
		n := GPUDeviceCount()
		if n == 0 {
			return nil, fmt.Errorf("no CUDA device found")
		}
		devs := make([]int, n)
		for i := range devs {
			devs[i] = i
		}
		return devs, nil
	}

	var devs []int
	seen := map[int]bool{}
	for _, f := range strings.Split(v, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		d, err := strconv.Atoi(f)
		if err != nil || d < 0 || d >= maxGPUs {
			return nil, fmt.Errorf("%q is not a device index between 0 and %d", f, maxGPUs-1)
		}
		if !seen[d] {
			seen[d] = true
			devs = append(devs, d)
		}
	}
	if len(devs) == 0 {
		return nil, fmt.Errorf("no devices listed")
	}
	return devs, nil
}

// startCPUProfile begins a CPU profile at path and returns the function that
// finishes it. The returned function is safe to call once.
//
// os.Exit skips deferred calls, so every early exit that matters -- --bench and
// --preview both return normally -- has to leave through the end of main for
// the profile to be written at all.
func startCPUProfile(path string) (func(), error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			pprof.StopCPUProfile()
			f.Close()
		})
	}, nil
}
