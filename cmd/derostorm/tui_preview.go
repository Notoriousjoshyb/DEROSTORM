package main

// --preview: one frame of the console, with representative data, printed to
// stdout and gone.
//
// It exists for two reasons. Someone choosing a theme should not have to point
// a miner at a node to see what they are choosing. And every change to the
// layout needs to be looked at, at several window sizes, without waiting for a
// hashrate to build up -- which is why the sample below has a full history, a
// GPU, some rejected shares and a temperature: a dashboard only ever looks
// right with numbers in it.
//
// It goes through renderFrame, the same function the live console uses. A
// preview with a rendering path of its own would eventually be a picture of a
// program that does not exist.

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/notoriousjoshyb/derostorm/internal/ui"
)

// runTUIPreview prints one frame at the given size and returns.
func runTUIPreview(themeName, size, screen string, isTTY bool) {
	cols, rows := TerminalWidth(), TerminalHeight()
	if cols <= 0 {
		cols = 160
	}
	if rows <= 0 {
		rows = 48
	}
	if size != "" {
		if c, r, ok := parseSize(size); ok {
			cols, rows = c, r
		} else {
			fmt.Fprintf(os.Stderr, "--size wants <cols>x<rows>, e.g. 160x48\n")
			os.Exit(2)
		}
	}

	theme, _ := PickTheme(themeName, isTTY)
	sc := screenByName(screen)

	cv := ui.NewCanvas(cols, rows)
	renderFrame(cv, previewSnapshot(), theme, frameOpts{screen: sc, frame: 7})

	for _, line := range cv.Lines(theme.Name != "mono") {
		fmt.Println(line)
	}
}

func parseSize(s string) (int, int, bool) {
	a, b, ok := strings.Cut(strings.ToLower(strings.TrimSpace(s)), "x")
	if !ok {
		return 0, 0, false
	}
	c, err1 := strconv.Atoi(strings.TrimSpace(a))
	r, err2 := strconv.Atoi(strings.TrimSpace(b))
	if err1 != nil || err2 != nil || c < 10 || r < 5 {
		return 0, 0, false
	}
	return c, r, true
}

func screenByName(s string) screenID {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "mining":
		return screenMining
	case "stats", "statistics":
		return screenStats
	case "network":
		return screenNetwork
	case "threads":
		return screenThreads
	case "config":
		return screenConfig
	case "logs", "log":
		return screenLogs
	case "pools":
		return screenPools
	case "help":
		return screenHelp
	}
	return screenDashboard
}

