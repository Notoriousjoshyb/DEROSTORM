// overlap.cu -- can stage 1 and the suffix sort run at the same time?
//
// The three kernels run one after another on one stream, and two of them are
// now worth hiding: stage 1 is 14.7% of GPU time and the SHA check 7.5%, against
// 77.8% for the suffix sort. They want different things from an SM -- stage 1 is
// capped at 192 threads by 516 bytes of shared memory a thread and spends its
// life waiting on dependent 64-bit multiplies, while the suffix sort runs 4
// blocks of 256 threads on 11 KB each and waits on memory -- so in principle a
// batch's stage 1 could run underneath the previous batch's suffix sort for
// free.
//
// In principle. Two blocks that both want shared memory are competing for one
// 100 KB pool, and if hosting stage 1 costs the suffix sort a resident block
// then the overlap is paid for out of the thing it was meant to help. That is a
// measurement, not an argument, and it is the one that decides whether the
// double-buffered chunk in derostorm_gpu.cu is worth building.
//
// Reports the two kernels alone and then together on two streams. Perfect
// overlap is max(alone); no overlap is the sum.
//
//   gpu\overlap.bat
//   gpu\overlap.exe [hashes]

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cuda_runtime.h>

#include "stage1.cuh"
#include "sa_doubling.cuh"
#include "desc.cuh"

#define S1_STRIDE 516
#define S1_BLOCK  64
#define DSG_WORK_SIZE 48

#define CK(x) do { cudaError_t e_ = (x); if (e_ != cudaSuccess) { \
    printf("CUDA error %s at line %d\n", cudaGetErrorString(e_), __LINE__); exit(1); } } while (0)

struct Pool {
    int32_t*  words;
    uint64_t* keys;
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
        s.sa = words + w; s.rank = s.sa + ASTRO_MAX_TEXT; s.tmp = s.rank + ASTRO_MAX_TEXT;
        s.tmp2 = s.tmp + ASTRO_MAX_TEXT; s.act = s.tmp2 + ASTRO_MAX_TEXT; s.act2 = s.act + ASTRO_MAX_TEXT;
        s.wordA = keys + k; s.wordB = s.wordA + ASTRO_MAX_TEXT;
        return s;
    }
};

__global__ void stage1_kernel(const uint8_t* work, uint32_t base, int count,
                              uint8_t* texts, int32_t* lens)
{
    extern __shared__ uint8_t tile[];
    const int i = blockIdx.x * blockDim.x + threadIdx.x;
    if (i >= count) return;
    uint8_t* mine = tile + (size_t)threadIdx.x * S1_STRIDE;

    uint8_t w[DSG_WORK_SIZE];
    for (int k = 0; k < DSG_WORK_SIZE; k++) w[k] = work[k];
    const uint32_t nonce = base + (uint32_t)i;
    w[43] = (uint8_t)(nonce >> 24); w[44] = (uint8_t)(nonce >> 16);
    w[45] = (uint8_t)(nonce >> 8);  w[46] = (uint8_t)(nonce);

    lens[i] = (int32_t)astroStage1(w, DSG_WORK_SIZE,
                                   texts + (size_t)i * ASTRO_MAX_TEXT,
                                   mine, mine + 256);
}

__global__ void suffix_kernel(const uint8_t* texts, const int32_t* lens, int count,
                              Pool pool, int32_t* sa, int32_t* next)
{
    for (;;) {
        __shared__ int slot;
        if (threadIdx.x == 0) slot = atomicAdd(next, 1);
        __syncthreads();
        const int i = slot;
        if (i >= count) return;
        extern __shared__ uint8_t sh[];
        descSuffixArrayBlock(texts + (size_t)i * ASTRO_MAX_TEXT, lens[i],
                             sa + (size_t)i * ASTRO_MAX_TEXT,
                             pool.descMine(blockIdx.x), (BlockRadixScratch*)sh);
        __syncthreads();
    }
}

__global__ void sha_kernel(const int32_t* sa, const int32_t* lens, int count, uint8_t* out)
{
    const int i = blockIdx.x * blockDim.x + threadIdx.x;
    if (i >= count) return;
    sha256(out + (size_t)i * 32, (const uint8_t*)(sa + (size_t)i * ASTRO_MAX_TEXT), lens[i] * 4);
}

static float timeIt(cudaEvent_t a, cudaEvent_t b) { float ms = 0; cudaEventElapsedTime(&ms, a, b); return ms; }

