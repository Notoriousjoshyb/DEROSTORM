// derostorm_gpu.cu -- the mining kernels and the C API around them.
//
// A hash has three stages and they want three different mappings, so this runs
// three kernels rather than one:
//
//   1. stage 1   thread per hash. A 256-byte state machine with a 256-way
//                branch: nothing in it to spread across a block. With its state
//                in shared memory it runs at ~340,000 hashes a second, which is
//                about 1% of the total.
//   2. suffix    block per hash, prefix doubling (sa_doubling.cuh). This is
//                essentially the whole cost -- 97% of GPU hash time -- and the
//                one stage with enough parallelism to fill a block and enough
//                memory traffic to care about coalescing.
//   3. SHA-256   thread per hash, fused with the difficulty check so only
//                winning nonces come back. Serial by construction, but there
//                are thousands of independent ones.
//
// Measured on an RTX 5080: 6,766 H/s, against 3,961 for the same hash with a
// thread-per-hash SA-IS suffix sort. Both are exact -- gpu/hash_test.exe and
// gpu/hash_parallel_test.exe check each against 512 real CPU vectors.
//
// The batch is processed in chunks. The suffix kernel's blocks loop over a
// chunk and reuse their scratch, so resident blocks and batch size are
// independent; what scales with the chunk is the text and suffix array held
// between kernels, at ~346 KB per hash.
//
// The chunk is as large as the batch whenever VRAM allows, which is the setting
// that measured fastest by a wide margin -- 6.88 KH/s against 6.37 at half that
// and 5.69 at a quarter. The reason is the drain at the end of each suffix
// launch: the last blocks to claim work finish alone, and that tail is paid once
// per launch, so fewer and larger launches lose less to it.
//
// A default of 8,192 used to cap that, and it was the wrong default: stage 1
// and SHA-256 are one thread per hash, so 8,192 threads leave the SMs half
// empty, and measured on a 5080 the same kernels at 20,032 hashes (what this
// card's VRAM actually holds) read 82.5 KH/s against 76.4 at 8,192. batch=0
// now sizes from free VRAM, capped at DSG_BATCH_MAX so a 80 GB card does not
// sit on a two-second job. An explicit --gpu-batch still wins if you want the
// old latency. The suffix-block peak on that 5080 was 336, not 1252; pin
// --gpu-blocks=336 there. Job latency is about 350 ms at 30,016 hashes.
//
// Overlapping two chunks on two streams was tried, to hide the stage-1 and SHA
// kernels behind the suffix kernel. It does not pay. Those two are 3% of GPU
// time together, and buying the overlap means halving the chunk to fit two sets
// of inter-kernel storage, which costs more than 3%: measured 6.48 KH/s against
// 6.88 for one chunk of the same total size. The suffix kernels cannot overlap
// each other anyway -- they share one scratch pool indexed by blockIdx.
//
// The resident block count is tuned at run time rather than fixed here. It used
// to be capped at half a block per SM, on the reasoning that a bandwidth-bound
// kernel gains nothing from more. Measured on an RTX 5080 that cap cost 16%:
// 6151 H/s at 42 blocks against 7084 at 84 and 7114 at 336. The right number is
// a property of the card, so dsg_set_blocks lets the caller sweep for it.

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include "gpuapi.cuh"

#include "stage1.cuh"
#include "sa_doubling.cuh"
#include "desc.cuh"
#include "derostorm_gpu.h"

// Shared tile for stage 1: step_3 and the RC4 permutation, 256 bytes each per
// thread, with 4 bytes of padding so lanes land on different shared banks. A
// bank is picked by (byte address / 4) % 32, so a flat 512-byte stride would
// put every lane of a warp on one bank and serialise it 32 ways.
#define S1_STRIDE 516
// Threads a stage-1 block carries. It does not change how many stage-1 threads
// an SM can hold -- 516 bytes of shared memory a thread pins that at ~193
// whatever the block size -- but it does change the granularity the scheduler
// has to work with when stage 1 is sharing an SM with the sort.
#ifndef S1_BLOCK
#define S1_BLOCK  64
#endif

// Nonces per dsg_search call when the caller does not say. This is the job
// latency knob: the miner cannot notice a new job until a batch returns, and a
// batch is a bit under a second on a current card. The chunk is derived from it,
// so this also decides the VRAM footprint of the inter-kernel storage at
// ~346 KB per hash.
#ifndef DSG_BATCH
#define DSG_BATCH 8192
#endif
#ifndef DSG_BATCH_MAX
#define DSG_BATCH_MAX 32768
#endif

// Batches that may be in flight at once. Two is the whole point: one running on
// the card, one queued behind it. See dsg_slot.
#define DSG_SLOTS 2

// Sets of inter-kernel storage, and with them how much of the batch can be in
// the air at once.
//
// One bank is the obvious arrangement and it leaves the card doing one thing at
// a time: stage 1, then the suffix sort, then the SHA check, each waiting for
// the last because they all share one set of texts and suffix arrays. Measured
// on a 5080 that is 14.7% of GPU time in stage 1 and 7.5% in the SHA check, both
// of which the suffix sort could have been running underneath.
//
// It can. gpu/overlap.cu puts stage 1 and the suffix sort on two streams over
// separate storage and times them: 80% of stage 1 disappears into the sort, and
// 80% of the SHA check with it. They are limited by different things -- stage 1
// by 516 bytes of shared memory a thread, the sort by memory latency, the SHA
// check by bandwidth -- so an SM hosting two of them is not paying twice.
//
// Two banks cost a chunk half the size, because the storage is the whole VRAM
// budget either way, and they cost the sort ~6.5% of its own elapsed time for
// hosting stage 1. What is left after both is real: 123,265 -> 127,532 H/s on a
// 5080, +3.5%, and +2.1% on the real mining path with the CPU beside it. See
// DSG_OVERLAP_SHA for the half of this that does not pay.
//
// Build with -DDSG_BANKS=1 for the old one-thing-at-a-time shape.
#ifndef DSG_BANKS
#define DSG_BANKS 2
#endif

