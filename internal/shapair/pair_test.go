package shapair

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"testing"
)

func TestPairMatchesStdlib(t *testing.T) {
	lengths := []int{1, 3, 55, 56, 63, 64, 65, 100, 127, 128, 200, 1024, 278 * 1024}
	for _, n := range lengths {
		a := make([]byte, n)
		b := make([]byte, n+17)
		rand.Read(a)
		rand.Read(b)
		var outA, outB [32]byte
		if !hashPairStd(a, b, &outA, &outB) {
			t.Fatalf("hashPairStd declined n=%d", n)
		}
		wantA := sha256.Sum256(a)
		wantB := sha256.Sum256(b)
		if outA != wantA {
			t.Fatalf("n=%d first digest: got %x want %x", n, outA, wantA)
		}
		if outB != wantB {
			t.Fatalf("n=%d second digest: got %x want %x", n, outB, wantB)
		}
	}
	a := bytes.Repeat([]byte{0x5a}, 3)
	b := make([]byte, 100)
	var outA, outB [32]byte
	if !hashPairStd(a, b, &outA, &outB) {
		t.Fatal("hashPairStd declined unequal lengths")
	}
	if outA != sha256.Sum256(a) || outB != sha256.Sum256(b) {
		t.Fatal("unequal-length pair disagreed with stdlib")
	}
}
