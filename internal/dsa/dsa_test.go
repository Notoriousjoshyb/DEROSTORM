package dsa

import (
	"math/rand"
	"sort"
	"testing"
)

func TestKey32AndU64leMatchReference(t *testing.T) {
	// key32, u64le, and blockDiff were hand-inlined off binary.BigEndian /
	// binary.LittleEndian. Any divergence silently reorders the SA, so check
	// the helpers against the stdlib forms directly, including the
	// past-the-end padding key32 applies near EOF.
	rng := rand.New(rand.NewSource(1))
	buf := make([]byte, 2*blockSize+16)
	rng.Read(buf)
	for trial := range 200 {
		n := 1 + rng.Intn(len(buf))
		text := buf[:n]
		for _, p := range []int{0, 1, n / 2, n - keyBytes, n - 1, n} {
			if p < 0 || p > n {
				continue
			}
			var want uint32
			for i := range keyBytes {
				want <<= 8
				if p+i < n {
					want |= uint32(text[p+i])
				}
			}
			if got := key32(text, n, p); got != want {
				t.Fatalf("trial %d key32(%d,%d): got %x want %x", trial, n, p, got, want)
			}
		}
		if n >= 8 {
			p := rng.Intn(n - 7)
			var want uint64
			for i := range 8 {
				want |= uint64(text[p+i]) << (8 * i)
			}
			if got := u64le(text, p); got != want {
				t.Fatalf("trial %d u64le(%d): got %x want %x", trial, p, got, want)
			}
		}
	}
	a := buf[:blockSize]
	b := buf[blockSize : 2*blockSize]
	want, got := 0, blockDiff(a, b)
	for i := range blockSize {
		if a[i] != b[i] {
			want++
		}
	}
	if got != want {
		t.Fatalf("blockDiff random blocks: got %d want %d", got, want)
	}
}

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
	// Exactly two occurrences of a key exercise the direct two-list merge.
	// Include both head orders, long equal prefixes, and zero-padded EOF keys.
	cases := [][]byte{
		[]byte("abc0abc1"),
		[]byte("abc1abc0"),
		[]byte("abcdefghijklmnop1abcdefghijklmnop0"),
		{1, 0, 0, 1, 0, 0, 0},
	}
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
