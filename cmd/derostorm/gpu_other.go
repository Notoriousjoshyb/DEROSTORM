//go:build !windows

package main

// GPU mining on everything except Windows: not built yet.
//
// The kernels themselves are portable CUDA (gpu/*.cuh); what is missing is the
// packaging. The Windows path embeds a DLL and binds it with LoadLibrary, and
// the same trick needs a .so plus dlopen here. Until that exists, these builds
// mine on the CPU.
//
// Shapes match gpu_windows.go exactly so engine.go and gpu_worker.go need no
// build tags of their own, and so a Linux build still type-checks every call
// site rather than letting the two drift apart unnoticed.

import "errors"

// GPUAvailable reports whether this build can use a GPU at all.
const GPUAvailable = false

// GPUKind names the hardware this would support, for messages to the user.
const GPUKind = "NVIDIA CUDA"

var errNoGPUHere = errors.New("GPU mining is only built for Windows at the moment")

// GPUDeviceCount is always zero here.
func GPUDeviceCount() int { return 0 }

// GPUDeviceInfo has nothing to describe.
func GPUDeviceInfo(device int) string { return "" }

// GPUContext is the same handle as on Windows, with nothing behind it.
type GPUContext struct{}

func NewGPUContext(device, batch, blocks int) (*GPUContext, error) { return nil, errNoGPUHere }

func (g *GPUContext) Close()              {}
func (g *GPUContext) Batch() int          { return 0 }
func (g *GPUContext) Name() string        { return "" }
func (g *GPUContext) Blocks() int         { return 0 }
func (g *GPUContext) MaxBlocks() int      { return 0 }
func (g *GPUContext) SMs() int            { return 0 }
func (g *GPUContext) SetBlocks(int) error { return errNoGPUHere }

func (g *GPUContext) Search(work []byte, nonceStart uint32, t *Target) ([]uint32, error) {
	return nil, errNoGPUHere
}

func (g *GPUContext) HashOne(work []byte, nonce uint32) ([32]byte, error) {
	return [32]byte{}, errNoGPUHere
}