// previewSnapshot is a plausible machine: fifteen CPU threads and one card,
// a few minutes in, with a couple of rejected shares behind it.
func previewSnapshot() Snapshot {
	now := time.Now()
	const cpuRate, gpuRate = 33040.0, 72090.0
	total := cpuRate + gpuRate

	threads := 15
	rates := make([]float64, threads)
	for i := range rates {
		// A little spread, and one worker deliberately behind, because that is
		// the case the panel exists to make visible.
		rates[i] = cpuRate / float64(threads) * (1 - 0.03*float64(i%4))
	}
	rates[threads-1] *= 0.86

	hist := previewSeries(300, total, 0.06, 0.0)

	return Snapshot{
		State:       StateMining,
		Hashrate:    total,
		Threads:     threads,
		GPUs:        1,
		CPURate:     cpuRate,
		GPURate:     gpuRate,
		PeakRate:    total * 1.06,
		AvgRate:     total * 0.98,
		Avg1m:       total * 1.001,
		Avg5m:       total * 0.995,
		Avg15m:      total * 0.988,
		History:     hist,
		Hist15m:     previewSeries(180, total, 0.05, 0.0),
		Hist1h:      previewSeries(180, total, 0.09, -0.04),
		Hist24h:     previewSeries(240, total, 0.12, -0.10),
		CPUHist:     previewSeries(60, cpuRate, 0.10, 0),
		GPUHist:     previewSeries(60, gpuRate, 0.14, 0),
		ThreadRates: rates,
		TotalHashes: 39_874_982_145,
		Height:      7_541_894,
		Difficulty:  5_587_798,
		Blocks:      0,
		MiniBlocks:  1245,
		Rejected:    12,
		Submitted:   1257,
		BestShare:   1_230_000,
		LastShare:   now.Add(-2 * time.Second),
		ConnectedAt: now.Add(-3*time.Hour - 12*time.Minute),
		LastJob:     now.Add(-2 * time.Second),
		HeightAt:    now.Add(-53 * time.Second),
		Uptime:      3*time.Hour + 12*time.Minute + 8*time.Second,
		Node:        "dero-node.mysrv.cloud:10100",
		ConfigPath:  "derostorm.json",
		LogFile:     "derostorm.log",
		ThemeName:   "cyber",
		GPUList:     []int{0},
		SANote:      "fast suffix sort installed (libsais)",
		NodeNote:    "chain detail from http://dero-node.mysrv.cloud:10102/json_rpc",
		SensorNote:  []string{"CPU temperature from hardware monitor"},
		Info: NodeInfo{
			OK: true, At: now, Latency: 42 * time.Millisecond,
			PeersIn: 6, PeersOut: 6, Peers: 12,
			Height: 7_541_894, TopoHeight: 7_602_113,
			Difficulty: 100_580_000, BlockTime: 18, AvgBlockTime50: 17.6,
			Version: "3.5.3-131", Network: "Mainnet",
			Miners: 3, MiniInMemory: 4, TxPool: 2,
			Endpoint: "http://dero-node.mysrv.cloud:10102/json_rpc",
		},
		Sys: SysSample{
			At: now, HaveLoad: true, LoadPct: 96.4,
			HaveMem: true, MemUsedMB: 8396, MemTotalMB: 32688,
			HaveFreq: true, FreqMHz: 4218,
		},
		Sensors: SensorSample{
			HaveCPU: true, CPUTempC: 56, CPUSource: "hardware monitor",
			GPUs: []GPUSensor{{
				Index: 0, Name: "NVIDIA GeForce RTX 5080", TempC: 62,
				FanPct: 54, HaveFan: true, UtilPct: 99, HaveUtil: true,
				PowerW: 215, PowerCapW: 360, HavePower: true,
				MemUsedMB: 8396, MemTotalMB: 16384, HaveMem: true,
				ClockMHz: 2730, MemClockMHz: 10501, HaveClock: true,
			}},
		},
		Devices: []DeviceStat{
			{Label: "CPU", Rate: cpuRate, TempC: 56},
			{Label: "GPU 0", Rate: gpuRate, TempC: 62, Note: "215W", IsGPU: true},
		},
		Log: []LogEntry{
			{At: now.Add(-109 * time.Second), Level: LogInfo, Tag: "start", Text: "mining started successfully"},
			{At: now.Add(-96 * time.Second), Level: LogGood, Tag: "connect", Text: "connected to dero-node.mysrv.cloud:10100"},
			{At: now.Add(-78 * time.Second), Level: LogInfo, Tag: "info", Text: "mining is running smoothly"},
			{At: now.Add(-67 * time.Second), Level: LogWarn, Tag: "warn", Text: "high difficulty detected"},
			{At: now.Add(-42 * time.Second), Level: LogGood, Tag: "accepted", Text: "share accepted (11ms) [GPU]"},
			{At: now.Add(-22 * time.Second), Level: LogInfo, Tag: "job", Text: "new job received (diff 5587798)"},
			{At: now.Add(-6 * time.Second), Level: LogGood, Tag: "accepted", Text: "share accepted (15ms) [CPU]"},
			{At: now.Add(-2 * time.Second), Level: LogGood, Tag: "accepted", Text: "share accepted (12ms) [GPU]"},
		},
	}
}

// previewSeries is a plausible hashrate trace: a ramp on to the target, then a
// steady state with jitter and a slow drift.
func previewSeries(n int, target, jitter, drift float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		f := float64(i) / float64(n)
		v := target * (1 + drift*(1-f))
		// Two incommensurable sine waves plus the ramp: enough structure that
		// the chart shows a shape rather than a band of noise.
		v *= 1 + jitter*(0.6*math.Sin(float64(i)*0.31)+0.4*math.Sin(float64(i)*0.11))
		out[i] = v
	}
	return out
}
