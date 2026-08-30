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
	ha, hb := sha256.New(), sha256.New()
	na, nb := len(a), len(b)
	ba, bb := na/64, nb/64
	common := ba
	if bb < common {
		common = bb
	}
	for i := 0; i < common; i++ {
		off := i * 64
		ha.Write(a[off : off+64])
		hb.Write(b[off : off+64])
	}
	if ba > common {
		ha.Write(a[common*64 : ba*64])
	}
	if bb > common {
		hb.Write(b[common*64 : bb*64])
	}
	if rem := ba * 64; rem < na {
		ha.Write(a[rem:])
	}
	if rem := bb * 64; rem < nb {
		hb.Write(b[rem:])
	}
	var sa, sb [32]byte
	copy(outA[:], ha.Sum(sa[:0]))
	copy(outB[:], hb.Sum(sb[:0]))
	return true
}
