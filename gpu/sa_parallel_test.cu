// sa_parallel_test.cu -- option B checked against the CPU, then timed against
// option A on the same data and the same card.
//
// Both mappings compute the same function, so the comparison is direct: A gives
// a thread to each hash and runs SA-IS; B gives a block to each hash and runs
// prefix doubling. The question this answers is whether trading O(n) work for a
// coalesced access pattern is worth it here, and by how much.
//
//   nvcc -O3 -arch=sm_120 -o gpu/sa_parallel_test.exe gpu/sa_parallel_test.cu
//   gpu\sa_parallel_test.exe [vectors.bin]

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <vector>
#include <cuda_runtime.h>

#include "vectors_host.h"
#include "sa_doubling.cuh"
#include "sais.cuh"

#define CK(x) do { cudaError_t e=(x); if(e!=cudaSuccess){ \
  printf("CUDA error: %s (line %d)\n", cudaGetErrorString(e), __LINE__); exit(1);} } while(0)

// Per-block scratch, laid out as one struct so the kernel and the host agree
// on the arithmetic in one place.
struct Pool {
    int32_t*  words;       // 8 int32 arrays, back to back
    uint64_t* keys;        // 2 uint64 arrays, back to back
    int       stride;      // elements reserved per block per array

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

// One block per suffix array.
__global__ __launch_bounds__(BR_BLOCK) void doubling_kernel(
        const uint8_t* texts, const int32_t* lens, int stride,
        Pool pool, int nvec, int blocks, int32_t* out, int outStride)
{
    const int b = blockIdx.x;
    if (b >= blocks) return;
    const int v = b % nvec;
    const int n = lens[v];

    // Dynamic, not static: the scratch is ~54 KB at 1,024 threads and static
    // shared memory is capped at 48 KB on every architecture.
    extern __shared__ char smem[];
    BlockRadixScratch* sh = (BlockRadixScratch*)smem;

    SADoublingScratch sc = pool.mine(b);
    saDoublingBlock(texts + (size_t)v * stride, n, sc, sh);

    for (int i = threadIdx.x; i < n; i += BR_BLOCK)
        out[(size_t)b * outStride + i] = sc.sa[i];
}

// Option A, for the timing comparison on this exact data.
__global__ void sais_kernel(const uint8_t* texts, const int32_t* lens, int stride,
                            int32_t* saOut, uint32_t* tScratch, int32_t* bktScratch,
                            int tWords, int bktSize, int nvec, int threads)
{
    const int tid = blockIdx.x * blockDim.x + threadIdx.x;
    if (tid >= threads) return;
    const int v = tid % nvec;
    saisSuffixArray(texts + (size_t)v * stride, lens[v],
                    saOut + (size_t)tid * (stride + 1),
                    tScratch + (size_t)tid * tWords,
                    bktScratch + (size_t)tid * bktSize);
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

int main(int argc, char** argv)
{
    const char* path = argc > 1 ? argv[1] : "gpu/vectors.bin";
    Vectors v = loadVectors(path);

    cudaDeviceProp p; CK(cudaGetDeviceProperties(&p, 0));
    printf("\n  block-per-hash suffix array (option B) vs thread-per-hash SA-IS (option A)\n\n");
    printf("  device     %s (sm_%d%d, %d SMs)\n", p.name, p.major, p.minor, p.multiProcessorCount);
    printf("  vectors    %d, max text %d bytes\n", v.count, v.maxLen);
    printf("  L2         %.0f MB\n", p.l2CacheSize / 1048576.0);

    const int stride = v.maxLen;
    const size_t perBlock = (size_t)stride * SAD_BYTES_PER_SYMBOL;
    printf("  per hash   %.0f KB for B  (32 bytes per text byte)\n", perBlock / 1024.0);

    const int tWords  = SAIS_T_WORDS(v.maxLen);
    const int bktSize = SAIS_BKT(v.maxLen);
    const size_t perThread = (size_t)(stride + 1) * 4 + (size_t)tWords * 4 + (size_t)bktSize * 4;
    printf("  per hash   %.0f KB for A\n\n", perThread / 1024.0);

    CK(cudaDeviceSetLimit(cudaLimitStackSize, 8 * 1024));

    // Shared memory beyond 48 KB has to be asked for explicitly.
    printf("  shared     %.1f KB per block for the radix sort\n", BR_SHARED_BYTES / 1024.0);
    CK(cudaFuncSetAttribute(doubling_kernel,
                            cudaFuncAttributeMaxDynamicSharedMemorySize, BR_SHARED_BYTES));

    size_t freeB, totB; CK(cudaMemGetInfo(&freeB, &totB));
    size_t budget = freeB > (size_t)2e9 ? freeB - (size_t)2e9 : 0;

    // B is measured over a sweep of block counts, so allocate for the largest
    // one that fits and is worth trying: past a few per SM the working set is
    // well beyond L2 again and the point of the mapping is lost.
    int maxBlocks = (int)(budget / 2 / perBlock);
    if (maxBlocks > 16 * p.multiProcessorCount) maxBlocks = 16 * p.multiProcessorCount;
    if (maxBlocks < 1) { printf("  not enough VRAM for even one block\n"); return 1; }

    int maxThreads = (int)(budget / 2 / perThread);
    maxThreads -= maxThreads % 64;
    printf("  VRAM allows %d concurrent hashes for B, %d for A\n\n", maxBlocks, maxThreads);

    uint8_t* dTexts = nullptr; int32_t* dLens = nullptr; int32_t* dOut = nullptr;
    CK(cudaMalloc(&dTexts, v.texts.size()));
    CK(cudaMalloc(&dLens, (size_t)v.count * 4));
    CK(cudaMemcpy(dTexts, v.texts.data(), v.texts.size(), cudaMemcpyHostToDevice));
    CK(cudaMemcpy(dLens, v.lens.data(), (size_t)v.count * 4, cudaMemcpyHostToDevice));

    Pool pool{}; pool.stride = stride;
    CK(cudaMalloc(&pool.words, (size_t)maxBlocks * stride * 6 * 4));
    CK(cudaMalloc(&pool.keys,  (size_t)maxBlocks * stride * 2 * 8));

    // ---- correctness -----------------------------------------------------
    const int checkBlocks = v.count < maxBlocks ? v.count : maxBlocks;
    CK(cudaMalloc(&dOut, (size_t)checkBlocks * stride * 4));

    doubling_kernel<<<checkBlocks, BR_BLOCK, BR_SHARED_BYTES>>>(dTexts, dLens, stride, pool,
                                               v.count, checkBlocks, dOut, stride);
    CK(cudaDeviceSynchronize());
    CK(cudaGetLastError());

    std::vector<int32_t> got((size_t)checkBlocks * stride);
    CK(cudaMemcpy(got.data(), dOut, got.size() * 4, cudaMemcpyDeviceToHost));

    int bad = 0, firstBad = -1; long long firstIdx = -1;
    for (int i = 0; i < checkBlocks; i++) {
        const int32_t* g = got.data() + (size_t)i * stride;
        const int32_t* w = v.sa.data() + (size_t)i * stride;
        for (int k = 0; k < v.lens[i]; k++) {
            if (g[k] != w[k]) {
                if (firstBad < 0) { firstBad = i; firstIdx = k; }
                bad++;
                break;
            }
        }
    }
    if (bad) {
        printf("  MISMATCH: %d of %d suffix arrays differ\n", bad, checkBlocks);
        const int32_t* g = got.data() + (size_t)firstBad * stride;
        const int32_t* w = v.sa.data() + (size_t)firstBad * stride;
        printf("  first at vector %d index %lld: gpu %d, cpu %d\n",
               firstBad, firstIdx, g[firstIdx], w[firstIdx]);
        return 1;
    }
    printf("  CORRECT: all %d suffix arrays match the CPU exactly\n\n", checkBlocks);
    CK(cudaFree(dOut));
    CK(cudaMalloc(&dOut, (size_t)maxBlocks * 4));   // throwaway sink for timing

    // ---- throughput ------------------------------------------------------
    // Best of three: kernels here run for seconds and the boost clock sags
    // under sustained load, so one sample measures thermals as much as code.
    printf("  option B, one block per hash\n");
    printf("  %-9s %10s %12s %10s\n", "blocks", "per SM", "SA/s", "ms");
    double bestB = 0; int bestBlocks = 0;
    for (int b = p.multiProcessorCount / 4; b <= maxBlocks; b *= 2) {
        double best = 0, bms = 0;
        for (int rep = 0; rep < 3; rep++) {
            float ms = timeKernel([&]{
                doubling_kernel<<<b, BR_BLOCK, BR_SHARED_BYTES>>>(dTexts, dLens, stride, pool,
                                                 v.count, b, dOut, 0);
            });
            double r = b / (ms / 1000.0);
            if (r > best) { best = r; bms = ms; }
        }
        printf("  %-9d %10.1f %12.0f %10.1f\n",
               b, (double)b / p.multiProcessorCount, best, bms);
        if (best > bestB) { bestB = best; bestBlocks = b; }
        fflush(stdout);
    }

    CK(cudaFree(pool.words)); CK(cudaFree(pool.keys));

    // ---- option A on the same data --------------------------------------
    int32_t* aSA = nullptr; uint32_t* aT = nullptr; int32_t* aBkt = nullptr;
    CK(cudaMemGetInfo(&freeB, &totB));
    budget = freeB > (size_t)1e9 ? freeB - (size_t)1e9 : 0;
    maxThreads = (int)(budget / perThread);
    maxThreads -= maxThreads % 64;

    CK(cudaMalloc(&aSA, (size_t)maxThreads * (stride + 1) * 4));
    CK(cudaMalloc(&aT, (size_t)maxThreads * tWords * 4));
    CK(cudaMalloc(&aBkt, (size_t)maxThreads * bktSize * 4));

    printf("\n  option A, one thread per hash\n");
    printf("  %-9s %12s %10s\n", "threads", "SA/s", "ms");
    double bestA = 0; int bestT = 0;
    for (int t = 2048; t <= maxThreads; t *= 2) {
        double best = 0, bms = 0;
        for (int rep = 0; rep < 3; rep++) {
            float ms = timeKernel([&]{
                sais_kernel<<<(t + 63) / 64, 64>>>(dTexts, dLens, stride, aSA, aT, aBkt,
                                                   tWords, bktSize, v.count, t);
            });
            double r = t / (ms / 1000.0);
            if (r > best) { best = r; bms = ms; }
        }
        printf("  %-9d %12.0f %10.1f\n", t, best, bms);
        if (best > bestA) { bestA = best; bestT = t; }
        fflush(stdout);
    }

    printf("\n  A: %.0f SA/s at %d threads\n", bestA, bestT);
    printf("  B: %.0f SA/s at %d blocks (%.1f per SM)\n",
           bestB, bestBlocks, (double)bestBlocks / p.multiProcessorCount);
    printf("  B is %.2fx A\n\n", bestB / bestA);

    CK(cudaFree(aSA)); CK(cudaFree(aT)); CK(cudaFree(aBkt));
    CK(cudaFree(dTexts)); CK(cudaFree(dLens)); CK(cudaFree(dOut));
    return 0;
}
