/* descriptor.c -- a suffix sort that exploits how AstroBWTv3 builds its text.
 *
 * libsais is a general suffix sorter and treats the stage-1 text as arbitrary
 * bytes. It is not arbitrary. Stage 1 writes out its whole 256-byte state after
 * every one of ~277 iterations, and an iteration rewrites at most 32 of those
 * bytes, so consecutive 256-byte blocks are near-copies of each other. Measured
 * over 512 real texts (go test ./gpu/lcpstat -run SharedColumns): across a run
 * of four consecutive blocks, 45.7% of the 256 byte offsets hold the same value
 * in every block; across two, 73.2%.
 *
 * That is what this exploits, and the way it does is worth stating plainly
 * because the rest of the file is bookkeeping around it.
 *
 * ---------------------------------------------------------------------------
 * The idea
 *
 * Take a run of consecutive blocks and look at it as 256 columns. Walk the
 * columns from right to left, keeping the run's blocks in the order of their
 * suffixes starting at the current column:
 *
 *     order[] holds the run's block starts, arranged so that the suffixes
 *     beginning at order[i] + rel are in ascending order.
 *
 * At rel = 255 that is just the order of the suffixes starting at each block's
 * last byte, which is one small sort.
 *
 * Stepping from rel to rel-1 prepends one byte to every one of those suffixes.
 * So the new order is the old order re-sorted by that byte, *stably* -- the
 * existing order is exactly the right tie-break. And if the column is constant
 * across the run, every suffix gets the same byte prepended, the stable sort is
 * the identity, and there is nothing to do at all.
 *
 * That is the whole saving: for ~70% of columns the order is inherited rather
 * than computed, and the sort never looks at those suffixes again.
 *
 * ---------------------------------------------------------------------------
 * Getting from there to a suffix array
 *
 * The walk gives, for every (run, column), a small list of suffixes already in
 * their true relative order. Those lists have to be merged into one array of n.
 *
 * Merging thousands of lists directly would cost more than it saved, so each
 * list is first split into maximal groups sharing their leading four bytes, and
 * each group is recorded as a descriptor: a 32-bit key, and a slice of an arena
 * holding its positions in order. Descriptors are then radix sorted by that key
 * -- four passes over a few tens of thousands of entries -- which puts almost
 * every suffix in its final place, because four bytes nearly determine the
 * order. Only descriptors that collide on all four bytes need comparing, and
 * those groups are small.
 *
 * ---------------------------------------------------------------------------
 * Correctness
 *
 * The suffix array of a string is unique, so this either produces exactly what
 * libsais produces or it is wrong; there is no third option and no judgement
 * call. It is checked against libsais over all 512 real texts by
 * native\sabench.exe, and the miner keeps libsais as the fallback.
 *
 * Where the run boundaries fall does not affect correctness -- only speed. Any
 * partition of the blocks works; a partition that follows where stage 1 rewrote
 * everything works better.
 *
 * libsais is Apache-2.0, Copyright (c) 2021-2025 Ilya Grebnov. This file is
 * part of DeroStorm and borrows the idea above from the Dirtybird C miner
 * (MIT), which arrived at it first.
 */

#include <stdint.h>
#include <stddef.h>
#include <string.h>
#include <stdlib.h>

#include "descriptor.h"

/* AVX2 is used in the merge scatter, the run-boundary popcount, the constant
 * column mask, and the suffix comparison. MSVC does not define __AVX2__ from
 * /arch:AVX2, so this asks the other way round: __AVX2__ for the compilers that
 * do define it, and the MSVC AVX flag for the one that does not.
 * Define DSA_NO_AVX2 to force the scalar path. */
#if !defined(DSA_NO_AVX2) && (defined(__AVX2__) || (defined(_MSC_VER) && defined(__AVX__)))
#define DSA_AVX2 1
#include <immintrin.h>
#endif

/* NEON is the same three places on arm64 -- Apple Silicon and linux/arm64.
 * The masked store has no NEON equivalent of _mm256_maskstore_epi32, so that
 * one stays a short scalar write; everything that was a 32-byte vector on
 * x86 is a 16-byte vector here. */
#if !defined(DSA_AVX2) && (defined(__ARM_NEON) || defined(__ARM_NEON__))
#define DSA_NEON 1
#include <arm_neon.h>
#endif

#if defined(_MSC_VER)
#define DSA_CTZ32(v) _tzcnt_u32(v)
#define DSA_CTZ64(v) _tzcnt_u64(v)
#elif defined(__GNUC__) || defined(__clang__)
#define DSA_CTZ32(v) ((uint32_t)__builtin_ctz(v))
#define DSA_CTZ64(v) ((uint32_t)__builtin_ctzll(v))
#endif

#if defined(_MSC_VER)
#include <intrin.h>
#define DSA_BSWAP32(v) _byteswap_ulong(v)
#define DSA_BSWAP64(v) _byteswap_uint64(v)
#elif defined(__GNUC__) || defined(__clang__)
#define DSA_BSWAP32(v) __builtin_bswap32(v)
#define DSA_BSWAP64(v) __builtin_bswap64(v)
#else
#define DSA_BSWAP32(v) (((v) >> 24) | (((v) >> 8) & 0xff00u) |                         (((v) << 8) & 0xff0000u) | ((v) << 24))
#define DSA_BSWAP64(v) (((uint64_t)DSA_BSWAP32((uint32_t)(v)) << 32) |          (uint64_t)DSA_BSWAP32((uint32_t)((v) >> 32)))
#endif

/* SCAFFOLDING: leave one thing out of the merge's singleton group and time what
 * is left. Every setting but 0 produces a wrong suffix array, so sabench has to
 * be run with its force argument: native\sabench.exe gpu/vectors.bin 3 1 1
 *
 *   1  no arena read (the value stored is the column alone)
 *   2  no store
 *   3  no group body at all -- only the walk over the descriptors and the key
 *      test that separates a singleton from a collision
 *
 * One thread, best of three over the 512 real texts, against 6,711 texts/s:
 * 6,793 at 1, 6,669 at 2, and 9,157 at 3. So the body is 37% of the sort, and
 * neither the read nor the store is any of it on its own -- taking either one
 * away leaves the loop exactly as fast, and taking both away is worth a third.
 * That is a throughput limit rather than a latency one, and it is why the store
 * shapes below all measure the same.
 */
#ifndef DSA_MABLATE
#define DSA_MABLATE 0
#endif

/* How the singleton group's positions are written.
 *
 *   0  AVX2 masked store, lane mask from a table indexed by len   6,645 / 6,797
 *   1  the same store, lane mask from a compare instead           6,685 / 6,696
 *   2  AVX-512 masked store, needs /arch:AVX512                   6,836 / 6,811
 *
 * Two rounds each, one thread, interleaved. The mask table load is free and the
 * AVX-512 form is at best 1-2%, which does not justify a second code path and a
 * runtime dispatch -- and /arch:AVX512 changes codegen across the whole file
 * rather than just here, so even that 1-2% is not attributable. 0 ships.
 */
#ifndef DSA_MSTYLE
#define DSA_MSTYLE 0
#endif

/* ---------------------------------------------------------------------------
 * Phase timing, compiled to nothing unless DSA_PROF is defined.
 *
 * The three phases have completely different shapes -- a column walk that is
 * linear in the text, a radix sort over descriptors, and a merge whose cost is
 * set by how often four bytes fail to separate two suffixes -- so which one to
 * work on is not a thing to reason about from the source.
 * ------------------------------------------------------------------------ */
#ifdef DSA_PROF
#include <intrin.h>
unsigned long long dsa_prof[6];
unsigned long long dsa_stat[9];
#define PROF_T0()      const unsigned long long _t0 = __rdtsc()
#define PROF_ADD(i, t) (dsa_prof[i] += __rdtsc() - (t))
#define PROF_MARK()    unsigned long long _m = __rdtsc()
#define PROF_LAP(i)    do { const unsigned long long _c = __rdtsc(); \
                            dsa_prof[i] += _c - _m; _m = _c; } while (0)
#define PROF_STAT(i, v) (dsa_stat[i] += (unsigned long long)(v))
#define PROF_MAX(i, v)                                              \
    do {                                                            \
        const unsigned long long _v = (unsigned long long)(v);       \
        if (_v > dsa_stat[i]) dsa_stat[i] = _v;                      \
    } while (0)
#else
#define PROF_T0()      ((void)0)
#define PROF_ADD(i, t) ((void)0)
#define PROF_MARK()    ((void)0)
#define PROF_LAP(i)    ((void)0)
#define PROF_STAT(i, v) ((void)0)
#define PROF_MAX(i, v)  ((void)0)
#endif

/* dsa_prof slots */
#define PH_RUNS   0   /* finding run boundaries */
#define PH_WALK   1   /* the column walk, including descriptor emission */
#define PH_RADIX  2   /* radix sorting the descriptors */
#define PH_MERGE  3   /* resolving descriptors that share a key */
#define PH_TAIL   4
#define PH_COLL   5   /* nested inside PH_MERGE: the colliding groups alone */

/* dsa_stat slots */
#define ST_DESC     0  /* descriptors emitted */
#define ST_KEYGRP   1  /* key groups in the merge */
#define ST_COLLIDE  2  /* key groups with more than one descriptor */
#define ST_MERGED   3  /* positions passing through the merge */
#define ST_CMP      4  /* suffix_less calls */
#define ST_RUNS     5  /* runs */
#define ST_MAXDESC  6  /* high-water descriptors in one text */
#define ST_MAXMERGE 7  /* high-water positions in one colliding key group */
#define ST_MAXLIST  8  /* high-water descriptors in one colliding key group */

