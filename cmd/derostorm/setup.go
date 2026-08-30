package main

// First-run setup. Runs before the live console exists, so it is plain
// question-and-answer using ordinary line input -- no raw mode, no redrawing,
// nothing that can go wrong on an unfamiliar terminal.
//
// The network question comes first because it decides both which node to
// suggest and which address prefix is valid, and globals has to be initialised
// against the right network before an address can be checked.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/deroproject/derohe/globals"
)

type setupUI struct {
	in    *bufio.Reader
	out   io.Writer
	theme *Theme
	total int
}

func (s *setupUI) title(text string) {
	t := s.theme
	fmt.Fprintf(s.out, "\n  %s\n", t.C(t.Accent+t.Bold, text))
}

func (s *setupUI) note(text string) {
	fmt.Fprintf(s.out, "  %s\n", s.theme.C(s.theme.Dim, text))
}

func (s *setupUI) fail(text string) {
	t := s.theme
	fmt.Fprintf(s.out, "  %s %s\n", t.C(t.Err, "×"), t.C(t.Err, text))
}

// ask prints one numbered prompt and returns the trimmed answer, or def when
// the user just presses Enter. The step number is passed in rather than counted
// here, so re-asking after a bad answer does not advance it.
func (s *setupUI) ask(step int, label, def, help string) (string, error) {
	t := s.theme
	fmt.Fprintln(s.out)
	fmt.Fprintf(s.out, "  %s  %s\n",
		t.C(t.Accent2, fmt.Sprintf("%d/%d", step, s.total)),
		t.C(t.Text+t.Bold, label))
	if help != "" {
		fmt.Fprintf(s.out, "       %s\n", t.C(t.Dim, help))
	}
	if def != "" {
		fmt.Fprintf(s.out, "       %s\n", t.C(t.Muted, "default: "+def))
	}
	fmt.Fprintf(s.out, "  %s ", t.C(t.Accent, "›"))

	line, err := s.in.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	return line, nil
}

