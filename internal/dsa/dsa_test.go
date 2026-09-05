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

func TestBlockDiffCountsEveryChangedByte(t *testing.T) {
	var a, b [blockSize]byte
	for differing := range blockSize + 1 {
		clear(b[:])
		for i := range differing {
			b[i] = 1
		}
		if got := blockDiff(a[:], b[:]); got != differing {
			t.Fatalf("%d changed bytes: got %d", differing, got)
		}
	}
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
	// Exact one- through four-block inputs exercise every local run path.
	runBase := make([]byte, blockSize)
	rand.Read(runBase)
	for blocks := 1; blocks <= 4; blocks++ {
		input := make([]byte, blocks*blockSize)
		for g := range blocks {
			copy(input[g*blockSize:], runBase)
			if g != 0 {
				input[g*blockSize+g*37] ^= byte(0x40 + g)
			}
		}
		cases = append(cases, input)
	}
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

func TestSuffixArraySharedColumnOrders(t *testing.T) {
	// Cover local one-/two-block orders, rank sorting, insertion sorting and
	// counting sorting. Repeated keys at different columns must merge by the
	// key alone, while reconstruction must retain each descriptor's column.
	for _, blocks := range []int{1, 2, 4, 17, 49} {
		input := make([]byte, blocks*blockSize+7)
		for i := range input {
			input[i] = byte((i % 16) * 13)
		}
		for block := range blocks {
			// Long constant spans alternate with differing prepends. Keys
			// become uniform again when those differing bytes slide out.
			for _, col := range []int{1, 32, 64, 127, 253} {
				input[block*blockSize+col] ^= byte(block*19 + col)
			}
		}
		want := naiveSA(input)
		got := make([]int32, len(input))
		if !SuffixArray(input, got) {
			t.Fatalf("%d blocks: sorter declined", blocks)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%d blocks, position %d: got %d want %d", blocks, i, got[i], want[i])
			}
		}
	}
}
