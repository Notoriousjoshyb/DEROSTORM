// hash_parallel_test.cu -- the whole AstroBWTv3 hash with option B's suffix
// sort, checked against the CPU and timed against option A end to end.
//
// The three stages of a hash want three different mappings, so this runs three
// kernels rather than one:
//
//   1. stage 1   thread per hash. It is a 256-byte state machine with a 256-way
//                branch; there is nothing in it to spread across a block, and
//                with its state in shared memory it already runs at 370,000
//                hashes a second, about 1% of the total.
//   2. suffix    block per hash (sa_doubling.cuh). This is the whole cost, and
//                it is the one stage that has enough parallelism to fill a
//                block and enough memory traffic to care about coalescing.
//   3. SHA-256   thread per hash. Serial by construction, but one per hash and
//                thousands of hashes, so thread per hash is exactly right.
//
// Splitting them also removes the constraint that caps option A. A needs half a
// megabyte of private scratch for every hash *in flight*, so a batch of 27,000
// costs 13 GB and the batch size is set by VRAM. Here the suffix kernel's
// blocks loop over the batch and reuse their scratch, so the batch size and the
// scratch are independent.
//
//   nvcc -O3 -arch=sm_120 -DBR_BLOCK=256 -o gpu/hash_parallel_test.exe gpu/hash_parallel_test.cu
//   gpu\hash_parallel_test.exe [vectors.bin]

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <vector>
#include <cuda_runtime.h>

#include "vectors_host.h"
#include "stage1.cuh"
#include "sa_doubling.cuh"
#include "desc.cuh"

#define CK(x) do { cudaError_t e=(x); if(e!=cudaSuccess){ \
  printf("CUDA error: %s (line %d)\n", cudaGetErrorString(e), __LINE__); exit(1);} } while(0)

// Shared tile for stage 1: step_3 and the RC4 permutation, 256 bytes each per
// thread, plus 4 bytes of padding so lanes land on different banks.
#define S1_STRIDE 516
#ifndef S1_BLOCK
#define S1_BLOCK  64
#endif
#ifndef SHA_BLOCK
#define SHA_BLOCK 64
#endif
#ifndef DESC_PREFER_SHARED
#define DESC_PREFER_SHARED 1
#endif

struct Pool {
    int32_t*  words;      // 8 int32 arrays per block, back to back
    uint64_t* keys;       // 2 uint64 arrays per block, back to back
    int       stride;

    __device__ DescScratch descMine(int b) const {
        const size_t w = (size_t)b * stride * 6;
        const size_t k = (size_t)b * stride * 2;
        DescScratch s;
        s.words  = keys + k + (size_t)stride * 0;
        s.words2 = keys + k + (size_t)stride * 1;
        s.arena  = (uint16_t*)(words + w + (size_t)stride * 0);
        s.offs   = words + w + (size_t)stride * 1;
        s.mbuf   = words + w + (size_t)stride * 2;
        return s;
    }

    __device__ SADoublingScratch mine(int b) const {
        const size_t w = (size_t)b * stride * 6;
        const size_t k = (size_t)b * stride * 2;
        SADoublingScratch s;
        s.sa    = words + w + (size_t)stride * 0;
        s.rank  = words + w + (size_t)stride * 1;
        s.tmp   = words + w + (size_t)stride * 2;
        s.tmp2  = words + w + (size_t)stride * 3;
        s.act   = words + w + (size_t)stride * 4;
        s.act2  = words + w + (size_t)stride * 5;
        s.wordA = keys + k + (size_t)stride * 0;
        s.wordB = keys + k + (size_t)stride * 1;
        return s;
    }
};

__global__ void stage1_kernel(const uint8_t* inputs, int nvec, int batch,
                              uint8_t* texts, int32_t* lens)
{
    const int tid = blockIdx.x * blockDim.x + threadIdx.x;
    if (tid >= batch) return;

    extern __shared__ uint8_t tile[];
    uint8_t* mine = tile + (size_t)threadIdx.x * S1_STRIDE;

    lens[tid] = (int32_t)astroStage1(inputs + (size_t)(tid % nvec) * ASTRO_INPUT_LEN,
                                     ASTRO_INPUT_LEN,
                                     texts + (size_t)tid * ASTRO_MAX_TEXT,
                                     mine, mine + 256);
}

