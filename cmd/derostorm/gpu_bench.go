package main

// The GPU benchmark, behind --bench.
//
// It exists because the GPU's throughput depends on how many suffix blocks
// are resident (see gpu_tune.go), and someone tuning a machine wants to see
// that curve. Mining sits at four per SM; this still sweeps, so a new kernel
// can move the peak without anyone guessing. It also checks a kernel change
// without connecting to a node.
//
// Every batch is a real search against a real target, so this drives exactly
// the path mining drives: the same three kernels, the same difficulty check, the
// same PCIe traffic. The difficulty is set absurdly high so no nonce ever
// qualifies and nothing is ever submitted anywhere.
//
// Settings are interleaved rather than measured one after another, for the
// reason set out at the top of gpu_tune.go: on a GPU that is also driving a
// display, sequential measurement mostly reports the order it ran them in.

import (
	"fmt"
	"math/big"
	"time"

	"github.com/deroproject/derohe/astrobwt/astrobwtv3"
	"github.com/deroproject/derohe/block"
)

const (
	// benchWarmup is batches discarded on each visit to a setting.
	benchWarmup = 1
	// benchTimed is batches counted on each visit.
	benchTimed = 2
	// benchRounds is visits per setting.
	benchRounds = 3
)

// runGPUBench measures one device across its block-count candidates and prints
// the curve. Returns the best rate, or 0 if the device could not be measured.
func runGPUBench(t *Theme, device int, batch int) float64 {
	fmt.Printf("\n  %s\n\n", t.C(t.Accent+t.Bold, fmt.Sprintf("GPU %d · %s", device, GPUDeviceKind(device))))

	// Four blocks per SM is the mining default and the allocation ceiling.
	g, err := NewGPUContext(device, batch, 0)
	if err != nil {
		fmt.Printf("  %s\n\n", t.C(t.Err, err.Error()))
		return 0
	}
	defer g.Close()

	fmt.Printf("  %s\n", t.C(t.Muted, g.Name()))
	fmt.Printf("  %s\n\n", t.C(t.Dim, fmt.Sprintf(
		"%d hashes per batch · up to %d resident blocks", g.Batch(), g.MaxBlocks())))

	// Prove the device before reporting a number for it: a card that disagrees
	// with the CPU has a hashrate of zero however fast it runs.
	scratch := astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
	defer astrobwtv3.Pool.Put(scratch)

	work := make([]byte, block.MINIBLOCK_SIZE)
	work[0] = 1
	for i := 8; i < 43; i++ {
		work[i] = byte(i*7 + 29)
	}

	if err := verifyGPU(g, work, scratch); err != nil {
		fmt.Printf("  %s\n\n", t.C(t.Err, "does not agree with the CPU: "+err.Error()))
		return 0
	}
	fmt.Printf("  %s\n\n", t.C(t.Good, "matches the CPU exactly"))

	// 2^96 is far past any real DERO difficulty, so no nonce qualifies and the
	// benchmark never has a share to deal with.
	target := NewTarget(new(big.Int).Lsh(big.NewInt(1), 96))

	cands := blockCandidates(g.SMs(), g.MaxBlocks())
	sum := make([]float64, len(cands))
	n := make([]int, len(cands))
	nonce := uint32(0)

	fmt.Printf("  %s\n", t.C(t.Dim, fmt.Sprintf(
		"measuring %d settings, %d interleaved rounds each", len(cands), benchRounds)))

	failed := false
	for r := 0; r < benchRounds && !failed; r++ {
		for i, c := range cands {
			if err := g.SetBlocks(c); err != nil {
				continue
			}
			for k := 0; k < benchWarmup+benchTimed; k++ {
				start := time.Now()
				if _, err := g.Search(work, nonce, &target); err != nil {
					fmt.Printf("\n  %s\n", t.C(t.Err, err.Error()))
					failed = true
					break
				}
				nonce += uint32(g.Batch())
				if k >= benchWarmup {
					sum[i] += float64(g.Batch()) / time.Since(start).Seconds()
					n[i]++
				}
			}
			if failed {
				break
			}
		}
		fmt.Printf("  %s", t.C(t.Dim, "·"))
	}
	fmt.Printf("\n\n")

	fmt.Printf("  %s\n", t.C(t.Muted, fmt.Sprintf("%8s %14s %14s", "blocks", "H/s", "ms/batch")))
	best, bestBlocks := 0.0, 0
	for i, c := range cands {
		if n[i] == 0 {
			fmt.Printf("  %s\n", t.C(t.Dim, fmt.Sprintf("%8d %14s", c, "not measured")))
			continue
		}
		rate := sum[i] / float64(n[i])
		if rate > best {
			best, bestBlocks = rate, c
		}
		fmt.Printf("  %s\n", t.C(t.Text, fmt.Sprintf("%8d %14.1f %14.0f",
			c, rate, float64(g.Batch())/rate*1000)))
	}

	if best > 0 {
		fmt.Printf("\n  %s\n", t.C(t.Accent+t.Bold,
			fmt.Sprintf("best %s at %d blocks", humanRate(best), bestBlocks)))
		fmt.Printf("  %s\n", t.C(t.Dim, fmt.Sprintf(
			"pin it with --gpu-blocks=%d, or leave it and the miner measures it while mining",
			bestBlocks)))
	}
	fmt.Println()
	return best
}
