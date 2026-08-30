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
#include <cuda_runtime.h>

#include "stage1.cuh"
#include "sa_doubling.cuh"
#include "desc.cuh"
#include "derostorm_gpu.h"

// Shared tile for stage 1: step_3 and the RC4 permutation, 256 bytes each per
// thread, with 4 bytes of padding so lanes land on different shared banks. A
// bank is picked by (byte address / 4) % 32, so a flat 512-byte stride would
// put every lane of a warp on one bank and serialise it 32 ways.
#define S1_STRIDE 516
#define S1_BLOCK  64

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

    uint8_t*  texts;    // chunk * ASTRO_MAX_TEXT
    int32_t*  lens;     // chunk
    int32_t*  sa;       // chunk * ASTRO_MAX_TEXT
    int32_t*  next;     // the suffix kernel's work counter, one int

    // One stream, not the legacy default one, so the uploads and the kernels
    // are ordered against each other and against nothing else.
    cudaStream_t stream;

    uint8_t*  dWork;
    uint64_t* dTarget;
    uint32_t* dNonces;
    int32_t*  dFound;
    uint8_t*  dHashOne;
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
        s.arena  = (uint32_t*)(words + w + (size_t)ASTRO_MAX_TEXT * 0);
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
        setErr("no such CUDA device", e);
        return DSG_ERR_NO_DEVICE;
    }

    cudaDeviceProp prop;
    if ((e = cudaGetDeviceProperties(&prop, device)) != cudaSuccess) {
        setErr("cudaGetDeviceProperties", e);
        return DSG_ERR_NO_DEVICE;
    }
    snprintf(buf, (size_t)len, "%s, %.0f GB, %d SMs",
             prop.name, prop.totalGlobalMem / 1073741824.0, prop.multiProcessorCount);
    return DSG_OK;
}

