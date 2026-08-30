package dsa

import (
	"math/rand"
	"sort"
	"testing"
)

func naiveSA(t []byte) []int32 {
	n := len(t)
	sa := make([]int32, n)
	for i := range sa {
		sa[i] = int32(i)
	}
	sort.Slice(sa, func(i, j int) bool {
		a, b := int(sa[i]), int(sa[j])
		for {
			if a == n {
				return b != n
			}
			if b == n {
				return false
			}
			if t[a] != t[b] {
				return t[a] < t[b]
			}
			a++
			b++
		}
	})
	return sa
}

func TestSuffixArrayMatchesNaive(t *testing.T) {
	var cases [][]byte
	for n := 1; n <= 80; n++ {
		b := make([]byte, n)
		rand.Read(b)
		cases = append(cases, b)
	}
	cases = append(cases, make([]byte, 48))
	ones := make([]byte, 48)
	for i := range ones {
		ones[i] = 0xff
	}
	cases = append(cases, ones)
	// A long near-copy text, which is what stage 1 actually produces.
	long := make([]byte, 256*8+17)
	for i := range long {
		long[i] = byte(i)
	}
	for g := 1; g < 8; g++ {
		copy(long[g*256:(g+1)*256], long[:256])
		long[g*256+int(g)] ^= 0x5a
	}
	cases = append(cases, long)

	for i, in := range cases {
		want := naiveSA(in)
		got := make([]int32, len(in))
		if !SuffixArray(in, got) {
			t.Fatalf("case %d (%d bytes): declined", i, len(in))
		}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("case %d (%d bytes) pos %d: got %d want %d", i, len(in), j, got[j], want[j])
			}
		}
	}
}