// One block per hash, looping over the batch so the scratch is reused. This is
// what decouples the batch size from VRAM.
//
// The occupancy bound has to be the shipped one. Without it nvcc gives this
// kernel as many registers as it likes, which is a different image from the one
// the miner runs, and an A/B measured here then says nothing about the miner.
// derostorm_gpu.cu picks 5 on Blackwell and 4 elsewhere; this mirrors it.
#ifndef HP_OCC
#if defined(__CUDA_ARCH__) && __CUDA_ARCH__ >= 1200
#define HP_OCC 5
#else
#define HP_OCC 4
#endif
#endif
__global__ __launch_bounds__(BR_BLOCK, HP_OCC) void suffix_kernel(
        const uint8_t* texts, const int32_t* lens, int batch,
        Pool pool, int32_t* saOut, int saStride)
{
    // Dynamic, not static: the scratch is ~54 KB at 1,024 threads and static
    // shared memory is capped at 48 KB on every architecture.
    extern __shared__ char smem[];
    BlockRadixScratch* sh = (BlockRadixScratch*)smem;

    SADoublingScratch sc = pool.mine(blockIdx.x);
    DescScratch dsc = pool.descMine(blockIdx.x);

    for (int h = blockIdx.x; h < batch; h += gridDim.x) {
        const int n = lens[h];
        const uint8_t* text = texts + (size_t)h * ASTRO_MAX_TEXT;
        int32_t* out = saOut + (size_t)h * saStride;

#if DESC_PREFETCH_TEXT
#if DESC_PREFETCH_L1
        for (int i = (int)threadIdx.x * DESC_PREFETCH_STRIDE; i < n; i += BR_BLOCK * DESC_PREFETCH_STRIDE) {
            asm volatile("prefetch.global.L1 [%0];" :: "l"(text + i));
        }
#elif DESC_PREFETCH_L2
        for (int i = (int)threadIdx.x * DESC_PREFETCH_STRIDE; i < n; i += BR_BLOCK * DESC_PREFETCH_STRIDE) {
            asm volatile("prefetch.global.L2 [%0];" :: "l"(text + i));
        }
#else
        // One byte a stride, not one byte a thread.
        //
        // AMD has no prefetch instruction that reaches the last level without
        // also landing the line in a register, so the warm-up here is a real
        // load whose result is thrown away. That does not mean it has to read
        // every byte: a cache line is 128 bytes on RDNA, so a lane touching
        // every 64th byte pulls in the same lines and issues a sixty-fourth of
        // the loads. Over a 68 KB text that is about 1,100 loads a block
        // instead of 70,000 -- four iterations a thread instead of 273.
        for (int i = (int)threadIdx.x * DESC_PREFETCH_STRIDE; i < n;
             i += BR_BLOCK * DESC_PREFETCH_STRIDE) {
            (void)*(((const volatile uint8_t*)text) + i);
        }
#endif
        __syncthreads();
#endif

        // The same two sorts the miner uses, in the same order: the descriptor
        // sort, and prefix doubling as its fallback. Kept in step with
        // derostorm_gpu.cu deliberately -- a harness that measures a different
        // kernel from the one that ships is worse than no harness.
        if (descSuffixArrayBlock(text, n, out, dsc, sh) == 0) {
            __syncthreads();
            continue;
        }

        saDoublingBlock(text, n, sc, sh);
        for (int i = threadIdx.x; i < n; i += BR_BLOCK) out[i] = sc.sa[i];
        __syncthreads();
    }
}

__global__ void sha_kernel(const int32_t* sa, int saStride, const int32_t* lens,
                           int batch, uint8_t* hashOut)
{
    const int tid = blockIdx.x * blockDim.x + threadIdx.x;
    if (tid >= batch) return;
    // Go hashes the raw little-endian int32 suffix array; the GPU is little
    // endian too, so it is already in that layout.
    sha256(hashOut + (size_t)tid * 32,
           (const uint8_t*)(sa + (size_t)tid * saStride), lens[tid] * 4);
}

template <class F>
static float timeKernel(F f)
{
    f();
    CK(cudaDeviceSynchronize());

    cudaEvent_t a, b; CK(cudaEventCreate(&a)); CK(cudaEventCreate(&b));
    CK(cudaEventRecord(a));
    f();
    CK(cudaEventRecord(b));
    CK(cudaEventSynchronize(b));
    CK(cudaGetLastError());
    float ms = 0; CK(cudaEventElapsedTime(&ms, a, b));
    CK(cudaEventDestroy(a)); CK(cudaEventDestroy(b));
    return ms;
}

static void hex(const uint8_t* p, char* out)
{
    static const char* d = "0123456789abcdef";
    for (int i = 0; i < 32; i++) { out[i * 2] = d[p[i] >> 4]; out[i * 2 + 1] = d[p[i] & 15]; }
    out[64] = 0;
}

