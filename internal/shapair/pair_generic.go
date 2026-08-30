//go:build !arm64 || purego

package shapair

func hashPair(a, b []byte, outA, outB *[32]byte) bool {
	return hashPairStd(a, b, outA, outB)
}
