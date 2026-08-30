package main

// End-to-end tests of the GPU binding: the card has to agree with the CPU
// through the same API the miner uses, and its difficulty check has to pick the
// same winners.
//
//	gpu\buildlib.bat && go test -run GPU ./cmd/derostorm
//
// No build tag: GPU support is in every build now. These skip when no CUDA
// device is present, so they are safe on a machine without one.

import (
	"encoding/binary"
	"math/big"
	"sort"
	"testing"

	"github.com/deroproject/derohe/astrobwt/astrobwtv3"
	"github.com/deroproject/derohe/block"
)

func gpuTestContext(t *testing.T) *GPUContext {
	t.Helper()
	if !GPUAvailable {
		t.Skip("no GPU support in this build")
	}
	if GPUDeviceCount() == 0 {
		t.Skip("no CUDA device")
	}
	// A small batch and a modest block count keep the test quick and its VRAM
	// footprint small enough to run beside a miner.
	g, err := NewGPUContext(0, 256, 16)
	if err != nil {
		t.Skipf("cannot open device 0: %v", err)
	}
	return g
}

// testWork builds a miniblock shaped like a real one: version 1 in byte 0 and
// arbitrary but fixed bytes elsewhere.
func testWork() []byte {
	w := make([]byte, block.MINIBLOCK_SIZE)
	w[0] = 1
	for i := 8; i < 43; i++ {
		w[i] = byte(i*7 + 11)
	}
	return w
}

func TestGPUDeviceInfo(t *testing.T) {
	if !GPUAvailable || GPUDeviceCount() == 0 {
		t.Skip("no CUDA device")
	}
	// Must not open the device or allocate: the setup wizard calls this while
	// the user is still answering questions.
	if info := GPUDeviceInfo(0); info == "" {
		t.Fatal("GPUDeviceInfo(0) returned nothing")
	} else {
		t.Logf("device 0: %s", info)
	}
}

func TestGPUHashMatchesCPU(t *testing.T) {
	g := gpuTestContext(t)
	defer g.Close()

	scratch := astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
	defer astrobwtv3.Pool.Put(scratch)

	work := testWork()
	for _, nonce := range []uint32{0, 1, 2, 1000, 0x5f3759df, 0xfffffffe, 0xffffffff} {
		binary.BigEndian.PutUint32(work[block.MINIBLOCK_SIZE-5:], nonce)

		got, err := g.HashOne(work, nonce)
		if err != nil {
			t.Fatalf("nonce %d: %v", nonce, err)
		}
		want := astrobwtv3.AstroBWTv3_scratch(work, scratch)
		if got != want {
			t.Fatalf("nonce %d:\n gpu %x\n cpu %x", nonce, got, want)
		}
	}
}

// TestGPUSearchAgreesWithCPU runs a batch at a difficulty low enough that many
// nonces qualify, then checks the GPU reported exactly the set the CPU would.
// This exercises the target compare and the nonce arithmetic, not just the
// hash: an off-by-one in either would show up as a set difference.
func TestGPUSearchAgreesWithCPU(t *testing.T) {
	g := gpuTestContext(t)
	defer g.Close()

	scratch := astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
	defer astrobwtv3.Pool.Put(scratch)

	// Difficulty 64 accepts roughly one nonce in 64, so a batch of 256 should
	// produce a handful of winners rather than none or all.
	target := NewTarget(big.NewInt(64))

	work := testWork()
	const start = uint32(70000)

	hits, err := g.Search(work, start, &target)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	onGPU := map[uint32]bool{}
	for _, h := range hits {
		if h < start || h >= start+uint32(g.Batch()) {
			t.Fatalf("nonce %d is outside the batch [%d,%d)", h, start, start+uint32(g.Batch()))
		}
		onGPU[h] = true
	}

	nCPU := 0
	for i := 0; i < g.Batch(); i++ {
		nonce := start + uint32(i)
		binary.BigEndian.PutUint32(work[block.MINIBLOCK_SIZE-5:], nonce)
		h := astrobwtv3.AstroBWTv3_scratch(work, scratch)
		want := target.Meets(&h)
		if want {
			nCPU++
		}
		if want != onGPU[nonce] {
			t.Fatalf("nonce %d: cpu says meets=%v, gpu says meets=%v", nonce, want, onGPU[nonce])
		}
	}
	t.Logf("%d of %d nonces met difficulty 64, and both sides agreed on every one",
		nCPU, g.Batch())
}

// sortedCopy takes the winners out of the GPU's buffer in a defined order. The
// kernel claims its slot with an atomicAdd, so which winner lands where depends
// on which block got there first and is not the same twice; only the set is.
func sortedCopy(hits []uint32) []uint32 {
	out := append([]uint32(nil), hits...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// TestGPUPipelineMatchesSearch checks the two-batch path the miner runs against
// the one-batch path everything else runs.
//
// The pipeline is the whole reason the GPU keeps its rate on a busy machine, and
// what could go wrong with it is silent: two batches share the scratch the
// kernels sort in, so a slot that was not really private would return one
// batch's winners for the other's nonces and nobody would notice until a pool
// rejected the share. So the test asks for the same nonce ranges both ways and
// requires the same answers.
func TestGPUPipelineMatchesSearch(t *testing.T) {
	g := gpuTestContext(t)
	defer g.Close()

	target := NewTarget(big.NewInt(64))
	work := testWork()

	const rounds = 3
	starts := make([]uint32, rounds)
	for i := range starts {
		starts[i] = 70000 + uint32(i*g.Batch())
	}

	// One at a time, the old way.
	want := make([][]uint32, rounds)
	for i, s := range starts {
		hits, err := g.Search(work, s, &target)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		want[i] = sortedCopy(hits)
	}
	if g.InFlight() != 0 {
		t.Fatalf("Search left %d batches in flight", g.InFlight())
	}

	// Now with a batch always queued behind the running one.
	got := make([][]uint32, 0, rounds)
	next := 0
	for len(got) < rounds {
		for next < rounds && g.InFlight() < gpuPipelineDepth {
			if err := g.Submit(work, starts[next], &target); err != nil {
				t.Fatalf("submit %d: %v", next, err)
			}
			next++
		}
		// A third batch must be refused rather than overwriting a live slot.
		if g.InFlight() == gpuPipelineDepth && next < rounds {
			if err := g.Submit(work, starts[next], &target); err == nil {
				t.Fatal("a third Submit was accepted with the pipeline full")
			}
		}
		hits, err := g.Collect()
		if err != nil {
			t.Fatalf("collect: %v", err)
		}
		got = append(got, sortedCopy(hits))
	}
	if g.InFlight() != 0 {
		t.Fatalf("%d batches left in flight after draining", g.InFlight())
	}
	if _, err := g.Collect(); err == nil {
		t.Fatal("Collect succeeded with nothing in flight")
	}

	total := 0
	for i := range want {
		total += len(want[i])
		if len(got[i]) != len(want[i]) {
			t.Fatalf("batch %d: pipeline found %d nonces, Search found %d",
				i, len(got[i]), len(want[i]))
		}
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Fatalf("batch %d, hit %d: pipeline says %d, Search says %d",
					i, j, got[i][j], want[i][j])
			}
		}
	}
	if total == 0 {
		t.Fatal("no nonce met difficulty 64 in any batch, so nothing was compared")
	}
	t.Logf("%d winning nonces over %d batches, identical both ways", total, rounds)
}