// RunSetup walks the user through creating a config. It initialises globals as
// soon as the network is known, so the wallet address can be validated for real
// rather than just pattern-matched.
// preferred is the theme the user asked for, which is not always the theme the
// wizard is drawn in: colour gets forced to mono when output is not a terminal,
// and that forced choice must not be what gets written to the config file.
func RunSetup(theme *Theme, preferred, path string, existing *Config, flagTestnet bool) (*Config, error) {
	// Probing for a GPU here rather than at the question decides how many
	// questions there are, so the "answer N questions" promise at the top is
	// true. It opens nothing and allocates no VRAM.
	gpuCount := 0
	if GPUAvailable {
		gpuCount = GPUDeviceCount()
	}

	total := 5
	if gpuCount > 0 {
		total = 6
	}
	s := &setupUI{in: bufio.NewReader(os.Stdin), out: os.Stdout, theme: theme, total: total}
	t := theme

	if preferred == "" {
		preferred = "default"
	}
	cfg := &Config{
		Node:    "",
		Threads: DefaultThreads(),
		Theme:   preferred,
	}
	if existing != nil {
		*cfg = *existing
		if cfg.Threads <= 0 {
			cfg.Threads = DefaultThreads()
		}
		if cfg.Theme == "" {
			cfg.Theme = preferred
		}
	}
	if flagTestnet {
		cfg.Testnet = true
	}

	s.title("DeroStorm setup")
	s.note(fmt.Sprintf("Answer %d questions and this is saved to", total))
	s.note("  " + path)
	s.note("Press Enter to accept a default. Ctrl-C to quit.")

	switch {
	case gpuCount == 1:
		s.note("")
		s.note("GPU found: " + GPUDeviceInfo(0))
	case gpuCount > 1:
		s.note("")
		s.note(fmt.Sprintf("%d GPUs found:", gpuCount))
		for i := 0; i < gpuCount; i++ {
			s.note("  " + GPUDeviceInfo(i))
		}
	case GPUAvailable:
		s.note("")
		s.note("No " + GPUKind + " GPU found — mining on the CPU.")
		s.note("AMD and Intel GPUs are not supported.")
	}

	// ---- 1. network ------------------------------------------------------
	netDef := "mainnet"
	if cfg.Testnet {
		netDef = "testnet"
	}
	for {
		ans, err := s.ask(1, "Network", netDef, "mainnet or testnet")
		if err != nil {
			return nil, err
		}
		switch strings.ToLower(ans) {
		case "mainnet", "main", "m":
			cfg.Testnet = false
		case "testnet", "test", "t":
			cfg.Testnet = true
		default:
			s.fail("type mainnet or testnet")
			continue
		}
		break
	}

	// globals must know the network before an address can be validated.
	globals.Arguments["--testnet"] = cfg.Testnet
	globals.Initialize()

	// ---- 2. wallet -------------------------------------------------------
	prefix := "dero1..."
	if cfg.Testnet {
		prefix = "deto1..."
	}
	for {
		ans, err := s.ask(2, "Wallet address", cfg.Wallet,
			"mining rewards go here — starts with "+prefix)
		if err != nil {
			return nil, err
		}
		if ans == "" {
			s.fail("a wallet address is required")
			continue
		}
		addr, err := globals.ParseValidateAddress(ans)
		if err != nil {
			s.fail(fmt.Sprintf("not a valid %s address: %v", netName(cfg.Testnet), err))
			continue
		}
		cfg.Wallet = addr.String()
		fmt.Fprintf(s.out, "  %s %s\n", t.C(t.Good, "✓"), t.C(t.Dim, "valid "+netName(cfg.Testnet)+" address"))
		break
	}

	// ---- 3. node ---------------------------------------------------------
	nodeDef := cfg.Node
	if nodeDef == "" {
		nodeDef = DefaultNode(cfg.Testnet)
	}
	for {
		ans, err := s.ask(3, "Miner node", nodeDef, "host:port of the derod getwork port")
		if err != nil {
			return nil, err
		}
		if !strings.Contains(ans, ":") {
			s.fail("include the port, for example " + DefaultNode(cfg.Testnet))
			continue
		}
		cfg.Node = ans
		break
	}

	// ---- 4. threads ------------------------------------------------------
	cpus := runtime.NumCPU()
	for {
		// Measured on a 9800X3D: throughput climbs to one below the logical CPU
		// count and then falls back, because the last thread ends up competing
		// with everything else the machine has to do. Suggesting cpus-1 rather
		// than cpus is worth a few percent and costs nothing.
		suggest := cfg.Threads
		if suggest <= 0 {
			suggest = cpus - 1
		}
		if suggest < 1 {
			suggest = 1
		}
		ans, err := s.ask(4, "Mining threads", strconv.Itoa(suggest),
			fmt.Sprintf("this machine has %d logical CPUs; one below that is usually fastest", cpus))
		if err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(ans)
		if err != nil || n < 1 || n > maxThreads {
			s.fail(fmt.Sprintf("enter a number between 1 and %d", maxThreads))
			continue
		}
		if n > cpus {
			fmt.Fprintf(s.out, "  %s %s\n", t.C(t.Warn, "!"),
				t.C(t.Dim, fmt.Sprintf("more threads than CPUs (%d) — this is normally slower, not faster", cpus)))
		}
		cfg.Threads = n
		break
	}

	// ---- 5. GPU ----------------------------------------------------------
	//
	// Only asked when a card is actually present. Someone with no NVIDIA GPU
	// should not have to answer a question about one, so the step is skipped
	// and the count of questions promised at the top shrinks to match.
	step := 5
	if gpuCount > 0 {
		for {
			ans, err := s.ask(step, "Use the GPU as well?", boolWord(len(cfg.GPUs) > 0),
				"yes or no — the GPU mines alongside the CPU, not instead of it")
			if err != nil {
				return nil, err
			}
			yes, ok := parseYesNo(ans)
			if !ok {
				s.fail("type yes or no")
				continue
			}
			cfg.GPUs = nil
			if yes {
				for i := 0; i < gpuCount && i < maxGPUs; i++ {
					cfg.GPUs = append(cfg.GPUs, i)
				}
				fmt.Fprintf(s.out, "  %s %s\n", t.C(t.Good, "✓"),
					t.C(t.Dim, fmt.Sprintf("mining on %d GPU(s) as well as %d CPU threads",
						len(cfg.GPUs), cfg.Threads)))
			}
			break
		}
		step++
	} else {
		cfg.GPUs = nil
	}

	// ---- 6. theme --------------------------------------------------------
	for {
		ans, err := s.ask(step, "Colour theme", cfg.Theme, "one of: "+strings.Join(themeNames(), ", "))
		if err != nil {
			return nil, err
		}
		if _, ok := themes[strings.ToLower(ans)]; !ok {
			s.fail("unknown theme — pick one of: " + strings.Join(themeNames(), ", "))
			continue
		}
		cfg.Theme = strings.ToLower(ans)
		break
	}

	if err := cfg.Save(path); err != nil {
		return nil, fmt.Errorf("could not save config: %w", err)
	}

	fmt.Fprintf(s.out, "\n  %s %s\n", t.C(t.Good, "✓"), t.C(t.Text, "saved to "+path))
	fmt.Fprintf(s.out, "  %s\n\n", t.C(t.Dim, "run with --setup any time to change these"))
	return cfg, nil
}

// parseYesNo accepts the spellings people actually type. The bool is only
// meaningful when ok is true; anything unrecognised is re-asked rather than
// guessed, because guessing wrong here silently changes what the miner does.
func parseYesNo(s string) (val, ok bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "y", "yes", "yeah", "yep", "true", "1", "on":
		return true, true
	case "n", "no", "nope", "false", "0", "off":
		return false, true
	}
	return false, false
}

func boolWord(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func netName(testnet bool) string {
	if testnet {
		return "testnet"
	}
	return "mainnet"
}
