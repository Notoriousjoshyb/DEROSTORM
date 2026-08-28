package main

// The faster suffix sort must produce exactly the hash the built-in sort does.
//
// This is the most important test in the package. A wrong suffix sort does not
// crash and does not look broken: AstroBWTv3 would keep returning 32 bytes, they
// would simply never meet a target, and the miner would sit there at full
// hashrate finding nothing. So every hash is compared against the same input run
// through the built-in sort, over the shapes of input that actually occur.

import (
	"crypto/rand"
	"testing"

	"github.com/deroproject/derohe/astrobwt/astrobwtv3"
	"github.com/deroproject/derohe/block"
)

// withSuffixSort runs f with the hook set to sorter, and restores it after.
func withSuffixSort(t *testing.T, sorter func([]byte, []int32) bool, f func()) {
	t.Helper()
	saved := astrobwtv3.SuffixSort
	astrobwtv3.SuffixSort = sorter
	defer func() { astrobwtv3.SuffixSort = saved }()
	f()
}

func TestFastSuffixSortGivesTheSameHash(t *testing.T) {
	note, ok := InstallFastSuffixSort()
	// Restore immediately: the rest of the package's tests should run against
	// whatever they would normally.
	sorter := astrobwtv3.SuffixSort
	astrobwtv3.SuffixSort = nil
	if !ok {
		t.Skipf("no faster suffix sort here: %s", note)
	}
	t.Log(note)
	if sorter == nil {
		t.Fatal("InstallFastSuffixSort reported success but installed nothing")
	}

	scratch := astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
	defer astrobwtv3.Pool.Put(scratch)

	before := astrobwtv3.RecoveredPanics

	// Miniblock-shaped inputs across a nonce sweep, which is what mining does,
	// plus random ones and every short length, which is what finds the edges.
	var cases [][]byte

	work := make([]byte, block.MINIBLOCK_SIZE)
	work[0] = 1
	for i := 8; i < 43; i++ {
		work[i] = byte(i*7 + 11)
	}
	for _, nonce := range []uint32{0, 1, 2, 3, 255, 256, 65535, 0x5f3759df, 0xfffffffe, 0xffffffff} {
		w := append([]byte(nil), work...)
		w[43] = byte(nonce >> 24)
		w[44] = byte(nonce >> 16)
		w[45] = byte(nonce >> 8)
		w[46] = byte(nonce)
		cases = append(cases, w)
	}
	for i := 0; i < 200; i++ {
		w := append([]byte(nil), work...)
		w[43] = byte(i)
		w[44] = byte(i >> 8)
		cases = append(cases, w)
	}
	for n := 1; n <= 80; n++ {
		b := make([]byte, n)
		rand.Read(b)
		cases = append(cases, b)
	}
	for i := 0; i < 40; i++ {
		b := make([]byte, 48)
		rand.Read(b)
		cases = append(cases, b)
	}
	// All-zero and all-one inputs: degenerate texts are where a suffix sort is
	// most likely to differ, because every suffix ties with every other.
	cases = append(cases, make([]byte, 48))
	ones := make([]byte, 48)
	for i := range ones {
		ones[i] = 0xff
	}
	cases = append(cases, ones)

	for i, in := range cases {
		var want, got [32]byte
		withSuffixSort(t, nil, func() {
			want = astrobwtv3.AstroBWTv3_scratch(in, scratch)
		})
		withSuffixSort(t, sorter, func() {
			got = astrobwtv3.AstroBWTv3_scratch(in, scratch)
		})
		if got != want {
			t.Fatalf("case %d (%d bytes): hash differs\n libsais %x\n built-in %x", i, len(in), got, want)
		}
	}

	if p := astrobwtv3.RecoveredPanics - before; p != 0 {
		t.Fatalf("%d hash(es) panicked and returned a falsified result", p)
	}
	t.Logf("%d inputs, every hash identical to the built-in sort", len(cases))
}

