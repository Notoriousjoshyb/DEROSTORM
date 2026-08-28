package astrobwtv3

// Tooling hooks used by the GPU port. They cost one nil check per hash and are
// nil in every normal build, so the mining hot path is unaffected.

// CaptureStage1, when non-nil, is called with the accumulated state stream just
// before the suffix array is built. The slice is only valid for the duration of
// the call -- copy it if you need to keep it.
var CaptureStage1 func(text []byte)

// SuffixArray builds the AstroBWTv3 suffix array for text into a caller-owned
// scratch buffer and returns it. This is the exact function the GPU kernel has
// to reproduce, exposed so its output can be diffed against the CPU's.
func SuffixArray(text []byte, scratch *ScratchData) []int32 {
	n := len(text)
	copy(scratch.data[:n], text)
	text_32_fast(scratch.data[:n], scratch.sa[:n], scratch.lms[:], scratch.satmp[:])
	return scratch.sa[:n]
}
