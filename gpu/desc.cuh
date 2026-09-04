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
//             runs that is 256 * total_blocks, which is n.
//             Run starting at
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
// Swept in the README. The answer is the default below, and the sweep is worth
// keeping in mind before anyone reaches for this again: every cap is far worse
// than no cap, and not by a little. Measured against the current kernel on a
// 5080, suffix milliseconds for 512 vectors at 336 blocks:
//
//     no cap  130      64  188      32  236      24  274
//         16  338      12  384       8  515       6  618       4  797
//
// The reason is descriptors, not columns. A run split in two emits its own
// descriptors on both halves, so the count the radix sort and the merge have to
// carry grows with every cut. Buying threads that way costs more than the
// threads are worth.
#ifndef DESC_RUN_MAX
#define DESC_RUN_MAX 0
#endif

// Four-byte words a suffix comparison reads per iteration. See descSuffixLess.
//
// Two, until the text load became two aligned loads and a PRMT rather than four
// byte loads (descLoadBE32). That changed the trade this knob sits on. Reading
// further ahead never read less, it read sooner; with a byte gather each extra
// word cost four more load instructions, and four words measured as a real-miner
// null against two. With a wide load an extra word costs two, and the same sweep
// now says four, clearly and repeatably:
//
//     1  74.1 / 74.1        2  76.8 / 77.2        4  79.2 / 79.5
//     6  77.6 / 77.6        8  76.3 / 76.1
//
// Past four it turns over again -- the comparison starts fetching well beyond
// where it decides. Worth reading as a warning about the other knobs in this
// file: every one of them was swept against a load that no longer exists.
#ifndef DESC_CMP_WORDS
#define DESC_CMP_WORDS 4
#endif

// Read the text four bytes at a time with two aligned loads and a PRMT, rather
// than four byte loads. See descLoadBE32. The knob exists so the two can be
// built against each other; 0 is the old shape exactly.
#ifndef DESC_WIDE_LOAD
#define DESC_WIDE_LOAD 1
#endif

// First eight-byte compare via three aligned 32-bit loads instead of two
// descLoadBE32 (four loads). Kept: ~+1% at 336 on a 5080, and clearer at
// 168 where thermals are not the measurement.
#ifndef DESC_LOAD64
#define DESC_LOAD64 1
#endif

// Use descLoadBE64 for the wide tail as well as the first eight bytes.
// At 336 it is a tie; at 42–168 it is ~+1.5%, so it is the default for
// cards that cannot fill four blocks per SM.
#ifndef DESC_LOAD64_WIDE
#define DESC_LOAD64_WIDE 1
#endif
// Bytes a wide comparison sweep reads before it decides.
//
// Re-swept after the comparison's loads stopped being generic (see
// descLoadBE32), because that changed what a load costs. Three interleaved
// `--bench --gpu=0` rounds on an RTX 5080 at 336 blocks:
//
//     8   167.4 k      16  172.1 k      32  175.8 k
//     64  177.1 k     128  178.0 k     192  178.6 k     256  178.1 k
//
// Wider is better up to about 128 and then flat: 192 leads 128 by 0.3% over two
// separate sets and 256 falls back to it, which is a plateau rather than a peak.
// 128 is the middle of it, and it is the smaller unroll and the smaller
// read-ahead on a card with less bandwidth to waste.
//
// The reason wider wins here is the one the old note gave and then under-sold:
// reading ahead does not read less, it reads *sooner*, and colliding suffixes
// are long-prefix matches by construction. A 32-byte sweep that has to go round
// again is a full dependent round trip; a 128-byte one has sixteen loads per
// suffix in flight at once.
// Open a comparison with one eight-byte step before the wide sweep.
//
// It used to pay: most merge pairs separate in the first eight bytes past the
// shared key, so a narrow opening issued six loads where a full sweep issues
// six per eight bytes of it. That was against a 32-byte sweep. Against a
// 128-byte one the opening is a dependent round trip in front of every
// comparison that does *not* separate there -- which is all of the seed's, the
// blocks being near copies -- and it measures **+0.89% to remove**, three
// interleaved rounds at 336 blocks with no overlap.
//
// 1 restores it, for A/B.
#ifndef DESC_LEAD8
#define DESC_LEAD8 0
#endif

// The same opening step, but only for the comparisons that already skip a
// shared key -- the merge's -- and not for the column walk's seed.
//
// Those two want opposite things. The seed compares near-copy blocks, so it
// never separates in the first eight bytes and the opening is a wasted round
// trip, which is why DESC_LEAD8 was removed. The merge separates there most of
// the time, and the narrower the key that grouped it the more often: at
// DESC_GBITS 18 a group shares two bytes, so the third usually decides. `from`
// is a compile-time constant at both call sites, so this costs no branch.
#ifndef DESC_LEAD_MERGE
#define DESC_LEAD_MERGE 0
#endif

// Hand the longest runs to the lowest task numbers, so a warp's thirty-two lanes
// hold runs of about one length.
//
// The walk gives one task to one thread and its cost is set by the run's length:
// 15,687 cycles at length 1 against 2,676,696 at 45, and 44% of runs are length
// 1 or 2. A warp executes the union of what its lanes do, so a warp holding one
// long run pays that run's cost on all thirty-two lanes; the profiler reports
// the walk's slowest thread at 1.96x the average for exactly this reason.
//
// Tasks are numbered (run * DESC_CHUNKS + chunk), so the four chunks of one run
// already share a length and sit in one warp. What decides a warp's cost is the
// eight runs it holds, and those are consecutive in the text -- which says
// nothing about their lengths. Ordering runs by length before numbering them
// puts the eight longest in warp 0 and the eight shortest in warp 7, and the
// eight lanes of a warp then diverge only as far as neighbouring lengths do.
//
// It does not shorten the longest run and so does not shorten a block; what it
// removes is the issue slots the short lanes were holding open while they
// waited. At five resident blocks per SM those slots go to another block.
//
// A counting sort over DESC_TASK_BUCKETS buckets, done once per hash on ~62
// runs. Lengths at or past the last bucket share it.
//
// The sort has to be stable, and the bucket count is what decides how much text
// locality it spends. Runs are consecutive slices of the text, so runs that are
// neighbours in the text read neighbouring bytes, and a warp whose eight runs
// are neighbours reads eight nearby regions. Reordering them scatters that.
// Measured: the first version scattered with an atomic, so runs of one length
// came out in whatever order the atomic served them, and it cost 73.2 -> 83.2 ms
// on the suffix kernel -- the locality was worth more than the divergence.
// Keeping text order inside a bucket costs one serial pass over ~62 runs by one
// thread, against a walk of ~665,000 cycles.
//
// Few buckets keep more of that locality than many: at DESC_TASK_BUCKETS 4 the
// runs split into lengths 1, 2, 3 and 4-or-more, which is where the cost steps
// (15,687 / 209,154 / 536,534 cycles), and inside each class the text order is
// untouched.
//
// It is off, because none of that collected anything. Stable, on the suffix
// kernel at 336 blocks, against 73.2/74.4/74.6 ms with it off: 73.1 at 2
// buckets, 75.2 at 3, 75.7 at 4, 80.6 at 8, 82.9 at 64. Two buckets is inside
// the noise and every finer split is worse, which puts the whole curve on the
// locality it spends rather than on the divergence it saves.
//
// So the walk's 1.96x imbalance is not a scheduling problem. Reordering tasks
// cannot collect it: a warp's cost is set by its longest run, and the only way
// to reach the idle lanes is to give them part of a long run, which is the
// variable split that costs more in repeated seeding than it saves. Left here
// at 0 so the next person does not have to build it again to find that out.
#ifndef DESC_TASK_SORT
#define DESC_TASK_SORT 0
#endif
#ifndef DESC_TASK_BUCKETS
#define DESC_TASK_BUCKETS 4
#endif

// Seed the column walk by counting ranks rather than by insertion sort; see
// the note where it is done.
//
//   0  insertion sort, ~len*len/4 comparisons in a chain      73.2 ms
//   1  every ordered pair, len*len, none of them dependent    96.0 ms
//   2  the upper triangle, len*len/2, none of them dependent  95.9 ms
//
// Suffix kernel at 336 blocks, two interleaved rounds. 2 is 1 with half the
// comparisons and it measures the same, which says the cost is not the
// comparison count and not the chain: it is the rank counters, which have to
// live in shared memory because len reaches 45. The insertion sort keeps its
// state in registers and wins on that alone.
#ifndef DESC_SEED_RANK
#define DESC_SEED_RANK 0
#endif

// SCAFFOLDING: leave one thing out of the column walk and time what is left.
// Every setting but 0 produces a wrong suffix array; the point is where the
// walk's wall time goes, which the per-task table cannot say because a lane's
// clock keeps running while the rest of its warp diverges.
//   1  no descriptor store   2  no arena store
//   3  no scattered text read on a non-constant column
//   4  no seed sort (identity order)   5  no descriptor emitted at all
//   6  the order never moves (no insertion sort on a non-constant column)
//   7  step 5a takes the first descriptor of the window, not the owning one
//   8  step 5a writes the column alone, without the arena's block index
//   9  paint the owner map but do not read it
#ifndef DESC_ABLATE
#define DESC_ABLATE 0
#endif

#ifndef DESC_WIDE_STEP
#if defined(DSG_HIP) && DSG_HIP
// RDNA coalesces at 64 B lines and has less bandwidth to waste on read-ahead:
// 64 is the bandwidth-constrained default; NVIDIA stays wide (see below).
#define DESC_WIDE_STEP 64
#else
// 192 leads 128 by ~0.3% over two interleaved 5080 sets (8..256 sweep above);
// 256 falls back. Plateau, not peak — 192 is the top of it.
#define DESC_WIDE_STEP 192
#endif
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

// Four, and the curve around it is sharp: 2 measures 141 ms and 8 measures 215,
// against 130 at 4. Eight loses because the order and key tables are indexed by
// chunk, so doubling the chunks doubles them, and that costs a block per SM.
//
// A variable split -- more chunks for long runs, fewer for short ones -- was
// built and measured and is not here. The imbalance it aimed at is real: the
// walk's slowest thread takes 1.98x the average (gpu/prof/prof.exe reports it),
// so a perfectly balanced walk would cost half of what this one does, and the
// walk is ~32% of the kernel. But splitting columns does not collect it. Every
// chunk of a run repeats that run's seed insertion sort and its full key read,
// which is quadratic in the run's length, so the chunks a long run needs to
// come down to average cost add back about what the balance saves. Swept over
// five threshold pairs, from boosting runs past 4 blocks to past 16, every one
// landed within noise of the flat split. The packing that paid for it -- the
// order table as a block index rather than a text position, two bytes instead
// of four -- measured neutral on its own and went with it.
//
// So the imbalance stands, and it is the largest single thing known to be wrong
// with this kernel. What it needs is a way to cut the per-chunk seeding cost,
// not another way to cut the columns.
//
// Must divide 256 exactly, or the pieces do not cover the columns and the
// suffix array is silently wrong -- 3 and 6 were both measured producing a
// wrong answer before this line existed, because 3*85 and 6*42 leave a
// column nobody walks.
static_assert(DESC_CHUNKS * DESC_CHUNK_COLS == 256,
              "DESC_CHUNKS must divide 256");

