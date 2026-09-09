package main

// Grid-block count for the GPU suffix kernel.
//
// The suffix sort is ~97% of GPU hash time. Too few blocks leave the card idle;
// too many spend memory on work that cannot become resident. The GPU library
// reports a runtime-derived ceiling one row beyond physical occupancy: 504 on
// the 84-SM 5080, where five rows are resident and the queued sixth smooths the
// tail.
//
// The ceiling is where the curve lands on discrete cards, but it is not a law:
// on an integrated Radeon 8060S (gfx1151) the curve peaks near 80 blocks and
// reads 11% slower at the 180-block ceiling, whose extra resident rows fight
// the sort for memory bandwidth. So mining sweeps blockCandidates, exactly the
// curve --bench draws, and pins the fastest setting. Settings are interleaved
// and the first batch after each switch is discarded, so a GPU that is also
// driving a display does not just report the order it ran them in. After one
// full round, candidates below half the best rate so far are pruned: they got
// their one fair interleaved pass, and re-measuring a floor wastes minutes.
//
// --bench sweeps too, because the peak has moved before: it was at four blocks
// per SM, then at half a block per SM, then at the top of the range. Whatever
// the shape is on a given card, guessing it has a poor record.

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
	// tuneRounds is visits per setting. Two interleaved passes sample every
	// candidate at two different times, which is what the interleaving is for,
	// and with round-boundary pruning the survivors are the only cost after
	// that. Three read measurably better on no card we have seen, and the
	// sweep runs at mining speed: every extra round is a minute of a slower
	// miner.
	tuneRounds = 2
	// tuneTie is how close two candidates have to read before the smaller
	// block count wins. The ceiling costs real VRAM, and a setting within a
	// per cent of the fastest buys nothing with it.
	tuneTie = 0.01
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
// guessing it has a poor record, so the sweep covers a wide range and includes
// the allocation ceiling.
func blockCandidates(sms, maxBlocks int) []int {
	raw := []int{sms / 4, sms / 2, sms, sms * 2, sms * 4, sms * 5, sms * 6, sms * 8, maxBlocks}
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

// newBlockTuner pins an explicit --gpu-blocks, and otherwise sweeps
// blockCandidates while mining and settles on the fastest setting.
func newBlockTuner(g *GPUContext, dev int, pinned int,
	post func(LogLevel, string, string, ...interface{})) *blockTuner {

	if pinned > 0 {
		n := pinned
		if n > g.MaxBlocks() {
			post(LogWarn, "gpu", "device %d: %d suffix blocks exceeds the runtime/VRAM ceiling, using %d",
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

	// Unpinned: start at the runtime-derived ceiling and sweep down the same
	// candidate list --bench draws its curve from, then pin the fastest. One
	// candidate means the sweep would only ever re-measure the ceiling, so
	// skip it and mine.
	cands := blockCandidates(g.SMs(), g.MaxBlocks())
	if len(cands) < 2 {
		n := g.MaxBlocks()
		if err := g.SetBlocks(n); err != nil {
			post(LogWarn, "gpu", "device %d: %v — leaving the block count alone", dev, err)
		} else {
			post(LogInfo, "gpu", "device %d: %d suffix blocks (auto)", dev, n)
		}
		return nil
	}
	n := cands[len(cands)-1]
	if err := g.SetBlocks(n); err != nil {
		post(LogWarn, "gpu", "device %d: %v — not sweeping, leaving the block count alone", dev, err)
		return nil
	}
	post(LogInfo, "gpu", "device %d: measuring %d suffix-block settings — the console says TUNING GPU until this ends",
		dev, len(cands))
	return &blockTuner{
		g:     g,
		post:  post,
		dev:   dev,
		cands: cands,
		sum:   make([]float64, len(cands)),
		n:     make([]int, len(cands)),
	}
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
		t.prune()
		if len(t.cands) == 1 {
			t.settle() // one survivor: the sweep already has its answer
			return
		}
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

// prune drops candidates that, after a full interleaved round, have no chance
// of winning: their mean rate is below half the best so far. The sweep exists
// to find the peak, not to document the floor, and on a card where a small
// grid crawls — an APU spreads the same batch over far fewer blocks — the
// hopeless settings cost minutes of tuning time at a terrible hashrate.
// Pruning happens only at round boundaries, so every candidate gets the same
// one fair interleaved pass before it can be cut.
func (t *blockTuner) prune() {
	if len(t.cands) <= 2 {
		return // nothing worth cutting
	}
	best := 0.0
	for i := range t.cands {
		if r := t.rate(i); r > best {
			best = r
		}
	}
	if best <= 0 {
		return
	}
	keep := []int{}
	for i := range t.cands {
		if r := t.rate(i); r >= best/2 {
			keep = append(keep, i)
		}
	}
	if len(keep) == 0 || len(keep) == len(t.cands) {
		return
	}
	cands := make([]int, 0, len(keep))
	sum := make([]float64, 0, len(keep))
	n := make([]int, 0, len(keep))
	for _, i := range keep {
		cands = append(cands, t.cands[i])
		sum = append(sum, t.sum[i])
		n = append(n, t.n[i])
	}
	t.cands, t.sum, t.n = cands, sum, n
}

// settle picks the winner, applies it, and reports the curve so a person can
// see whether the peak was real or the sweep should have gone further.
func (t *blockTuner) settle() {
	t.done = true
	// bestRate wins outright; a later candidate within tuneTie of it does not
	// replace it, so the tie-break lands on the smallest block count that is
	// within noise of the fastest.

	best, bestRate := -1, 0.0
	for i := range t.cands {
		if r := t.rate(i); r > bestRate*(1+tuneTie) {
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
