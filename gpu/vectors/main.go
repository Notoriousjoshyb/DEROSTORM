// vectors dumps real AstroBWTv3 suffix-array inputs and their CPU-computed
// suffix arrays, so the CUDA kernel can be checked against the CPU byte for
// byte and benchmarked on realistic data rather than random noise.
//
//	go run ./gpu/vectors -n 512 -out gpu/vectors.bin
//
// File format (all little endian):
//
//	magic  "ABWTVEC2"        8 bytes
//	count  uint32            number of vectors
//	then count records of:
//	  n    uint32            text length
//	  text n bytes
//	  sa   n * int32
//	  hash 32 bytes          AstroBWTv3 of the input that produced this record
//
// The inputs are not stored: they are the deterministic sequence built by
// workInput below, which the CUDA test reproduces. Storing the final hash lets
// the GPU be checked end to end -- stage 1, the suffix sort and the closing
// SHA-256 in one comparison -- rather than only at the suffix-array boundary.
// ABWTVEC1 files (no hash field) still load; they just skip that check.
package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"os"

	"github.com/deroproject/derohe/astrobwt/astrobwtv3"
)

func main() {
	n := flag.Int("n", 512, "number of vectors to dump")
	out := flag.String("out", "gpu/vectors.bin", "output file")
	flag.Parse()

	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)

	w.WriteString("ABWTVEC2")
	binary.Write(w, binary.LittleEndian, uint32(*n))

	scratch := astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
	saScratch := astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)

	// The capture hook fires inside the hash, where the text is still owned by
	// the hasher's scratch; copy it out before doing anything else with it.
	var text []byte
	astrobwtv3.CaptureStage1 = func(t []byte) {
		text = append(text[:0], t...)
	}
	defer func() { astrobwtv3.CaptureStage1 = nil }()

	var minLen, maxLen, total uint64
	minLen = ^uint64(0)
	for i := 0; i < *n; i++ {
		work := workInput(i)
		hash := astrobwtv3.AstroBWTv3_scratch(work[:], scratch)

		sa := astrobwtv3.SuffixArray(text, saScratch)
		if len(sa) != len(text) {
			fmt.Fprintf(os.Stderr, "length mismatch %d vs %d\n", len(sa), len(text))
			os.Exit(1)
		}

		binary.Write(w, binary.LittleEndian, uint32(len(text)))
		w.Write(text)
		binary.Write(w, binary.LittleEndian, sa)
		w.Write(hash[:])

		l := uint64(len(text))
		total += l
		if l < minLen {
			minLen = l
		}
		if l > maxLen {
			maxLen = l
		}
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if p := astrobwtv3.RecoveredPanics; p != 0 {
		fmt.Fprintf(os.Stderr, "WARNING: %d hashes aborted internally\n", p)
	}
	fmt.Printf("wrote %d vectors to %s\n", *n, *out)
	fmt.Printf("text length: min %d  max %d  mean %d\n", minLen, maxLen, total/uint64(*n))
}

// workInput builds vector i's 48-byte miner input. The CUDA side reproduces
// this exactly, so the inputs never have to be stored in the file. Any change
// here invalidates existing vector files.
func workInput(i int) [48]byte {
	var work [48]byte
	work[0] = 1
	for k := 8; k < 43; k++ {
		work[k] = byte(k*7 + 11)
	}
	work[43] = byte(i)
	work[44] = byte(i >> 8)
	work[45] = byte(i >> 16)
	return work
}
