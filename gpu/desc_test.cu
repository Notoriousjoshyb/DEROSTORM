// desc_test.cu -- is the descriptor suffix sort faster than prefix doubling on
// the GPU, and does it give the same answer?
//
// The descriptor sort is 2.8x libsais on the CPU because it knows how stage 1
// builds its text. The GPU runs prefix doubling, which knows nothing about it.
// This runs both over the same 512 real texts, checks each against the suffix
// arrays in gpu/vectors.bin, and times them.
//
// Correctness first and unconditionally: a suffix array is unique, so a faster
// build that differs is not a result. The timing is printed either way but
// labelled, because knowing that a wrong version is fast is worth something
// while iterating and worth nothing otherwise.
//
//   gpu\desc_test_build.bat
//   gpu\desc_test.exe gpu\vectors.bin

#include <cstdio>
#include <cstdlib>
#include <cstdint>
#include <vector>
#include <cuda_runtime.h>

#include "vectors_host.h"
#include "sa_doubling.cuh"
#include "desc.cuh"

#define CK(x) do { cudaError_t e=(x); if(e!=cudaSuccess){ \
  printf("CUDA error: %s (line %d)\n", cudaGetErrorString(e), __LINE__); exit(1);} } while(0)

// ---------------------------------------------------------------------------
// prefix doubling, the incumbent
// ---------------------------------------------------------------------------

