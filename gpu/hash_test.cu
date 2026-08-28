// hash_test.cu -- the whole AstroBWTv3 hash on the GPU, checked then timed.
//
// Three stages, checked in order, because a failure in an early one makes the
// later comparisons meaningless:
//
//   1. stage 1     the 256-way op switch -> text, diffed against Go's text
//   2. suffix sort sais.cuh, already proven by sais_test, re-run here in place
//   3. SHA-256     of the suffix array bytes -> the final hash, diffed against
//                  Go's AstroBWTv3 output
//
// Passing stage 3 on real vectors means the port is exact: every primitive
// (salsa20, rc4, fnv1a, xxhash, siphash, sha256) and every one of the 256 op
// cases has been exercised by 512 real nonces.
//
//   nvcc -O3 -arch=sm_120 -o gpu/hash_test.exe gpu/hash_test.cu
//   gpu\hash_test.exe [vectors.bin]

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <vector>
#include <cuda_runtime.h>

#include "vectors_host.h"
#include "stage1.cuh"
#include "sais.cuh"

#define CK(x) do { cudaError_t e=(x); if(e!=cudaSuccess){ \
  printf("CUDA error: %s (line %d)\n", cudaGetErrorString(e), __LINE__); exit(1);} } while(0)

// Shared-memory tile for stage 1: step_3 and the RC4 permutation, 256 bytes
// each, per thread.
//
// The 4 bytes of padding are what makes it worth doing. A shared bank is picked
// by (byte address / 4) % 32, so with a 512-byte stride every lane in a warp
// would land on the same bank for the same index and serialise 32 ways. 516 is
// 4 mod 128, which shifts lane t along by exactly one bank and spreads them.
#define S1_STRIDE 516

// Per-thread scratch, laid out as one struct of pointers so the two kernels
// agree on it without repeating the arithmetic.
struct Scratch {
    uint8_t*  text;     // ASTRO_MAX_TEXT
    int32_t*  sa;       // ASTRO_MAX_TEXT + 1
    uint32_t* types;    // tWords
    int32_t*  bkt;      // bktSize
    int       tWords;
    int       bktSize;

    __device__ __forceinline__ uint8_t*  myText(int tid) const { return text + (size_t)tid * ASTRO_MAX_TEXT; }
    __device__ __forceinline__ int32_t*  mySA(int tid)   const { return sa + (size_t)tid * (ASTRO_MAX_TEXT + 1); }
    __device__ __forceinline__ uint32_t* myT(int tid)    const { return types + (size_t)tid * tWords; }
    __device__ __forceinline__ int32_t*  myBkt(int tid)  const { return bkt + (size_t)tid * bktSize; }
};

// Stage 1 only, so its output can be diffed before the suffix sort runs on it.
__global__ void stage1_kernel(const uint8_t* inputs, int nvec, int threads,
                              Scratch sc, int32_t* lensOut)
{
    int tid = blockIdx.x * blockDim.x + threadIdx.x;
    if (tid >= threads) return;
    int v = tid % nvec;

    extern __shared__ uint8_t tile[];
    uint8_t* mine = tile + (size_t)threadIdx.x * S1_STRIDE;

    lensOut[tid] = (int32_t)astroStage1(inputs + (size_t)v * ASTRO_INPUT_LEN,
                                        ASTRO_INPUT_LEN, sc.myText(tid),
                                        mine, mine + 256);
}

// The whole hash.
__global__ void hash_kernel(const uint8_t* inputs, int nvec, int threads,
                            Scratch sc, uint8_t* hashOut)
{
    int tid = blockIdx.x * blockDim.x + threadIdx.x;
    if (tid >= threads) return;
    int v = tid % nvec;

    extern __shared__ uint8_t tile[];
    uint8_t* mine = tile + (size_t)threadIdx.x * S1_STRIDE;

    uint8_t* text = sc.myText(tid);
    int32_t* sa   = sc.mySA(tid);

    int n = (int)astroStage1(inputs + (size_t)v * ASTRO_INPUT_LEN, ASTRO_INPUT_LEN,
                             text, mine, mine + 256);
    int32_t* out = saisSuffixArray(text, n, sa, sc.myT(tid), sc.myBkt(tid));

    // Go hashes the raw little-endian int32 suffix array. The GPU is little
    // endian too, so the array is already in that layout.
    sha256(hashOut + (size_t)tid * 32, (const uint8_t*)out, n * 4);
}

