package main

import "testing"

// blockCandidates must be sorted, deduplicated, within [1, maxBlocks], and
// include the ceiling itself: mining starts there before sweeping down.
func TestBlockCandidates(t *testing.T) {
	for _, tc := range []struct {
		sms, maxBlocks int
	}{
		{20, 180},  // Radeon 8060S (Strix Halo)
		{84, 504},  // RTX 5080-class discrete card
		{1, 8},     // tiny card: duplicates collapse
		{2, 1},     // ceiling below every multiple: only the ceiling survives
		{128, 512}, // large card, everything fits
	} {
		cands := blockCandidates(tc.sms, tc.maxBlocks)
		if len(cands) == 0 {
			t.Fatalf("sms=%d max=%d: no candidates", tc.sms, tc.maxBlocks)
		}
		if cands[len(cands)-1] != tc.maxBlocks {
			t.Errorf("sms=%d max=%d: ceiling %d missing from %v", tc.sms, tc.maxBlocks, tc.maxBlocks, cands)
		}
		for i, c := range cands {
			if c < 1 || c > tc.maxBlocks {
				t.Errorf("sms=%d max=%d: candidate %d out of range: %d", tc.sms, tc.maxBlocks, i, c)
			}
			if i > 0 && c <= cands[i-1] {
				t.Errorf("sms=%d max=%d: candidates not strictly increasing at %d: %v", tc.sms, tc.maxBlocks, i, cands)
			}
		}
	}
}

// tunerForTest builds a blockTuner with the given candidates and pre-fed mean
// rates, as if each had been observed for one round.
func tunerForTest(cands []int, rates []float64) *blockTuner {
	t := &blockTuner{
		cands: cands,
		sum:   make([]float64, len(cands)),
		n:     make([]int, len(cands)),
	}
	for i, r := range rates {
		if r > 0 {
			t.sum[i] = r // n=1 → mean equals the sum
			t.n[i] = 1
		}
	}
	return t
}

// After one round, settings below half the best rate are cut; the rest stay in
// order with their measurements carried over.
func TestPruneCutsHopelessCandidates(t *testing.T) {
	tu := tunerForTest([]int{5, 10, 20, 40, 80, 180}, []float64{100, 400, 500, 3000, 6000, 5500})
	tu.prune()
	want := []int{40, 80, 180} // best 6000 → half 3000: 5/10/20 blocks all fall below
	if got := tu.cands; len(got) != len(want) {
		t.Fatalf("pruned to %v, want %v", got, want)
	}
	for i := range want {
		if tu.cands[i] != want[i] {
			t.Fatalf("pruned to %v, want %v", tu.cands, want)
		}
	}
	if tu.rate(0) != 3000 { // 40-block slot is now index 0, still 3000 H/s
		t.Errorf("measurements not carried over: rate(0)=%v", tu.rate(0))
	}
}

// A spread where everything is within a factor of two of the best prunes
// nothing, and the round logic must survive that.
func TestPruneKeepsContenders(t *testing.T) {
	tu := tunerForTest([]int{20, 40, 80}, []float64{5000, 5500, 6000})
	tu.prune()
	if len(tu.cands) != 3 {
		t.Fatalf("pruned contenders away: %v", tu.cands)
	}
}

// Two candidates is already cheap; pruning must not run there.
func TestPruneSkipsTinySweeps(t *testing.T) {
	tu := tunerForTest([]int{80, 180}, []float64{6000, 3000})
	tu.prune()
	if len(tu.cands) != 2 {
		t.Fatalf("pruned a two-candidate sweep: %v", tu.cands)
	}
}

// Degenerate input: no samples at all must leave the candidate list alone.
func TestPruneNoSamples(t *testing.T) {
	tu := tunerForTest([]int{20, 40, 80}, []float64{0, 0, 0})
	tu.prune()
	if len(tu.cands) != 3 {
		t.Fatalf("pruned without samples: %v", tu.cands)
	}
}
