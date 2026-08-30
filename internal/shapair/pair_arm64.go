//go:build arm64 && !purego

package shapair

// ARMv8 SHA-2 pairing for Mac and linux/arm64 binaries that were cross-compiled
// without cgo. The block function is the Go standard library's (BSD), called
// once per 64-byte block of each message in lockstep so the SHA unit stays fed.
// Apple Silicon has no SMT, so this is the second mining thread that does not exist.

var _K = [64]uint32{
	0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
	0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
	0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
	0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
	0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
	0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
	0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
	0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
}

var iv = [8]uint32{
	0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
	0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
}

//go:noescape
func blockSHA2(h *[8]uint32, p []byte)

func hashPair(a, b []byte, outA, outB *[32]byte) bool {
	ha, hb := iv, iv
	na, nb := len(a), len(b)
	ba, bb := na/64, nb/64
	common := ba
	if bb < common {
		common = bb
	}
	for i := 0; i < common; i++ {
		off := i * 64
		blockSHA2(&ha, a[off:off+64])
		blockSHA2(&hb, b[off:off+64])
	}
	if ba > common {
		blockSHA2(&ha, a[common*64:ba*64])
	}
	if bb > common {
		blockSHA2(&hb, b[common*64:bb*64])
	}
	finish(&ha, a[ba*64:], na, outA)
	finish(&hb, b[bb*64:], nb, outB)
	return true
}

func finish(h *[8]uint32, tail []byte, total int, out *[32]byte) {
	var pad [128]byte
	n := copy(pad[:], tail)
	pad[n] = 0x80
	nblk := 1
	if n >= 56 {
		nblk = 2
	}
	bits := uint64(total) * 8
	off := nblk*64 - 8
	for i := 0; i < 8; i++ {
		pad[off+i] = byte(bits >> (56 - 8*i))
	}
	blockSHA2(h, pad[:nblk*64])
	for i := 0; i < 8; i++ {
		out[4*i+0] = byte(h[i] >> 24)
		out[4*i+1] = byte(h[i] >> 16)
		out[4*i+2] = byte(h[i] >> 8)
		out[4*i+3] = byte(h[i])
	}
}