extern "C" DSG_API int dsg_init(int device, int batch, int blocks, dsg_context** out,
                                int* batch_out, int* blocks_out)
{
    if (!out) { snprintf(g_err, sizeof(g_err), "dsg_init: out is NULL"); return DSG_ERR_STATE; }
    *out = NULL;

    int n = 0;
    cudaError_t e = cudaGetDeviceCount(&n);
    if (e != cudaSuccess || n == 0) { setErr("no CUDA device", e); return DSG_ERR_NO_DEVICE; }
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

    if ((e = cudaSetDevice(device)) != cudaSuccess) { setErr("cudaSetDevice", e); return DSG_ERR_NO_DEVICE; }

    cudaDeviceProp prop;
    if ((e = cudaGetDeviceProperties(&prop, device)) != cudaSuccess) {
        setErr("cudaGetDeviceProperties", e); return DSG_ERR_NO_DEVICE;
    }

    // Shared memory beyond 48 KB has to be asked for explicitly, and it is a
    // per-function attribute, so this must happen before the first launch.
    if ((e = cudaFuncSetAttribute(suffix_kernel,
                                  cudaFuncAttributeMaxDynamicSharedMemorySize,
                                  BR_SHARED_BYTES)) != cudaSuccess) {
        setErr("shared memory limit", e);
        return DSG_ERR_STATE;
    }

    dsg_context* c = (dsg_context*)calloc(1, sizeof(dsg_context));
    if (!c) { snprintf(g_err, sizeof(g_err), "out of host memory"); return DSG_ERR_ALLOC; }
    c->device = device;
    c->sms = prop.multiProcessorCount;
    snprintf(c->name, sizeof(c->name), "%s (sm_%d%d, %d SMs)",
             prop.name, prop.major, prop.minor, prop.multiProcessorCount);

    const size_t perBlock = (size_t)ASTRO_MAX_TEXT * SAD_BYTES_PER_SYMBOL;
    const size_t perChunkHash = (size_t)ASTRO_MAX_TEXT          // text
                              + (size_t)ASTRO_MAX_TEXT * 4      // suffix array
                              + 4;                              // length

    size_t freeB = 0, totB = 0;
    if ((e = cudaMemGetInfo(&freeB, &totB)) != cudaSuccess) {
        setErr("cudaMemGetInfo", e); free(c); return DSG_ERR_ALLOC;
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

    // One chunk per batch when it fits, halving until it does. A card too small
    // for the whole batch at once simply runs more launches, and the work queue
    // in the suffix kernel is what keeps that from costing much.
    c->chunk = c->batch;
    while (c->chunk > S1_BLOCK && (size_t)c->chunk * perChunkHash > forChunk)
        c->chunk /= 2;

#define ALLOC(p, bytes) do { \
        if ((e = cudaMalloc((void**)&(p), (bytes))) != cudaSuccess) { \
            setErr("cudaMalloc", e); dsg_free(c); return DSG_ERR_ALLOC; } \
    } while (0)

    ALLOC(c->words, (size_t)c->maxBlocks * ASTRO_MAX_TEXT * 6 * 4);
    ALLOC(c->keys,  (size_t)c->maxBlocks * ASTRO_MAX_TEXT * 2 * 8);
    // Eight bytes of tail. descLoadBE32 reads the two aligned words that
    // straddle a text offset, so the last few positions of the last text in the
    // chunk reach up to four bytes past it. They are fetched and never used,
    // but an unmapped fetch is still a fault.
    ALLOC(c->texts, (size_t)c->chunk * ASTRO_MAX_TEXT + 8);
    ALLOC(c->lens,  (size_t)c->chunk * 4);
    ALLOC(c->sa,    (size_t)c->chunk * ASTRO_MAX_TEXT * 4);
    ALLOC(c->next,  sizeof(int32_t));
    ALLOC(c->dWork, DSG_WORK_SIZE);
    ALLOC(c->dTarget, 4 * sizeof(uint64_t));
    ALLOC(c->dNonces, 64 * sizeof(uint32_t));
    ALLOC(c->dFound, sizeof(int32_t));
    ALLOC(c->dHashOne, 32);
#undef ALLOC

    // cudaStreamNonBlocking: this must not be serialised against the legacy
    // default stream, which is where the plain cudaMemcpy calls elsewhere run.
    if ((e = cudaStreamCreateWithFlags(&c->stream, cudaStreamNonBlocking)) != cudaSuccess) {
        setErr("cudaStreamCreate", e); dsg_free(c); return DSG_ERR_ALLOC;
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
    if (ctx->stream) cudaStreamDestroy(ctx->stream);
    cudaFree(ctx->texts);
    cudaFree(ctx->lens);
    cudaFree(ctx->sa);
    cudaFree(ctx->next);
    cudaFree(ctx->words);
    cudaFree(ctx->keys);
    cudaFree(ctx->dWork);
    cudaFree(ctx->dTarget);
    cudaFree(ctx->dNonces);
    cudaFree(ctx->dFound);
    cudaFree(ctx->dHashOne);
    free(ctx);
}

// Queues the three kernels for `count` consecutive nonces from base. Returns as
// soon as they are enqueued; the caller synchronises at the end of the batch.
//
// Everything goes on the context's one stream, which is what orders the work
// counter's reset against the kernel that reads it and each chunk against the
// last.
static cudaError_t runChunk(dsg_context* c, uint32_t base, int count,
                            int targetAll, int cap)
{
    Pool pool; pool.words = c->words; pool.keys = c->keys;
    cudaStream_t st = c->stream;
    cudaError_t e;

    stage1_kernel<<<(count + S1_BLOCK - 1) / S1_BLOCK, S1_BLOCK,
                    S1_BLOCK * S1_STRIDE, st>>>(
        c->dWork, base, count, c->texts, c->lens);

    if ((e = cudaMemsetAsync(c->next, 0, sizeof(int32_t), st)) != cudaSuccess) return e;

    suffix_kernel<<<c->blocks, BR_BLOCK, BR_SHARED_BYTES, st>>>(
        c->texts, c->lens, count, pool, c->sa, c->next);

    sha_check_kernel<<<(count + 63) / 64, 64, 0, st>>>(
        c->sa, c->lens, count, base,
        c->dTarget, targetAll, c->dNonces, cap, c->dFound);

    return cudaSuccess;
}

extern "C" DSG_API int dsg_search(dsg_context* ctx,
                                  const uint8_t work[DSG_WORK_SIZE],
                                  uint32_t nonce_start,
                                  const uint64_t target[4],
                                  int target_all,
                                  uint32_t* nonces, int max_nonces, int* found)
{
    if (!ctx || !found) { snprintf(g_err, sizeof(g_err), "dsg_search: null argument"); return DSG_ERR_STATE; }
    *found = 0;

    cudaError_t e;
    if ((e = cudaSetDevice(ctx->device)) != cudaSuccess) { setErr("cudaSetDevice", e); return DSG_ERR_STATE; }

    // Uploaded on the context's stream, which is what puts them in place before
    // the stage-1 kernel that reads them. A plain cudaMemcpy would not order
    // against a non-blocking stream.
    if ((e = cudaMemcpyAsync(ctx->dWork, work, DSG_WORK_SIZE,
                             cudaMemcpyHostToDevice, ctx->stream)) != cudaSuccess) {
        setErr("upload work", e); return DSG_ERR_LAUNCH;
    }
    if ((e = cudaMemcpyAsync(ctx->dTarget, target, 4 * sizeof(uint64_t),
                             cudaMemcpyHostToDevice, ctx->stream)) != cudaSuccess) {
        setErr("upload target", e); return DSG_ERR_LAUNCH;
    }
    if ((e = cudaMemsetAsync(ctx->dFound, 0, sizeof(int32_t), ctx->stream)) != cudaSuccess) {
        setErr("reset counter", e); return DSG_ERR_LAUNCH;
    }

    const int cap = max_nonces < 64 ? max_nonces : 64;

    for (int done = 0; done < ctx->batch; done += ctx->chunk) {
        const int count = (ctx->batch - done) < ctx->chunk ? (ctx->batch - done) : ctx->chunk;
        if ((e = runChunk(ctx, nonce_start + (uint32_t)done, count,
                          target_all, cap)) != cudaSuccess) {
            setErr("launch", e); return DSG_ERR_LAUNCH;
        }
        if ((e = cudaGetLastError()) != cudaSuccess) { setErr("kernel", e); return DSG_ERR_LAUNCH; }
    }

    if ((e = cudaStreamSynchronize(ctx->stream)) != cudaSuccess) {
        setErr("kernel", e); return DSG_ERR_LAUNCH;
    }
    if ((e = cudaGetLastError()) != cudaSuccess) { setErr("kernel", e); return DSG_ERR_LAUNCH; }

    int32_t nf = 0;
    if ((e = cudaMemcpy(&nf, ctx->dFound, sizeof(int32_t), cudaMemcpyDeviceToHost)) != cudaSuccess) {
        setErr("read counter", e); return DSG_ERR_LAUNCH;
    }
    if (nf > cap) nf = cap;
    if (nf > 0) {
        if ((e = cudaMemcpy(nonces, ctx->dNonces, (size_t)nf * sizeof(uint32_t),
                            cudaMemcpyDeviceToHost)) != cudaSuccess) {
            setErr("read nonces", e); return DSG_ERR_LAUNCH;
        }
    }
    *found = nf;
    return DSG_OK;
}

extern "C" DSG_API int dsg_hash_one(dsg_context* ctx,
                                    const uint8_t work[DSG_WORK_SIZE],
                                    uint32_t nonce, uint8_t out[32])
{
    if (!ctx) { snprintf(g_err, sizeof(g_err), "dsg_hash_one: null context"); return DSG_ERR_STATE; }

    cudaError_t e;
    if ((e = cudaSetDevice(ctx->device)) != cudaSuccess) { setErr("cudaSetDevice", e); return DSG_ERR_STATE; }
    if ((e = cudaMemcpy(ctx->dWork, work, DSG_WORK_SIZE, cudaMemcpyHostToDevice)) != cudaSuccess) {
        setErr("upload work", e); return DSG_ERR_LAUNCH;
    }

    Pool pool; pool.words = ctx->words; pool.keys = ctx->keys;

    if ((e = cudaMemset(ctx->next, 0, sizeof(int32_t))) != cudaSuccess) {
        setErr("reset work counter", e); return DSG_ERR_LAUNCH;
    }
    stage1_kernel<<<1, S1_BLOCK, S1_BLOCK * S1_STRIDE>>>(ctx->dWork, nonce, 1,
                                                         ctx->texts, ctx->lens);
    suffix_kernel<<<1, BR_BLOCK, BR_SHARED_BYTES>>>(ctx->texts, ctx->lens,
                                                    1, pool, ctx->sa, ctx->next);
    sha_one_kernel<<<1, 1>>>(ctx->sa, ctx->lens, ctx->dHashOne);

    if ((e = cudaDeviceSynchronize()) != cudaSuccess) { setErr("kernel", e); return DSG_ERR_LAUNCH; }
    if ((e = cudaGetLastError()) != cudaSuccess) { setErr("kernel", e); return DSG_ERR_LAUNCH; }
    if ((e = cudaMemcpy(out, ctx->dHashOne, 32, cudaMemcpyDeviceToHost)) != cudaSuccess) {
        setErr("read hash", e); return DSG_ERR_LAUNCH;
    }
    return DSG_OK;
}