/* Blocks per run.
 *
 * Longer runs share fewer columns -- 73.2% across two blocks, 45.7% across
 * four, 18.8% across eight -- which argues for short ones. Measured, that is
 * backwards: the pre-ordered group a run produces is what saves the global
 * sort, and bigger groups win by more than the lost column skips cost.
 * Against libsais, sweeping only this:
 *
 *     2 blocks   485 texts/s   -60%      32 blocks  2065 texts/s   +68%
 *     4 blocks   643 texts/s   -47%      64 blocks  2078 texts/s   +69%
 *     8 blocks   963 texts/s   -21%     128 blocks  2097 texts/s   +70%
 *    16 blocks  1598 texts/s   +30%     512 blocks  2099 texts/s   +72%
 *
 * It plateaus past 64 because DSA_RUN_SPLIT below is what actually ends runs by
 * then. This is left high so that threshold decides.
 */
#ifndef DSA_RUN_MAX
#define DSA_RUN_MAX 512
#endif

/* A run is cut short when a block differs from its predecessor by more than
 * this many bytes, which is what an RC4 rekey inside stage 1 looks like: it
 * rewrites all 256 rather than the usual 32.
 *
 * This is not a tuning knob, it is load bearing. Carrying a run through a rekey
 * makes almost every column of it non-constant, and the column step is an
 * insertion sort over the run's blocks -- quadratic in a length that is then
 * unbounded. Never splitting measured 443 texts/s against 2,139 for splitting
 * at 160, which is a third of libsais rather than nearly double it.
 *
 *     64 -> 2118/s      96 -> 2097/s      160 -> 2139/s      never -> 443/s
 */
#ifndef DSA_RUN_SPLIT
#define DSA_RUN_SPLIT 160
#endif

#define DSA_BLOCK 256

/* Key bytes, and the radix that follows from it.
 *
 * Four bytes with three 11-bit passes is the starting point. Three bytes is
 * worth testing rather than dismissing: it makes the key coarser, so more
 * suffixes share one and there are fewer, larger groups -- fewer group
 * iterations in the merge and only two radix passes -- at the cost of more
 * groups needing a real comparison. The profile says collisions are nearly free
 * here (1.3% of groups, 4,326 comparisons a text), so there is room to trade.
 *
 * Measured at four bytes it was flat: 3,365 texts/s at three against 3,355 at
 * four, because the key width is not what sets the group count. A descriptor is
 * emitted per (run, column) and there are 62 runs of 256 columns, so ~15,900
 * groups exist before any key collides; three bytes only took 19,160 groups down
 * to 18,511. Widening does not help either, for the same reason.
 *
 * Three won later, and only because something else changed. The tie-break used
 * to call memcmp with a length of tens of thousands of bytes; once it resolved
 * the first 32 bytes inline instead, an extra collision stopped being worth
 * paying a radix pass to avoid, and three bytes measured 5,319 texts/s against
 * 5,099. That is the whole reason this constant is three: a knob that measures
 * flat is not settled, it is waiting for the thing that pays for it. */
#ifndef DSA_KEY_BYTES
#define DSA_KEY_BYTES 3
#endif


/* A descriptor is eight bytes, not twelve.
 *
 * The radix sort reads and writes every descriptor once per pass, three passes,
 * twenty thousand descriptors: at twelve bytes that is 1.45 MB of traffic per
 * text, which is about half of everything this sort moves. Packing the offset
 * and the length into one word takes a third off it, and the effect is larger
 * the more threads are mining, because they are all pulling on the same L3.
 *
 * The offset indexes the arena, which holds n positions, and n is at most
 * MAX_LENGTH = 98,303: 17 bits. The length is the number of blocks in a run
 * sharing four leading bytes, so at most DSA_RUN_MAX: 10 bits at 512. Twenty
 * seven bits of the thirty two, checked below so a future DSA_RUN_MAX cannot
 * quietly overflow it.
 */
#define DSA_LEN_BITS  12
#define DSA_LEN_MASK  ((1u << DSA_LEN_BITS) - 1u)
#define DSA_MAX_OFF   (1u << (32 - DSA_LEN_BITS))

/* The arena holds a block index, not a position, and the column lives in the
 * descriptor's spare byte.
 *
 * A three-byte key leaves the top eight bits of Desc.key unused, and the radix
 * sort already only orders the low twenty-four (two passes of twelve). Every
 * position this sort emits is `block * 256 + column`, and every position in one
 * descriptor shares the column -- a descriptor *is* one column of one run -- so
 * the column belongs in the descriptor and the arena needs only the block.
 *
 * That halves the arena: 277 KB of uint32 becomes 138 KB of uint16, on a
 * working set that is about a megabyte against a 1 MB L2. It halves what the
 * column walk writes and what the merge reads, and the merge pays one shift and
 * one OR to put the two halves back together.
 *
 * The GPU sort has been shaped this way since 1.6; this is the same change on
 * the CPU. Define DSA_COMPACT_ARENA=0 for the old layout. */
#ifndef DSA_COMPACT_ARENA
#define DSA_COMPACT_ARENA 1
#endif

/* Columns that share an order share one arena slice.
 *
 * This is what the compact arena is for. With positions in the arena every
 * column's entries differ, because the column is part of the number; with block
 * indices they differ only when the *order* differs, and a constant column
 * leaves the order alone by definition. About 70% of columns are constant
 * across their run, and a one-block run has 256 of them, so the walk writes the
 * arena a fraction as often and the arena itself stops being one slot per
 * suffix.
 *
 * Correctness does not depend on which slice a descriptor points at, only that
 * the slice holds its blocks in order: the groups of a column partition
 * order[0..blocks) in order, so every group is a contiguous run of whatever
 * slice currently holds it.
 *
 * Requires DSA_COMPACT_ARENA. Define DSA_ARENA_REUSE=0 to write every column. */
#ifndef DSA_ARENA_REUSE
#define DSA_ARENA_REUSE DSA_COMPACT_ARENA
#endif

/* Carry "every block of this run has the same key" as a state.
 *
 * The same_key table below asks the question one column at a time, from the
 * constant-column mask. Carrying the answer instead is strictly stronger --
 * three constant columns mean the keys agree, but the keys can agree without
 * them -- and it buys two things the table cannot.
 *
 * While the keys are all equal, `blocks` of them in memory are copies of one
 * register, so a constant column slides the register and leaves the array
 * alone. And a constant column prepends the *same byte* to every suffix, so it
 * needs one text read rather than one per block, whether the keys agree or not:
 * the old slide read t[order[x]+col] for every x on every column, and about 70%
 * of columns are constant.
 *
 * The state can only change at a column that is not constant, which is the one
 * column that has to read per block anyway. Ported from gpu/desc.cuh, where it
 * measured +2%. Define DSA_UNIFORM=0 for the old shape. */
#ifndef DSA_UNIFORM
#define DSA_UNIFORM 1
#endif


#if DSA_ARENA_REUSE && !DSA_COMPACT_ARENA
#error "DSA_ARENA_REUSE needs the compact arena: positions carry the column"
#endif

#if DSA_COMPACT_ARENA
typedef uint16_t Arena;
#define DSA_KEY_MASK   0x00FFFFFFu
#define DSA_KEY_EQ(a, b) ((((a) ^ (b)) & DSA_KEY_MASK) == 0)
#define DSA_KEYREL(k, rel) ((k) | ((uint32_t)(rel) << 24))
#define DSA_REL_OF(k)  ((k) >> 24)
#define DSA_POS(a, rel) (((uint32_t)(a) << 8) | (uint32_t)(rel))
#define DSA_ARENA_VAL(blk, pos) (blk)
#else
typedef uint32_t Arena;
#define DSA_KEY_MASK   0xFFFFFFFFu
#define DSA_KEY_EQ(a, b) ((a) == (b))
#define DSA_KEYREL(k, rel) (k)
#define DSA_REL_OF(k)  0u
#define DSA_POS(a, rel) (a)
#define DSA_ARENA_VAL(blk, pos) (pos)
#endif

typedef struct {
    uint32_t key;    /* the four leading bytes, big-endian so it sorts as bytes */
    uint32_t packed; /* arena offset in the high bits, length in the low ones */
} Desc;

static inline uint32_t dsa_off(Desc d) { return d.packed >> DSA_LEN_BITS; }
static inline uint32_t dsa_len(Desc d) { return d.packed & DSA_LEN_MASK; }
static inline uint32_t desc_pack(uint32_t off, uint32_t len)
{
    return (off << DSA_LEN_BITS) | len;
}
/* The packing above gives the length DSA_LEN_BITS bits and the offset the rest.
 * Both bounds are checked here rather than trusted: a run longer than the length
 * field, or a text longer than the offset field, would corrupt descriptors
 * silently and the only symptom would be a wrong suffix array. */
typedef char dsa_len_fits[(DSA_RUN_MAX <= (int)DSA_LEN_MASK) ? 1 : -1];
typedef char dsa_off_fits[((256 * 384) <= (int)DSA_MAX_OFF) ? 1 : -1];

/* Scratch caps.
 *
 * These were all sized for their worst case, and the worst cases are absurdly
 * far from what the texts actually do. Measured over all 512 real texts:
 *
 *                            worst case     really needed
 *   descriptors              n = 68755      26992
 *   positions in a key group n = 68755      879
 *   lists in a key group     n = 68755      262
 *
 * That mattered because it is per thread. At 3.0 MB of scratch a thread, and
 * 1.3 MB more for the hash itself, fifteen threads wanted 64 MB -- and this
 * machine has 96 MB of L3. The hashrate curve showed it plainly: 2231 H/s on
 * one thread, 1495 on thirteen, and *falling* past fourteen.
 *
 * So they are sized for what happens, with a way out when it does not:
 *
 *   - Descriptors start at half the worst case. On overflow the buffer grows to
 *     the full bound and the text is redone. Costs one wasted walk, and never
 *     happened on any of the 512.
 *   - A key group larger than DSA_GROUP_CAP makes the whole call fail, and the
 *     caller falls back to libsais for that hash. Four times the observed
 *     maximum (879 positions, 262 lists over 512 texts), so this is for a text
 *     unlike anything measured, and giving up a hash to libsais costs a few
 *     microseconds rather than being wrong. Halved from 8192: merge/merge2/bnd/
 *     bnd2 are 4x (cap+8) words per thread, so this saves ~128 KB per thread,
 *     ~1.9 MB at 15 threads, kept out of the shared L3.
 */
