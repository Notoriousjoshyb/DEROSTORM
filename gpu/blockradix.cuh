// blockradix.cuh -- a stable LSD radix sort of (uint64 key, int32 value) pairs
// carried out by one thread block, plus the block-wide scans the suffix-array
// code needs alongside it.
//
// This is the primitive option B is built on. The thread-per-hash suffix sort
// is stuck because each thread owns half a megabyte and touches it randomly:
// 16,000 of those is a 7 GB working set against a 64 MB L2, so essentially
// every access is a cold 32-byte sector fetch. A block per hash inverts that.
// Only 42-84 hashes are in flight, the working set drops to a few hundred
// megabytes, and every access below is a streaming one.
//
// ---------------------------------------------------------------------------
// Stability
//
// LSD radix is only correct if each pass preserves the order the previous pass
// produced, so the scatter ranks elements by their original position, not by
// whichever warp reaches an atomic first:
//
//   - within a warp, __match_any_sync groups the lanes sharing a digit and
//     __popc of the lanes below gives each one its rank, in lane order;
//   - across the warps of a tile, each warp's count for a digit is published to
//     a small shared matrix and the warps prefix-sum it in warp order;
//   - across tiles, the digit cursors advance only after every warp in the tile
//     has read them, so tiles land in order.
//
// The matrix is left zeroed after every tile by the same lanes that wrote it,
// which is why it is not cleared per tile: clearing 8,192 entries for a tile of
// 1,024 elements would cost more than the sort.
//
// ---------------------------------------------------------------------------
// Why the tile is staged in shared memory
//
// The obvious scatter -- every thread writing its pair straight to its global
// destination -- makes a warp of 32 lanes touch 32 unrelated addresses, and the
// memory system turns that into roughly 32 separate sector transactions for
// 384 useful bytes.
//
// That the writes were the problem is not a guess. Every pass over the keys can
// have its histogram counted once up front, because a pass only permutes the
// keys and cannot change how many of each digit there are; doing that removes a
// whole read of the key array per pass. It bought exactly nothing, measured,
// which says the reads were already being served and the writes were the cost.
//
// So the tile is first written into shared memory in digit order, and only then
// copied out. Same-digit elements are then adjacent both in shared memory and
// at their destination, so consecutive lanes write consecutive addresses within
// each digit's run.
//
// It was worth about 5%, not the 20-30% expected, and the reason is worth
// recording: the hardware had been coalescing most of those writes on its own.
// Both experiments -- fusing the histograms, then staging the scatter -- say the
// sort is not held up by how it touches memory.
//
// It is not held up by how much memory it touches either, which is what the
// note here used to conclude. Measured on an RTX 5080 at the tuned settings,
// during real mining: the memory controller is busy 65% of the time, the card
// draws 195 W of a 360 W limit, and the sort moves about 350 GB/s of a ~960 GB/s
// peak. Roughly a third of the available bandwidth, not three quarters.
//
// So the headroom was on this side of the memory system -- the shared traffic and
// the barriers -- rather than in the width of the key.
//
// ---------------------------------------------------------------------------
// Where the time actually goes
//
// Nsight Compute cannot read this machine's counters without a driver
// permission that needs admin, so the kernel times itself: thread 0 samples
// clock64() at each phase boundary and the deltas accumulate per phase. That
// instrumentation lives in prof.cuh and compiles to nothing unless DS_PROF is
// defined -- it used to live in copies of this file under gpu/prof/, which went
// stale the moment this one changed, which is exactly when a profile is wanted.
//
// Measured after the packed word and the four-byte seed, as shares of the
// suffix sort:
//
//   tile: rank / match_any           30.4%    round: build keys          10.1%
//   tile: scanBins                    9.3%    round: flags + scanFlags    8.0%
//   tile: leader walk                 7.7%    round: groupStarts          5.9%
//   tile: copy out                    4.7%    round: rank update          4.6%
//   tile: cursors                     4.2%    round: compaction           4.3%
//   tile: stage to shared             4.1%    round: write sa back        3.6%
//   histograms                        2.6%    offset scan                 0.6%
//
// Read the first line before believing any of the rest: an earlier run of this
// same table put "write sa back" at 35.9%, which is absurd for one int32 store
// per element. The round's timer was still running through blockRadixSort, so
// that phase was billing for the entire sort. Nested timers need the outer one
// restarted behind the inner; see the PROF_MARK after the sort call.
//
// The top phase is a global load, a __match_any_sync and a barrier, and the
// interesting question was which. It is the load: replacing match_any with one
// ballot per digit bit moved the phase 30.4% -> 28.8% and the sort 11,939 ->
// 12,089 arrays a second, inside the noise. So what is left at the top is the
// irreducible read of a radix pass, one word per element per pass, and only
// fewer passes touches it.
//
// Experiments run against this file, six of nine flat or negative:
//
//   kept      pack key and payload into one 64-bit word    6,910 -> 9,881/s
//   kept      seed the doubling with four bytes, not one   9,881 -> 13,390/s
//   kept      cooperative column scan for the leader walk  7.01 -> 7.73 KH/s
//   kept      stage the tile in shared before writing out  about +5%
//   reverted  eight-bit seed bytes                         wrong on 3 of 512
//   flat      fuse the per-pass histograms into one walk   no change
//   flat      nine-bit digits, four passes instead of five 13,165 vs 13,390/s
//   flat      ballots instead of __match_any_sync          12,089 vs 11,939/s
//   flat      shuffle scan for the cross-warp step         7.70 vs 7.73 KH/s
//   0.2%      dense group numbering to narrow the key      measured, not built
//
// The last one is worth a note: r1 in the sort key is a position, 17 bits, but
// it only has to tell the active groups apart. Numbering them densely would
// narrow the key -- except that gpu/lcpstat measures 10,753 active groups in
// the first round, needing 14 bits, so the key stays 31 bits and the pass count
// stays 5. It only pays in the tail rounds, which carry 3% of the work.

