//go:build !windows && (!linux || !amd64)

package main

// The fallback for builds with no CUDA library to embed: macOS, and Linux on
// anything but amd64.
//
// macOS is not a packaging gap but a dead end. Apple dropped NVIDIA driver
// support in 10.14 and Apple Silicon never had it, so there is no CUDA to bind
// to on any Mac made since 2018; a GPU path there would be Metal and a rewrite
// of gpu/*.cuh, not a loader. Linux arm64 is only missing a build -- see
// gpu_cuda_linux.go.
//
// Shapes match gpu_cuda.go exactly so engine.go and gpu_worker.go need no build
// tags of their own, and so these builds still type-check every call site
// rather than letting the two drift apart unnoticed.

import "errors"

// GPUAvailable reports whether this build can use a GPU at all.
const GPUAvailable = false

// GPUKind names the hardware this would support, for messages to the user.
const GPUKind = "NVIDIA CUDA"

var errNoGPUHere = errors.New("this build has no GPU support: NVIDIA CUDA needs Windows, or Linux on amd64")

// GPUDeviceCount is always zero here.
func GPUDeviceCount() int { return 0 }

// GPUDeviceInfo has nothing to describe.
func GPUDeviceInfo(device int) string { return "" }

// GPUContext is the same handle as gpu_cuda.go's, with nothing behind it.
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