// Whether the SHA check is allowed to run under the next chunk's sort as well.
//
// Stage 1 and the SHA check look alike from a distance -- both are short kernels
// sitting either side of the sort -- and they behave nothing alike underneath
// it. Stage 1 waits on shared memory and hides almost for free. The SHA check
// reads back the suffix arrays the sort has just written, so it fights the sort
// for the one thing the sort is short of, and both lose: measured with it
// allowed, its own elapsed time nearly trebled and the batch came out slower
// than with one bank at all.
//
//   one bank                              123,265 H/s
//   two banks, SHA overlapping too        116,116
//   two banks, SHA kept out of the way    127,532
//
// So the sort's turn is released after the SHA check rather than before it.
// Stage 1 still overlaps; the SHA check no longer does.
#ifndef DSG_OVERLAP_SHA
#define DSG_OVERLAP_SHA 0
#endif

// Winning nonces one batch can report. A share is rare enough that this is
// never approached; it only bounds the buffer.
#define DSG_MAX_HITS 64

// One batch's private state. Two of these hang off a context, so one batch can
// be enqueued while the previous one is still on the card. Everything a batch
// needs that is not shared scratch lives here: the uploaded work and target,
// the winning nonces, and the event that says the readback is done.
//
// The shared scratch (texts, lens, sa, next) needs no second copy. Both batches
// ride the same stream, so batch N's last kernel has finished before batch
// N+1's first one starts and they never touch it at the same time. What the
// second slot buys is not overlap on the card; it is that the card always has
// the next batch queued behind the one it is running, so it never sits idle
// while the host wakes up and re-enqueues.
struct dsg_slot {
    uint8_t*  dWork;
    uint64_t* dTarget;
    uint32_t* dNonces;
    int32_t*  dFound;

    // Page-locked, so the uploads and the readback are real DMA and the host
    // never blocks inside a copy.
    uint8_t*  hWork;
    uint64_t* hTarget;
    uint32_t* hNonces;
    int32_t*  hFound;

    cudaEvent_t done;
};

struct dsg_context {
    int   device;
    int   batch;        // nonces per dsg_search call
    int   blocks;       // resident suffix-kernel blocks, in use now
    int   maxBlocks;    // resident suffix-kernel blocks allocated for
    int   chunk;        // hashes in flight between kernels, per slot
    int   sms;          // SMs on the device, for the caller's tuning sweep
    char  name[256];

    int32_t*  words;    // suffix scratch: 8 int32 arrays per block
    uint64_t* keys;     // suffix scratch: 2 uint64 arrays per block

    // One bank per set of inter-kernel storage. A chunk owns a bank for its
    // whole life -- stage 1 writes its texts, the sort reads them and writes its
    // suffix arrays, the SHA check reads those -- so consecutive chunks on
    // different banks do not have to wait for each other.
    struct {
        uint8_t*  texts;   // chunk * ASTRO_MAX_TEXT
        int32_t*  lens;    // chunk
        int32_t*  sa;      // chunk * ASTRO_MAX_TEXT
        int32_t*  next;    // the suffix kernel's work counter, one int

        // Not the legacy default stream, so the uploads and the kernels are
        // ordered against each other and against nothing else. One per bank is
        // what lets two chunks be in the air; the ordering a chunk needs
        // against its own bank's previous chunk is the stream's own.
        cudaStream_t stream;

        // Recorded after this bank's suffix launch. The other bank waits on it
        // before its own: the sort's scratch pool is indexed by blockIdx and
        // shared by every block, so two suffix kernels must never overlap even
        // though everything around them may.
        cudaEvent_t  sorted;
        cudaEvent_t  drained;   // after this bank's last kernel of a batch
    } bank[DSG_BANKS];

    int nextBank;       // bank the next chunk takes

    uint8_t*  dHashOne;

    dsg_slot slot[DSG_SLOTS];

    int head;       // slot the next submit fills
    int tail;       // slot the next collect waits on
    int inflight;   // batches submitted and not yet collected
};

// Per-thread so two GPU workers cannot overwrite each other's message.
static thread_local char g_err[512];

static void setErr(const char* what, cudaError_t e)
{
    snprintf(g_err, sizeof(g_err), "%s: %s", what, cudaGetErrorString(e));
}

// Copies into the caller's buffer; see the note in the header.
static int copyOut(char* buf, int len, const char* src)
{
    if (!buf || len <= 0) return DSG_ERR_STATE;
    snprintf(buf, (size_t)len, "%s", src ? src : "");
    return DSG_OK;
}

extern "C" DSG_API int dsg_error(char* buf, int len)
{
    return copyOut(buf, len, g_err);
}

// ---------------------------------------------------------------------------
// device helpers
// ---------------------------------------------------------------------------

// The four-limb compare from cmd/derostorm/target.go. The PoW hash is read as
// a 256-bit little-endian integer, so limb 3 covers h[24:32].
__device__ __forceinline__ bool meetsTarget(const uint8_t h[32], const uint64_t t[4])
{
    uint64_t v = ld64le(h + 24);
    if (v != t[3]) return v < t[3];
    v = ld64le(h + 16);
    if (v != t[2]) return v < t[2];
    v = ld64le(h + 8);
    if (v != t[1]) return v < t[1];
    return ld64le(h) <= t[0];
}

// Builds one 48-byte miniblock. Bytes 43..46 carry the nonce big endian,
// matching binary.BigEndian.PutUint32(work[43:47], i) on the CPU side.
__device__ __forceinline__ void buildWork(uint8_t out[DSG_WORK_SIZE],
                                          const uint8_t* base, uint32_t nonce)
{
    for (int i = 0; i < DSG_WORK_SIZE; i++) out[i] = base[i];
    out[43] = (uint8_t)(nonce >> 24);
    out[44] = (uint8_t)(nonce >> 16);
    out[45] = (uint8_t)(nonce >> 8);
    out[46] = (uint8_t)(nonce);
}

struct Pool {
    int32_t*  words;
    uint64_t* keys;

    // The descriptor sort's scratch is carved out of the same allocation as the
    // doubling sort's, because only one of them runs for any given hash: the
    // descriptor goes first and the doubling is the fallback, by which point
    // everything the descriptor put here is finished with.
    //
    // Doubling wants six int32 and two uint64 a symbol; the descriptor wants
    // three int32 and two uint64. So the descriptor fits inside the doubling
    // footprint with room over, and adding it costs no VRAM at all.
    __device__ DescScratch descMine(int b) const {
        const size_t w = (size_t)b * ASTRO_MAX_TEXT * 6;
        const size_t k = (size_t)b * ASTRO_MAX_TEXT * 2;
        DescScratch s;
        s.words  = keys + k + (size_t)ASTRO_MAX_TEXT * 0;
        s.words2 = keys + k + (size_t)ASTRO_MAX_TEXT * 1;
        s.arena  = (uint16_t*)(words + w + (size_t)ASTRO_MAX_TEXT * 0);
        s.offs   = words + w + (size_t)ASTRO_MAX_TEXT * 1;
        s.mbuf   = words + w + (size_t)ASTRO_MAX_TEXT * 2;
        return s;
    }

