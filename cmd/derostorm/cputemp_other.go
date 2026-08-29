//go:build !windows && !linux

package main

// CPU temperature on everything else: macOS and the BSDs.
//
// There is nothing portable to read here. macOS exposes the SMC through a
// private IOKit interface whose sensor keys change between Mac models and are
// not documented; the BSDs each have their own sysctl. Both are real options
// for someone who wants them, and neither is one line.
//
// So this build reports no CPU temperature, and the panel draws "--" rather
// than a number nobody checked. The shape matches cputemp_windows.go and
// cputemp_linux.go so sensors.go needs no build tags of its own.

const cpuTempHint = "CPU temperature is not read on this platform"

func readCPUTemp() (float64, string, bool) { return 0, "", false }
