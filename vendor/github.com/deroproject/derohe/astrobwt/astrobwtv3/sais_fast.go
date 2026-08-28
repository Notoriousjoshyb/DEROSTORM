package astrobwtv3

// sais_fast.go: a drop-in faster driver for the SA-IS suffix sort used by
// AstroBWTv3. It produces byte-identical suffix arrays to sais_8_32 /
// sais_32 in sais.go / sais2.go -- the suffix array of a string is unique, so
// this cannot change the PoW result.
//
// The win is purely redundant work removal. The stock implementation walks the
// whole text three times with the identical branchy "LMS-substring iterator"
// (placeLMS_*, length_*, unmap_*). Profiling AstroBWTv3 showed those three
// walks at ~17% of total hash time. Here placeLMS records the LMS-substring
// start indexes into a side buffer while it walks, and length/unmap then
// iterate that buffer (numLMS entries, ~n/3) instead of re-walking n text
// positions. unmap additionally drops a whole write pass over sa by indexing
// the side buffer directly.
//
// The side buffer is consumed as a stack: level 0 takes lms[:numLMS] and hands
// lms[numLMS:] to the recursion. Each level's numLMS is at most half its text
// length, so the total needed is bounded by len(text). If the buffer is ever
// too small we fall back to the stock functions, which need no side buffer.

// text_32_fast is the drop-in replacement for text_32_0alloc.
// lms must have at least len(text) entries for the fast path to be taken.
//
// tmp is the frequency/bucket workspace. The stock code passes a 512-entry
// stack array, which is all level 0 needs (alphabet 256) but leaves the
// recursion levels to scavenge a slice out of the middle of sa. When that
// scavenged slice is smaller than 2*textMax, freq caching switches off and
// every one of the six bucketMin/bucketMax calls per level rescans the text.
// Passing a buffer big enough for the deepest level keeps caching on
// throughout. 2*(len(text)/2+1) entries always suffice: textMax at any
// recursion level is at most that level's numLMS, which is at most half the
// level's text length, and text lengths only shrink going down.
func text_32_fast(text []byte, sa []int32, lms []int32, tmp []int32) {
	if int(int32(len(text))) != len(text) || len(text) != len(sa) {
		panic("suffixarray: misuse of text_32_fast")
	}
	for i := range sa {
		sa[i] = 0
	}
	if len(tmp) < 2*256 {
		var memory [2 * 256]int32
		tmp = memory[:]
	}
	sais_fast_8_32(text, 256, sa, tmp, lms)
}

func sais_fast_8_32(text []byte, textMax int, sa, tmp, lms []int32) {
	if len(sa) != len(text) || len(tmp) < int(textMax) {
		panic("suffixarray: misuse of sais_fast_8_32")
	}
	if len(lms) < len(text) {
		// Not enough side storage: use the stock implementation.
		sais_8_32(text, textMax, sa, tmp)
		return
	}

	if len(text) == 0 {
		return
	}
	if len(text) == 1 {
		sa[0] = 0
		return
	}

	var freq, bucket []int32
	if len(tmp) >= 2*textMax {
		freq, bucket = tmp[:textMax], tmp[textMax:2*textMax]
		freq[0] = -1 // mark as uninitialized
	} else {
		freq, bucket = nil, tmp[:textMax]
	}

	numLMS := placeLMSrec_8_32(text, sa, freq, bucket, lms)
	if numLMS <= 1 {
		// 0 or 1 items are already sorted. Do nothing.
	} else {
		induceSubL_8_32(text, sa, freq, bucket)
		induceSubS_8_32(text, sa, freq, bucket)
		lengthFromLMS_8_32(text, sa, lms[:numLMS])
		maxID := assignID_8_32(text, sa, numLMS)
		if maxID < numLMS {
			mapFromLMS_32(sa, lms[:numLMS])
			recurse_fast_32(sa, tmp, lms[numLMS:], numLMS, maxID)
			unmapFromLMS_32(sa, lms[:numLMS])
		} else {
			copy(sa, sa[len(sa)-numLMS:])
		}
		expand_8_32(text, freq, bucket, sa, numLMS)
	}
	induceL_8_32(text, sa, freq, bucket)
	induceS_8_32(text, sa, freq, bucket)

	tmp[0] = -1 // mark for caller that we overwrote tmp
}