    __device__ SADoublingScratch mine(int b) const {
        const size_t w = (size_t)b * ASTRO_MAX_TEXT * 6;
        const size_t k = (size_t)b * ASTRO_MAX_TEXT * 2;
        SADoublingScratch s;
        s.sa    = words + w + (size_t)ASTRO_MAX_TEXT * 0;
        s.rank  = words + w + (size_t)ASTRO_MAX_TEXT * 1;
        s.tmp   = words + w + (size_t)ASTRO_MAX_TEXT * 2;
        s.tmp2  = words + w + (size_t)ASTRO_MAX_TEXT * 3;
        s.act   = words + w + (size_t)ASTRO_MAX_TEXT * 4;
        s.act2  = words + w + (size_t)ASTRO_MAX_TEXT * 5;
        s.wordA = keys + k + (size_t)ASTRO_MAX_TEXT * 0;
        s.wordB = keys + k + (size_t)ASTRO_MAX_TEXT * 1;
        return s;
    }
};

// ---------------------------------------------------------------------------
// kernels
// ---------------------------------------------------------------------------

__global__ void stage1_kernel(const uint8_t* work, uint32_t nonceBase, int count,
                              uint8_t* texts, int32_t* lens)
{
    const int tid = blockIdx.x * blockDim.x + threadIdx.x;
    if (tid >= count) return;

    extern __shared__ uint8_t tile[];
    uint8_t* mine = tile + (size_t)threadIdx.x * S1_STRIDE;

    uint8_t w[DSG_WORK_SIZE];
    buildWork(w, work, nonceBase + (uint32_t)tid);

    lens[tid] = (int32_t)astroStage1(w, DSG_WORK_SIZE,
                                     texts + (size_t)tid * ASTRO_MAX_TEXT,
                                     mine, mine + 256);
}

// The suffix kernel takes its work from a shared counter rather than from a
// blockIdx-strided loop.
//
// Two things make static striding a poor fit. Texts differ in length by up to a
// few percent, and prefix doubling costs more rounds on a text with a long
// common prefix, so blocks do not finish together. And unless the chunk divides
// evenly by the grid, some blocks get one more hash than the rest -- with 336
// blocks over a 2,048-hash chunk that is seven hashes against six, so the whole
// kernel waits on a 15% tail. Taking the next index from an atomic instead
// makes throughput independent of both, and of the chunk size.
//
// Measured against the strided loop it replaced, at two chunk sizes, best of
// two interleaved runs each: 6.87 against 6.56 KH/s at a chunk of 8,192 and
// 6.34 against 6.22 at 2,048. Worth about 4%, and worth more the smaller the
// chunk a card's VRAM forces.
//
// One atomicAdd per hash, by one thread of the block, against tens of
// milliseconds of work per hash: the counter is not a bottleneck.
__global__ __launch_bounds__(BR_BLOCK) void suffix_kernel(
        const uint8_t* texts, const int32_t* lens, int count,
        Pool pool, int32_t* saOut, int32_t* nextHash)
{
    // Dynamic, not static: the scratch is ~54 KB at 1,024 threads and static
    // shared memory is capped at 48 KB on every architecture.
    extern __shared__ char smem[];
    BlockRadixScratch* sh = (BlockRadixScratch*)smem;

    // The claimed index has to reach the whole block, and the block-wide
    // barrier that publishes it is the same one that keeps the previous hash's
    // readers out of the way.
    __shared__ int claimed;

    SADoublingScratch sc = pool.mine(blockIdx.x);
    DescScratch dsc = pool.descMine(blockIdx.x);

    for (;;) {
        if (threadIdx.x == 0) claimed = atomicAdd(nextHash, 1);
        __syncthreads();
        const int h = claimed;
        if (h >= count) return;

        const int n = lens[h];
        const uint8_t* text = texts + (size_t)h * ASTRO_MAX_TEXT;
        int32_t* out = saOut + (size_t)h * ASTRO_MAX_TEXT;

#if DESC_PREFETCH_TEXT
#if DESC_PREFETCH_L2 && DSG_HAVE_L2_PREFETCH
        for (int i = (int)threadIdx.x * DESC_PREFETCH_STRIDE; i < n; i += BR_BLOCK * DESC_PREFETCH_STRIDE) {
            asm volatile("prefetch.global.L2 [%0];" :: "l"(text + i));
        }
#else
        for (int i = threadIdx.x; i < n; i += BR_BLOCK) {
            (void)*(((const volatile uint8_t*)text) + i);
        }
#endif
        __syncthreads();
#endif

        // The descriptor sort first. It knows how stage 1 built this text --
        // whole 256-byte states written out one after another, at most 32 bytes
        // changing between them -- and skips the columns that did not change.
        // Measured against the prefix doubling below, over the 512 real texts in
        // gpu/vectors.bin: 23,821 suffix arrays a second against 12,356.
        //
        // It writes straight into saOut, using it as its own merge scratch on
        // the way, so there is no copy-out on this path.
        const int rc = descSuffixArrayBlock(text, n, out, dsc, sh);
        if (rc == 0) {
            __syncthreads();
            continue;
        }

        // Prefix doubling is the fallback, and it is a real one: it shares the
        // scratch, needs nothing the descriptor sort has not finished with, and
        // knows nothing about the text. The descriptor sort declines only if a
        // key group needs more boundary words than the scratch holds, which no
        // measured text comes close to -- but "no measured text" is not "no
        // text", and a hash is not allowed to be wrong.
        saDoublingBlock(text, n, sc, sh);
        for (int i = threadIdx.x; i < n; i += BR_BLOCK) out[i] = sc.sa[i];
        __syncthreads();
    }
}

