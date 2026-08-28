package main

// The suffix-sort comparison, part of --bench.
//
// The suffix array is ~90% of a hash, and there are two implementations of it:
// the Go SA-IS built into astrobwtv3, and libsais bound at run time. Which is
// faster is a property of the machine, so --bench measures it rather than
// asserting it, and prints both so the difference is visible.
//
// Interleaved, for the reason set out in gpu_tune.go: on a desktop doing other
// things -- including running another miner -- measuring one after the other
// mostly reports which one ran while the machine was quieter.

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/deroproject/derohe/astrobwt/astrobwtv3"
)

const (
	// saBenchWarmup is hashes discarded on each visit to a sort, so the switch
	// itself is not measured.
	saBenchWarmup = 16
	// saBenchBlock is hashes counted on each visit.
	saBenchBlock = 48
	// saBenchRounds is visits per sort.
	saBenchRounds = 4
)

// runSABench times the two suffix sorts against each other at the given thread
// count and prints the result. sorter is the faster one; nil means there is
// nothing to compare and this prints nothing.
func runSABench(t *Theme, threads int, sorter func([]byte, []int32) bool) {
	if sorter == nil || threads < 1 {
		return
	}

	fmt.Printf("\n  %s\n\n", t.c(t.Accent+t.Bold, "suffix sort · the 90% of a hash"))

	type acc struct {
		d [2]time.Duration
		n [2]int
	}
	res := make([]acc, threads)

	// The hook is a global read once per hash, so it may only change while no
	// thread is hashing. That is what these two channels buy: every worker is
	// blocked receiving from gate when the coordinator sets it, and every worker
	// has reported on done before the coordinator sets it again.
	gate := make(chan int)
	done := make(chan struct{})

	var wg sync.WaitGroup
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func(slot int, a *acc) {
			defer wg.Done()
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			pinToCPU(slot)

			scratch := astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
			defer astrobwtv3.Pool.Put(scratch)

			var w [48]byte
			w[0] = 1
			for k := 8; k < 43; k++ {
				w[k] = byte(k*7 + slot*13)
			}
			it := slot * 1000003

			hash := func(n int) {
				for k := 0; k < n; k++ {
					w[43] = byte(it)
					w[44] = byte(it >> 8)
					w[45] = byte(it >> 16)
					w[46] = byte(slot)
					it++
					_ = astrobwtv3.AstroBWTv3_scratch(w[:], scratch)
				}
			}

			for which := range gate {
				hash(saBenchWarmup)
				start := time.Now()
				hash(saBenchBlock)
				a.d[which] += time.Since(start)
				a.n[which] += saBenchBlock
				done <- struct{}{}
			}
		}(i, &res[i])
	}

	saved := astrobwtv3.SuffixSort
	sorters := [2]func([]byte, []int32) bool{nil, sorter}

	for r := 0; r < saBenchRounds*2; r++ {
		which := r % 2
		astrobwtv3.SuffixSort = sorters[which]
		for i := 0; i < threads; i++ {
			gate <- which
		}
		for i := 0; i < threads; i++ {
			<-done
		}
	}
	close(gate)
	wg.Wait()
	astrobwtv3.SuffixSort = saved

	var d [2]time.Duration
	var n [2]int
	for _, a := range res {
		for i := 0; i < 2; i++ {
			d[i] += a.d[i]
			n[i] += a.n[i]
		}
	}

	// d is summed per-thread wall time, so hashes over it is the per-thread rate
	// and the machine rate is that times the thread count.
	rate := func(i int) float64 {
		if n[i] == 0 || d[i] <= 0 {
			return 0
		}
		return float64(n[i]) / d[i].Seconds() * float64(threads)
	}

	fmt.Printf("  %s\n", t.c(t.Muted, fmt.Sprintf("%-26s %14s", "sort", "H/s")))
	fmt.Printf("  %s\n", t.c(t.Text, fmt.Sprintf("%-26s %14.1f", "built-in (Go SA-IS)", rate(0))))
	fmt.Printf("  %s\n", t.c(t.Text, fmt.Sprintf("%-26s %14.1f", "descriptor + libsais", rate(1))))

	if rate(0) > 0 && rate(1) > 0 {
		diff := (rate(1)/rate(0) - 1) * 100
		colour := t.Good
		if diff < 0 {
			colour = t.Warn
		}
		fmt.Printf("\n  %s\n", t.c(colour+t.Bold,
			fmt.Sprintf("the native sort is %+.1f%% at %s", diff, plural(threads, "thread"))))
	}
	fmt.Println()
}

// plural writes "1 thread" and "15 threads". Small, but this line is the
// headline of a benchmark someone will paste somewhere.
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
