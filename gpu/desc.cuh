// desc.cuh -- the descriptor suffix sort, one thread block per hash.
//
// This is the CPU descriptor sort (native/descriptor.c) mapped onto a block. The
// idea is identical and stated in full there; the short version is that stage 1
// writes its whole 256-byte state after every iteration and an iteration
// rewrites at most 32 of those bytes, so the text is a sequence of near-copies.
// Treating a run of consecutive blocks as 256 columns and walking them right to
// left, a column that is constant across the run leaves the suffix order
// unchanged and costs nothing.
//
// On the CPU that is 2.8x libsais. The question this file exists to answer is
// whether it is also faster than the prefix doubling in sa_doubling.cuh, which
// is a general algorithm that knows none of the above.
//
// ---------------------------------------------------------------------------
// The mapping, and why it is this one
//
// The column walk is sequential in the columns: column r-1 needs the order from
// column r. So the parallelism has to come from somewhere else, and it does --
// there are ~62 runs per text and they are completely independent. One thread
// per run, 256 sequential steps each, over the ~4 blocks a run typically holds.
//
// That is a poor fit for a GPU on its own: 62 of 1024 threads busy. It works
// because the phases either side of it are wide. Finding the run boundaries is
// one thread per block-pair, the radix sort over descriptors is the block-wide
// one from blockradix.cuh, and the final scatter is one thread per descriptor.
//
// ---------------------------------------------------------------------------
// Slicing without atomics
//
// Two of the three arrays partition exactly, which removes the need to
// coordinate between runs:
//
//   arena     Every column of a run emits every one of its blocks exactly once,
//             so a run of `len` blocks emits 256*len positions. Summed over
//             runs that is 256 * total_blocks, which is n. Run starting at
//             block g owns arena[256*g .. 256*(g+len)).
//
//   order     A run needs `len` entries and the run lengths sum to the block
//             count, so the whole text needs one entry per block. Run starting
//             at block g owns order[g .. g+len).
//
// Descriptors do not partition -- a column emits between 1 and len of them --
// so those go through one atomic counter per hash. Emission order among
// descriptors sharing a key does not matter: the radix sort is by key and ties
// are settled afterwards by comparing suffixes.
//
// ---------------------------------------------------------------------------
// Correctness
//
// The suffix array of a string is unique, so this either produces exactly what
// the CPU produces or it is wrong. gpu/desc_test.exe checks it against the
// suffix arrays in gpu/vectors.bin, which Go computed, over all 512 texts.

#pragma once
#include <cstdint>
#include "blockradix.cuh"
#include "prof.cuh"

// A run ends where a block differs from its predecessor by more than this, which
// is what an RC4 rekey looks like: it rewrites all 256 bytes rather than the
// usual 32. Carrying a run through one makes every column of it non-constant and
// throws away the entire saving; the CPU measured 1868 texts/s for never
// splitting against 3433 for splitting here.
#ifndef DESC_SPLIT
#define DESC_SPLIT 160
#endif

// Blocks a run may span before it is cut anyway, or 0 for no cap.
//
// This knob exists only on the GPU, and the reason is the mapping. On the CPU a
// longer run is strictly better -- it shares more columns and it hands the
// global sort a bigger pre-ordered group, and native/descriptor.c measured that
// out to a plateau past 64 blocks. Here the walk is one thread per run, so runs
// are also the only parallelism the phase has: ~62 of them against BR_BLOCK
// threads, in the phase that is 38% of the kernel. Cutting runs shorter buys
// threads at the price of columns.
//
// The cut is on a fixed grid rather than by counting from the last boundary,
// because the boundary test is one thread per block and knows nothing about its
// neighbours; counting would make a parallel phase sequential to speed up a
// sequential one. A grid cut bounds run length just as well.
//
// Swept in the README. The answer is the default below.
#ifndef DESC_RUN_MAX
#define DESC_RUN_MAX 0
#endif

// Four-byte words a suffix comparison reads per iteration. See descSuffixLess.
#ifndef DESC_CMP_WORDS
#define DESC_CMP_WORDS 2
#endif