struct DoublePool {
    int32_t*  words;
    uint64_t* keys;
    int       stride;
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

__global__ __launch_bounds__(BR_BLOCK) void doubling_kernel(
        const uint8_t* texts, const int32_t* lens, int stride,
        DoublePool pool, int count, int32_t* saOut, int32_t* next)
{
    extern __shared__ char smem[];
    BlockRadixScratch* sh = (BlockRadixScratch*)smem;
    __shared__ int claimed;
    SADoublingScratch sc = pool.mine(blockIdx.x);

    for (;;) {
        if (threadIdx.x == 0) claimed = atomicAdd(next, 1);
        __syncthreads();
        const int h = claimed;
        if (h >= count) return;
        const int n = lens[h];
        saDoublingBlock(texts + (size_t)h * stride, n, sc, sh);
        for (int i = threadIdx.x; i < n; i += BR_BLOCK)
            saOut[(size_t)h * stride + i] = sc.sa[i];
        __syncthreads();
    }
}

// ---------------------------------------------------------------------------
// the descriptor sort
// ---------------------------------------------------------------------------

struct DescPool {
    uint64_t* words;
    uint32_t* arena;
    int32_t*  offs;
    int32_t*  mbuf;
    int       stride;
    __device__ DescScratch mine(int b) const {
        DescScratch s;
        s.words  = words + (size_t)b * stride * 2 + (size_t)stride * 0;
        s.words2 = words + (size_t)b * stride * 2 + (size_t)stride * 1;
        s.arena  = arena + (size_t)b * stride;
        s.offs   = offs + (size_t)b * stride;
        s.mbuf   = mbuf + (size_t)b * stride;
        return s;
    }
};

__global__ __launch_bounds__(BR_BLOCK) void desc_kernel(
        const uint8_t* texts, const int32_t* lens, int stride,
        DescPool pool, int count, int32_t* saOut, int32_t* next, int32_t* fails)
{
    extern __shared__ char smem[];
    BlockRadixScratch* sh = (BlockRadixScratch*)smem;
    __shared__ int claimed;
    DescScratch sc = pool.mine(blockIdx.x);

    for (;;) {
        if (threadIdx.x == 0) claimed = atomicAdd(next, 1);
        __syncthreads();
        const int h = claimed;
        if (h >= count) return;
        const int n = lens[h];
        const int rc = descSuffixArrayBlock(texts + (size_t)h * stride, n,
                                            saOut + (size_t)h * stride, sc, sh);
        if (threadIdx.x == 0 && rc != 0) atomicAdd(fails, 1);
        __syncthreads();
    }
}

// ---------------------------------------------------------------------------

static float timeKernel(void (*launch)(void*), void* arg)
{
    cudaEvent_t a, b;
    CK(cudaEventCreate(&a));
    CK(cudaEventCreate(&b));
    CK(cudaEventRecord(a));
    launch(arg);
    CK(cudaEventRecord(b));
    CK(cudaDeviceSynchronize());
    float ms = 0;
    CK(cudaEventElapsedTime(&ms, a, b));
    CK(cudaEventDestroy(a));
    CK(cudaEventDestroy(b));
    return ms;
}

int main(int argc, char** argv)
{
    const char* path = argc > 1 ? argv[1] : "gpu/vectors.bin";
    const int rounds = argc > 2 ? atoi(argv[2]) : 3;
    Vectors v = loadVectors(path);

    cudaDeviceProp p;
    CK(cudaGetDeviceProperties(&p, 0));
    printf("\n  %s (sm_%d%d, %d SMs), BR_BLOCK=%d BR_BITS=%d\n",
           p.name, p.major, p.minor, p.multiProcessorCount, BR_BLOCK, BR_BITS);
    printf("  %d vectors, max text %d bytes, shared %.1f KB per block\n\n",
           v.count, v.maxLen, BR_SHARED_BYTES / 1024.0);

    CK(cudaFuncSetAttribute(doubling_kernel,
                            cudaFuncAttributeMaxDynamicSharedMemorySize, BR_SHARED_BYTES));
    CK(cudaFuncSetAttribute(desc_kernel,
                            cudaFuncAttributeMaxDynamicSharedMemorySize, BR_SHARED_BYTES));

    const int stride = v.maxLen;
    const int count = v.count;

    // Both kernels get the same number of resident blocks, so the comparison is
    // of the algorithms and not of how much of the card each was given.
    //
    // The default scales with BR_BLOCK so that sweeping the block width does not
    // silently sweep the thread count with it: a 256-thread block at the same
    // block count would have a quarter of the threads resident, and the answer
    // would be about that rather than about the width. Override with argv[3].
    int blocks = p.multiProcessorCount * (1024 / BR_BLOCK);
    if (argc > 3) blocks = atoi(argv[3]);
    if (blocks < 1) blocks = 1;

    uint8_t* dTexts = nullptr;
    int32_t* dLens = nullptr;
    int32_t* dSA = nullptr;
    int32_t* dNext = nullptr;
    int32_t* dFails = nullptr;
    // Eight bytes of tail: descLoadBE32 reads the two aligned words that
    // straddle a text offset, so the last positions of the last text reach a
    // few bytes past it. Fetched, never used, but still has to be mapped.
    CK(cudaMalloc(&dTexts, (size_t)count * stride + 8));
    CK(cudaMalloc(&dLens, (size_t)count * 4));
    CK(cudaMalloc(&dSA, (size_t)count * stride * 4));
    CK(cudaMalloc(&dNext, 4));
    CK(cudaMalloc(&dFails, 4));
    CK(cudaMemcpy(dTexts, v.texts.data(), (size_t)count * stride, cudaMemcpyHostToDevice));
    CK(cudaMemcpy(dLens, v.lens.data(), (size_t)count * 4, cudaMemcpyHostToDevice));

    DoublePool dpool{};
    dpool.stride = stride;
    CK(cudaMalloc(&dpool.words, (size_t)blocks * stride * 6 * 4));
    CK(cudaMalloc(&dpool.keys, (size_t)blocks * stride * 2 * 8));

    DescPool cpool{};
    cpool.stride = stride;
    CK(cudaMalloc(&cpool.words, (size_t)blocks * stride * 2 * 8));
    CK(cudaMalloc(&cpool.arena, (size_t)blocks * stride * 4));
    CK(cudaMalloc(&cpool.offs, (size_t)blocks * stride * 4));
    CK(cudaMalloc(&cpool.mbuf, (size_t)blocks * stride * 4));

    printf("  %d resident blocks each; doubling %.1f MB per block, descriptor %.1f MB\n\n",
           blocks,
           (stride * 6.0 * 4 + stride * 2.0 * 8) / 1048576.0,
           (stride * 2.0 * 8 + stride * 4.0 * 3) / 1048576.0);

    std::vector<int32_t> got((size_t)count * stride);

    auto check = [&](const char* name) {
        CK(cudaMemcpy(got.data(), dSA, got.size() * 4, cudaMemcpyDeviceToHost));
        int bad = 0, firstBad = -1;
        for (int i = 0; i < count; i++) {
            for (int j = 0; j < v.lens[i]; j++) {
                if (got[(size_t)i * stride + j] != v.sa[(size_t)i * stride + j]) {
                    if (firstBad < 0) {
                        firstBad = i;
                        printf("  %s: first difference in vector %d at index %d:"
                               " got %d, want %d\n",
                               name, i, j, got[(size_t)i * stride + j],
                               v.sa[(size_t)i * stride + j]);
                    }
                    bad++;
                    break;
                }
            }
        }
        if (bad) {
            printf("  %-11s MISMATCH on %d of %d\n", name, bad, count);

            // Is it even a permutation? That splits the possible faults in two.
            // A permutation in the wrong order means the comparison or the merge
            // is wrong; anything else means the scatter wrote the wrong places,
            // and a position is missing or written twice.
            const int nv = v.lens[firstBad];
            std::vector<int> seen(nv, 0);
            int dup = 0, missing = 0;
            for (int j = 0; j < nv; j++) {
                const int val = got[(size_t)firstBad * stride + j];
                if (val < 0 || val >= nv) { dup++; continue; }
                if (seen[val]++) dup++;
            }
            for (int j = 0; j < nv; j++) if (!seen[j]) missing++;
            printf("  %-11s vector %d: %d duplicated or out of range, %d missing"
                   " -- %s\n",
                   name, firstBad, dup, missing,
                   (dup == 0 && missing == 0)
                       ? "a permutation, so the order is wrong"
                       : "not a permutation, so the scatter is wrong");
        } else {
            printf("  %-11s correct on all %d\n", name, count);
        }
        return bad == 0;
    };

    // ---- correctness ------------------------------------------------------
    CK(cudaMemset(dSA, 0, got.size() * 4));
    CK(cudaMemset(dNext, 0, 4));
    doubling_kernel<<<blocks, BR_BLOCK, BR_SHARED_BYTES>>>(
        dTexts, dLens, stride, dpool, count, dSA, dNext);
    CK(cudaDeviceSynchronize());
    CK(cudaGetLastError());
    const bool dblOK = check("doubling");

    CK(cudaMemset(dSA, 0, got.size() * 4));
    CK(cudaMemset(dNext, 0, 4));
    CK(cudaMemset(dFails, 0, 4));
    desc_kernel<<<blocks, BR_BLOCK, BR_SHARED_BYTES>>>(
        dTexts, dLens, stride, cpool, count, dSA, dNext, dFails);
    CK(cudaDeviceSynchronize());
    CK(cudaGetLastError());
    int32_t fails = 0;
    CK(cudaMemcpy(&fails, dFails, 4, cudaMemcpyDeviceToHost));
    const bool descOK = check("descriptor");
    if (fails) {
        printf("  descriptor  gave up on %d of %d\n", fails, count);
    }

    // ---- timing -----------------------------------------------------------
    double bestDbl = 1e30, bestDesc = 1e30;
    for (int r = 0; r < rounds; r++) {
        CK(cudaMemset(dNext, 0, 4));
        cudaEvent_t a, b;
        CK(cudaEventCreate(&a)); CK(cudaEventCreate(&b));
        CK(cudaEventRecord(a));
        doubling_kernel<<<blocks, BR_BLOCK, BR_SHARED_BYTES>>>(
            dTexts, dLens, stride, dpool, count, dSA, dNext);
        CK(cudaEventRecord(b));
        CK(cudaDeviceSynchronize());
        float ms = 0; CK(cudaEventElapsedTime(&ms, a, b));
        if (ms < bestDbl) bestDbl = ms;
        CK(cudaEventDestroy(a)); CK(cudaEventDestroy(b));
    }
    for (int r = 0; r < rounds; r++) {
        CK(cudaMemset(dNext, 0, 4));
        CK(cudaMemset(dFails, 0, 4));
        cudaEvent_t a, b;
        CK(cudaEventCreate(&a)); CK(cudaEventCreate(&b));
        CK(cudaEventRecord(a));
        desc_kernel<<<blocks, BR_BLOCK, BR_SHARED_BYTES>>>(
            dTexts, dLens, stride, cpool, count, dSA, dNext, dFails);
        CK(cudaEventRecord(b));
        CK(cudaDeviceSynchronize());
        float ms = 0; CK(cudaEventElapsedTime(&ms, a, b));
        if (ms < bestDesc) bestDesc = ms;
        CK(cudaEventDestroy(a)); CK(cudaEventDestroy(b));
    }

    printf("\n  %d texts, best of %d\n", count, rounds);
    printf("  %-12s %8.1f ms %10.0f SA/s   %s\n", "doubling", bestDbl,
           count / (bestDbl / 1000.0), dblOK ? "correct" : "WRONG");
    printf("  %-12s %8.1f ms %10.0f SA/s   %s   %+.1f%%\n", "descriptor", bestDesc,
           count / (bestDesc / 1000.0), descOK ? "correct" : "WRONG",
           (bestDbl / bestDesc - 1) * 100);
    printf("\n");
    return (dblOK && descOK) ? 0 : 1;
}
