package astrobwtv3

import "unsafe"
import "hash"
import "sync"

//import "fmt"
import "github.com/minio/sha256-simd"

const MAX_LENGTH uint32 = (256 * 384) - 1 // this is the maximum

// see here to improve the algorithms more https://github.com/y-256/libdivsufsort/blob/wiki/SACA_Benchmarks.md
// this optimized algorithm is used only  in the miner and not in the blockchain

// ScratchData is everything one mining thread needs to compute a hash, owned
// for the life of that thread.
//
// It used to also carry `indices` and `tmp_indices`, 384 KB each, for the v1/v2
// counting sort in sort_indices. AstroBWTv3 does not call that -- it goes
// through text_32_fast -- so those two were 768 KB per thread that was
// allocated, zeroed by the runtime on first touch, and then never read. They
// are gone, along with the two aliased views of them and the dead sort itself.
//
// This does not change the hash, and it did not change the hashrate on the
// machine it was measured on: the buffers were never touched, so they were
// never in cache. What it changes is the footprint, which was 2.06 MB per
// thread and is now 1.27 MB -- 12 MB less resident at sixteen threads, and 12 MB
// fewer pages to fault in at start-up.
type ScratchData struct {
	hasher   hash.Hash
	data     [MAX_LENGTH + 64]uint8
	sa       [MAX_LENGTH]int32
	sa_bytes *[(MAX_LENGTH) * 4]uint8
	digest   [32]byte
	// lms and satmp belong to the built-in suffix sort and to nothing else:
	// lms is the LMS-substring index side buffer, bounded by len(text) across
	// the whole recursion stack, and satmp is the freq/bucket workspace, sized
	// so frequency caching stays on at every recursion level. Both are
	// documented in sais_fast.go.
	//
	// Slices, allocated on first use, rather than arrays allocated always. When
	// a miner installs a faster sort through SuffixSort (see sa_hook.go) the
	// built-in one never runs, and these are 768 KB per thread of memory the
	// hash would be holding for a path it does not take. Fifteen threads is
	// 11 MB, against 96 MB of L3 on the machine this was measured on -- and the
	// hashrate curve there is capacity limited well before fifteen threads.
	//
	// Nil until saisBuffers is called, which the built-in path does and the hook
	// path does not.
	lms   []int32
	satmp []int32
}

// saisBuffers returns the built-in suffix sort's side buffers, allocating them
// the first time this scratch takes that path.
func (d *ScratchData) saisBuffers() (lms, satmp []int32) {
	if d.lms == nil {
		d.lms = make([]int32, MAX_LENGTH+1)
		d.satmp = make([]int32, MAX_LENGTH+2)
	}
	return d.lms, d.satmp
}

var Pool = sync.Pool{New: func() interface{} {
	var d ScratchData
	d.hasher = sha256.New()
	// sa_bytes is sa seen as bytes, so the final SHA-256 can hash the suffix
	// array without copying it out first. Sound on a little-endian machine,
	// which is what LittleEndian gates.
	d.sa_bytes = ((*[(MAX_LENGTH) * 4]byte)(unsafe.Pointer(&d.sa[0])))

	return &d
}}