// Blocks in the longest text, which is what the order and run tables are sized
// for. ASTRO_MAX_TEXT is ASTRO_MAX_TRIES*256, so this is ASTRO_MAX_TRIES.
#define DESC_MAX_BLOCKS 278

// How many of the descriptor key's four bytes the sort actually orders by.
//
// The walk builds a four-byte key and groups the suffixes that share all four.
// Nothing requires four. Grouping on three does two things at once: the radix
// sort orders 24 bits in four passes instead of 32 in five, and it has fewer
// descriptors to order, because a coarser key merges neighbouring groups into
// one. What it costs is collisions -- more key groups hold more than one
// descriptor -- and the merge that resolves them is 1.6% of the kernel against
// the radix's 20.7%, so there is room for it to grow.
//
// The CPU sort settled on three bytes for the same reason and measured the
// fourth byte a clear loss: it cut colliding groups 329 -> 247 and paid a third
// radix pass for it, 5,403 -> 5,267 texts/s. This is that trade on the GPU.
//
#ifndef DESC_KEY_BYTES
#define DESC_KEY_BYTES 3
#endif
// The compact arena below stores only the block index for each suffix and keeps
// the shared column in the descriptor. That needs the shipped three-byte key:
// the fourth byte of the descriptor's upper word is the column.
#if DESC_KEY_BYTES != 3
#error "the compact descriptor arena requires DESC_KEY_BYTES=3"
#endif
#define DESC_KEY_DROP  8

// Bits of the key the global sort actually orders by, and therefore the width
// that decides a key group.
//
// The radix sort is four passes of six bits over the whole 24-bit key, and it
// is the largest phase of this kernel -- the sort proper plus the tile phases
// underneath it come to about a third. blockradix.cuh says plainly that only
// fewer passes moves it, and eight-bit digits were measured a 12% loss because
// 256 bins cost a block per SM.
//
// So narrow the key instead. Eighteen bits is three passes, not four. What it
// costs is collisions: the descriptors sharing a group are the ones agreeing on
// 18 bits rather than 24, so there are more groups holding more than one, and
// the merge that resolves them by comparison grows. That merge is 2% of the
// kernel against the radix's ~36%, so there is room.
//
// Two things follow and both are handled below. The walk groups a column's
// blocks on this width too, which is not a cost but a saving -- a coarser key
// merges neighbouring groups, so there are fewer descriptors to sort at all.
// And a group now shares only DESC_CMP_FROM whole bytes, so the merge's
// comparisons must start there rather than at DESC_KEY_BYTES.
#ifndef DESC_GBITS
#define DESC_GBITS 24
#endif
#ifndef DESC_NO_RADIX
#define DESC_NO_RADIX 0
#endif
#define DESC_KEY_OF(k) ((uint32_t)(k) >> (32 - DESC_GBITS))
// Whole bytes every descriptor in a key group is known to share.
#define DESC_CMP_FROM (DESC_GBITS / 8)
#define DESC_REL_OF(k) ((uint32_t)(k) & 255u)
#define DESC_KEYREL(k, rel) (((uint32_t)(k) & 0xFFFFFF00u) | (uint32_t)(rel))
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

// Positions at which a colliding group stops being one thread's work and
// becomes the whole block's. See step 6b.
//
// The average group is not the cost and never was. Measured over the 512
// vectors, one thread per group leaves the busiest thread of a block doing
// **43.5% of the whole text's merge comparisons** -- and since every thread
// waits at the barrier after, that one thread is the phase. The distribution
// says why: of 247 colliding groups a text, the ~5 holding more than 32
// positions carry 62% of the comparisons.
//
// So those few go through a block-wide merge instead, one group at a time, and
// the per-thread loop keeps the ~242 small ones it was always good at.
//
//   positions   groups   share of comparisons
//     1 - 32    241.6         38%
//    33 - 64      3.1         12%
//    65 -128      0.7          6%
//   129 -256      0.4         11%
//     257+       0.5          32%
//
// The threshold is a trade between the barriers a block-wide merge costs and
// the serial tail it removes. 128 was the answer against the old uniform
// compare; after the merge started with an eight-byte step, 64 pulled more
// groups onto the block and measured faster at 336 and 672 on a 5080.
#ifndef DESC_MERGE_WIDE
#define DESC_MERGE_WIDE 64
#endif

// Keep the constant-column table in two registers rather than a 64-byte
// thread-local array; see the note where it is built.
#ifndef DESC_COLMASK
#define DESC_COLMASK 1
#endif

// Skip the per-column group scan where the constant-column mask already says
// every block shares the key. Needs DESC_COLMASK, and DESC_KEY_BYTES == 3.
#ifndef DESC_SAMEKEY
#define DESC_SAMEKEY DESC_COLMASK
#endif

// Carry "every block of this run has the same key" as a state, rather than
// rediscovering it from the constant-column mask each column.
//
// It subsumes DESC_SAMEKEY -- three constant columns mean every block has the
// same three bytes there, so the mask implies the state but not the other way
// round -- and it buys a second thing the mask cannot: while the keys are all
// equal, `len` of them in shared memory are `len` copies of one register, so a
// constant column slides the register and leaves the array alone. About 70% of
// columns are constant, so that is most of the walk's shared traffic.
//
// The state only ever changes at a column that is not constant, which is the
// one column that reads text per block anyway.
#ifndef DESC_UNIFORM
#define DESC_UNIFORM 1
#endif

// Rewrite the arena slice when the order actually moves, rather than whenever a
// column is not constant. 0 restores the weaker test.
#ifndef DESC_DIRTY_MOVED
#define DESC_DIRTY_MOVED 1
#endif

// Answer "which descriptor owns this output position" from a painted byte map
// rather than a binary search; see step 5a. The map is what is left of
// BlockRadixScratch once step 5a's three window tables are carved out of it.
#ifndef DESC_OWNER_MAP
#define DESC_OWNER_MAP 1
#endif

// Hold the walk's order table as block indices in sixteen bits rather than as
// text positions in thirty-two. 0 restores the old width, for A/B.
#ifndef DESC_ORD16
#define DESC_ORD16 1
#endif
#if DESC_ORD16
typedef uint16_t DescOrd;
#define DESC_ORD_POS(o)   (((int)(o)) << 8)
#define DESC_ORD_VAL(blk) ((uint16_t)(blk))
#else
typedef int32_t DescOrd;
#define DESC_ORD_POS(o)   ((int)(o))
#define DESC_ORD_VAL(blk) ((int32_t)((blk) << 8))
#endif
#define DESC_OWN_CAP (BR_SHARED_BYTES - 3 * BR_BLOCK * (int)sizeof(int32_t))

#if DESC_UNIFORM
#undef  DESC_SAMEKEY
#define DESC_SAMEKEY 0
#endif
#if DESC_SAMEKEY && !DESC_COLMASK
#error "DESC_SAMEKEY reads col_mask, which DESC_COLMASK builds"
#endif

// Touch the whole text before the column walk's scattered loads. A PTX
// L2 prefetch per 64-byte sector beat a coalesced byte touch by ~1% at 336
// on a 5080, and a 64-byte stride beat 128 by a fraction more.
#ifndef DESC_PREFETCH_TEXT
#define DESC_PREFETCH_TEXT 1
#endif
#ifndef DESC_PREFETCH_L2
#define DESC_PREFETCH_L2 1
#endif
#ifndef DESC_PREFETCH_L1
#define DESC_PREFETCH_L1 0
#endif
#ifndef DESC_PREFETCH_STRIDE
#define DESC_PREFETCH_STRIDE 64
#endif

// Copy the ~69 KB text into dynamic shared so the walk and merge hit SMEM
// instead of four texts thrashing a 128 KB L1. Caps occupancy at two blocks
// per SM on a 5080 (register file still allows four). Off until measured.
#ifndef DESC_TEXT_SHARED
#define DESC_TEXT_SHARED 0
#endif
#ifndef DESC_TEXT_CAP
#define DESC_TEXT_CAP 70928
#endif
// Sort the descriptors most significant digit first, in one pass, instead of
// least significant digit first in four.
//
// The LSD sort in blockradix.cuh is the largest thing in this kernel: skipping
// it entirely -- a deliberately wrong answer, timed only to find the ceiling --
// takes the suffix sort from 122,000 arrays a second to 180,000, so it is about
// a third of the wall time. blockradix.cuh says plainly that only fewer passes
// moves it, and every way of getting fewer *while staying LSD* has been tried
// and measured a loss: eight-bit digits cost a block per SM, and a narrower key
// (DESC_GBITS) pays for the pass with collisions that cost more.
//
// MSD gets there from the other side. LSD needs four passes because it needs
// every pass to be stable -- that is what makes the earlier digits survive --
// and the stability is what the per-tile rank, the warp matrix, the leader walk
// and the digit cursors are all for. MSD needs no stability at all: the top
// digit partitions the descriptors into buckets that are already in their final
// order relative to each other, and what happens inside a bucket is that
// bucket's own business.
//
// So this is one histogram, one scan, one scatter, and then a small sort inside
// each bucket -- which is nothing, because 2,048 buckets over ~19,700
// descriptors is about ten each, and ranking those ten words in parallel costs
// less than another full pass over the array would.
//
// What it gives up is the staged, coalesced scatter, which the note in
// blockradix.cuh measures at about 5%. Three passes are worth more than that.
//
// The bucket cap is not a tuning knob. Parallel ranking is still quadratic in
// a bucket length nothing bounds; past the
// cap the sort declines and prefix doubling does the hash, exactly as it does
// when a key group needs more boundary words than the scratch holds. Keys are
// three bytes of stage-1 output and 2,048 buckets hold ten on average, so this
// is a guard and not a case: it has not fired on any of the 512 vectors.
#ifndef DESC_MSD
#define DESC_MSD 1
#endif
#ifndef DESC_MSD_PACKED16
#define DESC_MSD_PACKED16 1
#endif
// Eleven bits is the full-miner answer. Packing two counters per word gives it
// the same 4 KB table an ordinary ten-bit histogram needs, while halving the
// mean bucket length. Twelve bits doubles the table and shortens the buckets
// again, but its longer scan measured slower under the five-block Blackwell
// register budget.
#ifndef DESC_MSD_BITS
#if DESC_MSD_PACKED16
#define DESC_MSD_BITS 11
#else
#define DESC_MSD_BITS 10
#endif
#endif
#define DESC_MSD_BINS (1 << DESC_MSD_BITS)
// What the digit leaves of the key, which must fit a uint16.
#define DESC_MSD_LOWMASK ((1u << (32 - DESC_KEY_DROP - DESC_MSD_BITS)) - 1u)
#if DESC_MSD_LOWKEY
static_assert(32 - DESC_KEY_DROP - DESC_MSD_BITS <= 16,
              "DESC_MSD_BITS must leave at most 16 bits of key");
