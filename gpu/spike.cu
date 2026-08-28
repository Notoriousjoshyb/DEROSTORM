// spike.cu -- does an RTX-class GPU have any chance at AstroBWTv3?
//
// 93% of an AstroBWTv3 hash is a SA-IS suffix sort over ~68 KB of RC4-ish
// noise, and the bulk of SA-IS is its induce passes. This measures the induce
// inner loop at the occupancy the memory footprint allows, instead of porting
// 1400 lines of SA-IS to find out whether it was ever worth porting.
//
// The loop below is induceSubL_8_32 from sais.go, transcribed:
//
//   j := sa[i]                          streaming read
//   if j == 0 { continue }              most entries, early in a pass
//   if j < 0  { sa[i] = -j; continue }
//   sa[i] = 0                           streaming write
//   k := j - 1
//   c0, c1 := text[k-1], text[k]        RANDOM read into the 68 KB text
//   if c0 < c1 { k = -k }
//   if cB != c1 { bucket[cB] = b; cB = c1; b = bucket[cB] }   register-cached
//   sa[b] = k; b++                      scatter write into one of 256 streams
//
// Two properties decide the whole question:
//
//   1. Each thread owns a private sa[] and text[]. Neighbouring lanes touch
//      addresses 336 KB apart, so NOT ONE access coalesces -- every one is its
//      own 32-byte sector fetch, 8x the bytes actually wanted.
//   2. That private footprint caps how many hashes fit in VRAM, which caps
//      occupancy -- and occupancy is exactly what a GPU uses to hide the
//      latency of uncoalesced access. The two constraints fight each other.
//
// The bucket cursor is cached in registers by the real code (cB/b above), so
// bucket[] is touched only on a character change. That is modelled, because
// getting it wrong would make the GPU look far worse than it really is.
//
//   nvcc -O3 -arch=sm_120 -o gpu/spike.exe gpu/spike.cu

#include <cstdio>
#include <cstdint>
#include <cuda_runtime.h>

#define CK(x) do { cudaError_t e=(x); if(e!=cudaSuccess){ \
  printf("CUDA error %s at line %d\n", cudaGetErrorString(e), __LINE__); exit(1);} } while(0)

// Mean text length over 512 real vectors (gpu/vectors.bin).
static const int N = 68754;

// Induce passes per hash. Level 0 runs four (induceSubL, induceSubS, induceL,
// induceS). The recursion runs four more on ~n/2, then ~n/4, and so on; that
// geometric tail is worth about one more level-0-sized set. Eight n-sized
// passes per hash is the generous-to-the-GPU figure used to convert steps
// back into hashes.
static const double PASSES_PER_HASH = 8.0;

__global__ void fill_kernel(int32_t* sa, uint8_t* text, int threads, int n)
{
    int t = blockIdx.x * blockDim.x + threadIdx.x;
    if (t >= threads) return;
    int32_t* s = sa + (size_t)t * n;
    uint8_t* x = text + (size_t)t * n;
    uint32_t r = (uint32_t)t * 2654435761u + 1u;
    for (int i = 0; i < n; i++) {
        r ^= r << 13; r ^= r >> 17; r ^= r << 5;
        x[i] = (uint8_t)r;
        // Entry mix matching what placeLMS leaves behind: roughly a third of
        // the array carries work, the rest is empty.
        uint32_t m = r % 3u;
        int32_t v = (int32_t)(2u + r % (uint32_t)(n - 3));
        s[i] = (m == 0) ? 0 : ((m == 1) ? v : -v);
    }
}

// One thread = one hash. This is the induce inner loop, faithfully.
__global__ __launch_bounds__(128) void induce_kernel(
    int32_t* __restrict__ saBase, const uint8_t* __restrict__ textBase,
    int32_t* __restrict__ bucketBase, int n, int passes, uint32_t* sink)
{
    const size_t tid = (size_t)blockIdx.x * blockDim.x + threadIdx.x;
    int32_t* sa = saBase + tid * n;
    const uint8_t* text = textBase + tid * n;
    int32_t* bucket = bucketBase + tid * 256;

    for (int c = 0; c < 256; c++) bucket[c] = c * (n / 256);

    uint32_t acc = 0;
    for (int p = 0; p < passes; p++) {
        int cB = 0;
        int32_t b = bucket[0];
        for (int i = 0; i < n; i++) {
            int32_t j = sa[i];
            if (j == 0) continue;
            if (j < 0) { sa[i] = -j; continue; }
            sa[i] = 0;

            int32_t k = j - 1;
            if (k < 1) k = 1; else if (k >= n) k = n - 1;
            uint8_t c0 = text[k - 1], c1 = text[k];   // the random text read
            int32_t kk = (c0 < c1) ? -k : k;

            if (cB != (int)c1) { bucket[cB] = b; cB = (int)c1; b = bucket[cB]; }
            int32_t bb = b;
            if (bb >= n) bb -= n;
            sa[bb] = kk;
            b = bb + 1;
            acc += (uint32_t)c1;
        }
        bucket[cB] = b;
    }
    sink[tid] = acc;
}

