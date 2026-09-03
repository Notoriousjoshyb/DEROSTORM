//go:build windows || (linux && amd64)

package main

// Which telemetry library to ask about which card.
//
// The miner numbers every GPU in one list, NVIDIA cards first (gpu_backend.go).
// NVML and ROCm SMI each number only their own vendor's, so device 2 in the
// miner may be device 0 to ROCm SMI. This file is the translation, and it is
// the only place that knows about it: nvml.go and rocmsmi.go each see nothing
// but their own vendor's indices, and sensors.go sees nothing but the miner's.
//
// Nothing here fails loudly. Telemetry is the temperature column, not the
// mining, so a vendor whose library will not load contributes a note and no
// rows, and the cards on the other vendor still report.

import "strings"

// vendorRef names a card the way its own telemetry library does: which vendor,
// and that vendor's index. It is the key rows come back through.
type vendorRef struct {
	backend *gpuBackend
	local   int
}

// splitByVendor turns miner device indices into per-vendor ones, keeping a way
// back for the rows that come out. Any index with no device behind it is
// dropped rather than guessed at.
func splitByVendor(devices []int) (nvidia, amd []int, back map[vendorRef]int) {
	list := gpuDeviceList()
	back = make(map[vendorRef]int, len(devices))
	for _, d := range devices {
		if d < 0 || d >= len(list) {
			continue
		}
		switch list[d].backend {
		case cudaBackend:
			nvidia = append(nvidia, list[d].local)
		case hipBackend:
			amd = append(amd, list[d].local)
		default:
			continue
		}
		back[vendorRef{list[d].backend, list[d].local}] = d
	}
	return nvidia, amd, back
}

// openGPUTelemetry brings up whichever libraries the mined cards need. The
// returned note is for the event log, said once at start-up; an empty string
// means everything that was asked for came up.
func openGPUTelemetry(devices []int) string {
	nvidia, amd, _ := splitByVendor(devices)

	var notes []string
	if n := nvmlOpen(nvidia); n != "" {
		notes = append(notes, n)
	}
	if n := rsmiOpen(amd); n != "" {
		notes = append(notes, n)
	}
	return strings.Join(notes, "; ")
}

func closeGPUTelemetry() {
	nvmlClose()
	rsmiClose()
}

// readGPUSensors polls every mined card once and renumbers the rows back into
// the miner's own device numbering, which is what the panel labels them with.
func readGPUSensors(devices []int) []GPUSensor {
	nvidia, amd, back := splitByVendor(devices)
	if len(nvidia) == 0 && len(amd) == 0 {
		return nil
	}

	out := make([]GPUSensor, 0, len(devices))
	for _, r := range nvmlRead(nvidia) {
		if i, ok := back[vendorRef{cudaBackend, r.Index}]; ok {
			r.Index = i
			out = append(out, r)
		}
	}
	for _, r := range rsmiRead(amd) {
		if i, ok := back[vendorRef{hipBackend, r.Index}]; ok {
			r.Index = i
			out = append(out, r)
		}
	}
	return out
}