#ifndef DSA_GROUP_CAP
#define DSA_GROUP_CAP 4096
#endif

typedef struct {
    Arena*    arena;    /* n block indices, grouped by descriptor */
    Desc*     desc;     /* descriptors, then their radix-sorted copy */
    Desc*     desc2;
    uint32_t* order;    /* the run's block starts, in current column order */
    uint32_t* order2;   /* ping-pong for the counting sort in the column step */
    uint32_t* merge;    /* ping-pong space for merging a key group */
    uint32_t* merge2;
    uint32_t* bnd;      /* list boundaries within the above, ping-ponged too */
    uint32_t* bnd2;
    size_t    cap;      /* text length this scratch was sized for */
    size_t    desc_cap;
} Scratch;

/* One scratch per thread, kept for the life of the thread. Mining threads call
 * runtime.LockOSThread, so that is one per mining thread, and a hash costs no
 * allocation. */
#if defined(_WIN32)
#define DSA_TLS __declspec(thread)
#else
#define DSA_TLS __thread
#endif
static DSA_TLS Scratch* g_scratch = NULL;

static void scratch_free(Scratch* s)
{
    if (!s) return;
    free(s->arena);
    free(s->desc);
    free(s->desc2);
    free(s->order);
    free(s->order2);
    free(s->merge);
    free(s->merge2);
    free(s->bnd);
    free(s->bnd2);
    free(s);
}

/* desc_bound is the number of descriptors a text of n bytes can possibly
 * produce: one per (run, column), plus one per split within a column, and a
 * split cannot make more groups than the run has blocks. Summed over runs that
 * is 256 columns times the block count, which is n; plus the tail. */
static size_t desc_bound(size_t n) { return n + DSA_BLOCK + 8; }

static Scratch* scratch_get(size_t n, size_t want_desc)
{
    Scratch* s = g_scratch;

    if (s && s->cap >= n && s->desc_cap >= want_desc) return s;
    scratch_free(s);
    g_scratch = NULL;

    s = (Scratch*)calloc(1, sizeof(Scratch));
    if (!s) return NULL;
    s->cap = n;
    s->desc_cap = want_desc;
    const size_t gcap = DSA_GROUP_CAP;
    s->arena = (Arena*)malloc((n + 16) * sizeof(Arena));
    s->desc  = (Desc*)malloc(want_desc * sizeof(Desc));
    s->desc2 = (Desc*)malloc(want_desc * sizeof(Desc));
    s->order = (uint32_t*)malloc((DSA_RUN_MAX + 8) * sizeof(uint32_t));
    s->order2 = (uint32_t*)malloc((DSA_RUN_MAX + 8) * sizeof(uint32_t));
    s->merge = (uint32_t*)malloc((gcap + 8) * sizeof(uint32_t));
    s->merge2 = (uint32_t*)malloc((gcap + 8) * sizeof(uint32_t));
    s->bnd  = (uint32_t*)malloc((gcap + 8) * sizeof(uint32_t));
    s->bnd2 = (uint32_t*)malloc((gcap + 8) * sizeof(uint32_t));
    if (!s->arena || !s->desc || !s->desc2 || !s->order || !s->order2 ||
        !s->merge || !s->merge2 || !s->bnd || !s->bnd2) {
        scratch_free(s);
        return NULL;
    }
    g_scratch = s;
    return s;
}

/* key32 reads the four bytes at p, padding past the end of the text with 0.
 *
 * The padding has to be order preserving, and 0 is: a suffix that ends early
 * gets zeros where a longer one has real bytes, so its key is less than or
 * equal, and less is exactly the order a prefix should sort in. Equal falls
 * through to a full comparison, which settles it. */
static inline uint32_t key32(const uint8_t* t, size_t n, size_t p)
{
    if (p + 4 <= n) {
        /* One unaligned load and a byte swap, not four loads and three shifts.
         * Called about ninety thousand times per text, once per (column, block)
         * pair plus once more per group. */
        uint32_t v;
        memcpy(&v, t + p, 4);
        return DSA_BSWAP32(v) >> (8 * (4 - DSA_KEY_BYTES));
    }
    uint32_t k = 0;
    for (size_t i = 0; i < DSA_KEY_BYTES; i++) {
        k <<= 8;
        if (p + i < n) k |= t[p + i];
    }
    return k;
}

/* suffix_less_from is the tie-break, used where the leading key bytes were
 * not enough. `from` is a count of leading bytes the caller already knows are
 * equal -- a colliding key group agrees on DSA_KEY_BYTES by definition -- so
 * the comparison starts past them rather than gathering them again and finding
 * them equal. The GPU does the same (descSuffixLessFrom); the CPU used not to,
 * and every merge comparison opened by re-proving the fact that put the two
 * suffixes in the same group.
 *
 * memcmp is the obvious body and the wrong one. The length handed to it is
 * n - max(a,b), tens of thousands of bytes, so it is a call into a routine that
 * sets up an alignment-handling loop -- for a comparison that in this text
 * decides within the first few bytes almost every time. Comparing 32 bytes
 * inline first, with a vector compare where we have one, settles nearly all of
 * them without the call, and memcmp still finishes the rare pair that shares a
 * long prefix.
 *
 * The byte swap is what makes an integer comparison give the byte order: on a
 * little-endian load the first byte of the text lands in the low bits, so it
 * has to be moved to the top before comparing as a number. */
static inline int suffix_less_from(const uint8_t* t, size_t n, uint32_t a, uint32_t b,
                                    uint32_t from)
{
    PROF_STAT(ST_CMP, 1);
    const size_t la = n - a, lb = n - b;
    const size_t m = la < lb ? la : lb;
    size_t i = (m >= (size_t)from) ? (size_t)from : 0;

    /* Eight bytes first. The global mean LCP is ~97, but that is not how far
     * a merge comparison walks: the caller already skipped the shared key, and
     * most pairs then separate in the next word. A 32-byte vector as the first
     * step pulls two cache lines per suffix on every call to help the long tail;
     * at 15 threads those extra lines contend in L3. Eight bytes settles the
     * common case; the vector loop below still covers the tail. */
    if (i + 8 <= m) {
        uint64_t x, y;
        memcpy(&x, t + a + i, 8);
        memcpy(&y, t + b + i, 8);
        if (x != y) return DSA_BSWAP64(x) < DSA_BSWAP64(y);
        i += 8;
    }

#if (defined(__AVX512BW__) || defined(DSA_AVX512)) && !defined(DSA_NO_WIDE_CMP)
    for (; i + 64 <= m; i += 64) {
        const __m512i x = _mm512_loadu_si512((const void*)(t + a + i));
        const __m512i y = _mm512_loadu_si512((const void*)(t + b + i));
        const __mmask64 eq = _mm512_cmpeq_epi8_mask(x, y);
        if (eq != ~(__mmask64)0) {
            const uint32_t tz = (uint32_t)DSA_CTZ64((unsigned long long)~eq);
            return t[a + i + tz] < t[b + i + tz];
        }
    }
#endif

#if defined(DSA_AVX2) && !defined(DSA_NO_WIDE_CMP)
    /* 32 bytes at a time for the pairs that shared the first word. */
    for (; i + 32 <= m; i += 32) {
        const __m256i x = _mm256_loadu_si256((const __m256i*)(t + a + i));
        const __m256i y = _mm256_loadu_si256((const __m256i*)(t + b + i));
        const uint32_t eq =
            (uint32_t)_mm256_movemask_epi8(_mm256_cmpeq_epi8(x, y));
        if (eq != 0xFFFFFFFFu) {
            const uint32_t tz = DSA_CTZ32(~eq);
            return t[a + i + tz] < t[b + i + tz];
        }
    }
#elif defined(DSA_NEON)
    for (; i + 16 <= m; i += 16) {
        const uint8x16_t x = vld1q_u8(t + a + i);
        const uint8x16_t y = vld1q_u8(t + b + i);
        const uint8x16_t eq = vceqq_u8(x, y);
        const uint64x2_t eq64 = vreinterpretq_u64_u8(eq);
        if (vgetq_lane_u64(eq64, 0) != ~0ull || vgetq_lane_u64(eq64, 1) != ~0ull) {
            const uint64_t lo = vgetq_lane_u64(eq64, 0);
            if (lo != ~0ull) {
                const uint32_t tz = (uint32_t)__builtin_ctzll(~lo);
                return t[a + i + tz / 8] < t[b + i + tz / 8];
            }
            const uint64_t hi = vgetq_lane_u64(eq64, 1);
            const uint32_t tz = (uint32_t)__builtin_ctzll(~hi);
            return t[a + i + 8 + tz / 8] < t[b + i + 8 + tz / 8];
        }
    }
#endif

    const size_t remain = m - i;
    const size_t q = remain < 32 ? (remain & ~(size_t)7) : 32;
    for (size_t k = 0; k < q; k += 8) {
        uint64_t x, y;
        memcpy(&x, t + a + i + k, 8);
        memcpy(&y, t + b + i + k, 8);
        if (x != y) return DSA_BSWAP64(x) < DSA_BSWAP64(y);
    }
    const int c = memcmp(t + a + i + q, t + b + i + q, remain - q);
    if (c != 0) return c < 0;
    return la < lb;   /* a prefix sorts before what it prefixes */
}

static inline int suffix_less(const uint8_t* t, size_t n, uint32_t a, uint32_t b)
{
    return suffix_less_from(t, n, a, b, 0);
}

/* ---------------------------------------------------------------------------
 * the column step
 * ------------------------------------------------------------------------ */