// timeKernel runs f once to warm up, then times a second launch. Returns ms.
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
    printf("\n  CUDA AstroBWTv3 whole hash vs CPU\n\n");
    printf("  device     %s (sm_%d%d, %d SMs)\n", p.name, p.major, p.minor, p.multiProcessorCount);
    printf("  vectors    %d, max text %d bytes%s\n", v.count, v.maxLen,
           v.haveHash ? "" : "  (old format: no final-hash check)");

    // Scratch is sized for the worst case the algorithm can produce, not for
    // the longest text in this file, because a mining kernel gets nonces it has
    // not seen and must not overflow on a longer one.
    const int tWords  = SAIS_T_WORDS(ASTRO_MAX_TEXT);
    const int bktSize = SAIS_BKT(ASTRO_MAX_TEXT);
    const size_t perThread = (size_t)ASTRO_MAX_TEXT
                           + (size_t)(ASTRO_MAX_TEXT + 1) * 4
                           + (size_t)tWords * 4 + (size_t)bktSize * 4 + 32 + 4;
    printf("  per hash   %.0f KB  (text %.0f + sa %.0f + types %.0f + buckets %.0f)\n\n",
           perThread / 1024.0, ASTRO_MAX_TEXT / 1024.0, (ASTRO_MAX_TEXT + 1) * 4 / 1024.0,
           tWords * 4 / 1024.0, bktSize * 4 / 1024.0);

    // SA-IS recurses ~17 levels; the default device stack is not sized for it.
    CK(cudaDeviceSetLimit(cudaLimitStackSize, 8 * 1024));

    size_t freeB, totB; CK(cudaMemGetInfo(&freeB, &totB));
    size_t budget = freeB > (size_t)1e9 ? freeB - (size_t)1e9 : 0;
    int maxThreads = (int)(budget / perThread);
    maxThreads -= maxThreads % 64;
    if (maxThreads < v.count) {
        printf("  not enough VRAM: fits %d threads, need %d for the check\n", maxThreads, v.count);
        return 1;
    }
    printf("  VRAM allows %d concurrent hashes (%.1f GB free)\n\n", maxThreads, freeB / 1e9);

    uint8_t* dIn = nullptr; uint8_t* dHash = nullptr; int32_t* dLens = nullptr;
    Scratch sc{}; sc.tWords = tWords; sc.bktSize = bktSize;
    CK(cudaMalloc(&dIn, v.inputs.size()));
    CK(cudaMalloc(&dHash, (size_t)maxThreads * 32));
    CK(cudaMalloc(&dLens, (size_t)maxThreads * 4));
    CK(cudaMalloc(&sc.text, (size_t)maxThreads * ASTRO_MAX_TEXT));
    CK(cudaMalloc(&sc.sa, (size_t)maxThreads * (ASTRO_MAX_TEXT + 1) * 4));
    CK(cudaMalloc(&sc.types, (size_t)maxThreads * tWords * 4));
    CK(cudaMalloc(&sc.bkt, (size_t)maxThreads * bktSize * 4));
    CK(cudaMemcpy(dIn, v.inputs.data(), v.inputs.size(), cudaMemcpyHostToDevice));

    const int block = 64;
    const int grid = (v.count + block - 1) / block;
    printf("  shared     %d bytes/block for stage 1 (%d threads x %d)\n\n",
           block * S1_STRIDE, block, S1_STRIDE);

    // ---- 1. stage 1 ------------------------------------------------------
    stage1_kernel<<<grid, block, block * S1_STRIDE>>>(dIn, v.count, v.count, sc, dLens);
    CK(cudaDeviceSynchronize());
    CK(cudaGetLastError());

    std::vector<int32_t> gotLens(v.count);
    CK(cudaMemcpy(gotLens.data(), dLens, (size_t)v.count * 4, cudaMemcpyDeviceToHost));

    int badLen = 0, badText = 0, firstBad = -1;
    long long firstIdx = -1;
    std::vector<uint8_t> gotText(ASTRO_MAX_TEXT);
    for (int i = 0; i < v.count; i++) {
        if (gotLens[i] != v.lens[i]) {
            if (firstBad < 0) firstBad = i;
            badLen++;
            continue;
        }
        CK(cudaMemcpy(gotText.data(), sc.text + (size_t)i * ASTRO_MAX_TEXT,
                      v.lens[i], cudaMemcpyDeviceToHost));
        const uint8_t* want = v.texts.data() + (size_t)i * v.maxLen;
        for (int k = 0; k < v.lens[i]; k++) {
            if (gotText[k] != want[k]) {
                if (firstBad < 0) { firstBad = i; firstIdx = k; }
                badText++;
                break;
            }
        }
    }
    if (badLen || badText) {
        printf("  STAGE 1 MISMATCH: %d wrong length, %d wrong bytes (of %d)\n",
               badLen, badText, v.count);
        if (firstBad >= 0) {
            printf("  first at vector %d: gpu len %d, cpu len %d",
                   firstBad, gotLens[firstBad], v.lens[firstBad]);
            if (firstIdx >= 0) printf(", first differing byte at %lld", firstIdx);
            printf("\n");
        }
        return 1;
    }
    printf("  stage 1   CORRECT: all %d texts match the CPU exactly\n", v.count);

    // ---- 2 and 3. suffix sort and final SHA-256 --------------------------
    hash_kernel<<<grid, block, block * S1_STRIDE>>>(dIn, v.count, v.count, sc, dHash);
    CK(cudaDeviceSynchronize());
    CK(cudaGetLastError());

    if (v.haveHash) {
        std::vector<uint8_t> gotHash((size_t)v.count * 32);
        CK(cudaMemcpy(gotHash.data(), dHash, gotHash.size(), cudaMemcpyDeviceToHost));
        int badHash = 0, firstH = -1;
        for (int i = 0; i < v.count; i++) {
            if (memcmp(gotHash.data() + (size_t)i * 32, v.hash.data() + (size_t)i * 32, 32)) {
                if (firstH < 0) firstH = i;
                badHash++;
            }
        }
        if (badHash) {
            char a[65], b[65];
            hex(gotHash.data() + (size_t)firstH * 32, a);
            hex(v.hash.data() + (size_t)firstH * 32, b);
            printf("  HASH MISMATCH: %d of %d differ\n", badHash, v.count);
            printf("  first at vector %d\n    gpu %s\n    cpu %s\n", firstH, a, b);
            return 1;
        }
        printf("  full hash CORRECT: all %d hashes match the CPU exactly\n\n", v.count);
    } else {
        printf("  full hash NOT CHECKED: regenerate vectors.bin for the hash field\n\n");
    }

    // ---- throughput ------------------------------------------------------
    // Best of three. A single timing on this card swings by up to 2x between
    // runs -- the kernels are seconds long and the boost clock drops under
    // sustained load -- so one sample says more about thermals than about the
    // code. Best-of rejects the throttled samples.
    //
    // Stage 1 is timed on its own as well. On the CPU it is ~7% of a hash; if
    // it is a much larger share here, that is where the GPU work belongs, and
    // no amount of suffix-sort tuning would show up.
    struct Row { int t; double stage1; double full; };
    std::vector<Row> rows;

    std::vector<int> sweep;
    for (int t = 512; t <= maxThreads; t *= 2) sweep.push_back(t);
    if (sweep.empty() || sweep.back() != maxThreads) sweep.push_back(maxThreads);

    printf("  %-9s %12s %12s %10s\n", "threads", "stage1 H/s", "full H/s", "ms");
    double best = 0; int bestT = 0;
    for (int t : sweep) {
        int g2 = (t + block - 1) / block;
        double bs1 = 0, bfull = 0, bms = 0;

        for (int rep = 0; rep < 3; rep++) {
            float ms1 = timeKernel([&]{ stage1_kernel<<<g2, block, block * S1_STRIDE>>>(dIn, v.count, t, sc, dLens); });
            float msF = timeKernel([&]{ hash_kernel<<<g2, block, block * S1_STRIDE>>>(dIn, v.count, t, sc, dHash); });
            double h1 = t / (ms1 / 1000.0), hF = t / (msF / 1000.0);
            if (h1 > bs1) bs1 = h1;
            if (hF > bfull) { bfull = hF; bms = msF; }
        }

        printf("  %-9d %12.0f %12.0f %10.1f\n", t, bs1, bfull, bms);
        rows.push_back({t, bs1, bfull});
        if (bfull > best) { best = bfull; bestT = t; }
        fflush(stdout);
    }

    // Share of a hash spent in stage 1, from the two rates at the best point.
    double s1rate = 0;
    for (const Row& r : rows) if (r.t == bestT) s1rate = r.stage1;
    if (s1rate > 0) {
        printf("\n  stage 1 is %.0f%% of a GPU hash (it is ~7%% of a CPU hash)\n",
               best / s1rate * 100.0);
    }
    printf("  best %.0f H/s at %d threads\n", best, bestT);
    printf("  CPU reference (9800X3D, 14 threads): 4622 H/s -> %.2fx\n", best / 4622.0);
    printf("  combined CPU+GPU would be %.0f H/s (%.2fx)\n\n",
           best + 4622.0, (best + 4622.0) / 4622.0);

    CK(cudaFree(dIn)); CK(cudaFree(dHash)); CK(cudaFree(dLens));
    CK(cudaFree(sc.text)); CK(cudaFree(sc.sa)); CK(cudaFree(sc.types)); CK(cudaFree(sc.bkt));
    return 0;
}