// SHA-256 of the suffix array bytes, then the difficulty check. Fusing them
// means the only thing crossing PCIe per batch is the handful of nonces that
// won, rather than a 32-byte hash for every attempt.
__global__ void sha_check_kernel(const int32_t* sa, const int32_t* lens, int count,
                                 uint32_t nonceBase, const uint64_t* target,
                                 int targetAll, uint32_t* nonces, int maxNonces,
                                 int32_t* found)
{
    const int tid = blockIdx.x * blockDim.x + threadIdx.x;
    if (tid >= count) return;

    uint8_t h[32];
    // Go hashes the raw little-endian int32 suffix array; the GPU is little
    // endian too, so it is already in that layout.
    sha256(h, (const uint8_t*)(sa + (size_t)tid * ASTRO_MAX_TEXT), lens[tid] * 4);

    if (targetAll || meetsTarget(h, target)) {
        const int slot = atomicAdd(found, 1);
        if (slot < maxNonces) nonces[slot] = nonceBase + (uint32_t)tid;
    }
}

// Single hash, for proving the device against the CPU at start-up.
__global__ void sha_one_kernel(const int32_t* sa, const int32_t* lens, uint8_t* out)
{
    sha256(out, (const uint8_t*)sa, lens[0] * 4);
}

// ---------------------------------------------------------------------------
// C API
// ---------------------------------------------------------------------------

extern "C" DSG_API int dsg_device_count(void)
{
    int n = 0;
    if (cudaGetDeviceCount(&n) != cudaSuccess) return 0;
    return n;
}

extern "C" DSG_API int dsg_device_name(dsg_context* ctx, char* buf, int len)
{
    return copyOut(buf, len, ctx ? ctx->name : "");
}

// Deliberately opens nothing: cudaGetDeviceProperties does not create a context
// or reserve memory, so the setup wizard can name the card without a multi
// gigabyte allocation appearing while the user is still answering questions.
extern "C" DSG_API int dsg_device_info(int device, char* buf, int len)
{
    if (!buf || len <= 0) return DSG_ERR_STATE;
    buf[0] = 0;

    int n = 0;
    cudaError_t e = cudaGetDeviceCount(&n);
    if (e != cudaSuccess || device < 0 || device >= n) {
        setErr("no such device", e);
        return DSG_ERR_NO_DEVICE;
    }

    cudaDeviceProp prop;
    if ((e = cudaGetDeviceProperties(&prop, device)) != cudaSuccess) {
        setErr("device properties", e);
        return DSG_ERR_NO_DEVICE;
    }
    snprintf(buf, (size_t)len, "%s, %.0f GB, %d %s",
             prop.name, prop.totalGlobalMem / 1073741824.0,
             prop.multiProcessorCount, DSG_UNIT_PLURAL);
    return DSG_OK;
}