#pragma once
#include <cstdint>
#include "prof.cuh"

// Threads per block, i.e. threads per hash. Overridable from the compiler line
// so the sweep in sa_parallel_test can be repeated at several widths: a wider
// block hides memory latency better and gives longer same-digit runs, but costs
// BR_WARPS * BR_BINS words of shared memory.
#ifndef BR_BLOCK
#define BR_BLOCK 1024
#endif
#define BR_WARPS (BR_BLOCK / 32)

// Bits per pass, also overridable. This is a direct trade against coalescing:
// fewer bits means more passes, but a tile of BR_BLOCK elements spread over
// BR_BINS digits gives runs of BR_BLOCK/BR_BINS elements, and it is the length
// of those runs that decides how contiguous the copy-out is. At 1,024 threads:
// 8 bits gives runs of 4, 7 bits gives runs of 8, 6 gives 16, 5 gives 32 -- a
// whole warp -- for one, one and two extra passes respectively.
//
// Measured across 5, 6, 7 and 8: 6526, 6738, 6910, 6476 suffix arrays a second.
// The spread is small and the shape is not monotonic, which is the finding
// rather than the tuning -- see the note above the scatter.
//
// That sweep, and every one after it, ran at BR_BLOCK=1024. The shipped library
// is built at 256, and the answer is different there -- which is the finding
// this time, not the number. Shared memory per block scales with BR_WARPS *
// BR_BINS, so a narrower block moves the whole trade. Three interleaved
// `--bench --gpu=0` rounds at 336 blocks on an RTX 5080, at BR_BLOCK=256:
//
//     5  160.95 / 160.97 / 160.94 KH/s
//     6  163.77 / 163.79 / 163.71        <- peak, and no overlap with 5 or 7
//     7  160.44 / 160.00 / 160.56        <- what shipped
//     8  142.95 / 142.75 / 142.76
//     4  killed after 14 minutes on one round
//
// 8 is the interesting loss. The descriptor key is 24 bits, so 8-bit digits
// order it in three passes instead of four, and the note above the scatter says
// fewer passes is the only lever left on the top phase. It is a 12% loss anyway:
// 256 bins doubles warpCnt and hist, the block's shared memory goes from about
// 11 KB to about 19 KB, and the SM holds fewer blocks. Occupancy beats pass
// count here, and that is why 6 wins over 7 as well -- same four passes, less
// shared memory, and same-digit runs of 4 instead of 2.
#ifndef BR_BITS
#define BR_BITS 6
#endif
#define BR_BINS (1 << BR_BITS)

