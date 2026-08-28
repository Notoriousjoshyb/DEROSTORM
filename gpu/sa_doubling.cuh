// sa_doubling.cuh -- suffix array by prefix doubling, one thread block per hash.
//
// This is option B: the same answer as sais.cuh by a different route, chosen
// for how it uses memory rather than for how few operations it performs.
//
// SA-IS is O(n) and prefix doubling is O(n log n), so on paper this is the
// worse algorithm. It wins, if it wins, on access pattern. sais.cuh gives each
// thread half a megabyte and has it chase pointers around it; 16,000 of those
// is a 7 GB working set against a 64 MB L2, so nearly every access is a cold
// 32-byte sector fetch and the card runs at about a tenth of its bandwidth.
// Here a block owns a hash, only 84-336 hashes are in flight, and every access
// is a coalesced streaming one. Measured: ~660 GB/s against SA-IS's ~90.
//
// The access pattern is what makes this the better mapping, and the group
// skipping below is what keeps the volume down. Note that it is no longer
// bandwidth bound at the tuned settings: measured on an RTX 5080 it moves about
// 350 GB/s of a ~960 GB/s peak, with the memory controller busy 65% of the time
// and the card at 195 W of 360 W. See the note at the top of blockradix.cuh for
// where the remaining headroom looks like it is.
//
// ---------------------------------------------------------------------------
// Group skipping
//
// After each round the array is partitioned into groups of suffixes that are
// still tied. A group of one is finished forever: its place in the suffix array
// cannot change again. Only groups of two or more need re-sorting, and they
// shrink fast.
//
// How fast is a property of the data, and the data is not random: stage 1
// appends the 256-byte state after every iteration and most iterations change
// at most 32 of those bytes, so consecutive blocks are near copies. Measured
// over 512 real texts by gpu/lcpstat: mean LCP 97 bytes, worst 2,301, so 13
// rounds worst case -- but the share still tied falls 100, 93, 88, 85, 79, 70,
// 53, 27, 3.6, 1.7, 0.4, 0.02 percent. Sorting the whole array every round
// costs 13n; sorting only the tied groups costs 6n. That measurement is what
// makes this worth building: assuming RC4 noise would have predicted 3 rounds
// and been wrong by 4x.
//
// The compaction is sound because a group of two or more is entirely unfinished
// and therefore entirely present in the active list, so its members stay
// contiguous; and the sort key leads with the group rank, so groups keep their
// order relative to each other.
//
// ---------------------------------------------------------------------------
// One thing tried and rejected
//
// Each round begins with three dependent gathers per element: j = sa[act[q]],
// then rank[j] and rank[j+k]. Carrying j forward through the compaction removes
// the first of them and shortens the chain from three deep to two -- it is
// exactly what the next round would have read back out of sa. Cycle attribution
// put that phase at 15.6% of the sort, so it looked worth having.
//
// It measured slower: 7.43 KH/s against 7.73. Two int32 arrays more per symbol
// is 12% more block scratch, and the extra write per surviving element in the
// compaction costs more than the gather saves. The reason the gather is cheap is
// that at the tuned block count the hot arrays are largely L2 resident, so a
// "random" read is not the DRAM round trip it looks like.
//
// ---------------------------------------------------------------------------
// Convention matches sais.cuh and Go: the suffix array of text[0:n] with no
// sentinel, shorter suffixes sorting before longer ones they prefix. That falls
// out of giving rank 0 to "past the end", below every real rank.

#pragma once
#include <cstdint>
#include "blockradix.cuh"

// Per-hash scratch, all block-private and streamed.
//
//   sa            the answer, and the only array holding the full order
//   rank          suffix index -> the position its group starts at
//   tmp, tmp2     group markers, scan results and compaction flags
//   act, act2     the positions still tied, ascending, ping-ponged
//   wordA, wordB  the packed sort words, ping-ponged
//
// The sort must never be handed `sa` as one of its ping-pong buffers: it would
// overwrite the first m entries of the finished order with the active run.
//
// 40 bytes per text byte, so about 2.8 MB for a 71 KB text. Only `sa`, `rank`
// and the two key buffers are touched a full n times per round; the rest are
// touched m times, and m is what group skipping shrinks.
struct SADoublingScratch {
    int32_t*  sa;
    int32_t*  rank;
    int32_t*  tmp;
    int32_t*  tmp2;
    int32_t*  act;
    int32_t*  act2;
    uint64_t* wordA;
    uint64_t* wordB;
};

#define SAD_BYTES_PER_SYMBOL (6 * 4 + 2 * 8)

