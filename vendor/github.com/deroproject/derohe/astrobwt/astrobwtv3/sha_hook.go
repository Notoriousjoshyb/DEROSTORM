package astrobwtv3

// The paired-SHA-256 hook.
//
// After the suffix array is built, the hash is one SHA-256 over it as bytes --
// ~278 KB, and about a quarter of the whole hash once the sort is fast. Both
// Go's standard library and minio's assembly run that at 2.52 GB/s on a machine
// with the SHA extensions, and that is not the instruction's limit: sha256rnds2
// has a four-cycle latency and can start more than one per cycle, so one
// message leaves the unit idle most of the time.
//
// A single SHA-256 cannot be split -- round n needs round n-1. Two SHA-256s can
// be interleaved, and a miner always has another nonce to hash. Measured over
// this size, one message runs at 2.57 GB/s and two together at 3.71.
//
// So AstroBWTv3_pair exists: two inputs, two independent stage-1 and suffix-sort
// passes into two scratch buffers, and one paired SHA-256 at the end. It is a
// scheduling change and nothing else -- each digest is exactly the SHA-256 of
// that nonce's suffix array, so the proof of work is untouched.
//
// The hook keeps the native code out of this package, for the same reasons
// SuffixSort does (see sa_hook.go): it is a run-time fact whether the CPU has
// the SHA extensions and whether the library loaded, so the fallback has to live
// in the same binary.

// SHA256Pair, when not nil, computes the SHA-256 of a and of b together.
//
// It must write the SHA-256 of a into outA and of b into outB and return true.
// Returning false means "I did not do it", and two ordinary SHA-256s run
// instead; an implementation may decline anything it does not like -- a CPU
// without the instructions, a library that failed to load -- and the only cost
// is that pair being slower.
//
// The two lengths need not be equal and neither is a multiple of anything.
//
// Set it before mining starts. It is read once per pair without
// synchronisation, so changing it while threads are hashing is a data race.
var SHA256Pair func(a, b []byte, outA, outB *[32]byte) bool
