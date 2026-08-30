package main

// Resident-block count for the GPU suffix kernel.
//
// The suffix sort is ~97% of GPU hash time. How many of its blocks are resident
// decides the throughput: too few and the card is half idle, too many and they
// compete for L2. This kernel sits at 64 registers and 256 threads, which is four
// blocks per SM, and that is where the measured curve plateaus (336 on a 5080).
// Mining uses that default. Sweeping from a few KH/s up through twice full
// occupancy used to settle on 1,252, which was slower under a display.
//
// --bench still sweeps, because the peak has moved before. Settings are
// interleaved and the first batch after each switch is discarded, so a GPU
// that is also driving a display does not just report the order it ran them in.

import (
	"fmt"
	"sort"
	"time"
)

const (
	// tuneWarmup is batches discarded on each visit to a setting.
	tuneWarmup = 1
	// tuneTimed is batches counted on each visit.
	tuneTimed = 2
	// tuneRounds is visits per setting. Three passes over five or six candidates
	// is a couple of minutes of mining, which is long enough for the
	// interleaving to cancel the drift it is there to cancel.
	tuneRounds = 3
)

// blockTuner cycles a list of candidate block counts, timing each, then pins
// the best. Driven entirely by observe(), one call per batch.
type blockTuner struct {
	g    *GPUContext
	post func(level LogLevel, tag, format string, args ...interface{})
	dev  int

	cands []int
	sum   []float64 // summed H/s samples per candidate
	n     []int     // samples per candidate

	idx   int // candidate under test
	seen  int // batches observed at this candidate on this visit
	round int

	done bool
}

// blockCandidates are the settings to try, as fractions and multiples of the SM
// count, plus whatever the VRAM allowed. Duplicates and anything past the
// allocation are dropped, so a small card may end up with one candidate and no
// sweep at all.
//
// The range is deliberately wide, because where the peak sits has moved twice
// already. It was at four blocks per SM; when the per-tile bookkeeping in
// blockRadixPass got cheaper it moved to half a block per SM; and when the
// suffix kernel became the descriptor sort at 256 threads a block it moved to
// the top of the range and kept going. Whatever the shape is on a given card,
// guessing it has a poor record, so the sweep covers a factor of thirty-two and
// the allocation ceiling on top.
func blockCandidates(sms, maxBlocks int) []int {
	raw := []int{sms / 4, sms / 2, sms, sms * 2, sms * 4, sms * 8, maxBlocks}
	seen := map[int]bool{}
	out := []int{}
	for _, c := range raw {
		if c < 1 || c > maxBlocks || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	sort.Ints(out)
	return out
}

// newBlockTuner sets the resident block count and returns nil. Mining does
// not sweep: unpinned is four per SM, and a pin above that is clamped to the
// plateau. --bench builds its own curve from blockCandidates.
func newBlockTuner(g *GPUContext, dev int, pinned int,
	post func(LogLevel, string, string, ...interface{})) *blockTuner {

	if pinned > 0 {
		n := pinned
		plateau := g.SMs() * 4
		if plateau < 1 {
			plateau = 1
		}
		if n > plateau {
			post(LogInfo, "gpu", "device %d: %d suffix blocks is past the plateau, using %d (4 per SM)",
				dev, pinned, plateau)
			n = plateau
		}
		if n > g.MaxBlocks() {
			post(LogWarn, "gpu", "device %d: %d suffix blocks needs more VRAM than there is, using %d",
				dev, n, g.MaxBlocks())
			n = g.MaxBlocks()
		}
		if err := g.SetBlocks(n); err != nil {
			post(LogWarn, "gpu", "device %d: %v", dev, err)
		} else {
			post(LogInfo, "gpu", "device %d: %d suffix blocks (pinned)", dev, n)
		}
		return nil
	}

	// Unpinned: run at four blocks per SM, which is the occupancy the suffix
	// kernel actually reaches (64 registers, 256 threads). Sweeping from
	// SMs/4 up through 8×SMs starts the worker at a few KH/s and can settle
	// on 1,252, which measured slower than 336 on a 5080 that is also driving
	// a display. --bench still sweeps; this is the mining path.
	n := g.SMs() * 4
	if n < 1 {
		n = 1
	}
	if n > g.MaxBlocks() {
		n = g.MaxBlocks()
	}
	if err := g.SetBlocks(n); err != nil {
		post(LogWarn, "gpu", "device %d: %v — leaving the block count alone", dev, err)
	} else {
		post(LogInfo, "gpu", "device %d: %d suffix blocks (4 per SM) · pin with --gpu-blocks=%d",
			dev, n, n)
	}
	return nil
}

// observe records one batch and advances the sweep. d is how long the batch
// took, hashes how many nonces it covered.
func (t *blockTuner) observe(d time.Duration, hashes int) {
	if t == nil || t.done || d <= 0 {
		return
	}

	t.seen++
	if t.seen > tuneWarmup {
		t.sum[t.idx] += float64(hashes) / d.Seconds()
		t.n[t.idx]++
	}
	if t.seen < tuneWarmup+tuneTimed {
		return
	}

	// This visit is finished; move to the next candidate, wrapping into the
	// next round. Interleaving lives in this wrap.
	t.seen = 0
	t.idx++
	if t.idx >= len(t.cands) {
		t.idx = 0
		t.round++
		if t.round >= tuneRounds {
			t.settle()
			return
		}
	}
	if err := t.g.SetBlocks(t.cands[t.idx]); err != nil {
		t.settle() // cannot continue the sweep; take the best so far
	}
}

// rate is the mean H/s measured for candidate i, or 0 if it has no samples.
func (t *blockTuner) rate(i int) float64 {
	if t.n[i] == 0 {
		return 0
	}
	return t.sum[i] / float64(t.n[i])
}

// settle picks the winner, applies it, and reports the curve so a person can
// see whether the peak was real or the sweep should have gone further.
func (t *blockTuner) settle() {
	t.done = true

	best, bestRate := -1, 0.0
	for i := range t.cands {
		if r := t.rate(i); r > bestRate {
			best, bestRate = i, r
		}
	}
	if best < 0 {
		return
	}
	if err := t.g.SetBlocks(t.cands[best]); err != nil {
		t.post(LogWarn, "gpu", "device %d: %v", t.dev, err)
		return
	}

	worst := bestRate
	for i := range t.cands {
		if r := t.rate(i); r > 0 && r < worst {
			worst = r
		}
	}
	gain := ""
	if worst > 0 && bestRate > worst {
		gain = fmt.Sprintf(" (%.0f%% over the slowest)", (bestRate/worst-1)*100)
	}
	t.post(LogGood, "gpu", "device %d: %d suffix blocks%s · %s · pin with --gpu-blocks=%d",
		t.dev, t.cands[best], gain, t.curve(), t.cands[best])
}

func (t *blockTuner) curve() string {
	s := ""
	for i, c := range t.cands {
		if i > 0 {
			s += " "
		}
		s += fmt.Sprintf("%d:%s", c, humanRate(t.rate(i)))
	}
	return s
}

// Tuning reports whether the sweep is still running, so the console can say the
// hashrate is not settled yet.
func (t *blockTuner) Tuning() bool { return t != nil && !t.done }