// One 64-bit word per element carries both what is sorted and what is being
// sorted, laid out so the sort never has to touch a second array:
//
//     bits [2v, 2v+r)   r1, the rank of this suffix
//     bits [v,  2v)     r2, the rank of the suffix k further on, plus one
//     bits [0,  v)      j,  the suffix index itself
//
// A rank and an index are both positions in [0, n), so all three fields are the
// same width, and for a 71 KB text that is 17 bits each: 51 bits, comfortably
// inside a word. The sort is told to order by the top two fields and to leave
// the bottom one alone, so the index rides along for free.
//
// The pair-of-arrays version this replaced moved 12 bytes per element per pass
// and needed two more n-sized arrays to ping-pong the values through.
__device__ __forceinline__ uint64_t sadPack(int r1, int r2, int j, int vbits) {
    return ((uint64_t)r1 << (2 * vbits)) | ((uint64_t)(uint32_t)r2 << vbits) | (uint32_t)j;
}
__device__ __forceinline__ int sadIndex(uint64_t w, int vbits) {
    return (int)(w & (((uint64_t)1 << vbits) - 1));
}

// Bytes the first sort orders by, and therefore where the doubling starts.
//
// It does not have to be a power of two: the invariant is that ranks
// distinguish the first S characters and each round doubles that, so k starts
// at S and doubles from there. What it does have to satisfy is
// SAD_SEED * 9 + 17 <= 64, since the seed key and the packed suffix index share
// a word -- so at most 5. Measured: 1, 2 and 4 give 10.1k, 11.9k and 13.4k
// suffix arrays a second.
#ifndef SAD_SEED
#define SAD_SEED 4
#endif

// Smallest b with (1 << b) > v.
__device__ __forceinline__ int bitsFor(int v) {
    int b = 0;
    while ((1 << b) <= v) b++;
    return b;
}

// Marks group starts in key[0:m] and turns them into "the position my group
// starts at" for every entry, leaving the answer in dst[0:m].
//
// pos[q] is the position entry q occupies in the full array; pass null when the
// entries are the whole array and therefore already in position order. Handing
// this the suffix indices instead of the positions produces a suffix array that
// is right in its first entry and wrong everywhere after it, which is exactly
// what it looks like when you get it wrong.
// keyShift drops the packed suffix index, so two entries compare equal when
// their ranks agree, which is the whole point -- the index below is different
// for every element and would make every group a singleton.
__device__ void groupStarts(const uint64_t* word, const int32_t* pos, int keyShift,
                            int32_t* dst, int m, BlockRadixScratch* sh)
{
    for (int q = threadIdx.x; q < m; q += BR_BLOCK) {
        const int here = pos ? pos[q] : q;
        dst[q] = (q == 0 || (word[q] >> keyShift) != (word[q - 1] >> keyShift)) ? here : -1;
    }
    __syncthreads();
    blockScanMax(dst, m, sh);
}