/* Stepping one column left prepends a byte to every suffix in the run, so the
 * new order is the old one re-sorted by that byte -- *stably*, because the
 * existing order is already the correct tie-break for everything after it.
 *
 * Two ways to do a stable sort by one byte, and which is cheaper depends only
 * on how many blocks are in the run.
 *
 * Insertion sort is O(blocks^2) but its constant is tiny and it moves nothing
 * when the column is nearly sorted, which it often is. Counting sort is
 * O(blocks + 256): two passes over the run plus a 256-bucket prefix sum, so it
 * pays a fixed ~512 operations however short the run is.
 *
 * Break-even is around fifty blocks, and DSA_COUNT_MIN is set from measurement
 * rather than from that arithmetic.
 *
 * Why this matters beyond the column step itself: the quadratic term is what
 * forced DSA_RUN_SPLIT to cut runs at an RC4 rekey. A rekey rewrites all 256
 * bytes, so no column across it is constant, so every column pays the sort --
 * quadratic in a run length that would otherwise be unbounded. With the
 * quadratic gone, runs are free to cross a rekey, and longer runs make bigger
 * pre-ordered groups, which is what the global merge actually wants.
 */
#ifndef DSA_COUNT_MIN
#define DSA_COUNT_MIN 40
#endif

/* Above this many blocks in a run, the rank sort below stops being the cheapest
 * way to do a column: it costs blocks*blocks comparisons, against a few times
 * blocks for an insertion sort on nearly-sorted input.
 *
 * Runs average 4.4 blocks, so this is the case that matters and not a corner.
 *
 * Swept at one thread, three interleaved repeats, median texts/s:
 *
 *     off  5,321      8  5,359     16  5,439     24  5,281
 *
 * Sixteen, then, and +2.2% over the insertion sort. Worth recording that this
 * was expected to be much bigger: ablating column_step out of the walk entirely
 * said it was 54% of the walk and 21% of the whole sort, which would have made a
 * four-times-faster column step worth 16%. It was not, so most of that 21% is
 * not the sort at all -- removing column_step also freezes the block order,
 * which changes what the emit loop sees. An ablation measures the phase plus
 * everything downstream that depended on it. */
#ifndef DSA_RANK_MAX
#define DSA_RANK_MAX 16
#endif

static inline void column_step(const uint8_t* t, uint32_t* order, uint32_t* tmp,
                               uint32_t blocks, uint32_t col)
{
    if (blocks <= DSA_RANK_MAX) {
        /* Sort by rank: for each element, count how many sort below it, and that
         * count is where it goes.
         *
         * The insertion sort below is the obvious way and, at these sizes, the
         * expensive one. Ablation puts column_step at 54% of the column walk and
         * 21% of the whole sort -- 40 cycles a call to put four bytes in order --
         * and that is not the comparisons, it is the inner while loop, whose trip
         * count is one to four and unpredictable. Four mispredicts at 20 cycles
         * is the whole 40.
         *
         * This has no data-dependent branch at all: two loops of fixed length,
         * with the comparison producing a 0 or 1 that is added. Sixteen
         * comparisons for a four-block run beats four mispredicts.
         *
         * Stability comes out of the packing rather than the algorithm. The key
         * is the column byte shifted up with the element's own index underneath,
         * so no two packed values are equal and equal bytes are ordered by where
         * they already were -- which is exactly the tie-break the walk needs. */
        uint32_t v[DSA_RANK_MAX];
        for (uint32_t x = 0; x < blocks; x++) {
            v[x] = ((uint32_t)t[order[x] + col] << 9) | x;
        }
        for (uint32_t x = 0; x < blocks; x++) {
            uint32_t r = 0;
            for (uint32_t y = 0; y < blocks; y++) r += (v[y] < v[x]);
            tmp[r] = order[x];
        }
        memcpy(order, tmp, blocks * sizeof(uint32_t));
        return;
    }

    if (blocks < DSA_COUNT_MIN) {
        for (uint32_t x = 1; x < blocks; x++) {
            const uint32_t v = order[x];
            const uint8_t key = t[v + col];
            uint32_t y = x;
            while (y > 0 && t[order[y - 1] + col] > key) {
                order[y] = order[y - 1];
                y--;
            }
            order[y] = v;
        }
        return;
    }

    uint32_t cnt[256];
    memset(cnt, 0, sizeof(cnt));
    for (uint32_t x = 0; x < blocks; x++) cnt[t[order[x] + col]]++;

    uint32_t sum = 0;
    for (uint32_t b = 0; b < 256; b++) {
        const uint32_t c = cnt[b];
        cnt[b] = sum;
        sum += c;
    }
    /* Ascending x, so equal bytes keep their relative order: stable. */
    for (uint32_t x = 0; x < blocks; x++) {
        const uint32_t v = order[x];
        tmp[cnt[t[v + col]]++] = v;
    }
    memcpy(order, tmp, blocks * sizeof(uint32_t));
}

/* column_step_keys is column_step with the keys riding along.
 *
 * The walk now slides a key per block rather than re-reading four text bytes
 * at every column -- see emit_run. Stepping one column left then has to
 * permute those keys with the order, or they stop corresponding. The sort is
 * still by one byte (the new leading byte of the slid key), stably, so the
 * three bodies below are the same three as column_step, just moving two
 * arrays instead of one. */
static inline void column_step_keys(uint32_t* order, uint32_t* keys, uint32_t* tmp,
                                    uint32_t blocks)
{
    const uint32_t top = 8u * (DSA_KEY_BYTES - 1);

    if (blocks == 4) {
        const uint32_t v0 = ((keys[0] >> top) << 9) | 0u;
        const uint32_t v1 = ((keys[1] >> top) << 9) | 1u;
        const uint32_t v2 = ((keys[2] >> top) << 9) | 2u;
        const uint32_t v3 = ((keys[3] >> top) << 9) | 3u;
        const uint32_t r0 = (uint32_t)(v1 < v0) + (uint32_t)(v2 < v0) + (uint32_t)(v3 < v0);
        const uint32_t r1 = (uint32_t)(v0 < v1) + (uint32_t)(v2 < v1) + (uint32_t)(v3 < v1);
        const uint32_t r2 = (uint32_t)(v0 < v2) + (uint32_t)(v1 < v2) + (uint32_t)(v3 < v2);
        const uint32_t r3 = (uint32_t)(v0 < v3) + (uint32_t)(v1 < v3) + (uint32_t)(v2 < v3);
        uint32_t ko[4], kk[4];
        ko[r0] = order[0]; kk[r0] = keys[0];
        ko[r1] = order[1]; kk[r1] = keys[1];
        ko[r2] = order[2]; kk[r2] = keys[2];
        ko[r3] = order[3]; kk[r3] = keys[3];
        memcpy(order, ko, 4 * sizeof(uint32_t));
        memcpy(keys, kk, 4 * sizeof(uint32_t));
        return;
    }

    if (blocks <= DSA_RANK_MAX) {
        uint32_t v[DSA_RANK_MAX];
        uint32_t ko[DSA_RANK_MAX], kk[DSA_RANK_MAX];
        for (uint32_t x = 0; x < blocks; x++) {
            v[x] = ((keys[x] >> top) << 9) | x;
        }
        for (uint32_t x = 0; x < blocks; x++) {
            uint32_t r = 0;
            for (uint32_t y = 0; y < blocks; y++) r += (v[y] < v[x]);
            ko[r] = order[x];
            kk[r] = keys[x];
        }
        memcpy(order, ko, blocks * sizeof(uint32_t));
        memcpy(keys, kk, blocks * sizeof(uint32_t));
        return;
    }

    if (blocks < DSA_COUNT_MIN) {
        for (uint32_t x = 1; x < blocks; x++) {
            const uint32_t vo = order[x];
            const uint32_t vk = keys[x];
            const uint32_t kb = vk >> top;
            uint32_t y = x;
            while (y > 0 && (keys[y - 1] >> top) > kb) {
                order[y] = order[y - 1];
                keys[y] = keys[y - 1];
                y--;
            }
            order[y] = vo;
            keys[y] = vk;
        }
        return;
    }

    uint32_t cnt[256];
    memset(cnt, 0, sizeof(cnt));
    for (uint32_t x = 0; x < blocks; x++) cnt[keys[x] >> top]++;

    uint32_t sum = 0;
    for (uint32_t b = 0; b < 256; b++) {
        const uint32_t c = cnt[b];
        cnt[b] = sum;
        sum += c;
    }
    uint32_t ktmp[DSA_RUN_MAX];
    for (uint32_t x = 0; x < blocks; x++) {
        const uint32_t slot = cnt[keys[x] >> top]++;
        tmp[slot] = order[x];
        ktmp[slot] = keys[x];
    }
    memcpy(order, tmp, blocks * sizeof(uint32_t));
    memcpy(keys, ktmp, blocks * sizeof(uint32_t));
}

/* ---------------------------------------------------------------------------
 * the column walk
 * ------------------------------------------------------------------------ */

/* emit_run walks one run's columns right to left, appending descriptors.
 *
 * order[] is maintained as described at the top of the file: on entry to each
 * column the suffixes at order[i] + rel are in ascending order.
 */
/* order[x] + column, written into the arena. Typical run length is ~4, so the
 * 4-wide path is the one that matters; 8-wide covers the long-run tail. */