// Fuse the warp x digit column scan and the bin scan into one warp-resident
// step, which is three barriers per tile instead of six.
//
// The two steps either side of `preInTile` do almost nothing and cost almost
// everything. The column scan is one thread per digit walking BR_WARPS rows;
// scanBins is a prefix sum over the BR_BINS totals that walk just produced, and
// it spends three __syncthreads() doing it -- a warp scan, a serial fixup on
// thread 0, and a write-back. Six barriers a tile, ~310 tiles a hash, for a scan
// over 64 numbers.
//
// One warp can do both. Lane `l` takes bins l, l+32, ... : it walks the rows for
// each, which is the column scan, and carries an inclusive warp scan across them
// as it goes, which is scanBins. Shuffles need no barrier, so the whole thing
// costs the one __syncthreads() the column scan already needed and the other
// three disappear. The other warps were waiting at that barrier either way.
//
// Requires BR_BINS to be a whole number of warps; below BR_BITS=5 it falls back.
// 0 restores the two-step version exactly, for A/B.
#ifndef BR_FUSED_BINSCAN
#define BR_FUSED_BINSCAN 1
#endif
#if BR_BINS % 32 != 0
#undef  BR_FUSED_BINSCAN
#define BR_FUSED_BINSCAN 0
#endif

// Passes a key can need. The suffix sort packs two ranks of at most 17 bits
// each, so 34 bits; this covers 48 with room to spare.
#define BR_MAXPASSES ((48 + BR_BITS - 1) / BR_BITS)

// Shared state. One instance per block, reused by every pass.
//
// This lives in dynamic shared memory, not static: at 1,024 threads it is about
// 54 KB, and static shared memory is capped at 48 KB on every CUDA
// architecture. Dynamic has no such cap, only the per-block maximum the host
// opts into with cudaFuncAttributeMaxDynamicSharedMemorySize.
struct BlockRadixScratch {
    int32_t  digitOff[BR_BINS];             // running write cursor per digit
    int32_t  tileTot[BR_BINS];              // this tile's count per digit
    int32_t  tileStart[BR_BINS];            // where each digit starts in stage
    int32_t  warpCnt[BR_WARPS][BR_BINS];    // per-tile counts; zero between tiles
    int32_t  hist[BR_MAXPASSES][BR_BINS];   // one histogram per pass, built once
    uint64_t stageWord[BR_BLOCK];           // the tile, reordered by digit
    int32_t  warpScan[BR_WARPS];            // scratch for the scans below
    int32_t  scanCarry;                     // running total across tiles
};

// Bytes of dynamic shared memory a block needs. The host passes this to the
// launch and to cudaFuncSetAttribute.
#define BR_SHARED_BYTES ((int)sizeof(BlockRadixScratch))

__device__ __forceinline__ unsigned laneMaskLt() {
    unsigned m;
    asm("mov.u32 %0, %%lanemask_lt;" : "=r"(m));
    return m;
}

// Zeroes the parts of the scratch that must start clean. Call once per block
// before any other function here.
__device__ __forceinline__ void blockRadixInit(BlockRadixScratch* sh)
{
    for (int i = threadIdx.x; i < BR_WARPS * BR_BINS; i += BR_BLOCK)
        (&sh->warpCnt[0][0])[i] = 0;
    __syncthreads();
}

// Exclusive prefix sum of sh->tileTot into sh->tileStart, over BR_BINS entries.
//
// Done in parallel rather than serially on one thread: a tile needs one of
// these, there are thousands of tiles per hash, and 256 serial shared accesses
// per tile would cost more than the scatter it exists to speed up.
__device__ __forceinline__ void scanBins(BlockRadixScratch* sh)
{
    const int tid  = threadIdx.x;
    const int lane = tid & 31;
    const int warp = tid >> 5;
    const int nw   = (BR_BINS + 31) / 32;   // warps' worth of bins

    int v = 0;
    if (tid < BR_BINS) {
        v = sh->tileTot[tid];
        for (int off = 1; off < 32; off <<= 1) {   // inclusive scan in the warp
            const int y = __shfl_up_sync(0xffffffffu, v, off);
            if (lane >= off) v += y;
        }
        if (lane == 31) sh->warpScan[warp] = v;
    }
    __syncthreads();

    if (tid == 0) {
        int s = 0;
        for (int w = 0; w < nw; w++) {
            const int t = sh->warpScan[w];
            sh->warpScan[w] = s;
            s += t;
        }
    }
    __syncthreads();

    // exclusive = this warp's offset + inclusive - own value
    if (tid < BR_BINS) sh->tileStart[tid] = sh->warpScan[warp] + v - sh->tileTot[tid];
    __syncthreads();
}