// Pieces the 256 columns of a run are cut into, each walked by its own thread.
//
// The walk is sequential in the columns because column rel-1 inherits the order
// from column rel, and that is why the phase runs on ~62 threads: one per run,
// 256 steps each. Measured, its cost is not the work -- an extra text read per
// position per column is free and an extra arena word is 4.6% -- it is the
// chain. So the chain is what has to be cut.
//
// It can be, because the inheritance is an optimisation and not a definition.
// The order at column rel is just the run's blocks sorted by the suffixes
// starting at rel, which depends on the text and nothing else. A thread can
// therefore start anywhere: sort directly at the top of its own piece, then
// inherit down through it. K pieces means K seed sorts per run instead of one,
// against a chain of 256/K instead of 256, and K times the threads.
//
// 1 is exactly the old shape, seed at column 255 and walk to 0, which is what
// makes it the honest baseline for the sweep in the README.
#ifndef DESC_CHUNKS
#define DESC_CHUNKS 4
#endif
#define DESC_CHUNK_COLS (256 / DESC_CHUNKS)

// Must divide 256 exactly, or the pieces do not cover the columns and the
// suffix array is silently wrong -- 3 and 6 were both measured producing a
// wrong answer before this line existed, because 3*85 and 6*42 leave a
// column nobody walks.
static_assert(DESC_CHUNKS * DESC_CHUNK_COLS == 256,
              "DESC_CHUNKS must divide 256");

// Blocks in the longest text, which is what the order and run tables are sized
// for. ASTRO_MAX_TEXT is ASTRO_MAX_TRIES*256, so this is ASTRO_MAX_TRIES.
#define DESC_MAX_BLOCKS 278

// How a key group that collides on all four bytes is resolved.
//
// These are rare and small on average -- the CPU measures 1,764 merged positions
// spread over 247 groups a text, about seven each -- but the largest over the
// 512 texts holds 879 positions in 262 lists, and the shape of the answer has to
// suit that one rather than the average.
//
// A per-thread local buffer was the first attempt and is not viable: sized for
// the worst group it is 3.5 KB a thread, which nvcc allocates for all 1,024
// whether they use it or not. Sized for the average it gives up on 497 of the
// 512 texts.
//
// So the merge runs out of block scratch instead, one thread per group, with
// each group using the slice of it that matches its own span in the output.
// Spans are disjoint, so no two threads touch the same words and no
// coordination is needed.
//
// Pairwise, not insertion: each descriptor's positions are already in order, so
// this is a merge of L sorted lists and costs O(total log L) comparisons rather
// than O(total * L). For the worst group that is 7,000 comparisons instead of
// 230,000, and a comparison here walks ~97 bytes of text before it decides.

// Bits of the descriptor word. The key takes the top 32 so the radix sorts on
// it directly, and the arena offset and length share the bottom 32.
#define DESC_LEN_BITS 12
#define DESC_LEN_MASK ((1u << DESC_LEN_BITS) - 1u)

struct DescScratch {
    uint64_t* words;    // n descriptor words, and the radix sort's other half
    uint64_t* words2;
    uint32_t* arena;    // n positions, partitioned by run
    int32_t*  offs;     // n output offsets, from the scan over lengths
    int32_t*  mbuf;     // n, the merge's other half; see the note above
};

#define DESC_BYTES_PER_SYMBOL (2 * 8 + 4 + 4 + 4)

__device__ __forceinline__ uint32_t descKey32(const uint8_t* t, int n, int p)
{
    if (p + 4 <= n) {
        // One 32-bit load and a byte swap. The text is not aligned to 4 here, so
        // this relies on the device allowing unaligned loads through a byte
        // gather; nvcc emits the four-byte form either way.
        return ((uint32_t)t[p] << 24) | ((uint32_t)t[p + 1] << 16) |
               ((uint32_t)t[p + 2] << 8) | (uint32_t)t[p + 3];
    }
    uint32_t k = 0;
    for (int i = 0; i < 4; i++) {
        k <<= 8;
        if (p + i < n) k |= t[p + i];
    }
    return k;
}