static inline void emit_ord_plus(Arena* dst, const uint32_t* order,
                                 uint32_t n, uint32_t r)
{
#if DSA_COMPACT_ARENA
    /* order[] holds block starts, so the block index is one shift and the
     * column is not written at all. Packed sixteen bits at a time. */
    (void)r;
    uint32_t x = 0;
#if defined(DSA_AVX2)
    for (; x + 8 <= n; x += 8) {
        const __m256i v = _mm256_srli_epi32(
            _mm256_loadu_si256((const __m256i*)(order + x)), 8);
        /* packus takes the low sixteen bits of each lane, then the permute
         * undoes the 128-bit lane interleave it leaves behind. */
        const __m256i pk = _mm256_permute4x64_epi64(
            _mm256_packus_epi32(v, v), 0xD8);
        _mm_storeu_si128((__m128i*)(dst + x), _mm256_castsi256_si128(pk));
    }
#endif
    for (; x < n; x++) dst[x] = (Arena)(order[x] >> 8);
#else
#if defined(DSA_AVX2)
    const __m256i add8 = _mm256_set1_epi32((int32_t)r);
    const __m128i add4 = _mm_set1_epi32((int32_t)r);
    uint32_t x = 0;
    for (; x + 8 <= n; x += 8) {
        const __m256i v = _mm256_loadu_si256((const __m256i*)(order + x));
        _mm256_storeu_si256((__m256i*)(dst + x), _mm256_add_epi32(v, add8));
    }
    for (; x + 4 <= n; x += 4) {
        const __m128i v = _mm_loadu_si128((const __m128i*)(order + x));
        _mm_storeu_si128((__m128i*)(dst + x), _mm_add_epi32(v, add4));
    }
    for (; x < n; x++) dst[x] = order[x] + r;
#elif defined(DSA_NEON)
    const uint32x4_t add4 = vdupq_n_u32(r);
    uint32_t x = 0;
    for (; x + 4 <= n; x += 4) {
        vst1q_u32(dst + x, vaddq_u32(vld1q_u32(order + x), add4));
    }
    for (; x < n; x++) dst[x] = order[x] + r;
#else
    for (uint32_t x = 0; x < n; x++) dst[x] = order[x] + r;
#endif
#endif
}