int main(int argc, char** argv)
{
    const char* path = argc > 1 ? argv[1] : "gpu/vectors.bin";
    Vectors v = loadVectors(path);

    cudaDeviceProp p; CK(cudaGetDeviceProperties(&p, 0));
    printf("\n  AstroBWTv3 whole hash, option B suffix sort\n\n");
    printf("  device     %s (sm_%d%d, %d SMs)\n", p.name, p.major, p.minor, p.multiProcessorCount);
    printf("  block      %d threads per hash in the suffix kernel\n", BR_BLOCK);
    printf("  vectors    %d, max text %d bytes\n", v.count, v.maxLen);

    // The suffix kernel sizes its scratch for the worst case the algorithm can
    // produce, not for the longest text in this file: a miner sees nonces it
    // has not seen before and must not overflow on a longer one.
    const int    saStride = ASTRO_MAX_TEXT;
    const size_t perBlock = (size_t)ASTRO_MAX_TEXT * SAD_BYTES_PER_SYMBOL;
    const size_t perHash  = (size_t)ASTRO_MAX_TEXT      // text
                          + (size_t)saStride * 4        // suffix array
                          + 32 + 4;                     // hash and length

    printf("  per block  %.1f MB of suffix scratch\n", perBlock / 1048576.0);
    printf("  per hash   %.0f KB of batch storage\n\n", perHash / 1024.0);

    printf("  shared     %.1f KB per block for the radix sort\n\n", BR_SHARED_BYTES / 1024.0);
    CK(cudaFuncSetAttribute(suffix_kernel,
                            cudaFuncAttributeMaxDynamicSharedMemorySize, DESC_LAUNCH_SHARED));
#if DESC_PREFER_L1
    CK(cudaFuncSetCacheConfig(suffix_kernel, cudaFuncCachePreferL1));
#elif DESC_PREFER_SHARED
    CK(cudaFuncSetCacheConfig(suffix_kernel, cudaFuncCachePreferShared));
#endif

    size_t freeB, totB; CK(cudaMemGetInfo(&freeB, &totB));
    size_t budget = freeB > (size_t)1500e6 ? freeB - (size_t)1500e6 : 0;

    // Half the budget to the blocks' scratch, half to the batch.
    int blocks = (int)(budget / 2 / perBlock);
    // Scaled by the block width: four 256-thread blocks fill half an SM, not
    // all of it. See the note in gpubuildlib.bat.
    {
        const int cap = 2 * p.multiProcessorCount * 2048 / BR_BLOCK;
        if (blocks > cap) blocks = cap;
    }
    int batch = (int)(budget / 2 / perHash);
    if (batch > 32768) batch = 32768;
    batch -= batch % S1_BLOCK;
    if (blocks < 1 || batch < S1_BLOCK) { printf("  not enough VRAM\n"); return 1; }
    printf("  VRAM allows %d grid blocks and a batch of %d\n\n", blocks, batch);

    uint8_t*  dIn = nullptr; uint8_t* dTexts = nullptr; uint8_t* dHash = nullptr;
    int32_t*  dLens = nullptr; int32_t* dSA = nullptr;
    Pool pool{}; pool.stride = ASTRO_MAX_TEXT;

    CK(cudaMalloc(&dIn, v.inputs.size()));
    // Eight bytes of tail: descLoadBE32 reads the two aligned words that
    // straddle a text offset, so the last positions of the last text reach a
    // few bytes past it. Fetched, never used, but still has to be mapped.
    CK(cudaMalloc(&dTexts, (size_t)batch * ASTRO_MAX_TEXT + 8));
    CK(cudaMalloc(&dLens, (size_t)batch * 4));
    CK(cudaMalloc(&dSA, (size_t)batch * saStride * 4));
    CK(cudaMalloc(&dHash, (size_t)batch * 32));
    CK(cudaMalloc(&pool.words, (size_t)blocks * ASTRO_MAX_TEXT * 6 * 4));
    CK(cudaMalloc(&pool.keys,  (size_t)blocks * ASTRO_MAX_TEXT * 2 * 8));
    CK(cudaMemcpy(dIn, v.inputs.data(), v.inputs.size(), cudaMemcpyHostToDevice));

    auto runAll = [&](int b) {
        stage1_kernel<<<(b + S1_BLOCK - 1) / S1_BLOCK, S1_BLOCK, S1_BLOCK * S1_STRIDE>>>(
            dIn, v.count, b, dTexts, dLens);
        suffix_kernel<<<blocks, BR_BLOCK, DESC_LAUNCH_SHARED>>>(dTexts, dLens, b, pool, dSA, saStride);
        sha_kernel<<<(b + SHA_BLOCK - 1) / SHA_BLOCK, SHA_BLOCK>>>(dSA, saStride, dLens, b, dHash);
    };

    // ---- correctness -----------------------------------------------------
    const int check = v.count < batch ? v.count : batch;
    runAll(check);
    CK(cudaDeviceSynchronize());
    CK(cudaGetLastError());

    if (!v.haveHash) {
        printf("  old vector format: cannot check the final hash\n");
    } else {
        std::vector<uint8_t> got((size_t)check * 32);
        CK(cudaMemcpy(got.data(), dHash, got.size(), cudaMemcpyDeviceToHost));
        int bad = 0, first = -1;
        for (int i = 0; i < check; i++)
            if (memcmp(got.data() + (size_t)i * 32, v.hash.data() + (size_t)i * 32, 32)) {
                if (first < 0) first = i;
                bad++;
            }
        if (bad) {
            char a[65], b2[65];
            hex(got.data() + (size_t)first * 32, a);
            hex(v.hash.data() + (size_t)first * 32, b2);
            printf("  HASH MISMATCH: %d of %d differ\n", bad, check);
            printf("  first at vector %d\n    gpu %s\n    cpu %s\n", first, a, b2);
            return 1;
        }
        printf("  CORRECT: all %d hashes match the CPU exactly\n\n", check);
    }

    // ---- throughput ------------------------------------------------------
    // Best of three: these kernels run for a while and the boost clock sags
    // under sustained load, so one sample measures thermals as much as code.
    printf("  %-9s %10s %10s %10s %12s\n", "blocks", "stage1 ms", "suffix ms", "sha ms", "H/s");
    double best = 0; int bestBlocks = 0;
    for (int b = p.multiProcessorCount / 2; b <= blocks; b *= 2) {
        const int saveBlocks = blocks;
        blocks = b;

        double bs1 = 1e30, bsu = 1e30, bsh = 1e30;
        for (int rep = 0; rep < 3; rep++) {
            float t1 = timeKernel([&]{
                stage1_kernel<<<(batch + S1_BLOCK - 1) / S1_BLOCK, S1_BLOCK, S1_BLOCK * S1_STRIDE>>>(
                    dIn, v.count, batch, dTexts, dLens); });
            float t2 = timeKernel([&]{
                suffix_kernel<<<b, BR_BLOCK, DESC_LAUNCH_SHARED>>>(dTexts, dLens, batch, pool, dSA, saStride); });
            float t3 = timeKernel([&]{
                sha_kernel<<<(batch + SHA_BLOCK - 1) / SHA_BLOCK, SHA_BLOCK>>>(dSA, saStride, dLens, batch, dHash); });
            if (t1 < bs1) bs1 = t1;
            if (t2 < bsu) bsu = t2;
            if (t3 < bsh) bsh = t3;
        }
        const double total = bs1 + bsu + bsh;
        const double hps = batch / (total / 1000.0);
        printf("  %-9d %10.1f %10.1f %10.1f %12.0f\n", b, bs1, bsu, bsh, hps);
        if (hps > best) { best = hps; bestBlocks = b; }
        fflush(stdout);

        blocks = saveBlocks;
    }

    printf("\n  best %.0f H/s at %d blocks, batch %d\n", best, bestBlocks, batch);

    printf("\n  %-9s %10s %10s %10s %12s\n", "batch", "stage1 ms", "suffix ms", "sha ms", "H/s");
    {
        const int sizes[] = {4096, 8192, 16384, 24576, 32768};
        for (int n : sizes) {
            if (n > batch) continue;
            double bs1 = 1e30, bsu = 1e30, bsh = 1e30;
            for (int rep = 0; rep < 3; rep++) {
                float t1 = timeKernel([&]{
                    stage1_kernel<<<(n + S1_BLOCK - 1) / S1_BLOCK, S1_BLOCK, S1_BLOCK * S1_STRIDE>>>(
                        dIn, v.count, n, dTexts, dLens); });
                float t2 = timeKernel([&]{
                    suffix_kernel<<<bestBlocks, BR_BLOCK, DESC_LAUNCH_SHARED>>>(dTexts, dLens, n, pool, dSA, saStride); });
                float t3 = timeKernel([&]{
                    sha_kernel<<<(n + SHA_BLOCK - 1) / SHA_BLOCK, SHA_BLOCK>>>(dSA, saStride, dLens, n, dHash); });
                if (t1 < bs1) bs1 = t1;
                if (t2 < bsu) bsu = t2;
                if (t3 < bsh) bsh = t3;
            }
            const double hps = n / ((bs1 + bsu + bsh) / 1000.0);
            printf("  %-9d %10.1f %10.1f %10.1f %12.0f\n", n, bs1, bsu, bsh, hps);
            fflush(stdout);
        }
    }

    printf("  option A whole hash, same card: 3961 H/s -> %.2fx\n", best / 3961.0);
    printf("  CPU reference (9800X3D, 14 threads): 4622 H/s -> %.2fx\n", best / 4622.0);
    printf("  combined CPU+GPU would be %.0f H/s (%.2fx)\n\n",
           best + 4622.0, (best + 4622.0) / 4622.0);

    CK(cudaFree(dIn)); CK(cudaFree(dTexts)); CK(cudaFree(dLens));
    CK(cudaFree(dSA)); CK(cudaFree(dHash));
    CK(cudaFree(pool.words)); CK(cudaFree(pool.keys));
    return 0;
}
