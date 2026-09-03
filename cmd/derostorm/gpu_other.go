//go:build !windows && (!linux || !amd64)

package main

// The fallback for builds with no GPU library to embed: macOS, and Linux on
// anything but amd64.
//
// macOS is not a packaging gap but a dead end. Apple dropped NVIDIA driver
// support in 10.14 and Apple Silicon never had it, and AMD's ROCm has never
// targeted macOS either, so there is nothing on any Mac made since 2018 for
// either backend to bind to; a GPU path there would be Metal and a rewrite of
// gpu/*.cuh, not a loader. Linux arm64 is only missing a build -- see
// gpu_backend_linux.go.
//
// Shapes match gpu_backend.go exactly so engine.go and gpu_worker.go need no
// build tags of their own, and so these builds still type-check every call site
// rather than letting the two drift apart unnoticed.

import "errors"

// GPUAvailable reports whether this build can use a GPU at all.
const GPUAvailable = false

// GPUKind names the hardware this would support, for messages to the user.
func GPUKind() string { return "NVIDIA CUDA and AMD HIP" }

// GPUDeviceKind has no device to name.
func GPUDeviceKind(device int) string { return "" }

var errNoGPUHere = errors.New("this build has no GPU support: it needs Windows, or Linux on amd64")

// GPUDeviceCount is always zero here.
func GPUDeviceCount() int { return 0 }

// GPUBackendStatus has one thing to say and it is the same every time.
func GPUBackendStatus() []string { return []string{errNoGPUHere.Error()} }

// GPUDeviceInfo has nothing to describe.
func GPUDeviceInfo(device int) string { return "" }

// GPUContext is the same handle as gpu_backend.go's, with nothing behind it.
type GPUContext struct{}

func NewGPUContext(device, batch, blocks int) (*GPUContext, error) { return nil, errNoGPUHere }

func (g *GPUContext) Close()              {}
func (g *GPUContext) Batch() int          { return 0 }
func (g *GPUContext) Name() string        { return "" }
func (g *GPUContext) Kind() string        { return "" }
func (g *GPUContext) Blocks() int         { return 0 }
func (g *GPUContext) MaxBlocks() int      { return 0 }
func (g *GPUContext) SMs() int            { return 0 }
func (g *GPUContext) SetBlocks(int) error { return errNoGPUHere }

func (g *GPUContext) Search(work []byte, nonceStart uint32, t *Target) ([]uint32, error) {
	return nil, errNoGPUHere
}

func (g *GPUContext) Submit(work []byte, nonceStart uint32, t *Target) error {
	return errNoGPUHere
}

func (g *GPUContext) Collect() ([]uint32, error) { return nil, errNoGPUHere }

func (g *GPUContext) InFlight() int { return 0 }

func (g *GPUContext) HashOne(work []byte, nonce uint32) ([32]byte, error) {
	return [32]byte{}, errNoGPUHere
}
