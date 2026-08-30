// gapbench.cu -- how much of the wall clock is the card actually working?
//
// The question this answers: the miner's GPU rate rises when CPU mining threads
// are taken away, and the suspected reason is that the card goes idle between
// batches while the host thread waits for a scheduler slot to enqueue the next
// one. That is a claim about a gap in the GPU timeline, so measure the gap
// rather than the throughput -- throughput on this card swings ±10% run to run
// and will not settle an argument about a few percent.
//
// Events are recorded on the context's stream, so the elapsed time between them
// is GPU time. Summed over the batches and divided by the wall clock, that is
// the fraction of the run the card spent busy. Whatever is left is the gap.
//
// Build and run from the repository root:
//   gpu\gapbench.bat
//   gpu\gapbench.exe <serial|pipeline> <busy cpu threads> <batches> [nonces] [prio]
//
// prio 1 raises this thread above the busy ones, as the miner's feeder thread
// does; 0 leaves it at normal.

#include "derostorm_gpu.cu"

#include <chrono>
#include <thread>
#include <vector>
#include <atomic>

#ifdef _WIN32
#include <windows.h>
#endif

static std::atomic<int> g_stop{0};

// Load the machine the way mining does: one pinned thread per logical CPU,
// never yielding. The work itself does not have to be AstroBWT -- what starves
// the feeder thread is runnable threads holding every core, not what they
// compute -- but it does have to touch memory, so this walks a buffer that does
// not fit in L2.
static void burn(int cpu)
{
#ifdef _WIN32
    SetThreadAffinityMask(GetCurrentThread(), (DWORD_PTR)1 << cpu);
#endif
    const size_t n = 8u << 20;
    std::vector<uint32_t> buf(n / 4, 1u);
    uint32_t x = 1;
    while (!g_stop.load(std::memory_order_relaxed)) {
        for (size_t i = 0; i < buf.size(); i += 16) {
            x = x * 1664525u + 1013904223u;
            buf[(x >> 8) % buf.size()] += x;
        }
    }
    if (x == 0) printf("");   // keep the loop
}

int main(int argc, char** argv)
{
    const char* mode = argc > 1 ? argv[1] : "serial";
    const int threads = argc > 2 ? atoi(argv[2]) : 0;
    const int batches = argc > 3 ? atoi(argv[3]) : 30;
    const int askBatch = argc > 4 ? atoi(argv[4]) : 0;
    const int prio = argc > 5 ? atoi(argv[5]) : 0;
    const bool pipeline = strcmp(mode, "pipeline") == 0;

    dsg_context* ctx = NULL;
    int gotBatch = 0, gotBlocks = 0;
    if (dsg_init(0, askBatch, 0, &ctx, &gotBatch, &gotBlocks) != DSG_OK) {
        char e[512]; dsg_error(e, sizeof(e));
        printf("init failed: %s\n", e);
        return 1;
    }
    char name[256]; dsg_device_name(ctx, name, sizeof(name));

    uint8_t work[DSG_WORK_SIZE] = {0};
    work[0] = 1;
    for (int i = 8; i < 43; i++) work[i] = (uint8_t)(i * 7 + 29);
    // 2^160, far past any real difficulty, so nothing ever qualifies.
    uint64_t target[4] = {0, 0, 0xffffffffffffffffull, 0xffffffffffffffffull};

    // One pair of events a batch, recorded either side of that batch's work on
    // the context's stream.
    std::vector<cudaEvent_t> evA(batches), evB(batches);
    for (int i = 0; i < batches; i++) {
        cudaEventCreate(&evA[i]);
        cudaEventCreate(&evB[i]);
    }

    std::vector<std::thread> load;
    if (threads > 0) {
        for (int i = 0; i < threads; i++) load.emplace_back(burn, i);
        std::this_thread::sleep_for(std::chrono::seconds(3));
    }

#ifdef _WIN32
    if (prio) SetThreadPriority(GetCurrentThread(), THREAD_PRIORITY_ABOVE_NORMAL);
#endif

    uint32_t nonce = 0;
    uint32_t hits[64];
    int found = 0;

    // Warm the clocks before anything is timed.
    for (int i = 0; i < 3; i++) {
        dsg_search(ctx, work, nonce, target, 0, hits, 64, &found);
        nonce += (uint32_t)gotBatch;
    }

    // The events bracket each batch's kernels. In the pipelined case evA[i] is
    // recorded before the batch is enqueued but executes when the card reaches
    // it -- which, if the pipeline is doing its job, is the instant the previous
    // batch ended.
    auto t0 = std::chrono::steady_clock::now();
    if (pipeline) {
        int next = 0, taken = 0;
        while (taken < batches) {
            while (next < batches && dsg_inflight(ctx) < DSG_SLOTS) {
                cudaEventRecord(evA[next], ctx->bank[0].stream);
                dsg_submit(ctx, work, nonce, target, 0);
                cudaEventRecord(evB[next], ctx->bank[0].stream);
                nonce += (uint32_t)gotBatch;
                next++;
            }
            dsg_collect(ctx, hits, 64, &found);
            taken++;
        }
    } else {
        for (int i = 0; i < batches; i++) {
            cudaEventRecord(evA[i], ctx->bank[0].stream);
            dsg_submit(ctx, work, nonce, target, 0);
            cudaEventRecord(evB[i], ctx->bank[0].stream);
            dsg_collect(ctx, hits, 64, &found);
            nonce += (uint32_t)gotBatch;
        }
    }
    cudaDeviceSynchronize();
    double wall = std::chrono::duration<double>(std::chrono::steady_clock::now() - t0).count();

    g_stop = 1;
    for (auto& t : load) t.join();

    // Busy time is the sum of the per-batch spans; the gap is what the wall
    // clock has left over. Also report the span from the first batch's start to
    // the last one's end, so a slow start does not read as a gap.
    double busy = 0;
    for (int i = 0; i < batches; i++) {
        float ms = 0;
        cudaEventElapsedTime(&ms, evA[i], evB[i]);
        busy += ms / 1000.0;
    }
    float spanMs = 0;
    cudaEventElapsedTime(&spanMs, evA[0], evB[batches - 1]);
    double span = spanMs / 1000.0;

    printf("%s\n", name);
    printf("mode %-9s cpu %2d  batch %d  blocks %d\n", mode, threads, gotBatch, gotBlocks);
    printf("  wall      %8.3f s   %9.0f H/s\n", wall, batches * (double)gotBatch / wall);
    printf("  gpu busy  %8.3f s   %5.1f%% of the timeline\n", busy, busy / span * 100.0);
    printf("  gap       %8.3f s   %5.1f ms per batch\n",
           span - busy, (span - busy) * 1000.0 / batches);

    for (int i = 0; i < batches; i++) { cudaEventDestroy(evA[i]); cudaEventDestroy(evB[i]); }
    dsg_free(ctx);
    return 0;
}
