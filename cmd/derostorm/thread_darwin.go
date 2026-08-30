//go:build darwin

package main

// CPU affinity on macOS is advisory. What actually keeps a mining thread on a
// performance core is the QoS class: USER_INTERACTIVE is scheduled onto P-cores
// and is what a miner that used to be a no-op on Darwin was missing. Apple
// Silicon has no SMT, so a thread that lands on an E-core is not "the other
// hyperthread of a fast core" -- it is a different, slower core, and the
// hashrate gap versus Windows on the same silicon starts there.

import "github.com/ebitengine/purego"

const qosUserInteractive = 0x21

var darwinQoS func(qos uint32, relative int32) int32

func init() {
	lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW)
	if err != nil {
		return
	}
	purego.RegisterLibFunc(&darwinQoS, lib, "pthread_set_qos_class_self_np")
}

func pinToCPU(slot int) {
	if darwinQoS == nil {
		return
	}
	_ = slot
	darwinQoS(qosUserInteractive, 0)
}
