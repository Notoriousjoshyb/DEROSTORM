// lcpstat measures how hard the real AstroBWTv3 texts are to sort by prefix
// doubling, before any of it is written in CUDA.
//
//	go run ./gpu/lcpstat -in gpu/vectors.bin
//
// Prefix doubling resolves every suffix whose longest common prefix with its
// neighbours is under k, and doubles k each round. So the round count is
// ceil(log2(maxLCP)) + 1, and the work per round is proportional to how many
// suffixes are still tied at that k. Both come straight out of the LCP array,
// which the stored suffix arrays give for free.
//
// This matters because the texts are not random. Stage 1 appends the 256-byte
// state after every iteration and most iterations change at most 32 of those
// bytes, so consecutive 256-byte blocks are near-copies and long repeats are
// the norm rather than the exception. Guessing the round count from "it is RC4
// noise" would be badly wrong; this measures it.
package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"math/bits"
	"os"
	"sort"
)

func main() {
	in := flag.String("in", "gpu/vectors.bin", "vector file")
	limit := flag.Int("n", 0, "stop after this many vectors (0 = all)")
	flag.Parse()

	f, err := os.Open(*in)
	if err != nil {
		die(err)
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)

	magic := make([]byte, 8)
	if _, err := readFull(r, magic); err != nil {
		die(err)
	}
	v2 := string(magic) == "ABWTVEC2"
	if !v2 && string(magic) != "ABWTVEC1" {
		die(fmt.Errorf("%s is not a vector file", *in))
	}
	var count uint32
	if err := binary.Read(r, binary.LittleEndian, &count); err != nil {
		die(err)
	}
	if *limit > 0 && *limit < int(count) {
		count = uint32(*limit)
	}

	rounds := map[int]*roundStat{}

	var (
		maxLCP    int
		sumLCP    float64
		sumN      float64
		maxRounds int
		roundHist = map[int]int{}
		// unresolved[r] counts suffixes whose LCP with a neighbour is still at
		// least 2^r, i.e. the ones round r would still have to sort.
		unresolved = make([]float64, 40)
		lcpTop     []int
	)

	for i := uint32(0); i < count; i++ {
		var n uint32
		if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
			die(err)
		}
		text := make([]byte, n)
		if _, err := readFull(r, text); err != nil {
			die(err)
		}
		sa := make([]int32, n)
		if err := binary.Read(r, binary.LittleEndian, sa); err != nil {
			die(err)
		}
		if v2 {
			if _, err := readFull(r, make([]byte, 32)); err != nil {
				die(err)
			}
		}

		lcp := kasai(text, sa)
		accumulateRounds(lcp, rounds)

		// A suffix is unresolved at k when it shares a k-prefix with the
		// neighbour above or below it in the suffix array.
		for p := 0; p < int(n); p++ {
			l := 0
			if p > 0 && int(lcp[p]) > l {
				l = int(lcp[p])
			}
			if p+1 < int(n) && int(lcp[p+1]) > l {
				l = int(lcp[p+1])
			}
			if l > maxLCP {
				maxLCP = l
			}
			sumLCP += float64(l)
			for r := 0; r < len(unresolved); r++ {
				if l >= 1<<r {
					unresolved[r]++
				} else {
					break
				}
			}
		}
		sumN += float64(n)

		vecMax := 0
		for _, l := range lcp {
			if int(l) > vecMax {
				vecMax = int(l)
			}
		}
		lcpTop = append(lcpTop, vecMax)
		rounds := roundsFor(vecMax)
		roundHist[rounds]++
		if rounds > maxRounds {
			maxRounds = rounds
		}
	}

	sort.Ints(lcpTop)
	fmt.Printf("\n  prefix-doubling cost on %d real texts\n\n", count)
	fmt.Printf("  mean text length      %.0f\n", sumN/float64(count))
	fmt.Printf("  mean LCP per suffix   %.1f bytes\n", sumLCP/sumN)
	fmt.Printf("  worst LCP seen        %d bytes\n", maxLCP)
	fmt.Printf("  per-text worst LCP    median %d, p90 %d, max %d\n\n",
		lcpTop[len(lcpTop)/2], lcpTop[len(lcpTop)*9/10], lcpTop[len(lcpTop)-1])

	fmt.Printf("  rounds needed         %d worst case\n", maxRounds)
	var keys []int
	for k := range roundHist {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	for _, k := range keys {
		fmt.Printf("    %2d rounds  %4d texts\n", k, roundHist[k])
	}

	// The share still tied at each k is what a "sort only unresolved groups"
	// implementation would actually touch in that round. A full-array doubling
	// pays 100% every round regardless.
	fmt.Printf("\n  suffixes still tied at each k (this is the work per round)\n")
	fmt.Printf("    %-8s %10s\n", "k", "share")
	total := 0.0
	for r := 0; r < len(unresolved); r++ {
		if unresolved[r] == 0 {
			break
		}
		share := unresolved[r] / sumN
		fmt.Printf("    %-8d %9.2f%%\n", 1<<r, share*100)
		total += share
	}
	fmt.Printf("\n  total work if only tied groups are re-sorted: %.2f x n\n", total)
	fmt.Printf("  total work if the whole array is re-sorted:   %d x n\n\n", maxRounds)

	reportRounds(rounds, sumN)
}