// Builds every pass's histogram in one traversal of the keys.
//
// A pass only permutes the keys, so the multiset -- and therefore every digit
// histogram -- is the same before and after it.
__device__ void blockRadixHistograms(const uint64_t* k, int n, int passes,
                                     int lowBit, BlockRadixScratch* sh)
{
    PROF_DECL;
    PROF_MARK();
    for (int i = threadIdx.x; i < passes * BR_BINS; i += BR_BLOCK)
        (&sh->hist[0][0])[i] = 0;
    __syncthreads();

    for (int i = threadIdx.x; i < n; i += BR_BLOCK) {
        const uint64_t w = k[i];
        for (int p = 0; p < passes; p++)
            atomicAdd(&sh->hist[p][(int)((w >> (lowBit + p * BR_BITS)) & (BR_BINS - 1))], 1);
    }
    __syncthreads();
    PROF_ADD(PH_HIST);
}

// One stable LSD pass over n words, moving them from in to out ordered by bits
// [lowBit + pass*BR_BITS, +BR_BITS). blockRadixHistograms must have been called
// for these words with the same lowBit and at least pass+1 passes.
__device__ void blockRadixPass(const uint64_t* in, uint64_t* out,
                               int n, int pass, int lowBit, BlockRadixScratch* sh)
{
    const int tid   = threadIdx.x;
    const int warp  = tid >> 5;
    const int shift = lowBit + pass * BR_BITS;

    PROF_DECL;
    PROF_MARK();

    // ---- 1. turn this pass's histogram into first-slot offsets
    //
    // A serial scan of 256 bins on one thread. It happens once per pass, not
    // once per tile, so a parallel scan would buy nothing and cost a barrier.
    if (tid == 0) {
        int sum = 0;
        for (int i = 0; i < BR_BINS; i++) {
            const int c = sh->hist[pass][i];
            sh->digitOff[i] = sum;
            sum += c;
        }
    }
    __syncthreads();
    PROF_ADD(PH_OFFSCAN);

    // ---- 2. scatter, one tile at a time so the output stays stable
    for (int base = 0; base < n; base += BR_BLOCK) {
        PROF_COUNT(g_tiles);
        PROF_MARK();
        const int i = base + tid;
        const bool live = i < n;
        const int tileN = (n - base) < BR_BLOCK ? (n - base) : BR_BLOCK;

        uint64_t w = 0;
        int      d = -1;                    // dead lanes group together on -1
        if (live) {
            w = in[i];
            d = (int)((w >> shift) & (BR_BINS - 1));
        }

        // Lanes of this warp sharing my digit, and my rank among them.
        //
        // This phase is 30% of the whole suffix sort, and __match_any_sync
        // looked like the reason: it resolves an arbitrary number of distinct
        // values, and with 128 bins over 32 lanes almost every lane is its own
        // group. Replacing it with one __ballot_sync per digit bit -- eight
        // fixed-cost instructions computing the same mask -- moved the phase
        // from 30.4% to 28.8% and the sort from 11,939 to 12,089 arrays a
        // second. Inside the noise, for eight lines instead of one.
        //
        // Which says the phase is the global load above it and the barrier
        // below, not the match. That is the irreducible read of a radix pass:
        // one word per element per pass. Fewer passes is the only lever on it,
        // and 9-bit digits (four passes instead of five) measured flat too.
        const unsigned same = __match_any_sync(0xffffffffu, d);
        const int rankInWarp = __popc(same & laneMaskLt());
        const int isLeader   = (rankInWarp == 0);
        const int warpCount  = __popc(same);
        const int leaderLane = __ffs(same) - 1;

        if (live && isLeader) sh->warpCnt[warp][d] = warpCount;
        __syncthreads();
        PROF_ADD(PH_RANK);

        // Column-wise exclusive scan of the warp x digit matrix, one thread per
        // digit, so each (warp, digit) cell ends up holding the count of that
        // digit in all lower warps -- which is exactly the offset a leader needs.
        // The digit's total for the tile falls out as the running sum.
        //
        // This replaces every leader walking all BR_WARPS rows of its own
        // column. With 128 digits and 32 warps most threads are a leader, so
        // that was around 900 x 32 shared reads per tile against 32 x 128 here,
        // and the same answer. Cycle attribution puts the phase at 16.5% of the
        // suffix sort before and 5.8% after, counting the scan itself.
        //
        // Writing back only where the count is non-zero is what keeps the
        // matrix self-cleaning: a cell is non-zero only if a leader wrote it,
        // and that same leader zeroes it again in step 5, so cells no digit
        // landed in are never touched and stay zero for the next tile. Zeroing
        // the whole matrix per tile instead would cost more than this saves.
        //
        // Bank-conflict free: cell (w, b) is at word w * BR_BINS + b, and
        // BR_BINS is a multiple of 32, so the bank is b % 32 -- consecutive
        // threads take consecutive banks.
#if BR_FUSED_BINSCAN
        // Both scans on one warp, in one pass over the bins, with no barrier
        // between them. See BR_FUSED_BINSCAN.
        if (warp == 0) {
            const int lane = tid & 31;
            int carry = 0;
#pragma unroll
            for (int s = 0; s < BR_BINS / 32; s++) {
                const int b = s * 32 + lane;
                int run = 0;
                for (int w = 0; w < BR_WARPS; w++) {
                    const int c = sh->warpCnt[w][b];
                    if (c) sh->warpCnt[w][b] = run;
                    run += c;
                }
                sh->tileTot[b] = run;

                int inc = run;                     // inclusive scan of run
                for (int off = 1; off < 32; off <<= 1) {
                    const int y = __shfl_up_sync(0xffffffffu, inc, off);
                    if (lane >= off) inc += y;
                }
                sh->tileStart[b] = carry + inc - run;
                carry += __shfl_sync(0xffffffffu, inc, 31);
            }
        }
        __syncthreads();
        PROF_ADD(PH_LEADER);

        // One read each now; the barrier above covers warpCnt, tileTot and
        // tileStart together.
        int preInTile = 0;
        if (live && isLeader) preInTile = sh->warpCnt[warp][d];
        preInTile = __shfl_sync(0xffffffffu, preInTile, leaderLane);
#else
        for (int b = tid; b < BR_BINS; b += BR_BLOCK) {
            int run = 0;
            for (int w = 0; w < BR_WARPS; w++) {
                const int c = sh->warpCnt[w][b];
                if (c) sh->warpCnt[w][b] = run;
                run += c;
            }
            sh->tileTot[b] = run;
        }
        __syncthreads();
        PROF_ADD(PH_LEADER);

        // One read each now. The barrier above also covers tileTot for
        // scanBins, which is why there is no second one here.
        int preInTile = 0;
        if (live && isLeader) preInTile = sh->warpCnt[warp][d];
        preInTile = __shfl_sync(0xffffffffu, preInTile, leaderLane);

        // ---- 3. stage the tile in digit order
        scanBins(sh);
        PROF_ADD(PH_SCANBINS);
#endif

        if (live) sh->stageWord[sh->tileStart[d] + preInTile + rankInWarp] = w;
        __syncthreads();
        PROF_ADD(PH_STAGE);

        // ---- 4. copy out. Consecutive x share a digit for the length of that
        // digit's run, and those destinations are consecutive too, so a warp
        // writes a handful of contiguous stretches instead of 32 stray words.
        for (int x = tid; x < tileN; x += BR_BLOCK) {
            const uint64_t sw = sh->stageWord[x];
            const int sd = (int)((sw >> shift) & (BR_BINS - 1));
            out[sh->digitOff[sd] + (x - sh->tileStart[sd])] = sw;
        }
        __syncthreads();
        PROF_ADD(PH_COPYOUT);

        // ---- 5. advance the cursors and put the matrix back to zero
        for (int x = tid; x < BR_BINS; x += BR_BLOCK) sh->digitOff[x] += sh->tileTot[x];
        if (live && isLeader) sh->warpCnt[warp][d] = 0;
        __syncthreads();
        PROF_ADD(PH_CURSOR);
    }
}