// A sorter that declines must be fallen back on, not trusted. This is what makes
// "the library could not do this one" safe.
func TestDecliningSorterFallsBackToTheBuiltIn(t *testing.T) {
	scratch := astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
	defer astrobwtv3.Pool.Put(scratch)

	in := make([]byte, block.MINIBLOCK_SIZE)
	in[0] = 1
	for i := 8; i < 43; i++ {
		in[i] = byte(i * 3)
	}

	var want [32]byte
	withSuffixSort(t, nil, func() { want = astrobwtv3.AstroBWTv3_scratch(in, scratch) })

	called := 0
	decline := func(text []byte, sa []int32) bool {
		called++
		// Scribble on the output as well, to prove the fallback really recomputes
		// it rather than trusting whatever is in the buffer.
		for i := range sa {
			sa[i] = -1
		}
		return false
	}

	var got [32]byte
	withSuffixSort(t, decline, func() { got = astrobwtv3.AstroBWTv3_scratch(in, scratch) })

	if called == 0 {
		t.Fatal("the hook was never called")
	}
	if got != want {
		t.Fatalf("declining sorter changed the hash\n got  %x\n want %x", got, want)
	}
}

// The paired hash must give exactly the hashes the single one gives.
//
// The pairing only reschedules the final SHA-256, so if this ever disagrees the
// digest is wrong, and a wrong digest is invisible: the miner would run at full
// speed and never find a share. Both nonces are checked, over inputs whose
// suffix arrays are deliberately different lengths -- the pair splits into
// common whole blocks and a per-message remainder, so unequal lengths are the
// case that exercises the seam.
func TestPairedHashGivesTheSameHashes(t *testing.T) {
	note, ok := InstallFastSuffixSort()
	sorter := astrobwtv3.SuffixSort
	pairer := astrobwtv3.SHA256Pair
	astrobwtv3.SuffixSort = nil
	astrobwtv3.SHA256Pair = nil
	if !ok {
		t.Skipf("no faster suffix sort here: %s", note)
	}
	if !PairedSHAAvailable() || pairer == nil {
		t.Skipf("no paired SHA-256 here: %s", note)
	}
	t.Log(note)

	sa := astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
	sb := astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
	sc := astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
	defer astrobwtv3.Pool.Put(sa)
	defer astrobwtv3.Pool.Put(sb)
	defer astrobwtv3.Pool.Put(sc)

	before := astrobwtv3.RecoveredPanics

	var cases [][]byte
	base := make([]byte, block.MINIBLOCK_SIZE)
	base[0] = 1
	rand.Read(base[8:])
	for nonce := 0; nonce < 240; nonce++ {
		w := append([]byte(nil), base...)
		w[block.MINIBLOCK_SIZE-5] = byte(nonce)
		w[block.MINIBLOCK_SIZE-4] = byte(nonce >> 8)
		cases = append(cases, w)
	}
	for i := 0; i < 40; i++ {
		w := make([]byte, block.MINIBLOCK_SIZE)
		rand.Read(w)
		w[0] = 1
		cases = append(cases, w)
	}
	for n := 1; n <= 40; n++ {
		w := make([]byte, n)
		rand.Read(w)
		cases = append(cases, w)
	}

	// Reference hashes first, with both hooks off, so the comparison is against
	// the code that has been shipping and not against another new path.
	want := make([][32]byte, len(cases))
	for i, w := range cases {
		want[i] = astrobwtv3.AstroBWTv3_scratch(w, sc)
	}

	astrobwtv3.SuffixSort = sorter
	astrobwtv3.SHA256Pair = pairer
	defer func() {
		astrobwtv3.SuffixSort = nil
		astrobwtv3.SHA256Pair = nil
	}()

	// Every input paired with a different one, so each is exercised in both
	// positions and against a partner of a different suffix-array length.
	for i := range cases {
		j := (i + 7) % len(cases)
		gotA, gotB := astrobwtv3.AstroBWTv3_pair(cases[i], cases[j], sa, sb)
		if gotA != want[i] {
			t.Fatalf("pair(%d,%d) first hash: got %x want %x", i, j, gotA, want[i])
		}
		if gotB != want[j] {
			t.Fatalf("pair(%d,%d) second hash: got %x want %x", i, j, gotB, want[j])
		}
	}

	if p := astrobwtv3.RecoveredPanics - before; p != 0 {
		t.Fatalf("%d hashes panicked and were falsified: %v", p, astrobwtv3.LastPanic)
	}
	t.Logf("%d inputs, each hashed in both positions of a pair", len(cases))
}
