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