static int emit_run(const uint8_t* t, size_t n, uint32_t first_block,
                    uint32_t blocks, Scratch* s,
                    size_t* arena_len, size_t* desc_len)
{
    /* Each column emits at most `blocks` descriptors, so checking once per
     * column keeps the check off the inner loop. */
    const size_t desc_room = s->desc_cap;
    const uint32_t base = first_block * DSA_BLOCK;
    /* A one-block run needs no suffix ordering or constant-column discovery:
     * every column is already a singleton. Keep the rolling key used by the
     * general walk, but emit directly from registers. Single-block runs are
     * about 27% of real runs, so avoiding the order/key arrays and 256-entry
     * constant masks removes common fixed work on every architecture. */
    if (blocks == 1) {
        if (*desc_len + DSA_BLOCK > desc_room) return 0;
        uint32_t key = key32(t, n, base + DSA_BLOCK - 1);
#if DSA_ARENA_REUSE
        /* One block, so every column's arena entry is the same block index and
         * all 256 descriptors point at one slot. */
        const uint32_t one_off = (uint32_t)*arena_len;
        s->arena[(*arena_len)++] = (Arena)first_block;
#endif
        for (int rel = DSA_BLOCK - 1; rel >= 0; rel--) {
            Desc* d = &s->desc[*desc_len];
            d->key = DSA_KEYREL(key, rel);
#if DSA_ARENA_REUSE
            d->packed = desc_pack(one_off, 1);
#else
            d->packed = desc_pack((uint32_t)*arena_len, 1);
            s->arena[(*arena_len)++] = (Arena)DSA_ARENA_VAL(first_block,
                                                      base + (uint32_t)rel);
#endif
            (*desc_len)++;
            if (rel != 0) {
                key = ((uint32_t)t[base + (uint32_t)rel - 1] <<
                       (8 * (DSA_KEY_BYTES - 1))) | (key >> 8);
            }
        }
        return 1;
    }
    /* Two blocks also fit entirely in registers. Stable prepend ordering is a
     * single state bit: unequal new bytes decide it, equal bytes retain the
     * previous suffix order. This avoids the shared-sized temporary arrays and
     * the constant-column mask for another 17% of real runs. */
    if (blocks == 2) {
        if (*desc_len + 2 * DSA_BLOCK > desc_room) return 0;
        const uint32_t b0 = base;
        const uint32_t b1 = base + DSA_BLOCK;
        uint32_t key0 = key32(t, n, b0 + DSA_BLOCK - 1);
        uint32_t key1 = key32(t, n, b1 + DSA_BLOCK - 1);
        int swapped = suffix_less(t, n, b1 + DSA_BLOCK - 1,
                                  b0 + DSA_BLOCK - 1);

#if DSA_ARENA_REUSE
        uint32_t aw_off = 0;
        int aw_dirty = 1;
#endif
        for (int rel = DSA_BLOCK - 1; rel >= 0; rel--) {
            const uint32_t first = swapped ? b1 : b0;
            const uint32_t second = swapped ? b0 : b1;
            const uint32_t first_key = swapped ? key1 : key0;
            const uint32_t second_key = swapped ? key0 : key1;
#if DSA_ARENA_REUSE
            if (aw_dirty) {
                aw_off = (uint32_t)*arena_len;
                s->arena[(*arena_len)++] = (Arena)(first >> 8);
                s->arena[(*arena_len)++] = (Arena)(second >> 8);
                aw_dirty = 0;
            }
            const uint32_t off = aw_off;
#else
            const uint32_t off = (uint32_t)*arena_len;
            s->arena[(*arena_len)++] = (Arena)DSA_ARENA_VAL(first >> 8,
                                                      first + (uint32_t)rel);
            s->arena[(*arena_len)++] = (Arena)DSA_ARENA_VAL(second >> 8,
                                                      second + (uint32_t)rel);
#endif

            Desc* d = &s->desc[(*desc_len)++];
            d->key = DSA_KEYREL(first_key, rel);
            d->packed = desc_pack(off, first_key == second_key ? 2 : 1);
            if (first_key != second_key) {
                d = &s->desc[(*desc_len)++];
                d->key = DSA_KEYREL(second_key, rel);
                d->packed = desc_pack(off + 1, 1);
            }

            if (rel != 0) {
                const uint32_t col = (uint32_t)rel - 1;
                const uint8_t c0 = t[b0 + col];
                const uint8_t c1 = t[b1 + col];
                key0 = ((uint32_t)c0 << (8 * (DSA_KEY_BYTES - 1))) | (key0 >> 8);
                key1 = ((uint32_t)c1 << (8 * (DSA_KEY_BYTES - 1))) | (key1 >> 8);
                if (c0 != c1) {
                    const int next = c1 < c0;
#if DSA_ARENA_REUSE
                    aw_dirty |= (next != swapped);
#endif
                    swapped = next;
                }
            }
        }
        return 1;
    }
    uint32_t* order = s->order;

    /* Seed: the suffixes starting at each block's last byte, sorted. Insertion
     * sort -- blocks is at most DSA_RUN_MAX. */
    for (uint32_t i = 0; i < blocks; i++) order[i] = base + i * DSA_BLOCK + 255;
    for (uint32_t i = 1; i < blocks; i++) {
        const uint32_t v = order[i];
        uint32_t j = i;
        while (j > 0 && suffix_less(t, n, v, order[j - 1])) {
            order[j] = order[j - 1];
            j--;
        }
        order[j] = v;
    }
    for (uint32_t i = 0; i < blocks; i++) order[i] -= 255;

    /* Which columns are constant across every block of the run.
     *
     * Done a column at a time this is 256 short loops with a data-dependent
     * break, which is the worst shape a branch predictor can be given: ~46% of
     * the columns run to the end and the rest stop after one or two blocks, with
     * nothing to predict from. Done 32 columns at a time it is a compare and an
     * AND per block, no branches at all, and the whole table costs
     * 8 * (blocks - 1) vector operations.
     *
     * Runs only ever cover whole blocks -- the caller's full_blocks is n/256 and
     * the remainder is handled as single-suffix descriptors -- so every one of
     * these 32-byte loads is inside the text. */
    uint8_t constant[DSA_BLOCK];
#if defined(DSA_ABLATE) && DSA_ABLATE >= 1
    /* Ablation, for measurement only: pretend every column is constant. Removes
     * both the table and every column_step, and the suffix array it produces is
     * wrong. Never defined in a shipped build. */
    memset(constant, 1, sizeof(constant));
#elif defined(DSA_AVX2)
    {
        const uint8_t* const b0 = t + base;
        const __m256i one = _mm256_set1_epi8(1);
        for (uint32_t off = 0; off < DSA_BLOCK; off += 32) {
            const __m256i v0 = _mm256_loadu_si256((const __m256i*)(b0 + off));
            __m256i acc = _mm256_set1_epi8((char)0xFF);
            for (uint32_t g = 1; g < blocks; g++) {
                const __m256i vg =
                    _mm256_loadu_si256((const __m256i*)(b0 + g * DSA_BLOCK + off));
                acc = _mm256_and_si256(acc, _mm256_cmpeq_epi8(v0, vg));
            }
            /* cmpeq leaves 0xFF where equal; the AND turns that into a 1. */
            _mm256_storeu_si256((__m256i*)(constant + off),
                                _mm256_and_si256(acc, one));
        }
    }
#elif defined(DSA_NEON)
    {
        const uint8_t* const b0 = t + base;
        const uint8x16_t one = vdupq_n_u8(1);
        for (uint32_t off = 0; off < DSA_BLOCK; off += 16) {
            const uint8x16_t v0 = vld1q_u8(b0 + off);
            uint8x16_t acc = vdupq_n_u8(0xFF);
            for (uint32_t g = 1; g < blocks; g++) {
                acc = vandq_u8(acc, vceqq_u8(v0, vld1q_u8(b0 + g * DSA_BLOCK + off)));
            }
            vst1q_u8(constant + off, vandq_u8(acc, one));
        }
    }
#else
    for (uint32_t rel = 0; rel < DSA_BLOCK; rel++) {
        const uint8_t c = t[base + rel];
        uint8_t same = 1;
        for (uint32_t g = 1; g < blocks; g++) {
            if (t[base + g * DSA_BLOCK + rel] != c) { same = 0; break; }
        }
        constant[rel] = same;
    }
#endif

#if !DSA_UNIFORM
    /* Columns where the next DSA_KEY_BYTES are all constant.
     *
     * Where they are, every block of the run has the same DSA_KEY_BYTES bytes at
     * that offset, so every suffix in the column shares its key, the whole run is
     * one group, and the scan below cannot discover anything: it computes `blocks`
     * identical keys and compares them. Measured, 91% of columns emit exactly
     * one descriptor (20,124 descriptors over 62 runs x 256 columns), so this is
     * the common case and not a special one.
     *
     * With a DSA_KEY_BYTES=3 key this only needs three constant columns, not the
     * four below; the old same4 was a leftover of the four-byte-key era and let
     * column 253 (whose bytes 253..255 are all inside this block) fall off the
     * fast path. The last two columns are still excluded because constant[]
     * only covers this block; column 254 and 255 would read past it. */
    uint8_t same_key[DSA_BLOCK];
    for (uint32_t rel = 0; rel + DSA_KEY_BYTES <= DSA_BLOCK; rel++) {
        uint8_t s = constant[rel];
        for (uint32_t k = 1; k < DSA_KEY_BYTES; k++) s &= constant[rel + k];
        same_key[rel] = s;
    }
    for (uint32_t rel = DSA_BLOCK - DSA_KEY_BYTES + 1; rel < DSA_BLOCK; rel++)
        same_key[rel] = 0;
#endif

    /* One key per block, slid rather than re-read. A descriptor key is the
     * DSA_KEY_BYTES bytes at order[x]+rel, and stepping to column rel-1 keeps
     * all but one of them:
     *
     *   K(q-1) = t[q-1] << 8*(KEY-1) | (K(q) >> 8)
     *
     * The GPU found this was a quarter of the sort: the walk was re-reading
     * four text bytes at every column of every block, three of which it
     * already had. The identity needs no end-of-text case -- the shift drops
     * exactly the byte the zero padding would have invented. */
    uint32_t keys[DSA_RUN_MAX];
    for (uint32_t i = 0; i < blocks; i++) keys[i] = key32(t, n, order[i] + 255);
    const uint32_t key_shift = 8u * (DSA_KEY_BYTES - 1);
#if DSA_UNIFORM
    uint32_t k0 = keys[0];
    int uniform = 1;
    for (uint32_t i = 1; i < blocks; i++) {
        if (keys[i] != k0) { uniform = 0; break; }
    }
#endif

#if DSA_ARENA_REUSE
    /* The arena slice that currently holds order[0..blocks), and whether the
     * order has moved since it was written there. */
    uint32_t aw_off = 0;
    int      aw_dirty = 1;
#endif

    for (int rel = DSA_BLOCK - 1; rel >= 0; rel--) {
        uint32_t r = (uint32_t)rel;

        /* Split the (already ordered) suffixes into maximal groups sharing
         * their leading key bytes, and record each as one descriptor. */
        if (*desc_len + blocks > desc_room) return 0;   /* grow and retry */

#if DSA_ARENA_REUSE
        if (aw_dirty) {
            aw_off = (uint32_t)*arena_len;
            emit_ord_plus(s->arena + aw_off, order, blocks, r);
            *arena_len += blocks;
            aw_dirty = 0;
        }
#endif

#if DSA_UNIFORM
        if (uniform) {
#if DSA_ARENA_REUSE
            /* A constant prepend preserves both the uniform key and the order.
             * Emit that whole span with one arena slice and one capacity check,
             * without revisiting the arena/group dispatch for each column.
             * Every remaining column needs at least one descriptor, so this
             * reservation cannot reject a run that would otherwise fit. */
            if (*desc_len + r + 1 > desc_room) return 0;
            Desc* d = s->desc + *desc_len;
            const uint32_t packed = desc_pack(aw_off, blocks);
            for (;;) {
                d->key = DSA_KEYREL(k0, r);
                d->packed = packed;
                d++;
                if (rel == 0 || !constant[r - 1]) break;
                r--;
                rel--;
                k0 = ((uint32_t)t[base + r] << key_shift) | (k0 >> 8);
            }
            *desc_len = (size_t)(d - s->desc);
            if (rel == 0) return 1;
#else
            Desc* d = &s->desc[*desc_len];
            d->key = DSA_KEYREL(k0, r);
            d->packed = desc_pack((uint32_t)*arena_len, blocks);
            emit_ord_plus(s->arena + *arena_len, order, blocks, r);
            *arena_len += blocks;
            (*desc_len)++;
#endif
        } else
#elif !defined(DSA_NO_SAME4)
        if (same_key[r]) {
            /* Every block shares this column's key bytes, so there is exactly
             * one group and the scan below would only prove it the long way. */
            Desc* d = &s->desc[*desc_len];
            d->key = DSA_KEYREL(keys[0], r);
#if DSA_ARENA_REUSE
            d->packed = desc_pack(aw_off, blocks);
#else
            d->packed = desc_pack((uint32_t)*arena_len, blocks);
            emit_ord_plus(s->arena + *arena_len, order, blocks, r);
            *arena_len += blocks;
#endif
            (*desc_len)++;
        } else
#endif
        {
            uint32_t i = 0, groups = 0;
            while (i < blocks) {
                const uint32_t k = keys[i];
                uint32_t j = i + 1;
                while (j < blocks && keys[j] == k) j++;

                Desc* d = &s->desc[*desc_len];
                d->key = DSA_KEYREL(k, r);
#if DSA_ARENA_REUSE
                d->packed = desc_pack(aw_off + i, j - i);
#else
                d->packed = desc_pack((uint32_t)*arena_len, j - i);
                emit_ord_plus(s->arena + *arena_len, order + i, j - i, r);
                *arena_len += (j - i);
#endif
                (*desc_len)++;
                groups++;
                i = j;
            }
#if DSA_UNIFORM
            /* The scan has just answered the question the state asks. */
            if (groups == 1) { uniform = 1; k0 = keys[0]; }
#endif
        }

        if (rel == 0) break;

        /* Slide first, because the keys move whether or not the order does.
         * A constant column prepends the same byte to every suffix, so the
         * order is unchanged and there is nothing further to do. */
        const uint32_t col = r - 1;
        uint32_t x = 0;
#if DSA_UNIFORM
        if (constant[col]) {
            /* One byte for the whole run, not one per block. */
            const uint32_t c = t[base + col];
            if (uniform) {
                k0 = (c << key_shift) | (k0 >> 8);
                continue;
            }
            const uint32_t hi = c << key_shift;
#if defined(DSA_AVX2)
            const __m128i bc = _mm_set1_epi32((int)hi);
            for (; x + 4 <= blocks; x += 4) {
                const __m128i k = _mm_loadu_si128((const __m128i*)(keys + x));
                _mm_storeu_si128((__m128i*)(keys + x),
                                 _mm_or_si128(_mm_srli_epi32(k, 8), bc));
            }
#endif
            for (; x < blocks; x++) keys[x] = hi | (keys[x] >> 8);
            continue;
        }
        if (uniform) {
            /* About to read per block, so put the register back in the array. */
            for (uint32_t y = 0; y < blocks; y++) keys[y] = k0;
            uniform = 0;
        }
#endif
#if defined(DSA_AVX2)
        for (; x + 4 <= blocks; x += 4) {
            const uint32_t c0 = t[order[x] + col];
            const uint32_t c1 = t[order[x + 1] + col];
            const uint32_t c2 = t[order[x + 2] + col];
            const uint32_t c3 = t[order[x + 3] + col];
            const __m128i k = _mm_loadu_si128((const __m128i*)(keys + x));
            const __m128i b = _mm_set_epi32((int)c3, (int)c2, (int)c1, (int)c0);
            _mm_storeu_si128((__m128i*)(keys + x),
                _mm_or_si128(_mm_srli_epi32(k, 8),
                             _mm_slli_epi32(b, 8 * (DSA_KEY_BYTES - 1))));
        }
#elif defined(DSA_NEON)
        for (; x + 4 <= blocks; x += 4) {
            const uint32_t c[4] = {
                t[order[x] + col], t[order[x + 1] + col],
                t[order[x + 2] + col], t[order[x + 3] + col],
            };
            const uint32x4_t k = vld1q_u32(keys + x);
            const uint32x4_t b = vld1q_u32(c);
            vst1q_u32(keys + x,
                vorrq_u32(vshrq_n_u32(k, 8),
                            vshlq_n_u32(b, 8 * (DSA_KEY_BYTES - 1))));
        }
#endif
        for (; x < blocks; x++) {
            const uint8_t c = t[order[x] + col];
            keys[x] = ((uint32_t)c << key_shift) | (keys[x] >> 8);
        }
#if !defined(DSA_ABLATE)
#if DSA_UNIFORM
        {   /* col is not constant here; the branch above returned otherwise. */
            column_step_keys(order, keys, s->order2, blocks);
#if DSA_ARENA_REUSE
            aw_dirty = 1;
#endif
        }
#else
        if (!constant[col]) {
            column_step_keys(order, keys, s->order2, blocks);
#if DSA_ARENA_REUSE
            /* The order may have moved, so the slice it lives in stops
             * describing it. Conservative: a sort that changed nothing still
             * costs one rewrite, which is cheaper than proving it did not. */
            aw_dirty = 1;
#endif
        }
#endif
#endif
    }
    return 1;
}

/* ---------------------------------------------------------------------------
 * the global merge
 * ------------------------------------------------------------------------ */

/* Radix sort the descriptors by key, most significant byte last so the result
 * is ascending. Stable, so descriptors that tie keep the order they were
 * emitted in -- which is not the answer, but is a starting point the tie-break
 * below refines. */
/* Eleven bits a pass, so a 32-bit key takes three passes and not four.
 *
 * Sorting a four-byte permutation instead of the eight-byte descriptors was
 * tried, to halve what these passes move -- around 970 KB a text, the largest
 * single item of traffic in this sort. The digit then comes from
 * desc[perm[i]].key, a gather rather than a sequential read, and the bet was
 * that trading L3 bandwidth for L2 latency would pay at fifteen threads even if
 * it lost at one.
 *
 * It lost at both: 3,201 texts/s against 3,433 on one thread, and 20,223 H/s
 * against 21,365 in the miner at fifteen. Descriptors are 161 KB for a full
 * text, and at fifteen threads there is no L2 left for them -- so the "L2
 * gather" is an L3 gather, and there are 40,000 of them per text.
 *
 * Each pass reads and writes every descriptor -- twelve bytes, twenty thousand
 * of them -- so a pass saved is a quarter of the traffic of this phase. The cost
 * is 2048 counters per pass instead of 256, which is 24 KB of histogram against
 * a 48 KB L1: it fits, and it is touched once per descriptor either way.
 *
 * Three is odd, so the result ends up in the scratch array rather than back
 * where it started. Returning which one it is beats copying it back, which
 * would give away the pass this saved.
 */


