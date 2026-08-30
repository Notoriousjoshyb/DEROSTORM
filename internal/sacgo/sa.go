//go:build cgo && (darwin || (linux && arm64))

package sacgo

// The native suffix sort, compiled into the miner on darwin and linux/arm64
// when cgo is available. Windows and linux/amd64 embed a pre-built library
// instead -- see cmd/derostorm/sa_lib.go -- because those are cross-compiled
// from a machine that is not the target, and cgo would have ended that.
//
// One translation unit (bundle.c) so the descriptor merge can inline
// suffix_less the way /GL /LTCG does on Windows.

/*
#cgo CFLAGS: -O3 -fPIC -I${SRCDIR}/../../native
#cgo darwin,arm64 CFLAGS: -mcpu=apple-m1
#cgo darwin,amd64 CFLAGS: -march=x86-64-v3 -msha
#cgo linux,arm64 CFLAGS: -march=armv8-a+crypto

#include <stdint.h>
int32_t dsa_suffix_array(const uint8_t* t, int32_t* sa, int32_t n, int32_t fs);
int32_t dsa_probe(void);
void dsa_version(char* buf, int32_t len);
int32_t dsa_sha_probe(void);
void dsa_sha256_pair_go(const uint8_t* a, int32_t na, uint8_t* out_a,
                        const uint8_t* b, int32_t nb, uint8_t* out_b);
*/
import "C"
import "unsafe"

var (
	ok      bool
	pairOK  bool
	version string
)

func init() {
	if C.dsa_probe() != 0 {
		return
	}
	var buf [64]byte
	C.dsa_version((*C.char)(unsafe.Pointer(&buf[0])), 64)
	for i, b := range buf {
		if b == 0 {
			version = string(buf[:i])
			break
		}
	}
	ok = true
	if C.dsa_sha_probe() == 1 {
		pairOK = true
	}
}

func Available() bool { return ok }

func Version() string { return version }

func PairAvailable() bool { return pairOK }

func SuffixArray(text []byte, sa []int32) bool {
	if !ok || len(text) == 0 || len(sa) < len(text) {
		return false
	}
	fs := cap(sa) - len(text)
	if fs < 0 {
		fs = 0
	}
	return C.dsa_suffix_array((*C.uint8_t)(unsafe.Pointer(&text[0])),
		(*C.int32_t)(unsafe.Pointer(&sa[0])),
		C.int32_t(len(text)), C.int32_t(fs)) == 0
}

func Pair(a, b []byte, outA, outB *[32]byte) bool {
	if !pairOK || len(a) == 0 || len(b) == 0 {
		return false
	}
	C.dsa_sha256_pair_go(
		(*C.uint8_t)(unsafe.Pointer(&a[0])), C.int32_t(len(a)), (*C.uint8_t)(unsafe.Pointer(&outA[0])),
		(*C.uint8_t)(unsafe.Pointer(&b[0])), C.int32_t(len(b)), (*C.uint8_t)(unsafe.Pointer(&outB[0])),
	)
	return true
}