extern "C" DSG_API int dsg_init(int device, int batch, int blocks, dsg_context** out,
                                int* batch_out, int* blocks_out)
{
    if (!out) { snprintf(g_err, sizeof(g_err), "dsg_init: out is NULL"); return DSG_ERR_STATE; }
    *out = NULL;

    int n = 0;
    cudaError_t e = cudaGetDeviceCount(&n);
    if (e != cudaSuccess || n == 0) { setErr("no GPU device", e); return DSG_ERR_NO_DEVICE; }
    if (device < 0 || device >= n) {
        snprintf(g_err, sizeof(g_err), "device %d out of range (%d present)", device, n);
        return DSG_ERR_NO_DEVICE;
    }
    // Block the calling thread while a kernel runs instead of spinning on it.
    //
    // This matters because the GPU is not mining alone: fifteen CPU threads are
    // mining beside it, and the default schedule burns a whole core spinning in
    // cudaDeviceSynchronize for the second or so each batch takes. That is one
    // of sixteen hardware threads doing nothing, which costs more CPU hashrate
    // than the microseconds of extra wake-up latency costs GPU hashrate.
    //
    // It has to happen before the context exists. If one already does -- a
    // second GPU worker on the same device -- the call fails harmlessly and the
    // flag the first worker set still applies, so the error is dropped.
    cudaSetDeviceFlags(cudaDeviceScheduleBlockingSync);

    if ((e = cudaSetDevice(device)) != cudaSuccess) { setErr("select device", e); return DSG_ERR_NO_DEVICE; }

    cudaDeviceProp prop;
    if ((e = cudaGetDeviceProperties(&prop, device)) != cudaSuccess) {
        setErr("device properties", e); return DSG_ERR_NO_DEVICE;
    }

    // Shared memory beyond 48 KB has to be asked for explicitly, and it is a
    // per-function attribute, so this must happen before the first launch.
    //
    // Only asked for when it is actually needed. At the shipped BR_BLOCK=256
    // the scratch is under 7 KB and the call is a no-op on NVIDIA -- but AMD's
    // limit is the 64 KB of LDS a workgroup gets and cannot be raised, so on a
    // card where the request is meaningless it is also refused. Skipping it
    // below the 48 KB line keeps both happy and still fails loudly on a
    // wide-block build that genuinely needs more than a device will give.
    if (BR_SHARED_BYTES > 48 * 1024 &&
        (e = cudaFuncSetAttribute((const void*)suffix_kernel,
                                  cudaFuncAttributeMaxDynamicSharedMemorySize,
                                  BR_SHARED_BYTES)) != cudaSuccess) {
        setErr("shared memory limit", e);
        return DSG_ERR_STATE;
    }
    // A hint, not a requirement. NVIDIA honours it; AMD has no configurable
    // L1/LDS split and answers "not supported", which is not a reason to
    // refuse to mine, so the AMD build only asks.
    e = cudaFuncSetCacheConfig((const void*)suffix_kernel, cudaFuncCachePreferShared);
#if !DSG_HIP
    if (e != cudaSuccess) {
        setErr("cache config", e);
        return DSG_ERR_STATE;
    }
#endif

    dsg_context* c = (dsg_context*)calloc(1, sizeof(dsg_context));
    if (!c) { snprintf(g_err, sizeof(g_err), "out of host memory"); return DSG_ERR_ALLOC; }
    c->device = device;
    c->sms = prop.multiProcessorCount;
#if DSG_HIP
    // gcnArchName is the whole target string, "gfx1100:sramecc-:xnack-" and
    // the like. Only the gfx part names the architecture, so the feature
    // suffixes are cut off.
    char arch[32];
    snprintf(arch, sizeof(arch), "%s", prop.gcnArchName);
    for (char* q = arch; *q; q++) if (*q == ':') { *q = 0; break; }
    snprintf(c->name, sizeof(c->name), "%s (%s, %d CUs)",
             prop.name, arch, prop.multiProcessorCount);
#else
    snprintf(c->name, sizeof(c->name), "%s (sm_%d%d, %d SMs)",
             prop.name, prop.major, prop.minor, prop.multiProcessorCount);
#endif

    const size_t perBlock = (size_t)ASTRO_MAX_TEXT * SAD_BYTES_PER_SYMBOL;
    const size_t perChunkHash = (size_t)ASTRO_MAX_TEXT          // text
                              + (size_t)ASTRO_MAX_TEXT * 4      // suffix array
                              + 4;                              // length

    size_t freeB = 0, totB = 0;
    if ((e = cudaMemGetInfo(&freeB, &totB)) != cudaSuccess) {
        setErr("free memory", e); free(c); return DSG_ERR_ALLOC;
    }
    // Leave a gigabyte and a half for the display and the driver.
    size_t budget = freeB > (size_t)1500e6 ? freeB - (size_t)1500e6 : 0;

    // The blocks are sized first because what they need is bounded -- the
    // measured curve is flat past a couple of blocks per SM -- while the chunk
    // pays for every byte it gets. So the blocks take a quarter of the budget at
    // most and the chunk takes the rest, rather than splitting it down the
    // middle and leaving the chunk short on a small card.
    //
    // The allocation, not the setting: enough that dsg_set_blocks has somewhere
    // to sweep past wherever the knee turns out to be.
    //
    // Four 256-thread blocks fill an SM at 64 registers (the suffix kernel's
    // count, with no spills). That is 4 * SMs = 336 on a 5080, and it is where
    // the measured curve plateaus. Allocating twice full occupancy (1,344)
    // used to let the mining sweep pick 672 or 1,252, both slower than 336
    // under a display, and it spent ~2.5 GB on scratch the extra blocks never
    // paid for. Those gigabytes now go to the batch.
    const int plateau = prop.multiProcessorCount * 2048 / BR_BLOCK / 2;
    int want = blocks > 0 ? blocks : (plateau > 0 ? plateau : 1);
    c->maxBlocks = (int)(budget / 4 / perBlock);
    if (c->maxBlocks > want) c->maxBlocks = want;
    if (c->maxBlocks < 1) {
        snprintf(g_err, sizeof(g_err), "not enough VRAM: need %.1f MB per block",
                 perBlock / 1048576.0);
        free(c);
        return DSG_ERR_ALLOC;
    }
    c->blocks = c->maxBlocks;

    const size_t blockBytes = (size_t)c->maxBlocks * perBlock;
    const size_t forChunk = budget > blockBytes ? budget - blockBytes : 0;

    // batch=0 means fill the chunk budget. 8,192 was a latency guess from
    // when the suffix kernel was 1.1 s a batch; at current speed that leaves
    // stage 1 and SHA occupancy on the table. Cap at DSG_BATCH_MAX so job
    // latency stays under about half a second on a big card.
    if (batch <= 0) {
        int fit = (int)(forChunk / perChunkHash);
        if (fit > DSG_BATCH_MAX) fit = DSG_BATCH_MAX;
        if (fit < DSG_BATCH) fit = DSG_BATCH;
        batch = fit;
    }
    batch -= batch % S1_BLOCK;
    if (batch < S1_BLOCK) batch = S1_BLOCK;
    c->batch = batch;

    // The batch split across the banks, halving until the whole set fits. A
    // card too small for that simply runs more chunks, and the work queue in
    // the suffix kernel is what keeps that from costing much.
    //
    // Note what this does not cost: the banks together hold what one bank held
    // before, because the chunk is divided by the same number. The overlap is
    // paid for in chunk size, not in VRAM.
    c->chunk = c->batch / DSG_BANKS;
    if (c->chunk < S1_BLOCK) c->chunk = S1_BLOCK;
    while (c->chunk > S1_BLOCK &&
           (size_t)DSG_BANKS * c->chunk * perChunkHash > forChunk)
        c->chunk /= 2;

#define ALLOC(p, bytes) do { \
        if ((e = cudaMalloc((void**)&(p), (bytes))) != cudaSuccess) { \
            setErr("device allocation", e); dsg_free(c); return DSG_ERR_ALLOC; } \
    } while (0)

    ALLOC(c->words, (size_t)c->maxBlocks * ASTRO_MAX_TEXT * 6 * 4);
    ALLOC(c->keys,  (size_t)c->maxBlocks * ASTRO_MAX_TEXT * 2 * 8);
    // Eight bytes of tail. descLoadBE32 reads the two aligned words that
    // straddle a text offset, so the last few positions of the last text in the
    // chunk reach up to four bytes past it. They are fetched and never used,
    // but an unmapped fetch is still a fault.
    for (int b = 0; b < DSG_BANKS; b++) {
        ALLOC(c->bank[b].texts, (size_t)c->chunk * ASTRO_MAX_TEXT + 8);
        ALLOC(c->bank[b].lens,  (size_t)c->chunk * 4);
        ALLOC(c->bank[b].sa,    (size_t)c->chunk * ASTRO_MAX_TEXT * 4);
        ALLOC(c->bank[b].next,  sizeof(int32_t));
    }
    ALLOC(c->dHashOne, 32);

    // A few hundred bytes a slot. The second slot is what lets the miner keep
    // the card fed, and it costs nothing measurable against the gigabytes the
    // chunk takes.
    for (int i = 0; i < DSG_SLOTS; i++) {
        ALLOC(c->slot[i].dWork,   DSG_WORK_SIZE);
        ALLOC(c->slot[i].dTarget, 4 * sizeof(uint64_t));
        ALLOC(c->slot[i].dNonces, DSG_MAX_HITS * sizeof(uint32_t));
        ALLOC(c->slot[i].dFound,  sizeof(int32_t));
    }
#undef ALLOC

#define HALLOC(p, bytes) do {         if ((e = cudaHostAlloc((void**)&(p), (bytes), cudaHostAllocDefault)) != cudaSuccess) {             setErr("pinned host allocation", e); dsg_free(c); return DSG_ERR_ALLOC; }     } while (0)

    for (int i = 0; i < DSG_SLOTS; i++) {
        HALLOC(c->slot[i].hWork,   DSG_WORK_SIZE);
        HALLOC(c->slot[i].hTarget, 4 * sizeof(uint64_t));
        HALLOC(c->slot[i].hNonces, DSG_MAX_HITS * sizeof(uint32_t));
        HALLOC(c->slot[i].hFound,  sizeof(int32_t));
    }