// Same footprint and same pass count, but purely sequential. The gap between
// this and induce_kernel is what the scatter and the random text read cost.
__global__ __launch_bounds__(128) void stream_kernel(
    int32_t* __restrict__ saBase, const uint8_t* __restrict__ textBase,
    int32_t* __restrict__ bucketBase, int n, int passes, uint32_t* sink)
{
    (void)bucketBase;
    const size_t tid = (size_t)blockIdx.x * blockDim.x + threadIdx.x;
    int32_t* sa = saBase + tid * n;
    const uint8_t* text = textBase + tid * n;
    uint32_t acc = 0;
    for (int p = 0; p < passes; p++)
        for (int i = 0; i < n; i++) { acc += (uint32_t)sa[i] + text[i]; sa[i] = (int32_t)acc; }
    sink[tid] = acc;
}

typedef void (*Kernel)(int32_t*, const uint8_t*, int32_t*, int, int, uint32_t*);

static double runKernel(Kernel k, int32_t* sa, uint8_t* text, int32_t* bucket,
                        int threads, int passes, uint32_t* sink, double* msOut)
{
    const int block = 128;
    const int grid = (threads + block - 1) / block;

    // Both kernels rewrite sa, so restore the placeLMS-shaped entry mix before
    // every timed run. Otherwise a preceding stream_kernel leaves arbitrary
    // int32 in sa and induce_kernel derives out-of-range text offsets from it.
    fill_kernel<<<grid, block>>>(sa, text, threads, N);
    CK(cudaDeviceSynchronize());

    k<<<grid, block>>>(sa, text, bucket, N, 1, sink);   // warm up
    CK(cudaDeviceSynchronize());
    fill_kernel<<<grid, block>>>(sa, text, threads, N);
    CK(cudaDeviceSynchronize());

    cudaEvent_t a, b; CK(cudaEventCreate(&a)); CK(cudaEventCreate(&b));
    CK(cudaEventRecord(a));
    k<<<grid, block>>>(sa, text, bucket, N, passes, sink);
    CK(cudaEventRecord(b));
    CK(cudaEventSynchronize(b));
    CK(cudaGetLastError());
    float ms = 0; CK(cudaEventElapsedTime(&ms, a, b));
    CK(cudaEventDestroy(a)); CK(cudaEventDestroy(b));

    if (msOut) *msOut = ms;
    // threads*passes n-sized passes done; PASSES_PER_HASH of them make a hash.
    return ((double)threads * passes / PASSES_PER_HASH) / (ms / 1000.0);
}

int main()
{
    cudaDeviceProp p; CK(cudaGetDeviceProperties(&p, 0));
    size_t freeB, totB; CK(cudaMemGetInfo(&freeB, &totB));
    const int resident = p.maxThreadsPerMultiProcessor * p.multiProcessorCount;
    const size_t perHash = (size_t)N * 4 + (size_t)N + 256 * 4;   // sa + text + bucket

    printf("\n  GPU spike - AstroBWTv3 induce pass, one hash per thread\n\n");
    printf("  device         %s (sm_%d%d, %d SMs)\n", p.name, p.major, p.minor, p.multiProcessorCount);
    printf("  VRAM free      %.1f GB of %.1f GB\n", freeB / 1e9, totB / 1e9);
    printf("  resident cap   %d threads (%d per SM)\n", resident, p.maxThreadsPerMultiProcessor);
    printf("  L2 cache       %.0f MB\n", p.l2CacheSize / 1048576.0);
    printf("  per hash       %.0f KB  (sa %.0f KB + text %.0f KB)  n = %d\n\n",
           perHash / 1024.0, N * 4 / 1024.0, N / 1024.0, N);

    size_t budget = freeB > (size_t)1e9 ? freeB - (size_t)1e9 : freeB / 2;
    int maxThreads = (int)(budget / perHash);
    maxThreads -= maxThreads % 128;
    printf("  VRAM allows %d concurrent hashes = %.0f%% of resident cap\n\n",
           maxThreads, 100.0 * maxThreads / resident);

    int32_t* sa = nullptr; uint8_t* text = nullptr; int32_t* bucket = nullptr; uint32_t* sink = nullptr;
    CK(cudaMalloc(&sa, (size_t)maxThreads * N * 4));
    CK(cudaMalloc(&text, (size_t)maxThreads * N));
    CK(cudaMalloc(&bucket, (size_t)maxThreads * 256 * 4));
    CK(cudaMalloc(&sink, (size_t)maxThreads * 4));
    fill_kernel<<<(unsigned)((maxThreads + 127) / 128), 128>>>(sa, text, maxThreads, N);
    CK(cudaDeviceSynchronize());
    CK(cudaGetLastError());

    printf("  %-9s %-6s %14s %14s %10s\n", "threads", "occ", "induce H/s", "stream H/s", "ms");
    double best = 0; int bestT = 0;
    for (int t = 1024; t <= maxThreads; t *= 2) {
        int passes = t < 8192 ? 4 : 2;
        double ms = 0;
        double ind = runKernel(induce_kernel, sa, text, bucket, t, passes, sink, &ms);
        double str = runKernel(stream_kernel, sa, text, bucket, t, passes, sink, nullptr);
        printf("  %-9d %-5.0f%% %14.0f %14.0f %10.0f\n",
               t, 100.0 * t / resident, ind, str, ms);
        if (ind > best) { best = ind; bestT = t; }
        fflush(stdout);
    }

    printf("\n  best %.0f H/s at %d threads\n", best, bestT);
    printf("  CPU reference (Ryzen 7 9800X3D, 14 threads): 4622 H/s  ->  %.1fx\n\n", best / 4622.0);

    CK(cudaFree(sa)); CK(cudaFree(text)); CK(cudaFree(bucket)); CK(cudaFree(sink));
    return 0;
}