// Builds the suffix array of text[0:n] into sc.sa. The whole block must call
// this with blockDim.x == BR_BLOCK.
__device__ void saDoublingBlock(const uint8_t* text, int n,
                                SADoublingScratch sc, BlockRadixScratch* sh)
{
    if (n <= 0) return;
    if (n == 1) {
        if (threadIdx.x == 0) sc.sa[0] = 0;
        __syncthreads();
        return;
    }

    blockRadixInit(sh);

    // Ranks and indices are all positions in [0, n), so one width serves for
    // every field of the packed word.
    const int vbits = bitsFor(n);
    const int kbits = 2 * vbits;

    // ---- round 0: order by the leading SAD_SEED bytes, not by one
    //
    // Prefix doubling has to start somewhere, and starting at one byte means
    // the first two rounds do nothing a wider first sort could not have done in
    // one. Those two rounds are the most expensive in the whole run, because
    // nothing has been resolved yet and the active set is still the entire
    // array: measured over 512 real texts, 100% of suffixes are still tied
    // going into k=1 and 93% into k=2.
    //
    // Seeding with four bytes replaces them. The arithmetic, in passes over n:
    // one byte then k=1 then k=2 costs 2 + 5 + 4.65 = 11.65; four bytes costs 6.
    // About a fifth of the whole sort.
    //
    // Each byte takes nine bits, not eight, so that "ran off the end of the
    // text" is a value below every real byte -- including a real 0. Bytes are
    // stored plus one and the hole is 0.
    //
    // Eight bits and a 0 pad looks like it should work, on the reasoning that a
    // tie here is harmless because the doubling loop resolves whatever groups
    // it is handed. It does not, and the failure is narrow enough to be worth
    // recording: 3 of 512 real texts, wrong only in the first entry of the
    // array. Two suffixes that both run off the end within k get r2 = 0 each,
    // so the doubling cannot separate them either, and the tie survives to the
    // end. A suffix of three zero bytes and one of four are such a pair. Round
    // 0 is the only place they can be ordered, so it has to be able to.
    for (int i = threadIdx.x; i < n; i += BR_BLOCK) {
        uint64_t key = 0;
        for (int d = 0; d < SAD_SEED; d++)
            key = (key << 9) | (uint64_t)(i + d < n ? (uint32_t)text[i + d] + 1u : 0u);
        sc.wordA[i] = (key << vbits) | (uint32_t)i;
    }
    __syncthreads();

    uint64_t* w = blockRadixSort(sc.wordA, sc.wordB, n, vbits, SAD_SEED * 9, sh);

    for (int i = threadIdx.x; i < n; i += BR_BLOCK) sc.sa[i] = sadIndex(w[i], vbits);
    __syncthreads();

    // ---- group starts, ranks, and the first active list
    groupStarts(w, nullptr, vbits, sc.tmp, n, sh);

    for (int q = threadIdx.x; q < n; q += BR_BLOCK)
        sc.rank[sc.sa[q]] = sc.tmp[q];
    __syncthreads();

    // A position is finished when it is the whole of its group: it starts one
    // and the next position starts another.
    for (int q = threadIdx.x; q < n; q += BR_BLOCK)
        sc.tmp2[q] = (sc.tmp[q] == q && (q + 1 == n || sc.tmp[q + 1] == q + 1)) ? 0 : 1;
    __syncthreads();

    for (int q = threadIdx.x; q < n; q += BR_BLOCK) sc.tmp[q] = sc.tmp2[q];
    __syncthreads();
    int m = blockScanFlags(sc.tmp, n, sh);
    for (int q = threadIdx.x; q < n; q += BR_BLOCK)
        if (sc.tmp2[q]) sc.act[sc.tmp[q] - 1] = q;
    __syncthreads();

    // ---- double until nothing is tied
    //
    // Each round separates suffixes agreeing on 2k bytes, so k doubles and the
    // loop ends after ceil(log2(worst LCP)) + 1 of them. The k > n guard is a
    // belt and braces stop: by then every suffix has been compared over its
    // whole length and no group can survive.
    PROF_DECL;
    for (int k = SAD_SEED; m > 0; k <<= 1) {
        PROF_COUNT(g_rounds);
        PROF_MARK();
        for (int q = threadIdx.x; q < m; q += BR_BLOCK) {
            const int j  = sc.sa[sc.act[q]];
            const int r1 = sc.rank[j];
            // 0 means "the suffix ends before here". It sorts below every real
            // rank, which is what puts a shorter suffix ahead of the longer one
            // it prefixes.
            const int r2 = (j + k < n) ? sc.rank[j + k] + 1 : 0;
            sc.wordA[q] = sadPack(r1, r2, j, vbits);
        }
        __syncthreads();
        PROF_ADD(PH_KEYBUILD);

        uint64_t* sorted = blockRadixSort(sc.wordA, sc.wordB, m, vbits, kbits, sh);

        // The sort keeps its own timer, so restart this one behind it. Without
        // this the round's next phase bills for the whole sort as well, and
        // every share in the table is wrong -- it read as 36% of the sort being
        // spent writing one int32 per element.
        PROF_MARK();

        // Put the sorted run back. The set of positions it occupies is
        // unchanged, so writing them in ascending order is the whole update.
        for (int q = threadIdx.x; q < m; q += BR_BLOCK)
            sc.sa[sc.act[q]] = sadIndex(sorted[q], vbits);
        __syncthreads();
        PROF_ADD(PH_SAWRITE);

        groupStarts(sorted, sc.act, vbits, sc.tmp, m, sh);
        PROF_ADD(PH_GROUPS);

        for (int q = threadIdx.x; q < m; q += BR_BLOCK)
            sc.rank[sadIndex(sorted[q], vbits)] = sc.tmp[q];
        __syncthreads();
        PROF_ADD(PH_RANKUPD);

        for (int q = threadIdx.x; q < m; q += BR_BLOCK)
            sc.tmp2[q] = (sc.tmp[q] == sc.act[q] &&
                          (q + 1 == m || sc.tmp[q + 1] == sc.act[q + 1])) ? 0 : 1;
        __syncthreads();

        for (int q = threadIdx.x; q < m; q += BR_BLOCK) sc.tmp[q] = sc.tmp2[q];
        __syncthreads();
        const int m2 = blockScanFlags(sc.tmp, m, sh);
        PROF_ADD(PH_FLAGS);

        // Compact into the other list rather than in place: the destination
        // index is never greater than the source, so an in-place compaction
        // would let one thread overwrite a slot another has yet to read.
        for (int q = threadIdx.x; q < m; q += BR_BLOCK)
            if (sc.tmp2[q]) sc.act2[sc.tmp[q] - 1] = sc.act[q];
        __syncthreads();

        PROF_ADD(PH_COMPACT);

        int32_t* swap = sc.act; sc.act = sc.act2; sc.act2 = swap;
        m = m2;

        if (k > n) break;
    }
}