#endif
#ifndef DESC_MSD_MAX
#define DESC_MSD_MAX 512
#endif
// Rank on a narrow copy of the key rather than on the descriptor itself.
#ifndef DESC_MSD_LOWKEY
#define DESC_MSD_LOWKEY 1
#endif
// The bucket table sits past the radix scratch in the same dynamic allocation.
// At eleven bits, two 16-bit counters share one 32-bit atomic word. The whole
// table is therefore the same 4,100 bytes as 1,025 ordinary counters at ten
// bits, preserving the occupancy budget while halving the mean bucket length.
#if DESC_MSD_PACKED16
#define DESC_MSD_WORDS ((DESC_MSD_BINS + 2) / 2)
#define DESC_MSD_BYTES ((DESC_MSD) ? (int)(DESC_MSD_WORDS * sizeof(uint32_t)) : 0)
#else
#define DESC_MSD_BYTES ((DESC_MSD) ? (int)((DESC_MSD_BINS + 1) * sizeof(int32_t)) : 0)
#endif

#define DESC_LAUNCH_SHARED (BR_SHARED_BYTES + DESC_MSD_BYTES +                             ((DESC_TEXT_SHARED) ? DESC_TEXT_CAP : 0))

// Place the sorted positions with one thread per output position instead of one
// thread per descriptor.
//
// A descriptor is a run of ~5.7 positions and the old scatter gave a whole one
// to a thread, which then copied it a word at a time. Neighbouring threads were
// therefore writing addresses ~23 bytes apart and reading arena slots with no
// relation to each other at all, so a warp's 32 words cost ~28 memory
// transactions where 4 would do. Measured, that scatter was 26.5% of the suffix
// kernel -- the largest single phase in it.
//
// Turning it inside out fixes the writes completely: output position q always
// lands at sa[q], so a warp writes 32 consecutive words. What it costs is
// finding which descriptor owns q, and that is what the tile tables in step 5a
// are for -- one coalesced pass over the descriptors covering a tile, then a
// binary search in shared memory rather than a walk through global.
//
// Set to 0 for the old one-thread-per-descriptor scatter. Both are exact; this
// only moves who writes what.
#ifndef DESC_COOP_SCATTER
#define DESC_COOP_SCATTER 1
#endif

// Groups a text may hand to the block-wide merge. There are ~5, and a group
// past this cap is merged by one thread as before -- slower, never wrong.
#ifndef DESC_BIG_MAX
#define DESC_BIG_MAX 48
#endif

// Let a column point at another column's arena slot instead of writing its own.
//
// A constant column prepends the same byte to every suffix in the run, so the
// order does not change -- that is the whole idea this file is built on, and
// `col_same` already knows which columns those are. The walk still wrote the
// order out again for each of them: 256 * len entries per run, the same `len`
// block indices repeated for as long as the order held.
//
// It does not have to. Every column writes exactly ord[0..len) into one
// contiguous slot, in order, whatever way the keys split it into groups, and a
// column's slot is never touched again. So a column whose order has not moved
// since the last write can hand its descriptors the earlier slot, and write
// nothing at all. A group [i, j) reads from `awBase + i` either way.
//
// This removes a store loop from the walk -- the phase the profiler puts at
// 39.5% and the one the register ceiling says to shrink by removal, not by
// cleverness -- and it shrinks what the scatter reads back to the slots that
// were actually written, which is a smaller and hotter footprint.
//
// 0 restores the write-every-column behaviour exactly, for A/B.
#ifndef DESC_ARENA_REUSE
#define DESC_ARENA_REUSE 1
#endif

// Bits of the descriptor word. The top 24 are the radix key, the next eight
// hold the column shared by every entry in the descriptor, and the arena
// offset and length share the bottom 32. An arena entry is a uint16 block index;
// reconstructing `(block << 8) | column` halves the traffic of emitting and
// expanding the positions without increasing descriptor traffic.
#define DESC_LEN_BITS 12
#define DESC_LEN_MASK ((1u << DESC_LEN_BITS) - 1u)

struct DescScratch {
    uint64_t* words;    // n descriptor words, and the radix sort's other half
    uint64_t* words2;
    uint16_t* arena;    // n block indices, partitioned by run
    int32_t*  offs;     // n output offsets, from the scan over lengths
    int32_t*  mbuf;     // n, the merge's other half; see the note above
};

#define DESC_BYTES_PER_SYMBOL (2 * 8 + 2 + 4 + 4)

// Four text bytes at an arbitrary offset, as one big-endian word.
//
// This is the most executed operation in the kernel: the column walk and every
// suffix comparison are built out of it.
//
// The obvious body is four byte loads and a shift-or chain, and this file used
// to say nvcc would fold that into one 32-bit load. It does not, and cannot: a
// 32-bit load on this hardware must be four-byte aligned and p is arbitrary, so
// the compiler has no way to prove it. The SASS said so outright --
// suffix_kernel carried 98 LDG.E.U8 instructions.
//
// Four byte loads are not four times the DRAM traffic, because the addresses
// are adjacent and the coalescer merges the sectors. What they cost is four
// trips through the load/store unit, four L1 tag lookups and four entries of
// the outstanding-load budget, in a kernel whose whole problem is that it is
// waiting on memory.
//
// So: read the two aligned words that straddle the offset and let one PRMT pick
// the four bytes out of them. Two loads instead of four, and the big-endian
// order falls out of the same instruction rather than costing three shifts and
// three ORs. Byte loads in the kernel fell from 98 to 34.
//
// The selector. __byte_perm(a, b, s) sees an eight-byte value {b,a} and builds a
// result whose byte i, counting from the least significant, is source byte
// s>>(4*i) & 0xf. Wanting t[p] in the most significant byte and t[p+3] in the
// least, with the word starting `m` bytes into the first load:
//
//     result byte 3 = t[p+0] = source m+0      nibble 3 = m
//     result byte 2 = t[p+1] = source m+1      nibble 2 = m+1
//     result byte 1 = t[p+2] = source m+2      nibble 1 = m+2
//     result byte 0 = t[p+3] = source m+3      nibble 0 = m+3
//
// which is 0x0123 + 0x1111*m, and m is at most 3, so the top nibble reaches 6
// and never leaves the eight bytes available.
//
// The address is walked back with pointer arithmetic (`b - m`) rather than
// masked as an integer, and that is not cosmetic. Casting a uintptr_t to a
// pointer loses the address space with it, so ptxas has to assume the result
// could be shared or local and emits a *generic* load: `LD.E` rather than
// `LDG.E`. Nsight put 57% of the kernel's excessive global sectors on LD.E
// instructions for exactly this reason. Derived from `t` by subtraction the
// pointer stays provably global, and the comparison loop loads through the
// global path again.
//
// Two things this needs, neither obvious. Alignment: walking the address down is
// safe at any alignment of the text, because the selector is derived from the
// same address and cancels it out; what it needs is that the lowered address stay
// inside the allocation, which holds because texts are slices of one buffer.
// Slack: the second load reaches up to seven bytes past p, and every caller
// guarantees p+3 < n, so it reads at most four bytes past the text. Those bytes
// are fetched and never selected, but they must be mapped, so the texts
// allocation carries eight bytes of tail; see dsg_init.
// Text words through the read-only data path.
//
// The text is written once by stage 1 and only read here, but nothing in the
// signature says so: `t` is a plain const pointer that the compiler cannot
// prove does not alias the arena, the descriptors or sa, so it issues ordinary
// loads that go through L1 and are kept coherent. __ldg names the non-coherent
// path explicitly.
//
// It is off, and reproducibly so: 75.9 ms against 73.2 on the suffix kernel at
// 336 blocks, three interleaved rounds each, every round the same to a tenth of
// a millisecond. The read-only path is for data with no reuse, and this text
// has nothing but reuse -- a run's 256-byte blocks are read again on every one
// of its 64 columns, by four lanes at once, and the seed's comparisons walk the
// same bytes a second time. That is an L1 working set, and __ldg spends it.
#ifndef DESC_LDG
#define DESC_LDG 0
#endif
#if DESC_LDG
#define DESC_LDW(p) __ldg(p)
#else
#define DESC_LDW(p) (*(p))
#endif

__device__ __forceinline__ uint32_t descLoadBE32(const uint8_t* t, int p)
{
#if DESC_WIDE_LOAD
    const uint8_t*  b = t + p;
    const uint32_t  m = (uint32_t)((uintptr_t)b & 3u);
    const uint32_t* w = (const uint32_t*)(b - m);
    return __byte_perm(DESC_LDW(w), DESC_LDW(w + 1), 0x0123u + 0x1111u * m);
#else
    return ((uint32_t)t[p] << 24) | ((uint32_t)t[p + 1] << 16) |
           ((uint32_t)t[p + 2] << 8) | (uint32_t)t[p + 3];
#endif
}

// Eight text bytes at an arbitrary offset, as one big-endian word.
//
// Two descLoadBE32 issue four aligned loads; this issues three because the
// middle word is shared. The second load of the pair reaches four bytes past
// p+7 in the worst alignment, still inside the eight-byte text tail.
#if DESC_LOAD64
__device__ __forceinline__ uint64_t descLoadBE64(const uint8_t* t, int p)
{
    const uint8_t*  b = t + p;
    const uint32_t  m = (uint32_t)((uintptr_t)b & 3u);
    const uint32_t* w = (const uint32_t*)(b - m);
    const uint32_t sel = 0x0123u + 0x1111u * m;
    const uint32_t w0 = DESC_LDW(w), w1 = DESC_LDW(w + 1), w2 = DESC_LDW(w + 2);
    const uint32_t lo = __byte_perm(w0, w1, sel);
    const uint32_t hi = __byte_perm(w1, w2, sel);
    return ((uint64_t)lo << 32) | (uint64_t)hi;
}
#endif