// Sorts n pairs by the low `bits` bits of the key. The buffers are ping-ponged,
// so the result may end up in either pair; the returned pointer says which.
// bits is rounded up to a whole number of BR_BITS-wide passes.
// Sorts n words by the `bits` bits starting at lowBit, leaving the bits below
// lowBit alone -- which is what lets a caller carry a payload in them. The two
// buffers are ping-ponged, so the result may end up in either; the returned
// pointer says which.
//
// Sorting a word rather than a (key, value) pair is worth doing whenever the
// payload fits underneath the key. Here it does: the suffix sort's key is two
// 17-bit ranks and its payload is a 17-bit index, 51 bits together, so the pass
// moves 8 bytes per element instead of 12.
__device__ uint64_t* blockRadixSort(uint64_t* a, uint64_t* b,
                                    int n, int lowBit, int bits,
                                    BlockRadixScratch* sh)
{
    const int passes = (bits + BR_BITS - 1) / BR_BITS;
    blockRadixHistograms(a, n, passes, lowBit, sh);

    for (int pass = 0; pass < passes; pass++) {
        blockRadixPass(a, b, n, pass, lowBit, sh);
        uint64_t* t = a; a = b; b = t;
    }
    return a;
}

// Inclusive scan of 0/1 flags over n elements, in place, block-wide. Returns
// the total. Tiles are processed in order with a running carry, which is both
// simpler and cheaper here than a two-level scan.
//
// The cross-warp step is a serial loop over BR_WARPS on one thread, and it looks
// like it should hurt: 32 dependent steps per tile, tens of tiles per scan,
// thousands of scans per hash. Replacing it with a single-warp shuffle scan --
// here and in blockScanMax, which together measured 14.7% of the suffix sort --
// changed the hashrate by nothing: 7.70 KH/s against 7.73. The 32 shared loads
// are to different addresses, so they pipeline, and only the accumulate is a
// chain. Left as it is, being the shorter code.
__device__ int blockScanFlags(int32_t* f, int n, BlockRadixScratch* sh)
{
    const int tid  = threadIdx.x;
    const int warp = tid >> 5;
    const int lane = tid & 31;

    if (tid == 0) sh->scanCarry = 0;
    __syncthreads();

    for (int base = 0; base < n; base += BR_BLOCK) {
        const int i = base + tid;
        int x = (i < n) ? f[i] : 0;

        for (int off = 1; off < 32; off <<= 1) {
            const int y = __shfl_up_sync(0xffffffffu, x, off);
            if (lane >= off) x += y;
        }
        if (lane == 31) sh->warpScan[warp] = x;
        __syncthreads();

        // exclusive scan of the per-warp totals, on one thread: BR_WARPS values
        if (tid == 0) {
            int s = sh->scanCarry;
            for (int w = 0; w < BR_WARPS; w++) {
                const int t = sh->warpScan[w];
                sh->warpScan[w] = s;
                s += t;
            }
            sh->scanCarry = s;
        }
        __syncthreads();

        if (i < n) f[i] = x + sh->warpScan[warp];
        __syncthreads();
    }
    return sh->scanCarry;
}