#undef HALLOC

    // cudaStreamNonBlocking: these must not be serialised against the legacy
    // default stream, which is where the plain cudaMemcpy calls elsewhere run.
    for (int b = 0; b < DSG_BANKS; b++) {
        if ((e = cudaStreamCreateWithFlags(&c->bank[b].stream,
                                           cudaStreamNonBlocking)) != cudaSuccess) {
            setErr("create stream", e); dsg_free(c); return DSG_ERR_ALLOC;
        }
        if ((e = cudaEventCreateWithFlags(&c->bank[b].sorted,
                                          cudaEventDisableTiming)) != cudaSuccess) {
            setErr("create event", e); dsg_free(c); return DSG_ERR_ALLOC;
        }
        if ((e = cudaEventCreateWithFlags(&c->bank[b].drained,
                                          cudaEventDisableTiming)) != cudaSuccess) {
            setErr("create event", e); dsg_free(c); return DSG_ERR_ALLOC;
        }
        // Recorded once here, on an empty stream, so `sorted` is never waited
        // on before it has been recorded at all. CUDA treats that case as no
        // wait, which is what the first chunk of a run wants; HIP's answer is
        // less clearly documented, and an already-complete event means the same
        // thing to both without depending on either. Costs one record per bank,
        // once.
        if ((e = cudaEventRecord(c->bank[b].sorted, c->bank[b].stream)) != cudaSuccess) {
            setErr("record event", e); dsg_free(c); return DSG_ERR_ALLOC;
        }
    }

    // cudaEventDisableTiming: these events only ever gate a wait. Timing
    // events carry extra bookkeeping this does not read.
    for (int i = 0; i < DSG_SLOTS; i++) {
        if ((e = cudaEventCreateWithFlags(&c->slot[i].done,
                                          cudaEventDisableTiming | cudaEventBlockingSync))
            != cudaSuccess) {
            setErr("create event", e); dsg_free(c); return DSG_ERR_ALLOC;
        }
    }

    *out = c;
    if (batch_out)  *batch_out  = c->batch;
    if (blocks_out) *blocks_out = c->blocks;
    g_err[0] = 0;
    return DSG_OK;
}

// Changes the resident block count without reallocating. Values above what
// dsg_init allocated for are refused: the scratch pool is indexed by blockIdx,
// so a larger grid would run off the end of it.
extern "C" DSG_API int dsg_set_blocks(dsg_context* ctx, int blocks)
{
    if (!ctx) { snprintf(g_err, sizeof(g_err), "dsg_set_blocks: null context"); return DSG_ERR_STATE; }
    if (blocks < 1 || blocks > ctx->maxBlocks) {
        snprintf(g_err, sizeof(g_err), "blocks must be between 1 and %d", ctx->maxBlocks);
        return DSG_ERR_STATE;
    }
    ctx->blocks = blocks;
    return DSG_OK;
}

// Reports what the caller needs to drive a tuning sweep. Any pointer may be
// NULL.
extern "C" DSG_API int dsg_device_shape(dsg_context* ctx, int* sms, int* max_blocks, int* chunk)
{
    if (!ctx) { snprintf(g_err, sizeof(g_err), "dsg_device_shape: null context"); return DSG_ERR_STATE; }
    if (sms)        *sms        = ctx->sms;
    if (max_blocks) *max_blocks = ctx->maxBlocks;
    if (chunk)      *chunk      = ctx->chunk;
    return DSG_OK;
}

extern "C" DSG_API void dsg_free(dsg_context* ctx)
{
    if (!ctx) return;
    cudaSetDevice(ctx->device);
    // Anything still in flight is reading this memory. Wait for it before any
    // of it is freed.
    for (int b = 0; b < DSG_BANKS; b++) {
        if (ctx->bank[b].stream) {
            cudaStreamSynchronize(ctx->bank[b].stream);
            cudaStreamDestroy(ctx->bank[b].stream);
        }
        if (ctx->bank[b].sorted)  cudaEventDestroy(ctx->bank[b].sorted);
        if (ctx->bank[b].drained) cudaEventDestroy(ctx->bank[b].drained);
        cudaFree(ctx->bank[b].texts);
        cudaFree(ctx->bank[b].lens);
        cudaFree(ctx->bank[b].sa);
        cudaFree(ctx->bank[b].next);
    }
    cudaFree(ctx->words);
    cudaFree(ctx->keys);
    cudaFree(ctx->dHashOne);
    for (int i = 0; i < DSG_SLOTS; i++) {
        cudaFree(ctx->slot[i].dWork);
        cudaFree(ctx->slot[i].dTarget);
        cudaFree(ctx->slot[i].dNonces);
        cudaFree(ctx->slot[i].dFound);
        cudaFreeHost(ctx->slot[i].hWork);
        cudaFreeHost(ctx->slot[i].hTarget);
        cudaFreeHost(ctx->slot[i].hNonces);
        cudaFreeHost(ctx->slot[i].hFound);
        if (ctx->slot[i].done) cudaEventDestroy(ctx->slot[i].done);
    }
    free(ctx);
}

