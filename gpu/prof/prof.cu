// prof.cu -- SCAFFOLDING. Cycle attribution for the GPU suffix sort.
//
// Nsight Compute cannot read this machine's performance counters without a
// driver permission that needs admin, so the kernel times itself: thread 0 of
// each block samples clock64() at the phase boundaries and the deltas are
// accumulated per phase. See the note at the top of prof/blockradix.cuh.
//
// Absolute cycles mean little -- the marks add barriers where the code had none,
// and blocks share SMs so a block's elapsed cycles include its neighbour's work.
// The share each phase takes is what decides where to work next.
//
//   nvcc -O3 -arch=sm_120 -DBR_BLOCK=1024 -o gpu/prof/prof.exe gpu/prof/prof.cu
//   gpu\prof\prof.exe gpu\vectors.bin

#include <cstdio>
#include <cstdlib>
#include <cstdint>
#include <vector>
#include <cuda_runtime.h>

#include "../vectors_host.h"
#include "../sa_doubling.cuh"
#include "../desc.cuh"

#define CK(x) do { cudaError_t e=(x); if(e!=cudaSuccess){ \
  printf("CUDA error: %s (line %d)\n", cudaGetErrorString(e), __LINE__); exit(1);} } while(0)

struct Pool {
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

__global__ __launch_bounds__(BR_BLOCK) void prof_kernel(
        const uint8_t* texts, const int32_t* lens, int stride,
        Pool pool, int count, int32_t* saOut, int32_t* next)
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
        // The miner's path: the descriptor sort, with doubling as the fallback.
        DescScratch dsc;
        dsc.words  = (uint64_t*)sc.wordA;
        dsc.words2 = (uint64_t*)sc.wordB;
        dsc.arena  = (uint32_t*)sc.sa;
        dsc.offs   = sc.rank;
        dsc.mbuf   = sc.tmp;
        if (descSuffixArrayBlock(texts + (size_t)h * stride, n,
                                 saOut + (size_t)h * stride, dsc, sh) != 0) {
            saDoublingBlock(texts + (size_t)h * stride, n, sc, sh);
            for (int i = threadIdx.x; i < n; i += BR_BLOCK)
                saOut[(size_t)h * stride + i] = sc.sa[i];
        }
        __syncthreads();
    }
}

static const char* phName[PH_N] = {
    "histograms",
    "offset scan (1 thread)",
    "tile: rank / match_any",
    "tile: leader walk",
    "tile: scanBins",
    "tile: stage to shared",
    "tile: copy out",
    "tile: cursors",
    "round: build keys",
    "round: write sa back",
    "round: groupStarts+scanMax",
    "round: rank update",
    "round: flags+scanFlags",
    "round: compaction",
    // The descriptor sort's phases. Without these the table printed "(null)"
    // for every one of them, which is where the miner's time actually goes.
    "desc: run boundaries",
    "desc: column walk",
    "desc: tail descriptors",
    "desc: radix sort",
    "desc: offset scan",
    "desc: singleton scatter",
    "desc: merge collisions",
};

int main(int argc, char** argv)
{
    const char* path = argc > 1 ? argv[1] : "gpu/vectors.bin";
    Vectors v = loadVectors(path);

    cudaDeviceProp p;
    CK(cudaGetDeviceProperties(&p, 0));
    printf("\n  %s (sm_%d%d, %d SMs), BR_BLOCK=%d BR_BITS=%d\n",
           p.name, p.major, p.minor, p.multiProcessorCount, BR_BLOCK, BR_BITS);
    printf("  %d vectors, max text %d bytes, shared %.1f KB per block\n\n",
           v.count, v.maxLen, BR_SHARED_BYTES / 1024.0);

    CK(cudaFuncSetAttribute(prof_kernel,
                            cudaFuncAttributeMaxDynamicSharedMemorySize, BR_SHARED_BYTES));

    const int stride = v.maxLen;
    const int blocks = 2 * p.multiProcessorCount; // the resident maximum
    const int count  = v.count;

    uint8_t* dTexts = nullptr;
    int32_t* dLens = nullptr;
    int32_t* dSA = nullptr;
    int32_t* dNext = nullptr;
    Pool pool{};
    pool.stride = stride;

    CK(cudaMalloc(&dTexts, (size_t)count * stride));
    CK(cudaMalloc(&dLens, (size_t)count * 4));
    CK(cudaMalloc(&dSA, (size_t)count * stride * 4));
    CK(cudaMalloc(&dNext, 4));
    CK(cudaMalloc(&pool.words, (size_t)blocks * stride * 6 * 4));
    CK(cudaMalloc(&pool.keys, (size_t)blocks * stride * 2 * 8));
    CK(cudaMemcpy(dTexts, v.texts.data(), (size_t)count * stride, cudaMemcpyHostToDevice));
    CK(cudaMemcpy(dLens, v.lens.data(), (size_t)count * 4, cudaMemcpyHostToDevice));

    unsigned long long zero[PH_N] = {0}, zero1 = 0;
    CK(cudaMemcpyToSymbol(g_prof, zero, sizeof(zero)));
    CK(cudaMemcpyToSymbol(g_tiles, &zero1, 8));
    CK(cudaMemcpyToSymbol(g_rounds, &zero1, 8));
    CK(cudaMemset(dNext, 0, 4));

    cudaEvent_t a, b;
    CK(cudaEventCreate(&a));
    CK(cudaEventCreate(&b));
    CK(cudaEventRecord(a));
    prof_kernel<<<blocks, BR_BLOCK, BR_SHARED_BYTES>>>(
        dTexts, dLens, stride, pool, count, dSA, dNext);
    CK(cudaEventRecord(b));
    CK(cudaDeviceSynchronize());
    CK(cudaGetLastError());
    float ms = 0;
    CK(cudaEventElapsedTime(&ms, a, b));

    // A profiled kernel that computes the wrong answer is profiling the wrong
    // thing, so check before reporting anything.
    std::vector<int32_t> got((size_t)count * stride);
    CK(cudaMemcpy(got.data(), dSA, got.size() * 4, cudaMemcpyDeviceToHost));
    int bad = 0;
    for (int i = 0; i < count; i++) {
        for (int j = 0; j < v.lens[i]; j++) {
            if (got[(size_t)i * stride + j] != v.sa[(size_t)i * stride + j]) { bad++; break; }
        }
    }
    printf("  %s\n\n", bad ? "SUFFIX ARRAY MISMATCH -- the numbers below mean nothing"
                           : "suffix arrays match the CPU exactly");

    unsigned long long prof[PH_N], tiles, rounds;
    CK(cudaMemcpyFromSymbol(prof, g_prof, sizeof(prof)));
    CK(cudaMemcpyFromSymbol(&tiles, g_tiles, 8));
    CK(cudaMemcpyFromSymbol(&rounds, g_rounds, 8));

    unsigned long long tot = 0;
    for (int i = 0; i < PH_N; i++) tot += prof[i];
    if (tot == 0) tot = 1;

    printf("  %.0f ms for %d suffix arrays, %.0f/s\n", ms, count, count / (ms / 1000.0));
    printf("  %llu tiles, %llu doubling rounds, %.1f tiles per round\n\n",
           tiles, rounds, rounds ? (double)tiles / rounds : 0.0);

    printf("  %-30s %16s %8s\n", "phase", "cycles", "share");
    for (int i = 0; i < PH_N; i++) {
        printf("  %-30s %16llu %7.1f%%\n", phName[i], prof[i], 100.0 * prof[i] / tot);
    }
    printf("  %-30s %16llu\n\n", "total attributed", tot);
    return bad ? 1 : 0;
}