#if DSA_KEY_BYTES == 3
#define DSA_RBITS 12
#define DSA_RPASS 2
#else
#define DSA_RBITS 11
#define DSA_RPASS 3
#endif
#define DSA_RBINS (1u << DSA_RBITS)
#define DSA_RMASK (DSA_RBINS - 1u)

static Desc* sort_desc(Desc* a, Desc* b, size_t count)
{
    static const int shift[DSA_RPASS] = {
        0, DSA_RBITS,
#if DSA_RPASS > 2
        2 * DSA_RBITS,
#endif
    };

    uint32_t hist[DSA_RPASS][DSA_RBINS];
    memset(hist, 0, sizeof(hist));
    {
        size_t i = 0;
        for (; i + 4 <= count; i += 4) {
            const uint32_t k0 = a[i].key, k1 = a[i + 1].key;
            const uint32_t k2 = a[i + 2].key, k3 = a[i + 3].key;
            for (int p = 0; p < DSA_RPASS; p++) {
                const int sh = shift[p];
                hist[p][(k0 >> sh) & DSA_RMASK]++;
                hist[p][(k1 >> sh) & DSA_RMASK]++;
                hist[p][(k2 >> sh) & DSA_RMASK]++;
                hist[p][(k3 >> sh) & DSA_RMASK]++;
            }
        }
        for (; i < count; i++) {
            const uint32_t k = a[i].key;
            for (int p = 0; p < DSA_RPASS; p++) {
                hist[p][(k >> shift[p]) & DSA_RMASK]++;
            }
        }
    }
    for (int p = 0; p < DSA_RPASS; p++) {
        uint32_t sum = 0;
        for (uint32_t v = 0; v < DSA_RBINS; v++) {
            const uint32_t c = hist[p][v];
            hist[p][v] = sum;
            sum += c;
        }
    }
    for (int p = 0; p < DSA_RPASS; p++) {
        const int sh = shift[p];
        size_t i = 0;
        for (; i + 4 <= count; i += 4) {
            const Desc r0 = a[i], r1 = a[i + 1], r2 = a[i + 2], r3 = a[i + 3];
            b[hist[p][(r0.key >> sh) & DSA_RMASK]++] = r0;
            b[hist[p][(r1.key >> sh) & DSA_RMASK]++] = r1;
            b[hist[p][(r2.key >> sh) & DSA_RMASK]++] = r2;
            b[hist[p][(r3.key >> sh) & DSA_RMASK]++] = r3;
        }
        for (; i < count; i++) {
            b[hist[p][(a[i].key >> sh) & DSA_RMASK]++] = a[i];
        }
        Desc* tmp = a; a = b; b = tmp;
    }
    return a;   /* the swap after the last pass leaves the result here */
}

