// Copyright 2017-2021 DERO Project. All rights reserved.
// Use of this source code in any form is governed by RESEARCH license.
// license can be found in the LICENSE file.

package main

// Allocation-free difficulty check for the mining hot path.
//
// CheckPowHashBig (difficulty.go) is the reference: it reverses the 32-byte PoW
// hash, turns it into a big.Int, divides 2^256 by the difficulty to get the
// target, and compares. That is four big.Int allocations plus a 256-bit
// division for every single hash attempt.
//
// The target only changes when the job changes, so it can be computed once per
// job and kept as four uint64 limbs. Reversing the hash bytes and reading them
// big-endian is exactly the same as reading the original bytes as a 256-bit
// little-endian integer, so the per-hash work collapses to one 64-bit load and
// one compare in the overwhelmingly common reject case.
//
// Target.Meets is asserted bit-for-bit identical to CheckPowHashBig in
// target_test.go.

import "encoding/binary"
import "math/big"

// Target is floor(2^256 / difficulty) as four 64-bit limbs, limb[3] most
// significant.
type Target struct {
	limb [4]uint64
	// all is set when the target does not fit in 256 bits (difficulty 1), in
	// which case every hash meets it, matching the big.Int comparison.
	all bool
}

// NewTarget mirrors ConvertIntegerDifficultyToBig, including its panic on a
// zero difficulty.
func NewTarget(difficulty *big.Int) Target {
	if difficulty.Cmp(bigZero) == 0 {
		panic("difficulty can never be zero")
	}

	t := new(big.Int).Div(oneLsh256, difficulty)

	var tg Target
	if t.BitLen() > 256 {
		tg.all = true
		return tg
	}

	var buf [32]byte
	b := t.Bytes() // big-endian, minimal length
	copy(buf[32-len(b):], b)
	tg.limb[3] = binary.BigEndian.Uint64(buf[0:8])
	tg.limb[2] = binary.BigEndian.Uint64(buf[8:16])
	tg.limb[1] = binary.BigEndian.Uint64(buf[16:24])
	tg.limb[0] = binary.BigEndian.Uint64(buf[24:32])
	return tg
}

// Meets reports whether the PoW hash satisfies the target, i.e. whether
// CheckPowHashBig would have returned true.
//
// The hash is a 256-bit little-endian integer (see the note above), so limb 3
// covers h[24:32] and is the most significant.
func (t *Target) Meets(h *[32]byte) bool {
	if t.all {
		return true
	}
	v := binary.LittleEndian.Uint64(h[24:32])
	if v != t.limb[3] {
		return v < t.limb[3]
	}
	v = binary.LittleEndian.Uint64(h[16:24])
	if v != t.limb[2] {
		return v < t.limb[2]
	}
	v = binary.LittleEndian.Uint64(h[8:16])
	if v != t.limb[1] {
		return v < t.limb[1]
	}
	return binary.LittleEndian.Uint64(h[0:8]) <= t.limb[0]
}
