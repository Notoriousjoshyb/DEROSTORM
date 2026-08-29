//go:build !windows && (!linux || !amd64)

package main

// The no-NVML half, for the platforms gpu_other.go already covers: macOS, and
// Linux on anything but amd64. There is no NVIDIA driver to ask on either, so
// there is nothing to load and nothing to report.
//
// Shapes match nvml.go exactly, so sensors.go needs no build tags of its own.

func openNVML(devices []int) string {
	if len(devices) == 0 {
		return ""
	}
	return "GPU telemetry unavailable: no NVML on this platform"
}

func closeNVML() {}

func readGPUSensors(devices []int) []GPUSensor { return nil }