// Running inclusive maximum over n elements, in place, block-wide. Used to turn
// "this position starts a new group" markers into "the position my group starts
// at", which is the rank prefix doubling needs.
__device__ void blockScanMax(int32_t* a, int n, BlockRadixScratch* sh)
{
    const int tid  = threadIdx.x;
    const int warp = tid >> 5;
    const int lane = tid & 31;

    if (tid == 0) sh->scanCarry = -1;
    __syncthreads();

    for (int base = 0; base < n; base += BR_BLOCK) {
        const int i = base + tid;
        int x = (i < n) ? a[i] : -1;

        for (int off = 1; off < 32; off <<= 1) {
            const int y = __shfl_up_sync(0xffffffffu, x, off);
            if (lane >= off && y > x) x = y;
        }
        if (lane == 31) sh->warpScan[warp] = x;
        __syncthreads();

        if (tid == 0) {
            int m = sh->scanCarry;
            for (int w = 0; w < BR_WARPS; w++) {
                const int t = sh->warpScan[w];
                sh->warpScan[w] = m;
                if (t > m) m = t;
            }
            sh->scanCarry = m;
        }
        __syncthreads();

        if (i < n) a[i] = x > sh->warpScan[warp] ? x : sh->warpScan[warp];
        __syncthreads();
    }
}