// True when the suffix at a sorts before the suffix at b. Padding past the end
// with nothing: a suffix that is a prefix of another sorts first.
// The tie-break, used where the leading key bytes did not separate two suffixes.
//
// Four bytes at a time, not one. The byte-at-a-time version issues one load,
// compares it, and branches, so each byte costs a full dependent global-memory
// round trip that nothing overlaps; the profile put the colliding merge at 28.4%
// of this kernel, and a comparison here walks ~97 bytes before it decides.
//
// The bytes are gathered into a big-endian word so that one integer comparison
// decides four of them: eight independent loads in flight, then one branch. The
// loads are what matters -- they can all be outstanding at once instead of
// forming a chain -- and the branch saving comes free with it.
// `from` is a count of leading bytes the caller already knows are equal, so the
// comparison may start past them. The merge knows four: a key group is by
// definition the descriptors whose first four bytes agree, so its every
// comparison used to open by gathering eight bytes across two cache lines,
// comparing them, finding them equal, and going round again. That is one full
// dependent global round trip per comparison, spent to re-derive the fact that
// put the two suffixes in the same group.
//
// Only when the shorter suffix actually has that many bytes. Two suffixes can
// share a key with fewer, because a key past the end of the text is zero
// padded, and then the bytes are not known to be equal -- they are not all
// there. m < from falls back to a full comparison, which is what a text end
// costs and there are at most three of them.
__device__ __forceinline__ bool descSuffixLessFrom(const uint8_t* t, int n,
                                                   int a, int b, int from)
{
    const int la = n - a, lb = n - b;
    const int m = la < lb ? la : lb;
    int i = (m >= from) ? from : 0;

    // DESC_CMP_WORDS four-byte words per iteration, compared in order.
    //
    // The loads are the cost and they are not coalesced -- two suffixes at
    // unrelated offsets, one thread. What can be changed is how many are in
    // flight. A four-byte step issues eight loads, waits, and decides, so a
    // comparison that walks ~97 bytes before it separates is ~24 dependent
    // round trips one after another. Widening the step does not read less, it
    // reads further ahead: the same bytes, fewer stalls, at the price of
    // fetching a little past the answer on the iteration that finds it.
    //
    // Colliding suffixes are long-prefix matches by construction -- they come
    // from near-copy blocks -- so reading ahead is rarely wasted. Swept in the
    // README.
    for (; i + 4 * DESC_CMP_WORDS <= m; i += 4 * DESC_CMP_WORDS) {
        uint32_t wa[DESC_CMP_WORDS], wb[DESC_CMP_WORDS];
#pragma unroll
        for (int w = 0; w < DESC_CMP_WORDS; w++) {
            const int o = i + 4 * w;
            wa[w] = ((uint32_t)t[a + o] << 24) | ((uint32_t)t[a + o + 1] << 16) |
                    ((uint32_t)t[a + o + 2] << 8) | (uint32_t)t[a + o + 3];
            wb[w] = ((uint32_t)t[b + o] << 24) | ((uint32_t)t[b + o + 1] << 16) |
                    ((uint32_t)t[b + o + 2] << 8) | (uint32_t)t[b + o + 3];
        }
#pragma unroll
        for (int w = 0; w < DESC_CMP_WORDS; w++) {
            if (wa[w] != wb[w]) return wa[w] < wb[w];
        }
    }

    for (; i + 4 <= m; i += 4) {
        const uint32_t wa = ((uint32_t)t[a + i] << 24) | ((uint32_t)t[a + i + 1] << 16) |
                            ((uint32_t)t[a + i + 2] << 8) | (uint32_t)t[a + i + 3];
        const uint32_t wb = ((uint32_t)t[b + i] << 24) | ((uint32_t)t[b + i + 1] << 16) |
                            ((uint32_t)t[b + i + 2] << 8) | (uint32_t)t[b + i + 3];
        if (wa != wb) return wa < wb;
    }
    for (; i < m; i++) {
        const uint8_t ca = t[a + i], cb = t[b + i];
        if (ca != cb) return ca < cb;
    }
    return la < lb;
}

