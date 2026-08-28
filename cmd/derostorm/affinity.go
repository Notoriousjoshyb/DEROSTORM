package main

// spreadOverCores maps a mining-thread slot to a logical CPU so that the first
// half of the slots land on distinct physical cores before any SMT sibling is
// used. On a machine that reports CPUs as core0/sibling0, core1/sibling1, ...
// the even indices are the first thread of each core.
//
// Slot 0 maps to CPU 0. The upstream miner started at 1 -- it used the value
// returned by an atomic increment directly -- which left logical CPU 0 unused
// and pushed the last thread past the CPU count so it was never pinned at all.
func spreadOverCores(slot, count int) int {
	if count < 2 {
		return 0
	}
	if slot < count/2 {
		return slot * 2
	}
	return (slot-count/2)*2 + 1
}
