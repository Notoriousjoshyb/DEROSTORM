//go:build !windows && (!linux || !amd64)

package main

// The no-telemetry half, for the platforms gpu_other.go already covers: macOS,
// and Linux on anything but amd64. There is no GPU being mined on either, so
// there is nothing to load and nothing to report.
//
// Shapes match gpu_sensors.go exactly, so sensors.go needs no build tags of its
// own.

func openGPUTelemetry(devices []int) string {
	if len(devices) == 0 {
		return ""
	}
	return "GPU telemetry unavailable: not supported on this platform"
}

func closeGPUTelemetry() {}

func readGPUSensors(devices []int) []GPUSensor { return nil }