__device__ __forceinline__ bool descSuffixLess(const uint8_t* t, int n, int a, int b)
{
    return descSuffixLessFrom(t, n, a, b, 0);
}

// descSuffixArrayBlock builds the suffix array of t[0:n] into sa[0:n]. The whole
// block must call it with blockDim.x == BR_BLOCK. Returns 0 on success, or a
// negative value when a key group was too large to merge (see DESC_MERGE_CAP),
// in which case sa holds nothing useful.
__device__ int descSuffixArrayBlock(const uint8_t* t, int n, int32_t* sa,
                                    DescScratch sc, BlockRadixScratch* sh)
{
    // One order and key array per chunk, so chunks of the same run do not
    // overwrite each other. At DESC_CHUNKS == 1 this is the single array the
    // walk has always used.
    __shared__ int32_t  s_order[DESC_CHUNKS * DESC_MAX_BLOCKS];
    __shared__ uint32_t s_wkey[DESC_CHUNKS * DESC_MAX_BLOCKS];
    __shared__ int32_t s_start[DESC_MAX_BLOCKS + 1];
    __shared__ int32_t s_flag[DESC_MAX_BLOCKS];
    __shared__ int32_t s_keep[DESC_MAX_BLOCKS];   // s_flag before the scan ate it
    __shared__ int     s_nruns;
    __shared__ int     s_ndesc;
    __shared__ int     s_fail;
    __shared__ int     s_bump;   // bump allocator for the merge's boundaries

    PROF_DECL;
    const int tid = threadIdx.x;
    const int nblocks = n / 256;
    PROF_MARK();

    if (tid == 0) { s_ndesc = 0; s_fail = 0; s_bump = 0; }
    blockRadixInit(sh);   // zeroes the per-tile matrix the sort relies on

    // ---- 1. run boundaries.
    //
    // A run ends only where a block differs wholesale from its predecessor, and
    // that is a purely local test, so every boundary is found in parallel. There
    // is no cap on run length to make sequential: the block count is under 278
    // and a cap above that would never fire.
    // Strided, not one block per thread: the block count reaches 277 and
    // BR_BLOCK is a knob that may be smaller than that. The flags are kept in a
    // second array because the scan below overwrites the first with its result,
    // and the scatter needs both.
    for (int g = tid; g < nblocks; g += BR_BLOCK) {
        int start;
        if (g == 0) {
            start = 1;
        } else {
            const uint8_t* a = t + (g - 1) * 256;
            const uint8_t* b = t + g * 256;
            int diff = 0;
            for (int i = 0; i < 256 && diff <= DESC_SPLIT; i++) diff += (a[i] != b[i]);
            start = (diff > DESC_SPLIT);
#if DESC_RUN_MAX > 0
            start |= ((g % DESC_RUN_MAX) == 0);
#endif
        }
        s_flag[g] = start;
        s_keep[g] = start;
    }
    __syncthreads();

    const int nruns = blockScanFlags(s_flag, nblocks, sh);
    for (int g = tid; g < nblocks; g += BR_BLOCK) {
        if (s_keep[g]) s_start[s_flag[g] - 1] = g;
    }
    if (tid == 0) { s_nruns = nruns; s_start[nruns] = nblocks; }
    __syncthreads();
    PROF_ADD(PH_D_RUNS);

    // ---- 2. the column walk, one thread per run.
    //
    // The keys slide rather than being re-read. A descriptor key is the four
    // bytes at ord[x]+rel, and stepping to column rel-1 keeps three of them:
    //
    //   K(q-1) = t[q-1]<<24 | t[q]<<16 | t[q+1]<<8 | t[q+2]
    //          = t[q-1]<<24 | (K(q) >> 8)
    //
    // an identity that needs no end-of-text special case, because the shift
    // drops exactly the byte the zero padding would have had to invent.
    //
    // That is worth more than the arithmetic it saves. The one byte it does
    // read, t[ord[x]+col], is the same byte the constant-column test compares
    // and the same byte the insertion sort orders by, so all three now share a
    // single load: the column costs `len` scattered byte reads where it used to
    // cost about six times that, and the grouping scan reads shared memory
    // instead of text. This loop is 51% of the suffix sort and the suffix sort
    // is 85% of a GPU hash, and it runs on ~62 of BR_BLOCK threads because the
    // runs are the only parallelism there is -- so its latency is the kernel's,
    // and cutting loads is the whole game.
    //
    // One thread per (run, chunk) rather than per run; see DESC_CHUNKS. At
    // DESC_CHUNKS == 1 this is one thread per run and the loop below is the
    // walk it always was.
    const int ntask = s_nruns * DESC_CHUNKS;

    for (int q = tid; q < ntask; q += BR_BLOCK) {
        const int r = q / DESC_CHUNKS;
        const int c = q - r * DESC_CHUNKS;    // 0 is the top of the text
        const int g0 = s_start[r];
        const int len = s_start[r + 1] - g0;
        const int base = g0 * 256;

        // Columns this thread owns, walked hi down to lo.
        const int hi = 255 - c * DESC_CHUNK_COLS;
        const int lo = hi - DESC_CHUNK_COLS + 1;

        // Chunks of the same run walk it at the same time, so each needs its own
        // order and keys -- hence the stride. Runs partition the blocks, so the
        // slices within a chunk are still disjoint.
        int32_t*  ord = s_order + (size_t)c * DESC_MAX_BLOCKS + g0;
        uint32_t* key = s_wkey  + (size_t)c * DESC_MAX_BLOCKS + g0;
        uint32_t* arena = sc.arena + (size_t)g0 * 256;

        // Seed with the order of the suffixes starting at each block's last
        // byte. Insertion sort: len is the blocks in a run, typically four.
        for (int i = 0; i < len; i++) ord[i] = base + i * 256;
        for (int i = 1; i < len; i++) {
            const int32_t v = ord[i];
            int j = i;
            while (j > 0 && descSuffixLess(t, n, v + hi, ord[j - 1] + hi)) {
                ord[j] = ord[j - 1];
                j--;
            }
            ord[j] = v;
        }
        // The chunk's one full key read; every column below it slides.
        for (int i = 0; i < len; i++) key[i] = descKey32(t, n, ord[i] + hi);

        for (int rel = hi; rel >= lo; rel--) {
            // Where this column's positions go. Every column of a run emits
            // every one of its blocks exactly once, so the offset is the column
            // number times the run length and needs no running total -- which is
            // what lets chunks write into the same arena without meeting.
            uint32_t aw = (uint32_t)(255 - rel) * (uint32_t)len;
            // Split the ordered suffixes into maximal groups sharing four
            // leading bytes, and record each as one descriptor.
            int i = 0;
            while (i < len) {
                const uint32_t k = key[i];
                int j = i + 1;
                while (j < len && key[j] == k) j++;

                const uint32_t off = (uint32_t)((size_t)g0 * 256 + aw);
                for (int x = i; x < j; x++) arena[aw++] = (uint32_t)(ord[x] + rel);

                const int d = atomicAdd(&s_ndesc, 1);
                sc.words[d] = ((uint64_t)k << 32) |
                              ((uint64_t)off << DESC_LEN_BITS) | (uint32_t)(j - i);
                i = j;
            }

            if (rel == lo) break;

            // Step left, reading each block's byte in this column exactly once.
            // Slide first, because the keys move whether or not the order does.
            //
            // The constant test walks ord rather than the blocks in address
            // order, which is the same set of bytes and so the same predicate:
            // every block of the run appears in ord exactly once.
            const int col = rel - 1;
            const uint8_t c0 = (uint8_t)t[ord[0] + col];
            key[0] = ((uint32_t)c0 << 24) | (key[0] >> 8);

            bool constant = true;
            for (int x = 1; x < len; x++) {
                const uint8_t c = (uint8_t)t[ord[x] + col];
                constant &= (c == c0);
                key[x] = ((uint32_t)c << 24) | (key[x] >> 8);
            }

            // A column holding the same byte in every block of the run prepends
            // the same byte to every suffix, so the order does not change and
            // there is nothing further to do. This is the whole saving.
            if (constant) continue;

            // Stable insertion sort by that one byte, which is now the top byte
            // of the slid key, so this reads no text at all. The existing order
            // is exactly the right tie-break, which is what makes one byte
            // enough. ord and key permute together or they stop corresponding.
            for (int x = 1; x < len; x++) {
                const int32_t v = ord[x];
                const uint32_t kv = key[x];
                const uint8_t kb = (uint8_t)(kv >> 24);
                int y = x;
                while (y > 0 && (uint8_t)(key[y - 1] >> 24) > kb) {
                    ord[y] = ord[y - 1];
                    key[y] = key[y - 1];
                    y--;
                }
                ord[y] = v;
                key[y] = kv;
            }
        }
    }

    PROF_ADD(PH_D_WALK);

    // The bytes past the last whole block belong to no run, so they get one
    // descriptor each. Their arena slots are the ones the runs did not use:
    // runs own 256 * nblocks of it and the arena is n long.
    for (int p = (int)(nblocks * 256) + tid; p < n; p += BR_BLOCK) {
        const uint32_t off = (uint32_t)p;
        sc.arena[off] = (uint32_t)p;
        const int d = atomicAdd(&s_ndesc, 1);
        sc.words[d] = ((uint64_t)descKey32(t, n, p) << 32) |
                      ((uint64_t)off << DESC_LEN_BITS) | 1u;
    }
    __syncthreads();
    PROF_ADD(PH_D_TAIL);

    const int nd = s_ndesc;

    // ---- 3. sort the descriptors by key. The key is the top 32 bits of the
    // word, so this is the block-wide radix sort with nothing packed around it.
    uint64_t* sorted = blockRadixSort(sc.words, sc.words2, nd, 32, 32, sh);
    PROF_ADD(PH_D_RADIX);

    // ---- 4. output offsets: an exclusive scan over the sorted lengths. A key
    // group's positions occupy one contiguous span whether or not it collides.
    for (int i = tid; i < nd; i += BR_BLOCK) {
        sc.offs[i] = (int32_t)(sorted[i] & DESC_LEN_MASK);
    }
    __syncthreads();
    blockScanFlags(sc.offs, nd, sh);   // inclusive; exclusive = offs[i] - len
    PROF_ADD(PH_D_SCAN);

    // ---- 5. the scatter.
    //
    // A descriptor whose key nothing else shares is already in its final place
    // relative to everything else, so its positions go straight out. That is
    // ~98.7% of them.
    for (int i = tid; i < nd; i += BR_BLOCK) {
        const uint64_t w = sorted[i];
        const uint32_t key = (uint32_t)(w >> 32);
        const uint32_t len = (uint32_t)(w & DESC_LEN_MASK);
        const uint32_t off = (uint32_t)((w >> DESC_LEN_BITS) & 0xFFFFFu);

#ifndef DESC_NO_MERGE
        const bool first = (i == 0) || ((uint32_t)(sorted[i - 1] >> 32) != key);
        const bool last = (i == nd - 1) || ((uint32_t)(sorted[i + 1] >> 32) != key);
        if (!(first && last)) continue;
#else
        /* DESC_NO_MERGE writes every descriptor, colliding or not, in whatever
         * order the arena holds it, and skips step 6 entirely. The answer is
         * then wrong inside colliding groups but must still be a *permutation*
         * of 0..n-1, and that is what separates two very different faults.
         *
         * It earned its place finding one. The merge was writing 41 positions
         * twice and losing 41 others per text; with this defined the output was
         * a clean permutation, which said the walk, the arena partition, the
         * radix sort and the offset scan were all correct and put the fault in
         * the merge, where it was: the boundary arrays were indexed by output
         * offset over a closed interval, so each group's last boundary word
         * landed on the next group's first. */
        (void)key;
#endif

        const int32_t o = sc.offs[i] - (int32_t)len;
        for (uint32_t x = 0; x < len; x++) sa[o + x] = (int32_t)sc.arena[off + x];
    }

    PROF_ADD(PH_D_SCATTER);

    // The radix sort leaves its result in one of its two arrays; the other is
    // finished with, and is where the merge below keeps its list boundaries.
    // Two int32 per uint64, so it holds 2n of them and the merge needs 2n.
    int32_t* const dead = (int32_t*)(sorted == sc.words ? sc.words2 : sc.words);

    // ---- 6. the groups that collide on all four bytes. One thread per group:
    // there are ~247 of them per text holding ~7 positions each, so this is a
    // small amount of work spread thinly, and a block-wide treatment would cost
    // more in barriers than it saved.
#ifndef DESC_NO_MERGE
    for (int i = tid; i < nd; i += BR_BLOCK) {
        const uint32_t key = (uint32_t)(sorted[i] >> 32);
        if (i > 0 && (uint32_t)(sorted[i - 1] >> 32) == key) continue;  // not first

        int j = i + 1;
        while (j < nd && (uint32_t)(sorted[j] >> 32) == key) j++;
        if (j == i + 1) continue;   // singleton, already written

        const int32_t o = sc.offs[i] - (int32_t)(sorted[i] & DESC_LEN_MASK);
        const int nlist0 = j - i;

        // The data goes at this group's own output offset: output spans are
        // disjoint, so no two groups can touch the same words.
        int32_t* a = sa + o;            // the data, laid out end to end
        int32_t* b = sc.mbuf + o;       // and its ping-pong partner

        // The boundaries cannot be placed the same way, and getting that wrong
        // is subtle enough to be worth spelling out. A group of L lists needs
        // L+1 boundary words -- a closed interval -- and L can equal its total
        // when every list holds one position. Indexing them by the output offset
        // then puts one group's last boundary word exactly on the next group's
        // first, because the next group's offset is this one's offset plus its
        // total. Two groups then corrupt each other, and the symptom is a
        // handful of positions duplicated and a handful missing.
        //
        // So they come from a bump allocator instead. Demand is 2*(L+1) summed
        // over colliding groups, which is a few thousand words against the 2n
        // this array holds.
        const int nb = 2 * (nlist0 + 1);
        const int bbase = atomicAdd(&s_bump, nb);
        if (bbase + nb > 2 * n) { atomicExch(&s_fail, 1); continue; }
        int32_t* ba = dead + bbase;
        int32_t* bb = dead + bbase + nlist0 + 1;

        int total = 0;
        for (int k = i; k < j; k++) {
            const uint64_t w = sorted[k];
            const uint32_t len = (uint32_t)(w & DESC_LEN_MASK);
            const uint32_t off = (uint32_t)((w >> DESC_LEN_BITS) & 0xFFFFFu);
            ba[k - i] = total;
            for (uint32_t x = 0; x < len; x++) a[total + x] = (int32_t)sc.arena[off + x];
            total += (int)len;
        }
        ba[nlist0] = total;

        // Merge adjacent pairs until one list is left. An odd list out is
        // carried across unchanged.
        int nlist = nlist0;
        while (nlist > 1) {
            int outLists = 0, pos = 0;
            for (int l = 0; l < nlist; l += 2) {
                bb[outLists++] = pos;
                if (l + 1 == nlist) {
                    for (int x = ba[l]; x < ba[l + 1]; x++) b[pos++] = a[x];
                } else {
                    int p0 = ba[l], e0 = ba[l + 1];
                    int p1 = ba[l + 1], e1 = ba[l + 2];
                    while (p0 < e0 && p1 < e1) {
                        // 4: every position here shares the group's key.
                        b[pos++] = descSuffixLessFrom(t, n, a[p1], a[p0], 4)
                                       ? a[p1++] : a[p0++];
                    }
                    while (p0 < e0) b[pos++] = a[p0++];
                    while (p1 < e1) b[pos++] = a[p1++];
                }
            }
            bb[outLists] = pos;
            int32_t* tv = a; a = b; b = tv;
            int32_t* tb = ba; ba = bb; bb = tb;
            nlist = outLists;
        }

        // An odd number of merge rounds leaves the answer in the scratch half.
        if (a != sa + o) {
            for (int x = 0; x < total; x++) sa[o + x] = a[x];
        }
    }
#endif
    __syncthreads();
    PROF_ADD(PH_D_MERGE);

    return s_fail ? -1 : 0;
}
