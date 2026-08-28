package astrobwtv3

// The suffix-sort hook.
//
// Building the suffix array of the accumulated stage-1 state is ~90% of a hash,
// and text_32_fast is not the last word on doing it: libsais measures 1.35x on
// one thread and 1.42x on fifteen over exactly this data, with byte-for-byte
// identical output. This lets a miner supply that without this package taking on
// a native dependency, and without anything else that imports it changing at
// all -- the blockchain path leaves the hook nil and gets the Go sort.
//
// Why a hook rather than a build tag: the faster sort is a shared library bound
// at run time, so whether it is available is a run-time fact, not a build-time
// one. A machine without it has to fall back, and the fallback has to be in the
// same binary.

// SuffixSort, when not nil, replaces the built-in suffix sort.
//
// It must fill sa[0:len(text)] with the suffix array of text and return true.
// Returning false means "I did not do it" and the built-in sort runs instead, so
// a sorter may decline any input it does not like -- a length it cannot handle,
// a library that failed to load -- at the cost of that hash being slower and
// nothing else.
//
// The contract it must honour is exact: the suffix array of a string is unique,
// so any correct implementation produces the same answer and the proof of work
// is unchanged. An *incorrect* one changes every hash it touches, and because
// AstroBWTv3 swallows panics and returns a falsified hash, the symptom would be
// a miner that simply never finds a share. Anything installed here belongs under
// the same byte-for-byte comparison as the built-in sort: see
// astrobwt/difftest.
//
// Set it before mining starts. It is read once per hash without synchronisation,
// so changing it while threads are hashing is a data race.
var SuffixSort func(text []byte, sa []int32) bool