func sais_fast_32(text []int32, textMax int, sa, tmp, lms []int32) {
	if len(sa) != len(text) || len(tmp) < int(textMax) {
		panic("suffixarray: misuse of sais_fast_32")
	}
	if len(lms) < len(text) {
		sais_32(text, textMax, sa, tmp)
		return
	}

	if len(text) == 0 {
		return
	}
	if len(text) == 1 {
		sa[0] = 0
		return
	}

	var freq, bucket []int32
	if len(tmp) >= 2*textMax {
		freq, bucket = tmp[:textMax], tmp[textMax:2*textMax]
		freq[0] = -1
	} else {
		freq, bucket = nil, tmp[:textMax]
	}

	numLMS := placeLMSrec_32(text, sa, freq, bucket, lms)
	if numLMS <= 1 {
	} else {
		induceSubL_32(text, sa, freq, bucket)
		induceSubS_32(text, sa, freq, bucket)
		lengthFromLMS_32(sa, lms[:numLMS])
		maxID := assignID_32(text, sa, numLMS)
		if maxID < numLMS {
			mapFromLMS_32(sa, lms[:numLMS])
			recurse_fast_32(sa, tmp, lms[numLMS:], numLMS, maxID)
			unmapFromLMS_32(sa, lms[:numLMS])
		} else {
			copy(sa, sa[len(sa)-numLMS:])
		}
		expand_32(text, freq, bucket, sa, numLMS)
	}
	induceL_32(text, sa, freq, bucket)
	induceS_32(text, sa, freq, bucket)

	tmp[0] = -1
}

// placeLMSrec_8_32 replaces placeLMS_8_32. It does the same two things --
// record every LMS-substring start index and bucket it into sa -- but in two
// passes instead of one.
//
// The reason is branch misprediction. The stock single pass tests
// "text[i] < text[i+1]" and then "text[i] > text[i+1] && isTypeS" on data that
// is, here, the output of RC4 and a bit-mixing loop: both tests are close to a
// coin flip, so nearly every text position costs a mispredict. Measured at
// ~8.8 cycles per text byte on Zen 5, against ~1 cycle of actual work.
//
// Pass 1 is branchless. The three comparison outcomes become all-ones/all-zero
// masks, isTypeS becomes a mask threaded through a 1-cycle AND/OR chain, and
// the emit is a branchless compress-store: the index is written to
// lms[numLMS] unconditionally and numLMS only advances when it was really an
// LMS start. Pass 2 then walks the numLMS recorded starts (about n/3 of them)
// and does the bucket scatter with no conditional at all.
func placeLMSrec_8_32(text []byte, sa, freq, bucket, lms []int32) int {
	bucketMax_8_32(text, freq, bucket)
	bucket = bucket[:256] // eliminate bounds check for bucket[c1] below

	// Pass 1: branchless detection of LMS-substring starts, descending index.
	numLMS := 0
	c0 := int32(0) // text[i+1]; starts as the untruthful sentinel, as in the original
	s := uint32(0) // isTypeS as an all-ones/all-zero mask
	for i := len(text) - 1; i >= 0; i-- {
		c1 := c0
		c0 = int32(text[i])

		d := c0 - c1
		u := uint32(d)
		ltm := uint32(d >> 31)              // ^0 when text[i] < text[i+1]
		nzm := uint32(int32(u|(0-u)) >> 31) // ^0 when they differ
		eqm := ^nzm                         // ^0 when they are equal
		emit := nzm & ^ltm & s              // ^0 when text[i] > text[i+1] and i+1 is type S
		s = ltm | (eqm & s)                 // isTypeS(i) = lt || (eq && isTypeS(i+1))

		lms[numLMS] = int32(i + 1)
		numLMS += int(emit & 1)
	}

	if numLMS == 0 {
		return 0
	}

	// Pass 2: bucket the recorded starts, in the same order the original walk
	// produced them, so sa ends up byte-identical.
	var lastB int32
	for _, jj := range lms[:numLMS] {
		c := text[jj]
		b := bucket[c] - 1
		bucket[c] = b
		sa[b] = jj
		lastB = b
	}

	if numLMS > 1 {
		sa[lastB] = 0
	}
	return numLMS
}

