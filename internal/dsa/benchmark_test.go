package dsa_test

import (
	"testing"

	"github.com/deroproject/derohe/astrobwt/astrobwtv3"
	"github.com/notoriousjoshyb/derostorm/internal/dsa"
)

// BenchmarkSuffixArray uses real stage-1 texts so portable builds can measure
// the descriptor sort on the same workload on macOS, Linux, and Windows.
func BenchmarkSuffixArray(b *testing.B) {
	const vectors = 32
	texts := make([][]byte, 0, vectors)
	savedCapture := astrobwtv3.CaptureStage1
	defer func() { astrobwtv3.CaptureStage1 = savedCapture }()
	astrobwtv3.CaptureStage1 = func(text []byte) {
		texts = append(texts, append([]byte(nil), text...))
	}
	for i := range vectors {
		var work [48]byte
		work[0] = 1
		for j := 8; j < len(work); j++ {
			work[j] = byte(i*29 + j*17)
		}
		_ = astrobwtv3.AstroBWTv3(work[:])
	}
	astrobwtv3.CaptureStage1 = savedCapture
	if len(texts) != vectors {
		b.Fatalf("captured %d stage-1 texts, want %d", len(texts), vectors)
	}

	sa := make([]int32, 256*384)
	for _, text := range texts {
		if !dsa.SuffixArray(text, sa) {
			b.Fatal("portable descriptor sort declined a benchmark vector")
		}
	}
	b.ResetTimer()
	for i := range b.N {
		if !dsa.SuffixArray(texts[i%len(texts)], sa) {
			b.Fatal("portable descriptor sort declined a benchmark vector")
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "sorts/s")
}
