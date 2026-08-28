package main

import (
	"math/big"
	"math/rand"
	"testing"

	"github.com/deroproject/derohe/cryptography/crypto"
)

// TestTargetMatchesReference asserts Target.Meets is bit-for-bit identical to
// CheckPowHashBig, which is the code path the miner used before.
func TestTargetMatchesReference(t *testing.T) {
	diffs := []string{
		"1", "2", "3", "255", "256", "257",
		"1000", "65535", "65536", "1000000", "80000000",
		"18446744073709551615",                    // 2^64-1
		"18446744073709551616",                    // 2^64
		"340282366920938463463374607431768211456", // 2^128
	}
	r := rand.New(rand.NewSource(12345))

	for _, ds := range diffs {
		d, ok := new(big.Int).SetString(ds, 10)
		if !ok {
			t.Fatalf("bad difficulty %q", ds)
		}
		tg := NewTarget(d)

		// A hash whose little-endian value is exactly the target, plus its
		// neighbours, exercises the equality boundary in every limb.
		var edge []crypto.Hash
		if !tg.all {
			var h crypto.Hash
			putLE(&h, tg.limb)
			edge = append(edge, h)
			for limb := 0; limb < 4; limb++ {
				for _, delta := range []int64{-1, 1} {
					l := tg.limb
					l[limb] = uint64(int64(l[limb]) + delta)
					var hh crypto.Hash
					putLE(&hh, l)
					edge = append(edge, hh)
				}
			}
		}

		cases := edge
		var zero, ones crypto.Hash
		for i := range ones {
			ones[i] = 0xff
		}
		cases = append(cases, zero, ones)
		for i := 0; i < 4000; i++ {
			var h crypto.Hash
			r.Read(h[:])
			// bias towards small little-endian values so hits actually happen
			if i%3 == 0 {
				for k := 8; k < 32; k++ {
					h[k] = 0
				}
			}
			if i%7 == 0 {
				for k := 4; k < 32; k++ {
					h[k] = 0
				}
			}
			cases = append(cases, h)
		}

		for _, h := range cases {
			want := CheckPowHashBig(h, d)
			got := tg.Meets((*[32]byte)(&h))
			if want != got {
				t.Fatalf("difficulty %s hash %x: reference=%v fast=%v", ds, h, want, got)
			}
		}
	}
}

func putLE(h *crypto.Hash, limb [4]uint64) {
	for i := 0; i < 4; i++ {
		v := limb[i]
		for b := 0; b < 8; b++ {
			h[i*8+b] = byte(v >> (8 * b))
		}
	}
}

func TestTargetZeroDifficultyPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on zero difficulty")
		}
	}()
	NewTarget(big.NewInt(0))
}