// placeLMSrec_32 is placeLMSrec_8_32 for an int32 text. Characters here are
// dense IDs from the level above, so the difference of two of them cannot
// overflow int32 and the same mask arithmetic applies.
func placeLMSrec_32(text []int32, sa, freq, bucket, lms []int32) int {
	bucketMax_32(text, freq, bucket)

	numLMS := 0
	c0 := int32(0)
	s := uint32(0)
	for i := len(text) - 1; i >= 0; i-- {
		c1 := c0
		c0 = text[i]

		d := c0 - c1
		u := uint32(d)
		ltm := uint32(d >> 31)
		nzm := uint32(int32(u|(0-u)) >> 31)
		eqm := ^nzm
		emit := nzm & ^ltm & s
		s = ltm | (eqm & s)

		lms[numLMS] = int32(i + 1)
		numLMS += int(emit & 1)
	}

	if numLMS == 0 {
		return 0
	}

	var lastB int32
	for _, jj := range lms[:numLMS] {
		c := text[jj]
		b := bucket[c] - 1
		bucket[c] = b
		sa[b] = jj
		lastB = b
	}

	if numLMS > 1 {
		sa[lastB] = 0
	}
	return numLMS
}

// lengthFromLMS_8_32 replaces length_8_32. lms holds the LMS-substring start
// indexes in descending order, which is exactly the order the original
// backward text walk visited them, so `end` threads through identically.
//
// The original maintained cx incrementally: on reaching start j' with previous
// start j (end == j+1), cx held the packed bytes text[j'], text[j'+1], ...,
// text[j] with text[j'] in the low byte -- that is, exactly `code` bytes where
// code == end-j'. It is only consulted when code <= 4, so it can be rebuilt
// directly from the text with at most four loads.
func lengthFromLMS_8_32(text []byte, sa []int32, lms []int32) {
	n := uint32(len(text))
	end := 0
	for _, jj := range lms {
		j := int(jj)
		var code int32
		if end == 0 {
			code = 0
		} else {
			code = int32(end - j)
			if code <= 4 {
				cx := uint32(0)
				for m := int(code) - 1; m >= 0; m-- {
					cx = cx<<8 | uint32(text[j+m]+1)
				}
				if ^cx >= n {
					code = int32(^cx)
				}
			}
		}
		sa[j>>1] = code
		end = j + 1
	}
}

// lengthFromLMS_32 replaces length_32, which stores plain lengths only
// (the packed-text encoding is byte-text-only).
func lengthFromLMS_32(sa []int32, lms []int32) {
	end := 0
	for _, jj := range lms {
		j := int(jj)
		var code int32
		if end != 0 {
			code = int32(end - j)
		}
		sa[j>>1] = code
		end = j + 1
	}
}

// unmapFromLMS_32 replaces unmap_8_32 / unmap_32. The stock version rebuilt the
// inverse map by re-walking the text into sa[len(sa)-numLMS:], then applied it.
// lms already holds those indexes, in descending order, so the stock unmap[k]
// equals lms[numLMS-1-k] and the rebuild pass disappears entirely.
func unmapFromLMS_32(sa []int32, lms []int32) {
	numLMS := len(lms)
	last := numLMS - 1
	sa = sa[:numLMS]
	for i, v := range sa {
		sa[i] = lms[last-int(v)]
	}
}

// mapFromLMS_32 replaces map_32. The stock version scanned all of sa[:n/2+1]
// testing every slot for a nonzero ID, then compacted the survivors into the
// top of sa. The IDs live at sa[j>>1] for each LMS start j, and lms already
// lists exactly those j (descending), so the survivors can be gathered
// directly: no scan of the empty slots and no unpredictable branch.
//
// Aliasing is safe. Step k reads sa[lms[k]>>1] and writes sa[len(sa)-1-k].
// LMS starts are at least 2 apart, so the (k+1)-th largest satisfies
// lms[k] <= len(sa)-1-2k and hence lms[k]>>1 <= (len(sa)-1)/2 - k, which is
// strictly below len(sa)-1-k for len(sa) > 1. Every read index therefore sits
// below the current write index, and write indices only descend from there,
// so no write can clobber a slot a later step still needs. This mirrors the
// same argument that makes the stock descending compaction safe.
func mapFromLMS_32(sa []int32, lms []int32) {
	top := len(sa) - 1
	for k, jj := range lms {
		sa[top-k] = sa[jj>>1] - 1
	}
}

// recurse_fast_32 mirrors recurse_32 but threads the LMS side buffer down.
func recurse_fast_32(sa, oldTmp, lms []int32, numLMS, maxID int) {
	dst, saTmp, text := sa[:numLMS], sa[numLMS:len(sa)-numLMS], sa[len(sa)-numLMS:]

	tmp := oldTmp
	if len(tmp) < len(saTmp) {
		tmp = saTmp
	}
	if len(tmp) < numLMS {
		n := maxID
		if n < numLMS/2 {
			n = numLMS / 2
		}
		tmp = make([]int32, n)
	}

	for i := range dst {
		dst[i] = 0
	}
	sais_fast_32(text, maxID, dst, tmp, lms)
}
