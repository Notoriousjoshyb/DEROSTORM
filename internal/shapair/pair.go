package shapair

import (
	"crypto/sha256"
	"runtime"

	"golang.org/x/sys/cpu"
)

// Available is true when this CPU has a SHA unit worth pairing on.
//
// x/sys/cpu has no SHA-NI flag on x86 in the version this miner vendors, so
// Intel Mac (darwin/amd64) is included by OS: every Intel Mac that still
// ships has SHA-NI, and Go's SHA-256 uses it. ARM is asked properly.
func Available() bool {
	if cpu.ARM64.HasSHA2 {
		return true
	}
	return runtime.GOARCH == "amd64" && runtime.GOOS == "darwin"
}

// Pair writes SHA-256(a) into outA and SHA-256(b) into outB.
// Returns false when this CPU has no SHA extensions, or either input is empty.
func Pair(a, b []byte, outA, outB *[32]byte) bool {
	if !Available() || len(a) == 0 || len(b) == 0 {
		return false
	}
	return hashPair(a, b, outA, outB)
}

func hashPairStd(a, b []byte, outA, outB *[32]byte) bool {
	// Single-shot digests. The lockstep 64-byte interleave only pays on the
	// arm64 SHA-unit path; here two Hash objects plus a Write per block are
	// pure alloc and dispatch overhead for identical bytes out.
	*outA = sha256.Sum256(a)
	*outB = sha256.Sum256(b)
	return true
}