int main(int argc, char** argv)
{
    const int count = argc > 1 ? atoi(argv[1]) : 2048;

    cudaDeviceProp p;
    CK(cudaGetDeviceProperties(&p, 0));
    CK(cudaFuncSetAttribute(suffix_kernel, cudaFuncAttributeMaxDynamicSharedMemorySize, DESC_LAUNCH_SHARED));
    CK(cudaFuncSetAttribute(stage1_kernel, cudaFuncAttributeMaxDynamicSharedMemorySize, S1_BLOCK * S1_STRIDE));

    const int blocks = p.multiProcessorCount * 4;
    printf("  %s, %d SMs, %d suffix blocks, %d hashes\n\n", p.name, p.multiProcessorCount, blocks, count);

    uint8_t *dWork, *texts, *texts2;
    int32_t *lens, *lens2, *sa, *next;
    int32_t *words; uint64_t *keys;
    CK(cudaMalloc(&dWork, DSG_WORK_SIZE));
    CK(cudaMalloc(&texts, (size_t)count * ASTRO_MAX_TEXT + 8));
    CK(cudaMalloc(&texts2, (size_t)count * ASTRO_MAX_TEXT + 8));
    CK(cudaMalloc(&lens, (size_t)count * 4));
    CK(cudaMalloc(&lens2, (size_t)count * 4));
    CK(cudaMalloc(&sa, (size_t)count * ASTRO_MAX_TEXT * 4));
    CK(cudaMalloc(&next, 4));
    CK(cudaMalloc(&words, (size_t)blocks * ASTRO_MAX_TEXT * 6 * 4));
    CK(cudaMalloc(&keys, (size_t)blocks * ASTRO_MAX_TEXT * 2 * 8));
    Pool pool; pool.words = words; pool.keys = keys;

    uint8_t w[DSG_WORK_SIZE] = {0};
    w[0] = 1;
    for (int i = 8; i < 43; i++) w[i] = (uint8_t)(i * 7 + 29);
    CK(cudaMemcpy(dWork, w, DSG_WORK_SIZE, cudaMemcpyHostToDevice));

    cudaStream_t s1, s2;
    CK(cudaStreamCreateWithFlags(&s1, cudaStreamNonBlocking));
    CK(cudaStreamCreateWithFlags(&s2, cudaStreamNonBlocking));
    cudaEvent_t e0, e1, e2, e3;
    CK(cudaEventCreate(&e0)); CK(cudaEventCreate(&e1));
    CK(cudaEventCreate(&e2)); CK(cudaEventCreate(&e3));

    const int s1grid = (count + S1_BLOCK - 1) / S1_BLOCK;
    const int s1shared = S1_BLOCK * S1_STRIDE;

    // Fill texts once so the suffix sort has real work.
    stage1_kernel<<<s1grid, S1_BLOCK, s1shared>>>(dWork, 0, count, texts, lens);
    CK(cudaDeviceSynchronize());

    float bs1 = 1e9f, bsuf = 1e9f, bboth = 1e9f;
    for (int rep = 0; rep < 5; rep++) {
        // stage 1 alone
        CK(cudaEventRecord(e0));
        stage1_kernel<<<s1grid, S1_BLOCK, s1shared>>>(dWork, 1000, count, texts2, lens2);
        CK(cudaEventRecord(e1));
        CK(cudaEventSynchronize(e1));
        const float t1 = timeIt(e0, e1);

        // suffix alone
        CK(cudaMemset(next, 0, 4));
        CK(cudaEventRecord(e0));
        suffix_kernel<<<blocks, BR_BLOCK, DESC_LAUNCH_SHARED>>>(texts, lens, count, pool, sa, next);
        CK(cudaEventRecord(e1));
        CK(cudaEventSynchronize(e1));
        const float t2 = timeIt(e0, e1);

        // both, on two streams
        CK(cudaMemset(next, 0, 4));
        CK(cudaEventRecord(e0));
        suffix_kernel<<<blocks, BR_BLOCK, DESC_LAUNCH_SHARED, s1>>>(texts, lens, count, pool, sa, next);
        stage1_kernel<<<s1grid, S1_BLOCK, s1shared, s2>>>(dWork, 2000, count, texts2, lens2);
        CK(cudaEventRecord(e2, s1));
        CK(cudaEventRecord(e3, s2));
        CK(cudaStreamSynchronize(s1));
        CK(cudaStreamSynchronize(s2));
        const float t3 = timeIt(e0, e2) > timeIt(e0, e3) ? timeIt(e0, e2) : timeIt(e0, e3);

        if (t1 < bs1) bs1 = t1;
        if (t2 < bsuf) bsuf = t2;
        if (t3 < bboth) bboth = t3;
    }

    printf("  stage 1 alone      %8.2f ms\n", bs1);
    printf("  suffix alone       %8.2f ms\n", bsuf);
    printf("  both, two streams  %8.2f ms\n\n", bboth);
    printf("  sum (no overlap)   %8.2f ms\n", bs1 + bsuf);
    printf("  max (free overlap) %8.2f ms\n", bs1 > bsuf ? bs1 : bsuf);
    printf("  stage 1 hidden     %8.1f%%\n",
           100.0 * (bs1 + bsuf - bboth) / bs1);

    // And the SHA check, which reads the suffix arrays the sort just wrote.
    // Both are memory-hungry, so this one is far less obviously free.
    uint8_t* dHash;
    CK(cudaMalloc(&dHash, (size_t)count * 32));
    float bsha = 1e9f, bboth2 = 1e9f;
    const int shaGrid = (count + 255) / 256;
    for (int rep = 0; rep < 5; rep++) {
        CK(cudaEventRecord(e0));
        sha_kernel<<<shaGrid, 256>>>(sa, lens, count, dHash);
        CK(cudaEventRecord(e1));
        CK(cudaEventSynchronize(e1));
        const float t1 = timeIt(e0, e1);

        CK(cudaMemset(next, 0, 4));
        CK(cudaEventRecord(e0));
        suffix_kernel<<<blocks, BR_BLOCK, DESC_LAUNCH_SHARED, s1>>>(texts, lens, count, pool, sa, next);
        sha_kernel<<<shaGrid, 256, 0, s2>>>(sa, lens, count, dHash);
        CK(cudaEventRecord(e2, s1));
        CK(cudaEventRecord(e3, s2));
        CK(cudaStreamSynchronize(s1));
        CK(cudaStreamSynchronize(s2));
        const float t3 = timeIt(e0, e2) > timeIt(e0, e3) ? timeIt(e0, e2) : timeIt(e0, e3);
        if (t1 < bsha) bsha = t1;
        if (t3 < bboth2) bboth2 = t3;
    }
    printf("\n  sha alone          %8.2f ms\n", bsha);
    printf("  suffix + sha       %8.2f ms\n", bboth2);
    printf("  sha hidden         %8.1f%%\n", 100.0 * (bsha + bsuf - bboth2) / bsha);

    return 0;
}
