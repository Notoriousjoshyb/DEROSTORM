package main

// Where a hash's time goes, measured rather than assumed.
//
// Three benchmarks over the same inputs: the stage-1 state machine and suffix
// sort on their own, one whole hash, and a paired hash. Subtracting gives the
// SHA-256's share and what the pairing is worth, on this machine, with the
// library that is actually loaded.

import (
	"runtime"
	"testing"

	"github.com/deroproject/derohe/astrobwt/astrobwtv3"
)

// BenchmarkMiningCPU measures the complete paired mining loop at GOMAXPROCS
// workers. Each operation covers 1,000 nonces per worker, including scheduling
// and scratch acquisition. Use -cpu to compare a fixed thread count.
func BenchmarkMiningCPU(b *testing.B) {
	installForBench(b)
	threads := runtime.GOMAXPROCS(0)
	benchRound(threads, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchRound(threads, 1000)
	}
	b.StopTimer()
	b.ReportMetric(float64(b.N)*float64(threads)*1000/b.Elapsed().Seconds(), "H/s")
}

func benchWork(slot int) [48]byte {
	var w [48]byte
	w[0] = 1
	for k := 8; k < 43; k++ {
		w[k] = byte(k*7 + slot*13)
	}
	return w
}

// BenchmarkHashWhole is one hash, the way mining did before the pairing.
func BenchmarkHashWhole(b *testing.B) {
	installForBench(b)
	s := astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
	defer astrobwtv3.Pool.Put(s)
	w := benchWork(1)
	for i := 0; i < b.N; i++ {
		w[43] = byte(i)
		w[44] = byte(i >> 8)
		_ = astrobwtv3.AstroBWTv3_scratch(w[:], s)
	}
}

// BenchmarkHashPaired is two hashes sharing one paired SHA-256, reported per
// hash so it compares directly with the whole hash above.
func BenchmarkHashPaired(b *testing.B) {
	installForBench(b)
	if astrobwtv3.SHA256Pair == nil {
		b.Skip("no paired SHA-256 here")
	}
	sa := astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
	sb := astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
	defer astrobwtv3.Pool.Put(sa)
	defer astrobwtv3.Pool.Put(sb)
	wa, wb := benchWork(1), benchWork(1)
	for i := 0; i < b.N; i += 2 {
		wa[43] = byte(i)
		wa[44] = byte(i >> 8)
		wb[43] = byte(i + 1)
		wb[44] = byte((i + 1) >> 8)
		_, _ = astrobwtv3.AstroBWTv3_pair(wa[:], wb[:], sa, sb)
	}
}

// BenchmarkHashStage is everything except the final SHA-256: the stage-1 state
// machine and the suffix sort. AstroBWTv3_pair with the hook forced off and
// then subtracted is not available from outside the package, so this uses the
// pair entry point with a hook that does nothing -- the digests are wrong and
// the work up to them is exactly right.
func BenchmarkHashStage(b *testing.B) {
	installForBench(b)
	saved := astrobwtv3.SHA256Pair
	astrobwtv3.SHA256Pair = func(a, c []byte, outA, outB *[32]byte) bool { return true }
	defer func() { astrobwtv3.SHA256Pair = saved }()

	sa := astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
	sb := astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
	defer astrobwtv3.Pool.Put(sa)
	defer astrobwtv3.Pool.Put(sb)
	wa, wb := benchWork(1), benchWork(1)
	for i := 0; i < b.N; i += 2 {
		wa[43] = byte(i)
		wa[44] = byte(i >> 8)
		wb[43] = byte(i + 1)
		wb[44] = byte((i + 1) >> 8)
		_, _ = astrobwtv3.AstroBWTv3_pair(wa[:], wb[:], sa, sb)
	}
}

func installForBench(b *testing.B) {
	b.Helper()
	if astrobwtv3.SuffixSort == nil {
		if note, ok := InstallFastSuffixSort(); !ok {
			b.Logf("%s", note)
		}
	}
}