// roundsFor is how many doubling rounds are needed to separate suffixes that
// share up to lcp bytes: k must reach lcp+1, starting at k=1.
func roundsFor(lcp int) int {
	if lcp <= 0 {
		return 1
	}
	return bits.Len(uint(lcp)) + 1
}

// kasai builds the LCP array in O(n): lcp[i] is the longest common prefix of
// sa[i-1] and sa[i], with lcp[0] = 0.
func kasai(text []byte, sa []int32) []int32 {
	n := len(text)
	rank := make([]int32, n)
	for i, s := range sa {
		rank[s] = int32(i)
	}
	lcp := make([]int32, n)
	h := 0
	for i := 0; i < n; i++ {
		if rank[i] > 0 {
			j := int(sa[rank[i]-1])
			for i+h < n && j+h < n && text[i+h] == text[j+h] {
				h++
			}
			lcp[rank[i]] = int32(h)
			if h > 0 {
				h--
			}
		} else {
			h = 0
		}
	}
	return lcp
}

func readFull(r *bufio.Reader, b []byte) (int, error) {
	n := 0
	for n < len(b) {
		m, err := r.Read(b[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "lcpstat:", err)
	os.Exit(1)
}

// ---------------------------------------------------------------- rounds
//
// What the GPU's doubling loop actually faces, round by round, and whether
// numbering the groups densely would let the sort key be narrower.
//
// The key is (r1, r2): the rank of this suffix and the rank of the one k
// further on. r1 is currently a position in [0, n), so 17 bits. It only has to
// tell the active *groups* apart, though, and if those were numbered 0..g-1 it
// would need bits(g). r2 has to stay a full rank, because the suffix it names
// can be anywhere.
//
// Whether that is worth doing is entirely a question of how big g is, and g is
// a property of the data. Hence this.

const sadSeed = 4 // where the GPU's doubling starts; see gpu/sa_doubling.cuh

type roundStat struct {
	active float64 // suffixes still in a group of two or more
	groups float64 // how many such groups
	texts  int
}

// accumulateRounds walks the LCP array once per round. Positions p and p+1 are
// in the same k-group exactly when lcp[p+1] >= k, so a group is a maximal run
// of that, and only runs of two or more are still work.
func accumulateRounds(lcp []int32, out map[int]*roundStat) {
	n := len(lcp)
	for k := sadSeed; k < 2*n; k <<= 1 {
		active, groups := 0, 0
		p := 0
		for p < n {
			q := p + 1
			for q < n && int(lcp[q]) >= k {
				q++
			}
			if q-p >= 2 {
				active += q - p
				groups++
			}
			p = q
		}
		if active == 0 {
			break
		}
		st := out[k]
		if st == nil {
			st = &roundStat{}
			out[k] = st
		}
		st.active += float64(active)
		st.groups += float64(groups)
		st.texts++
	}
}

// passesFor is what the GPU's radix sort costs for a key of this many bits.
func passesFor(keyBits int) int { return (keyBits + brBits - 1) / brBits }

const (
	brBits   = 7  // BR_BITS in gpu/blockradix.cuh
	rankBits = 17 // enough for a position in a 71 KB text
)

func reportRounds(rounds map[int]*roundStat, sumN float64) {
	var ks []int
	for k := range rounds {
		ks = append(ks, k)
	}
	sort.Ints(ks)

	fmt.Printf("  what a dense group numbering would buy\n\n")
	fmt.Printf("    %-6s %10s %10s %8s %8s %8s\n",
		"k", "active", "groups", "bits(g)", "passes", "dense")

	var now, dense float64
	for _, k := range ks {
		st := rounds[k]
		a := st.active / sumN // active as a share of all suffixes
		g := st.groups / float64(st.texts)
		gb := bits.Len(uint(g))

		pNow := passesFor(2 * rankBits)
		pNew := passesFor(gb + rankBits)
		now += a * float64(pNow)
		dense += a * float64(pNew)

		fmt.Printf("    %-6d %9.2f%% %10.0f %8d %8d %8d\n",
			k, a*100, g, gb, pNow, pNew)
	}

	fmt.Printf("\n    doubling costs %.2f passes over n now, %.2f dense: %+.1f%%\n",
		now, dense, (dense/now-1)*100)

	// The seed sort is a fixed cost either way, and it is a large share of the
	// total, so the change is worth less than the line above suggests.
	seed := float64(passesFor(sadSeed * 9))
	fmt.Printf("    with the %d-byte seed's %.0f passes: %.2f -> %.2f, %+.1f%% of the sort\n\n",
		sadSeed, seed, now+seed, dense+seed, ((dense+seed)/(now+seed)-1)*100)
}
