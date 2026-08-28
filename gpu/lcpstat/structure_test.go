package main

// How repetitive is a stage-1 text, really?
//
// The suffix sort is 84% of a CPU hash, and the fastest known way to make it
// cheaper is not to sort faster but to sort less. AstroBWTv3's text is the
// 256-byte state written out after every one of ~277 iterations, and each
// iteration changes at most 32 of those bytes -- so on paper consecutive blocks
// are near-copies, and a suffix array builder that knew which blocks repeat
// could skip most of the work.
//
// That is the claim a competing miner is built on. Whether it holds is a
// property of the data, not of the argument, so this measures it on the same
// 512 real texts everything else here is checked against:
//
//	go test ./gpu/lcpstat -run Structure -v
//
// The two numbers that decide it are the share of 256-byte blocks identical to
// the one before, and the share of bytes that differ when they are not.

import (
	"bufio"
	"encoding/binary"
	"os"
	"testing"
)

func TestStructureOfRealTexts(t *testing.T) {
	f, err := os.Open("../vectors.bin")
	if err != nil {
		t.Skipf("no vectors.bin: %v", err)
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)

	magic := make([]byte, 8)
	if _, err := readFull(r, magic); err != nil {
		t.Fatal(err)
	}
	v2 := string(magic) == "ABWTVEC2"
	if !v2 && string(magic) != "ABWTVEC1" {
		t.Fatalf("%s is not a vector file", "vectors.bin")
	}
	var count uint32
	if err := binary.Read(r, binary.LittleEndian, &count); err != nil {
		t.Fatal(err)
	}

	var (
		texts       int
		blocks      int
		identical   int
		diffBytes   int64
		diffPairs   int64
		runsTotal   int64
		longestRun  int
		byteChanged [256]int64
	)

	for i := uint32(0); i < count; i++ {
		var n uint32
		if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
			t.Fatal(err)
		}
		text := make([]byte, n)
		if _, err := readFull(r, text); err != nil {
			t.Fatal(err)
		}
		sa := make([]int32, n)
		if err := binary.Read(r, binary.LittleEndian, sa); err != nil {
			t.Fatal(err)
		}
		if v2 {
			if _, err := readFull(r, make([]byte, 32)); err != nil {
				t.Fatal(err)
			}
		}

		texts++
		nb := int(n) / 256
		blocks += nb
		run := 1
		for b := 1; b < nb; b++ {
			cur := text[b*256 : b*256+256]
			prev := text[(b-1)*256 : (b-1)*256+256]

			d := 0
			for k := 0; k < 256; k++ {
				if cur[k] != prev[k] {
					d++
					byteChanged[k]++
				}
			}
			if d == 0 {
				identical++
				run++
			} else {
				diffBytes += int64(d)
				diffPairs++
				runsTotal++
				if run > longestRun {
					longestRun = run
				}
				run = 1
			}
		}
		if run > longestRun {
			longestRun = run
		}
	}

	pairs := blocks - texts // consecutive pairs across all texts
	t.Logf("texts %d, 256-byte blocks %d, consecutive pairs %d", texts, blocks, pairs)
	t.Logf("blocks identical to the previous one: %d (%.2f%%)",
		identical, 100*float64(identical)/float64(pairs))
	if diffPairs > 0 {
		t.Logf("when they differ, %.1f of 256 bytes differ on average",
			float64(diffBytes)/float64(diffPairs))
	}
	t.Logf("longest run of identical blocks: %d", longestRun)

	// Byte 255 is xored every iteration, so it changes unless the two operand
	// bytes happen to be equal. If it changes essentially always, no two
	// consecutive blocks can ever be identical and the whole run idea is dead.
	t.Logf("byte 255 changed in %d of %d pairs (%.2f%%)",
		byteChanged[255], pairs, 100*float64(byteChanged[255])/float64(pairs))
	t.Logf("byte 0 changed in %d of %d pairs (%.2f%%)",
		byteChanged[0], pairs, 100*float64(byteChanged[0])/float64(pairs))
}

// TestSharedColumnsAcrossRuns bounds what the competing miner's technique can
// save here.
//
// It does not need whole blocks to repeat -- that measurement above shows none
// do. It works on *columns*: for a run of consecutive 256-byte blocks, an
// offset where every block in the run holds the same byte gives a set of
// suffixes whose relative order is fixed by the order of the blocks, and which
// therefore never have to be compared. The saving is roughly the share of
// columns that are constant across a run.
//
// So the question is how that share falls as runs get longer, and the answer is
// a property of stage 1: each iteration rewrites at most 32 bytes of the state,
// except when RC4 fires and rewrites all 256.
func TestSharedColumnsAcrossRuns(t *testing.T) {
	f, err := os.Open("../vectors.bin")
	if err != nil {
		t.Skipf("no vectors.bin: %v", err)
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)

	magic := make([]byte, 8)
	if _, err := readFull(r, magic); err != nil {
		t.Fatal(err)
	}
	v2 := string(magic) == "ABWTVEC2"
	var count uint32
	if err := binary.Read(r, binary.LittleEndian, &count); err != nil {
		t.Fatal(err)
	}

	spans := []int{2, 3, 4, 6, 8, 16}
	shared := make([]float64, len(spans))
	windows := make([]float64, len(spans))

	const texts = 64 // enough to be stable; this is O(texts * blocks * 256 * span)
	for i := uint32(0); i < count && i < texts; i++ {
		var n uint32
		if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
			t.Fatal(err)
		}
		text := make([]byte, n)
		if _, err := readFull(r, text); err != nil {
			t.Fatal(err)
		}
		sa := make([]int32, n)
		if err := binary.Read(r, binary.LittleEndian, sa); err != nil {
			t.Fatal(err)
		}
		if v2 {
			if _, err := readFull(r, make([]byte, 32)); err != nil {
				t.Fatal(err)
			}
		}

		nb := int(n) / 256
		for si, span := range spans {
			for b := 0; b+span <= nb; b++ {
				eq := 0
				for k := 0; k < 256; k++ {
					c := text[b*256+k]
					same := true
					for g := 1; g < span; g++ {
						if text[(b+g)*256+k] != c {
							same = false
							break
						}
					}
					if same {
						eq++
					}
				}
				shared[si] += float64(eq) / 256
				windows[si]++
			}
		}
	}

	// Skip the rest of the file; only the first `texts` were read.
	t.Logf("run length -> share of the 256 columns constant across it")
	for si, span := range spans {
		t.Logf("  %2d blocks   %5.1f%%", span, 100*shared[si]/windows[si])
	}
}
