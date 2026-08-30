//go:build !windows && !linux

package main

// Machine metrics on the platforms with no user-mode way to ask for them.
//
// macOS and the BSDs can produce all three of these, but only through cgo or a
// shelled-out command. Neither is worth it here: the miner builds without a C
// toolchain today, and a dashboard that forks `sysctl` once a second to fill
// in a load percentage has bought a small nicety with a real cost. The panel
// says "--", which is the honest answer and the one this file exists to give.

func readCPULoad() (float64, bool) { return 0, false }

func readMemory() (usedMB, totalMB int, ok bool) { return 0, 0, false }

func readCPUFreqMHz() (int, bool) { return 0, false }