__device__ __forceinline__ uint32_t descKey32(const uint8_t* t, int n, int p)
{
    if (p + 4 <= n) return descLoadBE32(t, p);

    // The last three positions of the text, where a fourth byte does not exist
    // and the key is zero padded. Byte loads here, because the wide form would
    // read past the text for a case that happens three times in seventy
    // thousand.
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
    // README as a uniform width. The first eight bytes are a different trade:
    // most merge pairs separate there (the shared key is already skipped), so
    // one two-word step first, then the wide loop for the tail, issues fewer
    // loads on the common path without giving up in-flight loads on the long
    // ones.
    if ((DESC_LEAD8 || (DESC_LEAD_MERGE && from > 0)) && i + 8 <= m) {
#if DESC_LOAD64
        const uint64_t wa = descLoadBE64(t, a + i);
        const uint64_t wb = descLoadBE64(t, b + i);
        if (wa != wb) return wa < wb;
#else
        const uint32_t wa0 = descLoadBE32(t, a + i);
        const uint32_t wb0 = descLoadBE32(t, b + i);
        if (wa0 != wb0) return wa0 < wb0;
        const uint32_t wa1 = descLoadBE32(t, a + i + 4);
        const uint32_t wb1 = descLoadBE32(t, b + i + 4);
        if (wa1 != wb1) return wa1 < wb1;
#endif
        i += 8;
    }

#if DESC_LOAD64 && DESC_LOAD64_WIDE
    for (; i + DESC_WIDE_STEP <= m; i += DESC_WIDE_STEP) {
        // One unrolled sweep of DESC_WIDE_STEP bytes. Written as a loop rather
        // than as a hand-written ladder so the width is a knob and not a set of
        // #if arms; nvcc unrolls it and hoists the loads exactly the same way.
#pragma unroll
        for (int u = 0; u < DESC_WIDE_STEP; u += 8) {
            const uint64_t wau = descLoadBE64(t, a + i + u);
            const uint64_t wbu = descLoadBE64(t, b + i + u);
            if (wau != wbu) return wau < wbu;
        }
    }
    for (; i + 8 <= m; i += 8) {
        const uint64_t wa = descLoadBE64(t, a + i);
        const uint64_t wb = descLoadBE64(t, b + i);
        if (wa != wb) return wa < wb;
    }
#else
    for (; i + 4 * DESC_CMP_WORDS <= m; i += 4 * DESC_CMP_WORDS) {
        uint32_t wa[DESC_CMP_WORDS], wb[DESC_CMP_WORDS];
#pragma unroll
        for (int w = 0; w < DESC_CMP_WORDS; w++) {
            const int o = i + 4 * w;
            wa[w] = descLoadBE32(t, a + o);
            wb[w] = descLoadBE32(t, b + o);
        }
#pragma unroll
        for (int w = 0; w < DESC_CMP_WORDS; w++) {
            if (wa[w] != wb[w]) return wa[w] < wb[w];
        }
    }
#endif

    for (; i + 4 <= m; i += 4) {
        const uint32_t wa = descLoadBE32(t, a + i);
        const uint32_t wb = descLoadBE32(t, b + i);
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

#if DESC_MSD && DESC_MSD_PACKED16
// Two logical uint16 bucket cursors in one physical uint32. Adding/subtracting
// 1 or 1<<16 with a 32-bit shared atomic is exact while neither half can wrap;
// the descriptor-count guard below establishes that invariant.
__device__ __forceinline__ uint32_t descBucketGet(const uint32_t* a, int b)
{
    return (a[b >> 1] >> ((b & 1) * 16)) & 0xffffu;
}

__device__ __forceinline__ uint32_t descBucketAdd(uint32_t* a, int b)
{
    const int shift = (b & 1) * 16;
    return (atomicAdd(a + (b >> 1), 1u << shift) >> shift) & 0xffffu;
}

__device__ __forceinline__ uint32_t descBucketSub(uint32_t* a, int b)
{
    const int shift = (b & 1) * 16;
    return (atomicSub(a + (b >> 1), 1u << shift) >> shift) & 0xffffu;
}

// Inclusive scan of the packed counters. One warp gives each lane a contiguous
// 64-bucket slice, scans the 32 slice totals with shuffles, then revisits its
// slice to write the prefixes.
__device__ int descBucketScan(uint32_t* a, BlockRadixScratch* sh)
{
    const int tid = threadIdx.x;
    if (tid < 32) {
        const int first = tid * (DESC_MSD_BINS / 64);
        int total = 0;
        for (int k = 0; k < DESC_MSD_BINS / 64; k++) {
            const uint32_t w = a[first + k];
            total += (int)(w & 0xffffu) + (int)(w >> 16);
        }

        int inclusive = total;
        for (int off = 1; off < 32; off <<= 1) {
            const int v = DSG_SHFL_UP(inclusive, off);
            if (tid >= off) inclusive += v;
        }
        int run = inclusive - total;
        for (int k = 0; k < DESC_MSD_BINS / 64; k++) {
            const uint32_t w = a[first + k];
            run += (int)(w & 0xffffu);
            const uint32_t lo = (uint32_t)run;
            run += (int)(w >> 16);
            a[first + k] = lo | ((uint32_t)run << 16);
        }
        if (tid == 31) sh->scanCarry = inclusive;
    }
    __syncthreads();
    return sh->scanCarry;
}
#endif

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
    // Block indices, not text positions.
    //
    // The walk's order table is read twice a column -- once as an address,
    // t[ord[x]+col], and once as the arena's block index, ord[x]>>8 -- and a
    // block index gives both: the address is (ord[x]<<8)+col, which is one
    // shift the address unit folds in for free, and the arena entry is ord[x]
    // itself. Sixteen bits is enough for DESC_MAX_BLOCKS and halves both the
    // shared traffic of the hottest loop in the kernel and the 4.4 KB this
    // table takes, which is 4 KB of the shared budget the MSD sort's bucket
    // table competes for.
    // The radix scratch is idle during the walk and both tables are dead by the
    // sort. They fit together in that allocation, removing 6.7 KB of static
    // SMEM without increasing the dynamic launch request. The boundary flags
    // use the same bytes one phase earlier and are dead before these are built.
    DescOrd* const s_order = (DescOrd*)sh;
    uint32_t* const s_wkey = (uint32_t*)(s_order + DESC_CHUNKS * DESC_MAX_BLOCKS);
    int32_t* const s_flag = (int32_t*)sh;
    int32_t* const s_keep = s_flag + DESC_MAX_BLOCKS;
    static_assert((int)(DESC_CHUNKS * DESC_MAX_BLOCKS *
                        (sizeof(DescOrd) + sizeof(uint32_t))) <= BR_SHARED_BYTES,
                  "walk tables must fit in the radix scratch");
    __shared__ int32_t s_start[DESC_MAX_BLOCKS + 1];
#if DESC_TASK_SORT
    // Run indices, longest first. Live across the whole walk, so it cannot share
    // the radix scratch the walk tables are in.
    __shared__ uint16_t s_rorder[DESC_MAX_BLOCKS];
#endif
    __shared__ int     s_nruns;
    __shared__ int     s_ndesc;
    __shared__ int     s_fail;
    __shared__ int     s_bump;   // bump allocator for the merge's boundaries
    __shared__ int     s_nbig;   // colliding groups handed to the block-wide merge
    __shared__ int     s_ncol;   // colliding key groups the scatter found
    __shared__ int     s_bbase;  // that merge's boundary allocation, one at a time

    PROF_DECL;
    const int tid = threadIdx.x;
    const int nblocks = n / 256;
    PROF_MARK();

    // The bump allocator starts past the slot the big-group list occupies; see
    // step 6b, which parks that list in the same dead array.
    if (tid == 0) {
        s_ndesc = 0; s_fail = 0; s_bump = 2 * DESC_BIG_MAX; s_nbig = 0; s_ncol = 0;
    }
#if DESC_TEXT_SHARED
    if (n + 8 <= DESC_TEXT_CAP) {
        uint8_t* s_text = (uint8_t*)sh + BR_SHARED_BYTES + DESC_MSD_BYTES;
        for (int i = tid; i < n + 8; i += BR_BLOCK)
            s_text[i] = (i < n) ? t[i] : (uint8_t)0;
        __syncthreads();
        t = s_text;
    }
#endif

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
            // Sixteen bytes at a time, not one.
            //
            // The byte loop this replaces issued 512 loads per block pair and
            // almost always ran all 256 iterations, because its early exit only
            // fires on a rekey and a rekey is rare by construction. Blocks are
            // 256 bytes apart, so both pointers carry the alignment of the text
            // itself and a uint4 load is legal on both or neither. Measured, the
            // phase went from 4.2% of the kernel to 0.6%.
            //
            // __vsetne4 puts 0x01 in each byte lane where two words differ, so
            // multiplying by 0x01010101 and taking the top byte sums the four
            // lanes into the byte-difference count with nothing unpacked.
            //
            // The guard is not decoration. A uint4 load needs sixteen-byte
            // alignment and CUDA faults rather than fixing it up, so the shape of
            // the caller's buffer decides whether this is legal. The miner's
            // texts are slices of one allocation at a stride of 277*256 and are
            // always aligned; a harness laying texts out at the length of the
            // longest one is not, and gpu/prof and gpu/desc_test both did until
            // gpu/vectors_host.h started rounding that stride. Branching costs
            // one uniform test per pair and makes the wrong layout slow rather
            // than fatal.
            const uint8_t* a = t + (g - 1) * 256;
            const uint8_t* b = t + g * 256;
            int diff = 0;
            if ((((uintptr_t)a) & 15u) == 0) {
                const uint4* a4 = (const uint4*)a;
                const uint4* b4 = (const uint4*)b;
                for (int i = 0; i < 16 && diff <= DESC_SPLIT; i++) {
                    const uint4 x = a4[i], y = b4[i];
                    uint32_t d = __vsetne4(x.x, y.x);
                    d += __vsetne4(x.y, y.y);
                    d += __vsetne4(x.z, y.z);
                    d += __vsetne4(x.w, y.w);
                    diff += (int)((d * 0x01010101u) >> 24);
                }
            } else {
                for (int i = 0; i < 256 && diff <= DESC_SPLIT; i++) diff += (a[i] != b[i]);
            }
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
    PROF_RUNS(s_start, s_nruns);

#if DESC_TASK_SORT
    // ---- 2a. order the runs by length, longest first; see DESC_TASK_SORT.
    //
    // The histogram sits in the boundary flags' bytes, which are dead from here
    // and are not the walk tables' until the walk starts writing them.
    {
        int32_t* const s_hist = (int32_t*)sh;
        for (int b = tid; b < DESC_TASK_BUCKETS; b += BR_BLOCK) s_hist[b] = 0;
        __syncthreads();
        for (int r = tid; r < nruns; r += BR_BLOCK) {
            const int len = s_start[r + 1] - s_start[r];
            atomicAdd(&s_hist[len < DESC_TASK_BUCKETS ? len : DESC_TASK_BUCKETS - 1], 1);
        }
        __syncthreads();
        // Exclusive scan from the top, so bucket b starts after every longer one.
        if (tid == 0) {
            int acc = 0;
            for (int b = DESC_TASK_BUCKETS - 1; b >= 0; b--) {
                const int c = s_hist[b];
                s_hist[b] = acc;
                acc += c;
            }
        }
        __syncthreads();
        // Serial, and therefore stable: runs of one length keep their text
        // order, which is what the walk's scattered reads live on. ~62 trivial
        // iterations against a walk of ~665,000 cycles.
        if (tid == 0) {
            for (int r = 0; r < nruns; r++) {
                const int len = s_start[r + 1] - s_start[r];
                const int b = len < DESC_TASK_BUCKETS ? len : DESC_TASK_BUCKETS - 1;
                s_rorder[s_hist[b]++] = (uint16_t)r;
            }
        }
        __syncthreads();
    }
#endif

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

    PROF_WALK_BEG();
    for (int q = tid; q < ntask; q += BR_BLOCK) {
        PROF_TASK_DECL;
        PROF_TASK_BEG();
        const int slot = q / DESC_CHUNKS;
        const int c = q - slot * DESC_CHUNKS; // 0 is the top of the text
#if DESC_TASK_SORT
        const int r = s_rorder[slot];
#else
        const int r = slot;
#endif
        const int g0 = s_start[r];
        const int len = s_start[r + 1] - g0;
        const int base = g0 * 256;

        // Columns this thread owns, walked hi down to lo.
        const int hi = 255 - c * DESC_CHUNK_COLS;
        const int lo = hi - DESC_CHUNK_COLS + 1;

        // Chunks of the same run walk it at the same time, so each needs its own
        // order and keys -- hence the stride. Runs partition the blocks, so the
        // slices within a chunk are still disjoint.
        DescOrd*  ord = s_order + (size_t)c * DESC_MAX_BLOCKS + g0;
        uint32_t* key = s_wkey  + (size_t)c * DESC_MAX_BLOCKS + g0;
        uint16_t* arena = sc.arena + (size_t)g0 * 256;

        // A one-block run has no ordering problem: every column is a singleton
        // and therefore constant. Keep its rolling key and block index in
        // registers instead of building shared order/key arrays and a local
        // constant-column table. These are about 27% of real runs, and this
        // path is vendor-neutral CUDA/HIP code.
        if (len == 1) {
            const uint32_t awBase = (uint32_t)(255 - hi);
            arena[awBase] = (uint16_t)g0;
            const uint32_t off = (uint32_t)((size_t)g0 * 256 + awBase);
            uint32_t oneKey = descKey32(t, n, base + hi);
            for (int rel = hi; rel >= lo; rel--) {
                const int d = atomicAdd(&s_ndesc, 1);
                PROF_TASK_EMIT();
                sc.words[d] = ((uint64_t)DESC_KEYREL(oneKey, rel) << 32) |
                              ((uint64_t)off << DESC_LEN_BITS) | 1u;
                if (rel != lo)
                    oneKey = ((uint32_t)t[base + rel - 1] << 24) | (oneKey >> 8);
            }
            PROF_TASK_END(len);
            continue;
        }

        // For two blocks, suffix order is one register bit. Unequal prepended
        // bytes decide the next order; equal bytes preserve it. This retains
        // the exact stable ordering rule while removing shared arrays and the
        // constant-column table for another 17% of observed runs.
        if (len == 2) {
            const int b0 = base;
            const int b1 = base + 256;
            uint32_t key0 = descKey32(t, n, b0 + hi);
            uint32_t key1 = descKey32(t, n, b1 + hi);
            bool swapped = descSuffixLess(t, n, b1 + hi, b0 + hi);
            uint32_t awBase = 0;
            bool awDirty = true;

            for (int rel = hi; rel >= lo; rel--) {
                if (awDirty) {
                    awBase = (uint32_t)(255 - rel) * 2u;
                    arena[awBase] = (uint16_t)(swapped ? g0 + 1 : g0);
                    arena[awBase + 1] = (uint16_t)(swapped ? g0 : g0 + 1);
                    awDirty = false;
                }
                const uint32_t firstKey = swapped ? key1 : key0;
                const uint32_t secondKey = swapped ? key0 : key1;
                const uint32_t off = (uint32_t)((size_t)g0 * 256 + awBase);

                int d = atomicAdd(&s_ndesc, 1);
                PROF_TASK_EMIT();
                if (DESC_KEY_OF(firstKey) == DESC_KEY_OF(secondKey)) {
                    sc.words[d] = ((uint64_t)DESC_KEYREL(firstKey, rel) << 32) |
                                  ((uint64_t)off << DESC_LEN_BITS) | 2u;
                } else {
                    sc.words[d] = ((uint64_t)DESC_KEYREL(firstKey, rel) << 32) |
                                  ((uint64_t)off << DESC_LEN_BITS) | 1u;
                    d = atomicAdd(&s_ndesc, 1);
                    PROF_TASK_EMIT();
                    sc.words[d] = ((uint64_t)DESC_KEYREL(secondKey, rel) << 32) |
                                  ((uint64_t)(off + 1) << DESC_LEN_BITS) | 1u;
                }

                if (rel != lo) {
                    const int col = rel - 1;
                    const uint8_t c0 = t[b0 + col];
                    const uint8_t c1 = t[b1 + col];
                    key0 = ((uint32_t)c0 << 24) | (key0 >> 8);
                    key1 = ((uint32_t)c1 << 24) | (key1 >> 8);
                    const bool nextSwapped = (c0 == c1) ? swapped : (c1 < c0);
                    awDirty = nextSwapped != swapped;
                    swapped = nextSwapped;
                }
            }
            PROF_TASK_END(len);
            continue;
        }

        // Seed with the order of the suffixes starting at each block's last
        // byte. len is the blocks in a run, typically four.
        PROF_SEED_BEG();
#if DESC_SEED_RANK
        // Counted, not insertion sorted.
        //
        // An insertion sort's next comparison depends on how the last one came
        // out, so `len` blocks are ~len*len/4 comparisons *one after another* --
        // and a comparison here is a walk through global memory that decides
        // after ~97 bytes. The chain is the cost, not the comparisons: the
        // profiler puts this seed at a third of a short run's task and half of a
        // long one's.
        //
        // Counting has no chain. Every block asks how many of the others sort
        // before it and writes itself at that rank. It is len*len comparisons
        // rather than len*len/4, so about three times the work, and they can all
        // be in flight at once instead of queued behind each other.
        //
        // No ties to break: two suffixes of one text start at different
        // positions, so they have different lengths and can never be equal.
        for (int i = 0; i < len; i++) {
            const int pi = base + i * 256;
            int r = 0;
            for (int j = 0; j < len; j++) {
                if (j != i && descSuffixLess(t, n, base + j * 256 + hi, pi + hi)) r++;
            }
            ord[r] = DESC_ORD_VAL(pi >> 8);
        }
#elif DESC_SEED_RANK == 2
        // Counted, and each pair asked once. Measured worse; see DESC_SEED_RANK.
        //
        // The counted rank above buys independence from the insertion sort's
        // chain and pays len*len comparisons for it, which is why it loses:
        // half of those are the same question twice, since (i,j) and (j,i) have
        // one answer between them. Walking the upper triangle and crediting
        // whichever block loses keeps every comparison independent of the last
        // and issues half as many -- len*len/2 against the insertion sort's
        // len*len/4, but with no chain in front of any of them.
        //
        // The rank counters live in the key array, which is not written until
        // the loop below this one and is therefore free until then.
        for (int i = 0; i < len; i++) key[i] = 0;
        for (int i = 0; i < len; i++) {
            const int pi = base + i * 256 + hi;
            uint32_t ri = key[i];
            for (int j = i + 1; j < len; j++) {
                if (descSuffixLess(t, n, base + j * 256 + hi, pi)) ri++;
                else key[j]++;
            }
            key[i] = ri;
        }
        for (int i = 0; i < len; i++)
            ord[key[i]] = DESC_ORD_VAL(g0 + i);
#elif DESC_ABLATE == 4
        for (int i = 0; i < len; i++) ord[i] = DESC_ORD_VAL(g0 + i);
#else
        for (int i = 0; i < len; i++) ord[i] = DESC_ORD_VAL(g0 + i);
        for (int i = 1; i < len; i++) {
            const DescOrd v = ord[i];
            int j = i;
            while (j > 0 && descSuffixLess(t, n, DESC_ORD_POS(v) + hi,
                                           DESC_ORD_POS(ord[j - 1]) + hi)) {
                ord[j] = ord[j - 1];
                j--;
            }
            ord[j] = v;
        }
#endif
        // The chunk's one full key read; every column below it slides.
        for (int i = 0; i < len; i++)
            key[i] = descKey32(t, n, DESC_ORD_POS(ord[i]) + hi);
        PROF_SEED_END(len);
        PROF_COLS_BEG();

        // Which of this chunk's columns are constant across the run, in address
        // order. The walk used to rediscover that with `len` scattered loads per
        // column; a constant column then still paid those loads just to skip the
        // sort. One sequential pass lets those columns slide from t[base+col]
        // and skip the gather. Chunks start at 0/64/128/192, so the uint4 path
        // is aligned on the same texts the run finder already requires.
        // One bit per column instead of one byte.
        //
        // A 64-byte array indexed by a loop variable is thread-local memory,
        // which is global memory with a per-thread address: 64 bytes a thread
        // written once and read once per column, on 256 threads of 336 blocks.
        // A chunk is exactly 64 columns, so the same table is two registers,
        // and the read the walk does every column becomes a shift and an AND.
        // Set bit i means column lo+i holds the same byte in every block of the
        // run.
#if DESC_COLMASK
        uint64_t col_mask = 0;
#else
        uint8_t col_same[DESC_CHUNK_COLS];
#endif
        {
            int off = 0;
            if ((((uintptr_t)(t + base + lo)) & 15u) == 0) {
                for (; off + 16 <= DESC_CHUNK_COLS; off += 16) {
                    const uint32_t* b0 =
                        (const uint32_t*)(t + (size_t)base + lo + off);
                    const uint32_t a0 = b0[0], a1 = b0[1], a2 = b0[2], a3 = b0[3];
                    uint32_t m0 = 0xFFFFFFFFu, m1 = m0, m2 = m0, m3 = m0;
                    for (int g = 1; g < len; g++) {
                        const uint32_t* bg =
                            (const uint32_t*)(t + (size_t)base + g * 256 + lo + off);
                        m0 &= __vcmpeq4(a0, bg[0]);
                        m1 &= __vcmpeq4(a1, bg[1]);
                        m2 &= __vcmpeq4(a2, bg[2]);
                        m3 &= __vcmpeq4(a3, bg[3]);
                    }
                    const uint32_t ms[4] = { m0, m1, m2, m3 };
#if DESC_COLMASK
                    // __vcmpeq4 leaves each column's answer in bits 0, 8, 16
                    // and 24 of its word; gather those into four adjacent bits.
                    uint32_t nib = 0;
                    for (int k = 0; k < 4; k++) {
                        const uint32_t m = ms[k];
                        nib |= ((( m        & 1u)       |
                                 ((m >>  7) & 2u)       |
                                 ((m >> 14) & 4u)       |
                                 ((m >> 21) & 8u)) << (4 * k));
                    }
                    col_mask |= (uint64_t)nib << off;
#else
                    for (int k = 0; k < 4; k++) {
                        const uint32_t m = ms[k];
                        col_same[off + 4 * k + 0] = (uint8_t)( m        & 1u);
                        col_same[off + 4 * k + 1] = (uint8_t)((m >>  8) & 1u);
                        col_same[off + 4 * k + 2] = (uint8_t)((m >> 16) & 1u);
                        col_same[off + 4 * k + 3] = (uint8_t)((m >> 24) & 1u);
                    }
#endif
                }
            }
            for (; off < DESC_CHUNK_COLS; off++) {
                const uint8_t c0 = t[base + lo + off];
                uint8_t same = 1;
                for (int g = 1; g < len; g++) {
                    if (t[base + g * 256 + lo + off] != c0) { same = 0; break; }
                }
#if DESC_COLMASK
                col_mask |= (uint64_t)same << off;
#else
                col_same[off] = same;
#endif
            }
        }
        PROF_COLS_END(len);

#if DESC_SAMEKEY
        // Columns whose next DESC_KEY_BYTES bytes are constant across the run.
        const uint64_t same_mask = col_mask & (col_mask >> 1) & (col_mask >> 2);
#endif
#if DESC_UNIFORM
        // True while every block's key is the same; then k0 is that key and the
        // shared key array is not read or written at all.
        uint32_t k0 = key[0];
        bool uniform = true;
        for (int x = 1; x < len; x++) {
            if (DESC_KEY_OF(key[x]) != DESC_KEY_OF(k0)) { uniform = false; break; }
        }
#endif

        // The arena slot that currently holds ord[0..len), and whether ord has
        // moved since it was written there. See DESC_ARENA_REUSE.
        uint32_t awBase  = 0;
        bool     awDirty = true;

        for (int rel = hi; rel >= lo; rel--) {
            // Where this column's positions go. Every column of a run emits
            // every one of its blocks exactly once, so the offset is the column
            // number times the run length and needs no running total -- which is
            // what lets chunks write into the same arena without meeting.
            //
            // The groups below partition [0, len) in order, so a column's slot
            // is exactly ord[0..len) whichever way the keys split it. That is
            // what lets a column point at another column's slot.
#if DESC_ARENA_REUSE
            if (awDirty) {
                awBase = (uint32_t)(255 - rel) * (uint32_t)len;
#if DESC_ABLATE != 2
                for (int x = 0; x < len; x++)
                    arena[awBase + x] = (uint16_t)(DESC_ORD_POS(ord[x]) >> 8);
#endif
                awDirty = false;
            }
#else
            awBase = (uint32_t)(255 - rel) * (uint32_t)len;
            for (int x = 0; x < len; x++)
                arena[awBase + x] = (uint16_t)(DESC_ORD_POS(ord[x]) >> 8);
#endif
            // Split the ordered suffixes into maximal groups sharing the key's
            // leading bytes, and record each as one descriptor.
#if DESC_UNIFORM
            if (uniform) {
#if DESC_ABLATE != 5
                const uint32_t off = (uint32_t)((size_t)g0 * 256 + awBase);
                const int d = atomicAdd(&s_ndesc, 1);
                PROF_TASK_EMIT();
                sc.words[d] = ((uint64_t)DESC_KEYREL(k0, rel) << 32) |
                              ((uint64_t)off << DESC_LEN_BITS) | (uint32_t)len;
#endif
            } else
#endif
#if DESC_SAMEKEY
            // Where the next DESC_KEY_BYTES columns are all constant across the
            // run, every block of the run has the same key bytes there, so the
            // whole run is one group and the scan below can only prove it the
            // long way -- it reads len keys out of shared memory and compares
            // them to discover what the constant-column mask already knows.
            // Measured on the CPU sort, 91% of columns are like this.
            //
            // The mask makes it free. col_mask already has one bit per column
            // of this chunk, so ANDing it with itself shifted by one and two is
            // the whole test, once per task rather than once per column. The
            // chunk's top two columns fall out on their own: their bits would
            // need columns from the chunk above, and the shift brings in zeros,
            // so they take the scan.
            if ((same_mask >> (rel - lo)) & 1ull) {
                const uint32_t off = (uint32_t)((size_t)g0 * 256 + awBase);
                const int d = atomicAdd(&s_ndesc, 1);
                PROF_TASK_EMIT();
                sc.words[d] = ((uint64_t)DESC_KEYREL(key[0], rel) << 32) |
                              ((uint64_t)off << DESC_LEN_BITS) | (uint32_t)len;
            } else
#endif
            {
            int i = 0, groups = 0;
            while (i < len) {
                const uint32_t k = key[i];
                int j = i + 1;
                while (j < len && DESC_KEY_OF(key[j]) == DESC_KEY_OF(k)) j++;

                const uint32_t off = (uint32_t)((size_t)g0 * 256 + awBase + i);

                const int d = atomicAdd(&s_ndesc, 1);
                PROF_TASK_EMIT();
#if DESC_ABLATE != 1
                sc.words[d] = ((uint64_t)DESC_KEYREL(k, rel) << 32) |
                              ((uint64_t)off << DESC_LEN_BITS) | (uint32_t)(j - i);
#endif
                groups++;
                i = j;
            }
#if DESC_UNIFORM
            // The scan has just answered the question the state asks, so take
            // its answer rather than asking again next column.
            if (groups == 1) { uniform = true; k0 = key[0]; }
#endif
            }

            if (rel == lo) break;

            // Step left. A constant column prepends the same byte to every
            // suffix, so the order does not change; col_same already knows, and
            // t[base+col] is that byte.
            const int col = rel - 1;
#if DESC_COLMASK
            if ((col_mask >> (col - lo)) & 1ull) {
#else
            if (col_same[col - lo]) {
#endif
                const uint32_t c0 = (uint32_t)t[base + col];
#if DESC_UNIFORM
                // Equal keys stay equal: the same byte goes on the front of all
                // of them. So while uniform this is one register and no shared
                // memory at all.
                if (uniform) { k0 = (c0 << 24) | (k0 >> 8); continue; }
#endif
                for (int x = 0; x < len; x++)
                    key[x] = (c0 << 24) | (key[x] >> 8);
                continue;
            }

#if DESC_UNIFORM
            // This column reads a byte per block, so the keys are about to
            // stop being copies of one another. Put the register back into the
            // array first; the scan below decides whether they are equal again.
            if (uniform) {
                for (int x = 0; x < len; x++) key[x] = k0;
                uniform = false;
            }
#endif

#if DESC_ABLATE == 3
            for (int x = 0; x < len; x++) {
                const uint8_t c = (uint8_t)t[base + col];
                key[x] = ((uint32_t)c << 24) | (key[x] >> 8);
            }
#else
            const uint8_t c0 = (uint8_t)t[DESC_ORD_POS(ord[0]) + col];
            key[0] = ((uint32_t)c0 << 24) | (key[0] >> 8);
            for (int x = 1; x < len; x++) {
                const uint8_t c = (uint8_t)t[DESC_ORD_POS(ord[x]) + col];
                key[x] = ((uint32_t)c << 24) | (key[x] >> 8);
            }
#endif

            // Stable insertion sort by that one byte, which is now the top byte
            // of the slid key, so this reads no text at all. The existing order
            // is exactly the right tie-break, which is what makes one byte
            // enough. ord and key permute together or they stop corresponding.
#if DESC_ABLATE != 6
            // The slot ord lives in stops describing it only if ord actually
            // moves, and a column that is not constant is not the same thing as
            // a column that reorders: the byte it prepends can differ between
            // blocks and still arrive in the order they were already in. The
            // sort knows -- it shifted something or it did not -- so the arena
            // is rewritten on that and not on the weaker test.
            for (int x = 1; x < len; x++) {
                const DescOrd v = ord[x];
                const uint32_t kv = key[x];
                const uint8_t kb = (uint8_t)(kv >> 24);
                int y = x;
                while (y > 0 && (uint8_t)(key[y - 1] >> 24) > kb) {
                    ord[y] = ord[y - 1];
                    key[y] = key[y - 1];
                    y--;
                }
                if (y != x) {
                    ord[y] = v;
                    key[y] = kv;
                    awDirty = true;
                }
#if !DESC_DIRTY_MOVED
                awDirty = true;
#endif
            }
#else
            awDirty = true;
#endif
        }
        PROF_TASK_END(len);
    }

    PROF_WALK_END();
    PROF_ADD(PH_D_WALK);

    // The bytes past the last whole block belong to no run, so they get one
    // descriptor each. Their arena slots are the ones the runs did not use:
    // runs own 256 * nblocks of it and the arena is n long.
    for (int p = (int)(nblocks * 256) + tid; p < n; p += BR_BLOCK) {
        const uint32_t off = (uint32_t)p;
        sc.arena[off] = (uint16_t)(p >> 8);
        const int d = atomicAdd(&s_ndesc, 1);
        sc.words[d] = ((uint64_t)DESC_KEYREL(descKey32(t, n, p), p & 255) << 32) |
                      ((uint64_t)off << DESC_LEN_BITS) | 1u;
    }
    __syncthreads();
    PROF_ADD(PH_D_TAIL);

    const int nd = s_ndesc;

    // ---- 3. sort the descriptors by key. The key is the top 32 bits of the
    // word, so this is the block-wide radix sort with nothing packed around it.
#if DESC_NO_RADIX
    // SCAFFOLDING: skip the sort. The answer is wrong; the point is the time
    // everything else takes, which is the ceiling on any faster sort.
    uint64_t* sorted = sc.words;
#elif DESC_MSD
    // Two buffers and two moves: the scatter groups the descriptors by digit,
    // the count below puts each one in its place. sc.words is dead after the
    // scatter has read it, so the second move lands back there.
    uint64_t* const bucketed = sc.words2;
    uint16_t* const lowkey   = (uint16_t*)sc.offs;
    uint64_t* sorted = sc.words;
    {
        // The bucket table doubles as the histogram, the scan and the cursors,
        // which is what keeps it to one array of BINS+1 words: the scatter
        // fills each bucket downward from its inclusive end, so when it is done
        // the cursor has walked back to the bucket's start and the table reads
        // as the bucket boundaries it needs next.
#if DESC_MSD_PACKED16
        uint32_t* const s_bkt = (uint32_t*)((char*)sh + BR_SHARED_BYTES);
#else
        int32_t* const s_bkt = (int32_t*)((char*)sh + BR_SHARED_BYTES);
#endif
        const int msdShift = 64 - DESC_MSD_BITS;

#if DESC_MSD_PACKED16
        // Every logical cursor and their inclusive prefixes must fit in one
        // half-word. A pathological text can exceed this; declining sends it
        // through the exact prefix-doubling fallback rather than truncating it.
        if (nd > 65535) return -1;
        for (int w = tid; w < DESC_MSD_WORDS; w += BR_BLOCK) s_bkt[w] = 0;
        __syncthreads();
        for (int i = tid; i < nd; i += BR_BLOCK)
            descBucketAdd(s_bkt, (int)(sc.words[i] >> msdShift));
        __syncthreads();
        descBucketScan(s_bkt, sh);
        if (tid == 0) s_bkt[DESC_MSD_BINS >> 1] = (uint32_t)nd;
        __syncthreads();
#else
        for (int b = tid; b <= DESC_MSD_BINS; b += BR_BLOCK) s_bkt[b] = 0;
        __syncthreads();
        for (int i = tid; i < nd; i += BR_BLOCK)
            atomicAdd(&s_bkt[(int)(sc.words[i] >> msdShift)], 1);
        __syncthreads();
        blockScanFlags(s_bkt, DESC_MSD_BINS, sh);   // inclusive, in place
        if (tid == 0) s_bkt[DESC_MSD_BINS] = nd;
        __syncthreads();
#endif

        for (int i = tid; i < nd; i += BR_BLOCK) {
            const uint64_t w = sc.words[i];
            const int b = (int)(w >> msdShift);
#if DESC_MSD_PACKED16
            const int p = (int)descBucketSub(s_bkt, b) - 1;
#else
            const int p = atomicSub(&s_bkt[b], 1) - 1;
#endif
            bucketed[p] = w;
            // The bits the digit did not take, kept narrow for the count below.
            // The top DESC_MSD_BITS agree inside a bucket, so what is left of a
            // 24-bit key is 24 - DESC_MSD_BITS bits and fits a uint16 -- which
            // means a whole bucket's keys arrive in one 32-byte sector instead
            // of four. sc.offs is the offset scan's array and is not written
            // until after this sort returns.
            lowkey[p] = (uint16_t)(DESC_KEY_OF((uint32_t)(w >> 32)) & DESC_MSD_LOWMASK);
        }
        __syncthreads();

        // Inside a bucket the top DESC_MSD_BITS agree, so ordering by the whole
        // upper word orders by the rest of the key -- and by the column after
        // it, which the LSD sort left to emission order and nothing reads.
        //
        // One thread per descriptor, not per bucket. A bucket holds about ten
        // and an insertion sort over ten is nothing -- but an insertion sort
        // reads and writes the element it is walking past, and those are global
        // memory, so ten elements is ~45 *dependent* round trips and one thread
        // owns all of them. Counting instead makes every read independent: a
        // descriptor asks how many of its bucket-mates come before it and
        // writes itself there, once. The reads are the same handful of words
        // for every thread in the bucket, so they are an L1 broadcast, and
        // nothing waits on anything.
        //
        // Ties are broken by position in the bucket so the placement is a
        // permutation: two descriptors can carry the same key and the same
        // column when they come from different runs.
        for (int i = tid; i < nd; i += BR_BLOCK) {
            const uint64_t w = bucketed[i];
            const int b = (int)(w >> msdShift);
#if DESC_MSD_PACKED16
            const int lo = (int)descBucketGet(s_bkt, b);
            const int hi = (int)descBucketGet(s_bkt, b + 1);
#else
            const int lo = s_bkt[b], hi = s_bkt[b + 1];
#endif
            if (hi - lo > DESC_MSD_MAX) { atomicExch(&s_fail, 1); continue; }
#if DESC_MSD_LOWKEY
            const uint32_t k = lowkey[i];
            int rank = 0;
            for (int j = lo; j < hi; j++) {
                const uint32_t kj = lowkey[j];
                rank += (kj < k) || (kj == k && j < i);
            }
#else
            const uint32_t k = (uint32_t)(w >> 32);
            int rank = 0;
            for (int j = lo; j < hi; j++) {
                const uint32_t kj = (uint32_t)(bucketed[j] >> 32);
                rank += (kj < k) || (kj == k && j < i);
            }
#endif
            sorted[lo + rank] = w;
        }
        __syncthreads();
        if (s_fail) return -1;
    }
#else
    blockRadixInit(sh);
    uint64_t* sorted = blockRadixSort(sc.words, sc.words2, nd,
                                      64 - DESC_GBITS, DESC_GBITS, sh);
#endif
    PROF_ADD(PH_D_RADIX);

    // ---- 4. output offsets: an exclusive scan over the sorted lengths. A key
    // group's positions occupy one contiguous span whether or not it collides.
    for (int i = tid; i < nd; i += BR_BLOCK) {
        sc.offs[i] = (int32_t)(sorted[i] & DESC_LEN_MASK);
    }
    __syncthreads();
    blockScanFlags(sc.offs, nd, sh);   // inclusive; exclusive = offs[i] - len
    PROF_ADD(PH_D_SCAN);

    // The radix sort leaves its result in one of its two arrays; the other is
    // finished with, and is where the merge below keeps its list boundaries.
    // Two int32 per uint64, so it holds 2n of them and the merge needs 2n.
    int32_t* const dead = (int32_t*)(sorted == sc.words ? sc.words2 : sc.words);

#if DESC_COOP_SCATTER
    // ---- 5a. place every position, one thread per output position.
    //
    // The output is the arena read in sorted-descriptor order, so output
    // position q belongs to the descriptor whose span contains it and sa[q] is
    // where it goes. Driving the loop by q rather than by descriptor is what
    // makes the writes coalesce; see DESC_COOP_SCATTER.
    //
    // Colliding groups are written here too, in arena order rather than sorted
    // order. That is the order the merge below expects to find them in -- it
    // used to gather them itself, from the same arena, into the same words --
    // so both merges lose their gather loop and keep everything else.
    {
        // Window tables, carved out of the radix sort's shared scratch, which
        // is finished with by now.
        //
        // The loop walks descriptors, not output positions, and that is the
        // whole reason it is cheap. A window of BR_BLOCK descriptors is read
        // once, coalesced, into shared memory; the output positions it covers
        // -- about 1,460 of them, since a descriptor holds ~5.7 -- are then
        // written from it. Driving by output instead would reread the offset
        // array once per tile of 256 outputs, which is 5.7 times the loads for
        // the same answer, and would take 5.7 times the barriers to do it.
        int32_t*  const s_beg = (int32_t*)sh;              // first output of k
        uint32_t* const s_aof = (uint32_t*)(s_beg + BR_BLOCK);  // its arena slot
        uint32_t* const s_key = s_aof + BR_BLOCK;          // and its sort key
        __shared__ int32_t s_end;                          // one past the window
#if DESC_OWNER_MAP
        // Which descriptor owns each output position of the window, one byte
        // each, in the radix scratch this step has not otherwise claimed.
        //
        // The search it replaces is eight *dependent* shared loads per output
        // position, ~70,000 positions a hash. Ablating it -- taking the first
        // descriptor of the window instead of the owning one, which is wrong
        // but timed -- takes the phase from 312M cycles to 204M, so it is a
        // third of it. An earlier note here says a coarse index for the same
        // search measured null; that was against a kernel whose sort was four
        // radix passes, and this phase was a fifth of the size it is now.
        //
        // Painting it costs one write per position against eight reads, and no
        // extra shared memory: s_beg, s_aof and s_key take 3 KB of the 6.8 KB
        // BlockRadixScratch, which is finished with by the time step 5a runs.
        // A window whose span does not fit falls back to the search.
        uint8_t* const s_own = (uint8_t*)(s_key + BR_BLOCK);
        __shared__ int s_q0;      // first output position of the window
        __shared__ int s_fits;    // the whole window fits in s_own
#endif

        for (int dbase = 0; dbase < nd; dbase += BR_BLOCK) {
            const int nt = (nd - dbase) < BR_BLOCK ? (nd - dbase) : BR_BLOCK;

            if (tid < nt) {
                const uint64_t w = sorted[dbase + tid];
                const int32_t  e = sc.offs[dbase + tid];
                s_beg[tid] = e - (int32_t)(w & DESC_LEN_MASK);
                s_aof[tid] = (uint32_t)((w >> DESC_LEN_BITS) & 0xFFFFFu);
                s_key[tid] = (uint32_t)(w >> 32);
                if (tid == nt - 1) s_end = e;
#if DESC_OWNER_MAP
                if (tid == 0) {
                    s_q0 = s_beg[0];
                    s_fits = 1;
                }
#endif
            }
            __syncthreads();
#if DESC_OWNER_MAP
            if (s_fits && tid < nt) {
                const int b = s_beg[tid] - s_q0;
                const int e = b + (int)(sorted[dbase + tid] & DESC_LEN_MASK);
                if (e > DESC_OWN_CAP) s_fits = 0;
                else for (int z = b; z < e; z++) s_own[z] = (uint8_t)tid;
            }
            __syncthreads();
#endif

            // Which key groups collide, answered here rather than in a pass of
            // its own. Step 5 used to walk all ~20,000 descriptors again to ask
            // exactly this, of exactly these words, and it read three of them
            // per descriptor to do it -- the descriptor, its predecessor and its
            // successor. All three are in the window this loop has already read,
            // so the question costs shared memory instead of a second sweep of
            // the array, and step 5 is handed the ~250 groups that answer yes
            // instead of the whole array to sift.
            //
            // Only the two descriptors on the window's edges reach outside it,
            // and they read the one word next to the window rather than the
            // whole of it.
            if (tid < nt) {
                const uint32_t k = s_key[tid];
                const int i = dbase + tid;
                const bool head = (i == 0) ||
                    (tid > 0 ? (DESC_KEY_OF(s_key[tid - 1]) != DESC_KEY_OF(k))
                             : (DESC_KEY_OF(sorted[i - 1] >> 32) != DESC_KEY_OF(k)));
                if (head) {
                    const bool more = (tid + 1 < nt)
                        ? (DESC_KEY_OF(s_key[tid + 1]) == DESC_KEY_OF(k))
                        : (i + 1 < nd &&
                           DESC_KEY_OF(sorted[i + 1] >> 32) == DESC_KEY_OF(k));
                    // From the top of the dead array down, so the merge's bump
                    // allocator can keep growing from the bottom.
                    if (more) dead[2 * n - 1 - atomicAdd(&s_ncol, 1)] = i;
                }
            }

            for (int q = s_beg[0] + tid; q < s_end; q += BR_BLOCK) {
                // The last descriptor of the window whose span starts at or
                // before q owns it. Eight steps of shared memory, against a
                // walk through global memory.
                int lo = 0, hi = nt - 1;
#if DESC_OWNER_MAP
#if DESC_ABLATE == 10
                {
                    int lo2 = 0, hi2 = nt - 1;
                    while (lo2 < hi2) {
                        const int mid = (lo2 + hi2 + 1) >> 1;
                        if (s_beg[mid] <= q) lo2 = mid; else hi2 = mid - 1;
                    }
                    if (s_fits && (int)s_own[q - s_q0] != lo2) {
                        sa[q] = -1; continue;
                    }
                }
#endif
                if (s_fits && DESC_ABLATE != 9) {
                    lo = (int)s_own[q - s_q0];
                } else
#endif
                {
#if DESC_ABLATE != 7
                while (lo < hi) {
                    const int mid = (lo + hi + 1) >> 1;
                    if (s_beg[mid] <= q) lo = mid; else hi = mid - 1;
                }
#endif
                }
#if DESC_ABLATE == 8
                sa[q] = (int32_t)DESC_REL_OF(s_key[lo]);
#else
                sa[q] = (int32_t)(((uint32_t)sc.arena[s_aof[lo] + (q - s_beg[lo])] << 8) |
                                  DESC_REL_OF(s_key[lo]));
#endif
            }
            __syncthreads();
        }
    }
    __syncthreads();
    PROF_ADD(PH_D_EXPAND);
#endif

    // ---- 5. the colliding groups.
    //
    // A descriptor whose key nothing else shares is already in its final place
    // relative to everything else -- that is ~98.7% of them, and step 5a has
    // finished with all of them. What is left is the ~1.3% that collide, whose
    // positions are in sa but in arena order rather than sorted order.
    //
    // This pass is what finds them: one loop over the descriptors asking, of
    // each, whether it is the first of a key group and whether that group holds
    // anything else. Both questions are answered from the two words a thread
    // has already read, and only a group head does any work.
    //
    // The merge itself is one thread per group for the small ones -- there are
    // ~242 of them a text holding ~7 positions each, and a block-wide treatment
    // of those would cost more in barriers than it saved. The ~5 large ones are
    // set aside for step 6, because one thread on the largest of them was 43.5%
    // of the phase; see DESC_MERGE_WIDE.
#ifdef DESC_NO_MERGE
    /* DESC_NO_MERGE writes every descriptor, colliding or not, in whatever
     * order the arena holds it, and skips the merge entirely. The answer is
     * then wrong inside colliding groups but must still be a *permutation* of
     * 0..n-1, and that is what separates two very different faults.
     *
     * It earned its place finding one. The merge was writing 41 positions twice
     * and losing 41 others per text; with this defined the output was a clean
     * permutation, which said the walk, the arena partition, the radix sort and
     * the offset scan were all correct and put the fault in the merge, where it
     * was: the boundary arrays were indexed by output offset over a closed
     * interval, so each group\'s last boundary word landed on the next group\'s
     * first. */
#if !DESC_COOP_SCATTER
    for (int i = tid; i < nd; i += BR_BLOCK) {
        const uint64_t w = sorted[i];
        const uint32_t len = (uint32_t)(w & DESC_LEN_MASK);
        const uint32_t off = (uint32_t)((w >> DESC_LEN_BITS) & 0xFFFFFu);
        const int32_t o = sc.offs[i] - (int32_t)len;
        for (uint32_t x = 0; x < len; x++)
            sa[o + x] = (int32_t)(((uint32_t)sc.arena[off + x] << 8) |
                                  DESC_REL_OF(w >> 32));
    }
#endif
    __syncthreads();
#else
#if DESC_COOP_SCATTER
    const int ncol = s_ncol;    // step 5a already found them
#else
    const int ncol = nd;
#endif
    for (int ci = tid; ci < ncol; ci += BR_BLOCK) {
#if DESC_COOP_SCATTER
        const int i = dead[2 * n - 1 - ci];
#else
        const int i = ci;
#endif
        const uint64_t w = sorted[i];
        const uint32_t key = (uint32_t)(w >> 32);
#if !DESC_COOP_SCATTER
        if (i > 0 && DESC_KEY_OF(sorted[i - 1] >> 32) == DESC_KEY_OF(key)) continue;
#endif

        int j = i + 1;
        while (j < nd && DESC_KEY_OF(sorted[j] >> 32) == DESC_KEY_OF(key)) j++;

        const uint32_t len0 = (uint32_t)(w & DESC_LEN_MASK);
        const int32_t o = sc.offs[i] - (int32_t)len0;

        if (j == i + 1) {   // nothing shares this key
#if !DESC_COOP_SCATTER
            const uint32_t off = (uint32_t)((w >> DESC_LEN_BITS) & 0xFFFFFu);
            for (uint32_t x = 0; x < len0; x++)
                sa[o + x] = (int32_t)(((uint32_t)sc.arena[off + x] << 8) |
                                      DESC_REL_OF(w >> 32));
#endif
            continue;   // 5a placed it
        }

        const int nlist0 = j - i;

        // Large groups go to the block. offs is an inclusive scan of the
        // lengths, so the group\'s total is known here without gathering it
        // first. A group past the list\'s capacity falls through and is merged
        // by this one thread, which is the old behaviour and still correct.
        if (sc.offs[j - 1] - o >= DESC_MERGE_WIDE) {
            const int bi = atomicAdd(&s_nbig, 1);
            if (bi < DESC_BIG_MAX) {
                dead[2 * bi] = i;
                dead[2 * bi + 1] = j;
                continue;
            }
        }

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
        // The collision list step 5a wrote grows down from 2n, so the space
        // this allocator may use ends where that list starts.
        if (bbase + nb > 2 * n - ncol) { atomicExch(&s_fail, 1); continue; }
        int32_t* ba = dead + bbase;
        int32_t* bb = dead + bbase + nlist0 + 1;

        // The lists are already end to end at a: 5a wrote every descriptor at
        // its own output offset, and within a group those offsets are exactly
        // this layout. All that is left is where each one starts.
        int total = 0;
        for (int k = i; k < j; k++) {
            const uint64_t wk = sorted[k];
            const uint32_t len = (uint32_t)(wk & DESC_LEN_MASK);
            ba[k - i] = total;
#if !DESC_COOP_SCATTER
            const uint32_t off = (uint32_t)((wk >> DESC_LEN_BITS) & 0xFFFFFu);
            for (uint32_t x = 0; x < len; x++)
                a[total + x] = (int32_t)(((uint32_t)sc.arena[off + x] << 8) |
                                         DESC_REL_OF(wk >> 32));
#endif
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
                        b[pos++] = descSuffixLessFrom(t, n, a[p1], a[p0], DESC_CMP_FROM)
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
    __syncthreads();
    PROF_ADD(PH_D_SCATTER);

    // ---- 6. the few large colliding groups, the whole block on one at a time.
    //
    // Same algorithm as above -- merge adjacent pairs of sorted lists until one
    // is left -- with the round spread across the block instead of run by one
    // thread. Two things make that possible.
    //
    // A round's output occupies exactly the same index range as its input,
    // because a pair of lists merges into the span the two of them already fill.
    // So the whole round is one array of `total` outputs, and a thread can be
    // given a contiguous slice of it without knowing anything about the pairs.
    //
    // And a merge can be entered in the middle. For output position d of a pair,
    // the split (i, j) with i + j = d that a serial merge would have reached is
    // the unique one where the last element taken from either list is not
    // greater than the next element of the other -- a *merge path*, found by
    // binary search over i alone in O(log) comparisons. So a thread binary
    // searches its way to its own starting split and then merges serially from
    // there.
    //
    // The comparison count is what matters, because a comparison here walks ~62
    // bytes of text before it decides and the kernel is memory bound. This costs
    // the serial merge's comparisons plus one binary search per thread per pair
    // it touches, and nothing else. Placing every element independently by
    // binary search would have been simpler and multiplies the comparisons by
    // log of the list length, which on a memory-bound kernel is a loss.
    {
        const int nbig = s_nbig < DESC_BIG_MAX ? s_nbig : DESC_BIG_MAX;
        for (int bg = 0; bg < nbig; bg++) {
            const int gi = dead[2 * bg], gj = dead[2 * bg + 1];
            const int L = gj - gi;
            const int32_t o = sc.offs[gi] - (int32_t)(sorted[gi] & DESC_LEN_MASK);
            const int total = sc.offs[gj - 1] - o;

            if (tid == 0) s_bbase = atomicAdd(&s_bump, 2 * (L + 1));
            __syncthreads();
            const int bbase = s_bbase;
            if (bbase + 2 * (L + 1) > 2 * n) {
                if (tid == 0) atomicExch(&s_fail, 1);
                __syncthreads();
                continue;
            }

            int32_t* a  = sa + o;
            int32_t* b  = sc.mbuf + o;
            int32_t* ba = dead + bbase;
            int32_t* bb = ba + (L + 1);

            // Where each list starts. Each descriptor's start is its own
            // exclusive offset relative to the group's, so this needs no scan
            // and no atomic -- one thread per descriptor, all independent. The
            // data itself is already there; see step 5a.
            for (int k = gi + tid; k < gj; k += BR_BLOCK) {
                const uint64_t wk = sorted[k];
                const uint32_t len = (uint32_t)(wk & DESC_LEN_MASK);
                const int32_t base = sc.offs[k] - (int32_t)len - o;
                ba[k - gi] = base;
#if !DESC_COOP_SCATTER
                const uint32_t off = (uint32_t)((wk >> DESC_LEN_BITS) & 0xFFFFFu);
                for (uint32_t x = 0; x < len; x++)
                    a[base + x] = (int32_t)(((uint32_t)sc.arena[off + x] << 8) |
                                            DESC_REL_OF(wk >> 32));
#endif
            }
            if (tid == 0) ba[L] = total;
            __syncthreads();

            int nlist = L;
            while (nlist > 1) {
                const int npairs = (nlist + 1) >> 1;

                // Pair p merges lists 2p and 2p+1, so it owns output
                // [ba[2p], ba[2p+2]). An odd list out pairs with an empty one.
                for (int p = tid; p <= npairs; p += BR_BLOCK)
                    bb[p] = (2 * p < nlist) ? ba[2 * p] : total;
                __syncthreads();

                const int d0 = (int)(((int64_t)total * tid) / BR_BLOCK);
                const int d1 = (int)(((int64_t)total * (tid + 1)) / BR_BLOCK);

                int d = d0;
                while (d < d1) {
                    // Which pair owns output d.
                    int plo = 0, phi = npairs - 1;
                    while (plo < phi) {
                        const int pm = (plo + phi + 1) >> 1;
                        if (bb[pm] <= d) plo = pm; else phi = pm - 1;
                    }
                    const int s0 = ba[2 * plo];
                    const int e0 = ba[2 * plo + 1];
                    const int s1 = e0;
                    const int e1 = (2 * plo + 2 <= nlist) ? ba[2 * plo + 2] : total;
                    const int n0 = e0 - s0, n1 = e1 - s1;
                    const int diag = d - s0;

                    // The largest i for which taking i from the first list and
                    // diag-i from the second is a prefix of the merge. The
                    // predicate falls from true to false as i grows, so this is
                    // an ordinary binary search; the bounds keep both indices
                    // inside their lists, which is why neither end case appears.
                    int lo = diag - n1 > 0 ? diag - n1 : 0;
                    int hi = diag < n0 ? diag : n0;
                    while (lo < hi) {
                        const int mid = (lo + hi + 1) >> 1;
                        if (descSuffixLessFrom(t, n, a[s1 + diag - mid],
                                               a[s0 + mid - 1], DESC_CMP_FROM))
                            hi = mid - 1;
                        else
                            lo = mid;
                    }

                    int i = lo, j = diag - lo;
                    const int endp = bb[plo + 1];
                    const int stop = d1 < endp ? d1 : endp;
                    while (d < stop) {
                        const bool takeB = (i >= n0) ||
                            (j < n1 && descSuffixLessFrom(t, n, a[s1 + j],
                                                          a[s0 + i], DESC_CMP_FROM));
                        b[d++] = takeB ? a[s1 + j++] : a[s0 + i++];
                    }
                }
                __syncthreads();

                int32_t* tv = a; a = b; b = tv;
                int32_t* tb = ba; ba = bb; bb = tb;
                nlist = npairs;
            }

            // An odd number of rounds leaves the answer in the scratch half.
            if (a != sa + o) {
                for (int x = tid; x < total; x += BR_BLOCK) sa[o + x] = a[x];
            }
            __syncthreads();
        }
    }
#endif
    __syncthreads();
    PROF_ADD(PH_D_MERGE);

    return s_fail ? -1 : 0;
}