int dsa_descriptor_suffix_array(const uint8_t* t, int32_t* sa, int32_t n_in)
{
    if (!t || !sa || n_in <= 0) return -1;
    const size_t n = (size_t)n_in;

    /* Half the worst case to start with; grown on the retry below. */
    Scratch* s = scratch_get(n, n / 2 + DSA_BLOCK + 8);
    if (!s) return -2;

retry:

    const uint32_t full_blocks = (uint32_t)(n / DSA_BLOCK);
    size_t arena_len = 0, desc_len = 0;

    /* Runs: consecutive blocks, cut where a block differs wholesale from its
     * predecessor, and capped so the per-column insertion sorts stay short. */
    PROF_MARK();
    uint32_t g = 0;
    while (g < full_blocks) {
        uint32_t len = 1;
        while (len < DSA_RUN_MAX && g + len < full_blocks) {
            const uint8_t* a = t + (size_t)(g + len - 1) * DSA_BLOCK;
            const uint8_t* b = t + (size_t)(g + len) * DSA_BLOCK;
            int diff = 0;
#if defined(DSA_AVX2)
            /* How many of the 256 bytes differ, counted 32 at a time.
             *
             * The scalar version below reads a byte, compares, and checks the
             * early-out, 256 times a block pair and 272 block pairs a text. It
             * was 7.3% of the whole sort for what is eight compares and eight
             * popcounts of work. The early-out is dropped with it: it saved
             * reads only on the pairs that differ wholesale, and eight vector
             * compares are cheaper than the branch that decided whether to
             * bother. */
            for (uint32_t o = 0; o < DSA_BLOCK; o += 32) {
                const __m256i va = _mm256_loadu_si256((const __m256i*)(a + o));
                const __m256i vb = _mm256_loadu_si256((const __m256i*)(b + o));
                const uint32_t eq =
                    (uint32_t)_mm256_movemask_epi8(_mm256_cmpeq_epi8(va, vb));
                diff += 32 - (int)_mm_popcnt_u32(eq);
            }
#elif defined(DSA_NEON)
            for (uint32_t o = 0; o < DSA_BLOCK; o += 16) {
                const uint8x16_t ne = vmvnq_u8(vceqq_u8(vld1q_u8(a + o), vld1q_u8(b + o)));
                const uint8x16_t ones = vandq_u8(vshrq_n_u8(ne, 7), vdupq_n_u8(1));
                const uint64x2_t s = vpaddlq_u32(vpaddlq_u16(vpaddlq_u8(ones)));
                diff += (int)(vgetq_lane_u64(s, 0) + vgetq_lane_u64(s, 1));
            }
#else
            for (int i = 0; i < DSA_BLOCK && diff <= DSA_RUN_SPLIT; i++) {
                diff += (a[i] != b[i]);
            }
#endif
            if (diff > DSA_RUN_SPLIT) break;
            len++;
        }
        PROF_LAP(PH_RUNS);
        PROF_STAT(ST_RUNS, 1);
        if (!emit_run(t, n, g, len, s, &arena_len, &desc_len)) {
            const size_t full = desc_bound(n);
            if (s->desc_cap >= full) return -3;
            s = scratch_get(n, full);
            if (!s) return -2;
            goto retry;
        }
        PROF_LAP(PH_WALK);
        g += len;
    }

    /* The tail past the last whole block: one descriptor per suffix. Emitting
     * backwards lets each key inherit its trailing bytes from the preceding
     * key, replacing one unaligned four-byte load per suffix with one byte. */
    if (desc_len + (n - (size_t)full_blocks * DSA_BLOCK) > s->desc_cap) {
        const size_t full = desc_bound(n);
        if (s->desc_cap >= full) return -3;
        s = scratch_get(n, full);
        if (!s) return -2;
        goto retry;
    }
    const size_t tail = (size_t)full_blocks * DSA_BLOCK;
    if (tail < n) {
        uint32_t key = key32(t, n, n - 1);
        const uint32_t key_shift = 8u * (DSA_KEY_BYTES - 1);
        for (size_t p = n; p-- > tail;) {
            Desc* d = &s->desc[desc_len++];
            d->key = DSA_KEYREL(key, (uint32_t)p & 255u);
            d->packed = desc_pack((uint32_t)arena_len, 1);
            s->arena[arena_len++] = (Arena)DSA_ARENA_VAL((uint32_t)p >> 8, (uint32_t)p);
            if (p != tail)
                key = ((uint32_t)t[p - 1] << key_shift) | (key >> 8);
        }
    }

#if DSA_ARENA_REUSE
    if (arena_len > n) return -4;    /* reuse only ever writes fewer */
#else
    if (arena_len != n) return -4;   /* every suffix exactly once */
#endif
    PROF_LAP(PH_TAIL);
    PROF_STAT(ST_DESC, desc_len);
    PROF_MAX(ST_MAXDESC, desc_len);

    Desc* const ds = sort_desc(s->desc, s->desc2, desc_len);
    PROF_LAP(PH_RADIX);

    /* Write out, resolving descriptors that share all four leading bytes.
     *
     * Each descriptor's positions are already in order; only their interleaving
     * is unknown. The groups are small, so a straight insertion merge over the
     * gathered positions is both simplest and fastest here. */
    /* The gather out of the arena looked like it should want prefetching: this
     * walks descriptors in key order and their offsets point all over 275 KB.
     * Measured at distances of 1, 4, 8, 16, 32 and 64 descriptors ahead, every
     * one of them landed within noise of no prefetch at all, so there is none.
     * What actually cost time here was a branch, not a miss -- see below.
     *
     * Retried at fifteen threads, where the arena is an L3 hit rather than an L2
     * one and the argument for prefetching is much stronger, at distances 2, 4,
     * 8, 16 and 32. It came back inconclusive rather than negative: the spread
     * between repeats of the *same* binary was 32,703 / 30,033 / 15,455 texts/s
     * as background load on the machine came and went, which is far wider than
     * anything being looked for. Not kept, because "cannot tell" is not a
     * reason to add an instruction to the hottest loop in the sort. */
    size_t out = 0;
    size_t i = 0;
    while (i < desc_len) {
        const Desc* d0 = &ds[i];


        /* The overwhelmingly common case, and the one that decides the cost of
         * this loop: a key nothing else shares, so its positions go straight
         * out. There are ~19,000 groups per text and ~98.7% of them are this,
         * carrying about three positions each.
         *
         * The read out of the arena here is free, which is worth recording because
     * it looks like it should not be: it is a gather over 275 KB at offsets that
     * the radix sort scattered, ~19,000 of them a text, and the runs are ~14
     * bytes so most of each cache line fetched goes unused. Replacing the read
     * with nothing at all -- a deliberately wrong suffix array, timed only to
     * find the ceiling -- measured 3,430 texts/s against 3,448 with it. There is
     * nothing there to win, so the arena stays.
     *
     * Written as an element loop rather than memcpy on purpose. The length
         * is a variable, so memcpy is a call, and a call to move fourteen bytes
         * costs more than the move. Measured over the whole sort: 2,527 texts/s
         * with memcpy here, 3,447 with this.
         *
         * Checking one key rather than scanning forward for the end of the
         * group also matters -- a singleton is settled by a single comparison.
         */
        if (i + 1 == desc_len || !DSA_KEY_EQ(ds[i + 1].key, d0->key)) {
            PROF_STAT(ST_KEYGRP, 1);
            const Arena* src = s->arena + dsa_off(*d0);
            const uint32_t len = dsa_len(*d0);
#if DSA_COMPACT_ARENA
            /* Every position in a descriptor shares its column, so the arena's
             * block index is widened and the column ORed back in eight lanes at
             * a time. Two extra vector operations for half the bytes read. */
            const uint32_t rel = DSA_REL_OF(d0->key);
#else
            const uint32_t rel = 0;
            (void)rel;
#endif
            /* Four unconditional stores instead of a loop of `len`.
             *
             * len is one to a handful and unpredictable, so the loop cost a
             * branch mispredict per group -- about twenty cycles to move
             * fourteen bytes, times nineteen thousand groups a text.
             *
             * Over-copying is safe and self-correcting: the next group starts
             * at out+len and overwrites anything written past it, the arena has
             * eight words of slack so the reads stay in bounds, and the guard
             * keeps the last group from running off the end of sa. */
#if DSA_MABLATE == 3
            /* SCAFFOLDING: the singleton group's whole body removed, so what is
             * left is the walk over the descriptor array and the key test. The
             * answer is wrong; the point is what the group body costs. */
            (void)src;
#elif defined(DSA_AVX2)
            /* Exactly len words, with no branch and no overlap.
             *
             * This is the third version of four stores. Writing len of them in a
             * loop cost a branch mispredict per group, because len is one to a
             * handful and unpredictable: 2,527 texts/s. Writing four
             * unconditionally and advancing by len fixed that but made
             * consecutive groups write overlapping 16-byte ranges -- len averages
             * 3.4 -- so every store partially covered the one before it and the
             * store buffer paid for it: 3,450 texts/s. A masked store writes
             * only the lanes that count, so the ranges abut: 4,009 texts/s.
             *
             * Reading eight words when len is smaller is safe: the arena carries
             * eight words of slack past n for exactly this.
             *
             * Two more shapes were tried later, once the rest of the sort had
             * moved and the balance might have. A single unconditional 32-byte
             * store, advancing by len, was wrong -- it mismatched on the 512
             * texts -- and one unconditional 16-byte store for len <= 4 measured
             * 4,637 texts/s against 5,412. So the overlap really is what costs
             * here, and covering fewer words does not fix it: what matters is
             * that consecutive stores abut exactly, which only a mask gives. */
            if (len <= 8) {
                static const int32_t maskTab[9][8] = {
                    { 0,  0,  0,  0,  0,  0,  0,  0},
                    {-1,  0,  0,  0,  0,  0,  0,  0},
                    {-1, -1,  0,  0,  0,  0,  0,  0},
                    {-1, -1, -1,  0,  0,  0,  0,  0},
                    {-1, -1, -1, -1,  0,  0,  0,  0},
                    {-1, -1, -1, -1, -1,  0,  0,  0},
                    {-1, -1, -1, -1, -1, -1,  0,  0},
                    {-1, -1, -1, -1, -1, -1, -1,  0},
                    {-1, -1, -1, -1, -1, -1, -1, -1},
                };
#if DSA_MABLATE == 1
                const __m256i v = _mm256_set1_epi32((int32_t)rel);
#elif DSA_COMPACT_ARENA
                const __m256i v = _mm256_or_si256(
                    _mm256_slli_epi32(
                        _mm256_cvtepu16_epi32(
                            _mm_loadu_si128((const __m128i*)src)), 8),
                    _mm256_set1_epi32((int32_t)rel));
#else
                const __m256i v = _mm256_loadu_si256((const __m256i*)src);
#endif
#if DSA_MSTYLE == 1
                /* The lane mask from a compare rather than from a table.
                 *
                 * maskTab[len] is a 32-byte load whose address depends on len,
                 * which depends on the descriptor word just loaded: two
                 * dependent loads in front of the store. The same mask is three
                 * ALU instructions and no memory at all. */
                const __m256i m = _mm256_cmpgt_epi32(
                    _mm256_set1_epi32((int32_t)len),
                    _mm256_setr_epi32(0, 1, 2, 3, 4, 5, 6, 7));
#else
                const __m256i m = _mm256_loadu_si256((const __m256i*)maskTab[len]);
#endif
#if DSA_MABLATE == 2
                if (out == (size_t)-1) _mm256_maskstore_epi32(sa + out, m, v);
#elif DSA_MSTYLE == 2
                /* AVX-512's masked store, which is one instruction with a mask
                 * register. VPMASKMOVD -- what _mm256_maskstore_epi32 compiles
                 * to -- is microcoded on every AMD part to date. */
                (void)m;
                _mm256_mask_storeu_epi32(sa + out, (__mmask8)((1u << len) - 1u), v);
#else
                _mm256_maskstore_epi32(sa + out, m, v);
#endif

            } else {
                for (uint32_t z = 0; z < len; z++)
                    sa[out + z] = (int32_t)DSA_POS(src[z], rel);
            }
#else
            /* No AVX2: four unconditional stores, which is still better than a
             * loop of len. The guard keeps the last group from running past sa. */
            if (len <= 4 && out + 4 <= n) {
                sa[out + 0] = (int32_t)DSA_POS(src[0], rel);
                sa[out + 1] = (int32_t)DSA_POS(src[1], rel);
                sa[out + 2] = (int32_t)DSA_POS(src[2], rel);
                sa[out + 3] = (int32_t)DSA_POS(src[3], rel);
            } else {
                for (uint32_t z = 0; z < len; z++)
                    sa[out + z] = (int32_t)DSA_POS(src[z], rel);
            }
#endif
            out += len;
            i++;
            continue;
        }

        size_t j = i + 1;
        while (j < desc_len && DSA_KEY_EQ(ds[j].key, ds[i].key)) j++;

        PROF_STAT(ST_KEYGRP, 1);
        {
            PROF_T0();
            PROF_STAT(ST_COLLIDE, 1);
            /* Several descriptors share these four bytes, so their positions
             * have to be interleaved by comparison. Each descriptor's list is
             * already in order, so this is a merge and not a sort: lay them out
             * end to end with their boundaries, then merge adjacent pairs until
             * one list is left.
             *
             * Insertion sorting the group instead is quadratic, and these
             * groups are not small -- 88% of suffixes in these texts share
             * their first four bytes with a neighbour.
             *
             * The boundary tables are sized for every descriptor in the text,
             * so there is no cap to overflow. A fixed table here needed a case
             * for "too many lists", and the only thing that case could do was
             * be wrong: the buffer holds several separately sorted lists, not
             * one sorted array, so nothing can be inserted into it.
             */
            uint32_t* a = s->merge;
            uint32_t* b = s->merge2;
            uint32_t* ba = s->bnd;
            uint32_t* bb = s->bnd2;
            size_t nlist = 0, total = 0;

            /* Sized for what these texts do, not for the worst case; see the
             * note on DSA_GROUP_CAP. A group past it hands the whole hash to
             * libsais, which is slower and right. */
            {
                size_t need = 0;
                for (size_t k = i; k < j; k++) need += dsa_len(ds[k]);
                if (need > DSA_GROUP_CAP || (j - i) > DSA_GROUP_CAP) return -6;
            }

            for (size_t k = i; k < j; k++) {
                const Desc* d = &ds[k];
                ba[nlist++] = (uint32_t)total;
                const uint32_t dl = dsa_len(*d);
#if DSA_COMPACT_ARENA
                /* Expanded once, here, so the merge below compares positions
                 * exactly as it always has. */
                const Arena* asrc = s->arena + dsa_off(*d);
                const uint32_t drel = DSA_REL_OF(d->key);
                for (uint32_t z = 0; z < dl; z++)
                    a[total + z] = DSA_POS(asrc[z], drel);
#else
                memcpy(a + total, s->arena + dsa_off(*d), dl * sizeof(uint32_t));
#endif
                total += dl;
            }
            ba[nlist] = (uint32_t)total;
            PROF_MAX(ST_MAXMERGE, total);
            PROF_MAX(ST_MAXLIST, nlist);

            while (nlist > 1) {
                size_t out_lists = 0, pos = 0;
                for (size_t l = 0; l < nlist; l += 2) {
                    bb[out_lists++] = (uint32_t)pos;
                    if (l + 1 == nlist) {
                        const uint32_t s0 = ba[l], e0 = ba[l + 1];
                        memcpy(b + pos, a + s0, (e0 - s0) * sizeof(uint32_t));
                        pos += e0 - s0;
                    } else {
                        uint32_t p0 = ba[l], e0 = ba[l + 1];
                        uint32_t p1 = ba[l + 1], e1 = ba[l + 2];
                        while (p0 < e0 && p1 < e1)
                            b[pos++] = suffix_less_from(t, n, a[p1], a[p0], DSA_KEY_BYTES)
                                           ? a[p1++] : a[p0++];
                        while (p0 < e0) b[pos++] = a[p0++];
                        while (p1 < e1) b[pos++] = a[p1++];
                    }
                }
                bb[out_lists] = (uint32_t)pos;

                uint32_t* tv = a; a = b; b = tv;
                uint32_t* tb = ba; ba = bb; bb = tb;
                nlist = out_lists;
            }

            memcpy(sa + out, a, total * sizeof(uint32_t));
            out += total;
            PROF_STAT(ST_MERGED, total);
            PROF_ADD(PH_COLL, _t0);
        }
        i = j;
    }
    PROF_LAP(PH_MERGE);

    return out == n ? 0 : -5;
}

void dsa_descriptor_release(void)
{
    scratch_free(g_scratch);
    g_scratch = NULL;
}