// Queues the three kernels for `count` consecutive nonces from base on the next
// bank. Returns as soon as they are enqueued; the caller synchronises at the end
// of the batch.
//
// A bank's own stream orders everything that reuses its storage: this chunk's
// stage 1 cannot start writing the texts until the chunk two back has finished
// reading them, because both are on this stream and there is one chunk between
// them. That is the whole of the same-bank hazard, and it costs no events.
//
// What does need an event is the sort. Its scratch pool is indexed by blockIdx
// and shared by every block, so two suffix kernels must not overlap however
// independent their data is; each bank waits for the other's `sorted` before
// launching its own. Everything else -- this chunk's stage 1 against the last
// chunk's sort, this chunk's SHA check against the next chunk's sort -- is left
// free to run at the same time, and that is where the time comes from.
static cudaError_t runChunk(dsg_context* c, const dsg_slot* sl,
                            uint32_t base, int count, int targetAll, int cap)
{
    Pool pool; pool.words = c->words; pool.keys = c->keys;
    const int b = c->nextBank;
    c->nextBank = (b + 1) % DSG_BANKS;

    cudaStream_t st = c->bank[b].stream;
    cudaError_t e;

    stage1_kernel<<<(count + S1_BLOCK - 1) / S1_BLOCK, S1_BLOCK,
                    S1_BLOCK * S1_STRIDE, st>>>(
        sl->dWork, base, count, c->bank[b].texts, c->bank[b].lens);

    if ((e = cudaMemsetAsync(c->bank[b].next, 0, sizeof(int32_t), st)) != cudaSuccess) return e;

#if DSG_BANKS > 1
    // Wait for whichever bank sorted last. An event that has never been
    // recorded is not a wait, so the first chunk of the run falls straight
    // through.
    for (int o = 0; o < DSG_BANKS; o++) {
        if (o == b) continue;
        if ((e = cudaStreamWaitEvent(st, c->bank[o].sorted, 0)) != cudaSuccess) return e;
    }
#endif

    suffix_kernel<<<c->blocks, BR_BLOCK, BR_SHARED_BYTES, st>>>(
        c->bank[b].texts, c->bank[b].lens, count, pool,
        c->bank[b].sa, c->bank[b].next);

#if DSG_BANKS > 1 && DSG_OVERLAP_SHA
    // Released as soon as the sort is done, so the next bank's sort may start
    // while this one's SHA check is still running.
    if ((e = cudaEventRecord(c->bank[b].sorted, st)) != cudaSuccess) return e;
#endif

    sha_check_kernel<<<(count + 63) / 64, 64, 0, st>>>(
        c->bank[b].sa, c->bank[b].lens, count, base,
        sl->dTarget, targetAll, sl->dNonces, cap, sl->dFound);

#if DSG_BANKS > 1 && !DSG_OVERLAP_SHA
    // Released only after the SHA check, which keeps it out of the next sort's
    // way. Stage 1 still overlaps; the SHA check no longer does.
    if ((e = cudaEventRecord(c->bank[b].sorted, st)) != cudaSuccess) return e;
#endif

    return cudaSuccess;
}

// Queues one batch and returns without waiting for it.
//
// This is the half of dsg_search that decides GPU throughput. The old
// interface enqueued a batch and then blocked until it was done, which left the
// card idle for the whole of the host's wake-up, readback and re-enqueue. On a
// machine whose cores are all busy mining, that wake-up is a scheduler quantum
// rather than microseconds, and it cost double-digit percentages of GPU
// hashrate -- visibly so, since dropping a couple of CPU mining threads made
// the GPU faster.
//
// With submit and collect split, the caller keeps a second batch queued behind
// the running one, so the card starts it the instant the first ends and the
// host's wake-up happens off the critical path.
extern "C" DSG_API int dsg_submit(dsg_context* ctx,
                                  const uint8_t work[DSG_WORK_SIZE],
                                  uint32_t nonce_start,
                                  const uint64_t target[4],
                                  int target_all)
{
    if (!ctx || !work || !target) {
        snprintf(g_err, sizeof(g_err), "dsg_submit: null argument"); return DSG_ERR_STATE;
    }
    if (ctx->inflight >= DSG_SLOTS) {
        snprintf(g_err, sizeof(g_err), "dsg_submit: %d batches already in flight", ctx->inflight);
        return DSG_ERR_STATE;
    }

    cudaError_t e;
    if ((e = cudaSetDevice(ctx->device)) != cudaSuccess) { setErr("select device", e); return DSG_ERR_STATE; }

    dsg_slot* sl = &ctx->slot[ctx->head];

    // The batch's own scalars go up on bank 0's stream and the other banks wait
    // for them, so every chunk sees the work and target this batch was given
    // whichever bank it lands on.
    cudaStream_t lead = ctx->bank[0].stream;

    // Staged through this slot's page-locked buffers. The slot is not in
    // flight -- inflight was checked above -- so nothing on the card is reading
    // them, and a page-locked source is what keeps the upload from bouncing
    // through a driver copy on the calling thread.
    memcpy(sl->hWork, work, DSG_WORK_SIZE);
    memcpy(sl->hTarget, target, 4 * sizeof(uint64_t));

    if ((e = cudaMemcpyAsync(sl->dWork, sl->hWork, DSG_WORK_SIZE,
                             cudaMemcpyHostToDevice, lead)) != cudaSuccess) {
        setErr("upload work", e); return DSG_ERR_LAUNCH;
    }
    if ((e = cudaMemcpyAsync(sl->dTarget, sl->hTarget, 4 * sizeof(uint64_t),
                             cudaMemcpyHostToDevice, lead)) != cudaSuccess) {
        setErr("upload target", e); return DSG_ERR_LAUNCH;
    }
    if ((e = cudaMemsetAsync(sl->dFound, 0, sizeof(int32_t), lead)) != cudaSuccess) {
        setErr("reset counter", e); return DSG_ERR_LAUNCH;
    }
#if DSG_BANKS > 1
    // bank 0's `drained` does double duty here: recorded now, it is what the
    // other banks wait on before touching this batch's work and target.
    if ((e = cudaEventRecord(ctx->bank[0].drained, lead)) != cudaSuccess) {
        setErr("record upload", e); return DSG_ERR_LAUNCH;
    }
    for (int b = 1; b < DSG_BANKS; b++) {
        if ((e = cudaStreamWaitEvent(ctx->bank[b].stream,
                                     ctx->bank[0].drained, 0)) != cudaSuccess) {
            setErr("order upload", e); return DSG_ERR_LAUNCH;
        }
    }
#endif

    for (int done = 0; done < ctx->batch; done += ctx->chunk) {
        const int count = (ctx->batch - done) < ctx->chunk ? (ctx->batch - done) : ctx->chunk;
        if ((e = runChunk(ctx, sl, nonce_start + (uint32_t)done, count,
                          target_all, DSG_MAX_HITS)) != cudaSuccess) {
            setErr("launch", e); return DSG_ERR_LAUNCH;
        }
        if ((e = cudaGetLastError()) != cudaSuccess) { setErr("kernel", e); return DSG_ERR_LAUNCH; }
    }

#if DSG_BANKS > 1
    // Every bank's SHA check adds to this batch's counter, so the readback has
    // to follow all of them, not just the lead bank's.
    for (int b = 1; b < DSG_BANKS; b++) {
        if ((e = cudaEventRecord(ctx->bank[b].drained,
                                 ctx->bank[b].stream)) != cudaSuccess) {
            setErr("record drain", e); return DSG_ERR_LAUNCH;
        }
        if ((e = cudaStreamWaitEvent(lead, ctx->bank[b].drained, 0)) != cudaSuccess) {
            setErr("order drain", e); return DSG_ERR_LAUNCH;
        }
    }
#endif

    // The readback rides the stream too, so by the time the event fires the
    // results are already in host memory and collect only has to read them.
    // The whole nonce buffer comes back rather than just the winners, because
    // the count is not known on the host until it arrives; 256 bytes is
    // cheaper than the extra round trip it would take to find out first.
    if ((e = cudaMemcpyAsync(sl->hFound, sl->dFound, sizeof(int32_t),
                             cudaMemcpyDeviceToHost, lead)) != cudaSuccess) {
        setErr("read counter", e); return DSG_ERR_LAUNCH;
    }
    if ((e = cudaMemcpyAsync(sl->hNonces, sl->dNonces, DSG_MAX_HITS * sizeof(uint32_t),
                             cudaMemcpyDeviceToHost, lead)) != cudaSuccess) {
        setErr("read nonces", e); return DSG_ERR_LAUNCH;
    }
    if ((e = cudaEventRecord(sl->done, lead)) != cudaSuccess) {
        setErr("record event", e); return DSG_ERR_LAUNCH;
    }
    // Deliberately no barrier here. Making the other banks wait for this
    // batch's readback was the obvious safety and it is both unnecessary and
    // expensive: the result buffers belong to the slot, not the bank, and a
    // third batch cannot be submitted until the host has collected the first,
    // so a slot's buffers are always free by the time they come round again.
    // With the barrier in, a bank could not start the next batch until this one
    // had drained completely, which is exactly the serialisation the banks are
    // there to avoid -- measured, it gave the whole change back.

    ctx->head = (ctx->head + 1) % DSG_SLOTS;
    ctx->inflight++;
    g_err[0] = 0;
    return DSG_OK;
}

// Waits for the oldest outstanding batch and reports its winning nonces.
extern "C" DSG_API int dsg_collect(dsg_context* ctx,
                                   uint32_t* nonces, int max_nonces, int* found)
{
    if (!ctx || !found) { snprintf(g_err, sizeof(g_err), "dsg_collect: null argument"); return DSG_ERR_STATE; }
    *found = 0;
    if (ctx->inflight <= 0) {
        snprintf(g_err, sizeof(g_err), "dsg_collect: nothing in flight");
        return DSG_ERR_STATE;
    }

    cudaError_t e;
    dsg_slot* sl = &ctx->slot[ctx->tail];

    // The event is a blocking one, so this parks the thread rather than
    // spinning a core the CPU miners want. The card is not waiting on it: the
    // next batch is already queued behind this one.
    if ((e = cudaEventSynchronize(sl->done)) != cudaSuccess) {
        setErr("kernel", e); return DSG_ERR_LAUNCH;
    }
    if ((e = cudaGetLastError()) != cudaSuccess) { setErr("kernel", e); return DSG_ERR_LAUNCH; }

    ctx->tail = (ctx->tail + 1) % DSG_SLOTS;
    ctx->inflight--;

    int cap = max_nonces < DSG_MAX_HITS ? max_nonces : DSG_MAX_HITS;
    if (cap < 0) cap = 0;
    int32_t nf = *sl->hFound;
    if (nf < 0) nf = 0;
    if (nf > cap) nf = cap;
    if (nf > 0 && nonces) memcpy(nonces, sl->hNonces, (size_t)nf * sizeof(uint32_t));
    *found = nf;
    return DSG_OK;
}

// How many batches are queued and not yet collected.
extern "C" DSG_API int dsg_inflight(dsg_context* ctx)
{
    return ctx ? ctx->inflight : 0;
}

// Submit plus collect: one batch, start to finish. Kept for the benchmark and
// the block-count sweep, which measure one setting at a time and want no second
// batch blurring the timing. Mining uses dsg_submit and dsg_collect.
extern "C" DSG_API int dsg_search(dsg_context* ctx,
                                  const uint8_t work[DSG_WORK_SIZE],
                                  uint32_t nonce_start,
                                  const uint64_t target[4],
                                  int target_all,
                                  uint32_t* nonces, int max_nonces, int* found)
{
    if (!ctx || !found) { snprintf(g_err, sizeof(g_err), "dsg_search: null argument"); return DSG_ERR_STATE; }
    *found = 0;
    int rc = dsg_submit(ctx, work, nonce_start, target, target_all);
    if (rc != DSG_OK) return rc;
    return dsg_collect(ctx, nonces, max_nonces, found);
}

extern "C" DSG_API int dsg_hash_one(dsg_context* ctx,
                                    const uint8_t work[DSG_WORK_SIZE],
                                    uint32_t nonce, uint8_t out[32])
{
    if (!ctx) { snprintf(g_err, sizeof(g_err), "dsg_hash_one: null context"); return DSG_ERR_STATE; }

    cudaError_t e;
    if ((e = cudaSetDevice(ctx->device)) != cudaSuccess) { setErr("select device", e); return DSG_ERR_STATE; }
    if ((e = cudaMemcpy(ctx->slot[0].dWork, work, DSG_WORK_SIZE, cudaMemcpyHostToDevice)) != cudaSuccess) {
        setErr("upload work", e); return DSG_ERR_LAUNCH;
    }

    Pool pool; pool.words = ctx->words; pool.keys = ctx->keys;

    if ((e = cudaMemset(ctx->bank[0].next, 0, sizeof(int32_t))) != cudaSuccess) {
        setErr("reset work counter", e); return DSG_ERR_LAUNCH;
    }
    stage1_kernel<<<1, S1_BLOCK, S1_BLOCK * S1_STRIDE>>>(ctx->slot[0].dWork, nonce, 1,
                                                         ctx->bank[0].texts, ctx->bank[0].lens);
    suffix_kernel<<<1, BR_BLOCK, BR_SHARED_BYTES>>>(ctx->bank[0].texts, ctx->bank[0].lens,
                                                    1, pool, ctx->bank[0].sa, ctx->bank[0].next);
    sha_one_kernel<<<1, 1>>>(ctx->bank[0].sa, ctx->bank[0].lens, ctx->dHashOne);

    if ((e = cudaDeviceSynchronize()) != cudaSuccess) { setErr("kernel", e); return DSG_ERR_LAUNCH; }
    if ((e = cudaGetLastError()) != cudaSuccess) { setErr("kernel", e); return DSG_ERR_LAUNCH; }
    if ((e = cudaMemcpy(out, ctx->dHashOne, 32, cudaMemcpyDeviceToHost)) != cudaSuccess) {
        setErr("read hash", e); return DSG_ERR_LAUNCH;
    }
    return DSG_OK;
}
