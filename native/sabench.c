/* sabench.c -- checks and times the two suffix sorts on the real texts.
 *
 * The miner's --bench measures a whole hash, of which the suffix sort is 84%.
 * That is the right number to judge a change by, but the wrong one to iterate
 * with: a 5% change in the sort shows up as 4% there, inside the noise of a
 * desktop, and every measurement costs a rebuild of the DLL and the miner.
 *
 * This loads gpu/vectors.bin -- the same 512 real stage-1 texts every other
 * test in this repository uses -- and runs both sorts over them directly.
 *
 *   native\benchbuild.bat
 *   native\sabench.exe gpu\vectors.bin
 *
 * Both are checked against the suffix arrays Go computed, stored in the same
 * file, before either is timed. A suffix array is unique, so that is the whole
 * correctness question: there is no tolerance in it and no judgement call, and
 * a build that is faster and wrong is not a result.
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <time.h>

#include "libsais/libsais.h"
#include "descriptor.h"

#if defined(_WIN32)
#include <windows.h>
static double now_seconds(void)
{
    LARGE_INTEGER f, t;
    QueryPerformanceFrequency(&f);
    QueryPerformanceCounter(&t);
    return (double)t.QuadPart / (double)f.QuadPart;
}
#else
static double now_seconds(void)
{
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec + (double)ts.tv_nsec * 1e-9;
}
#endif

/* MAX_LENGTH from astrobwtv3: the miner's scratch is this big whatever the
 * text length, and the slack after it is what libsais gets as free space. */
#define SCRATCH_LEN ((256 * 384) - 1)

/* How many times each thread sorts the whole set inside one timed round.
 *
 * Starting fifteen threads costs about a millisecond and one pass over the 512
 * texts takes seventeen, so a single-pass round measures thread creation as much
 * as sorting. Eight passes puts that under a percent, which is what an A/B of a
 * few percent needs. */
#define THREADED_INNER 8

typedef struct {
    int32_t  n;
    uint8_t* text;
    int32_t* want;
} Vector;

/* ---------------------------------------------------------------------------
 * Threaded timing.
 *
 * One thread is the wrong place to tune this for a miner. A single sort's
 * working set -- 69 KB of text, a 278 KB arena, 161 KB of descriptors, a 278 KB
 * output -- fits in one core's L2 with room to spare, so every gather is an L2
 * hit and traffic costs nothing. At fifteen threads on eight cores two sorts
 * share each L2 and none of it fits, so the same gather is an L3 hit and traffic
 * is most of what there is to win. Changes have measured in opposite directions
 * at the two thread counts, which is not a subtlety to discover late.
 *
 *   native\sabench.exe gpu\vectors.bin 3 15
 *
 * Every thread sorts every text, so the work scales with the thread count and
 * the reported rate is throughput. Affinity is one thread per logical CPU in
 * order, which is not the miner's pin map -- it spreads over physical cores
 * first -- but is consistent between runs, which is what an A/B needs.
 * ------------------------------------------------------------------------ */
#if defined(_WIN32)

typedef struct {
    const Vector* v;
    uint32_t      count;
    int32_t*      sa;
    void*         ctx;    /* libsais context, NULL for the descriptor sort */
    int           cpu;
} Worker;

static DWORD WINAPI sort_worker(LPVOID arg)
{
    Worker* w = (Worker*)arg;
    SetThreadAffinityMask(GetCurrentThread(), (DWORD_PTR)1 << w->cpu);
    for (int r = 0; r < THREADED_INNER; r++) {
        for (uint32_t i = 0; i < w->count; i++) {
            if (w->ctx) {
                libsais_ctx(w->ctx, w->v[i].text, w->sa, w->v[i].n,
                            SCRATCH_LEN - w->v[i].n, NULL);
            } else {
                dsa_descriptor_suffix_array(w->v[i].text, w->sa, w->v[i].n);
            }
        }
    }
    return 0;
}

/* Returns the elapsed seconds for threads * THREADED_INNER * count sorts, or
 * -1 on failure. */
static double time_threaded(const Vector* v, uint32_t count, int threads,
                            int use_libsais)
{
    Worker* w = (Worker*)calloc((size_t)threads, sizeof(Worker));
    HANDLE* h = (HANDLE*)calloc((size_t)threads, sizeof(HANDLE));
    if (!w || !h) return -1;

    for (int i = 0; i < threads; i++) {
        w[i].v = v;
        w[i].count = count;
        w[i].cpu = i;
        w[i].sa = (int32_t*)malloc((size_t)SCRATCH_LEN * 4);
        if (!w[i].sa) return -1;
        if (use_libsais) {
            w[i].ctx = libsais_create_ctx();
            if (!w[i].ctx) return -1;
        }
    }

    const double t0 = now_seconds();
    for (int i = 0; i < threads; i++) {
        h[i] = CreateThread(NULL, 0, sort_worker, &w[i], 0, NULL);
        if (!h[i]) return -1;
    }
    WaitForMultipleObjects((DWORD)threads, h, TRUE, INFINITE);
    const double el = now_seconds() - t0;

    for (int i = 0; i < threads; i++) {
        CloseHandle(h[i]);
        free(w[i].sa);
        if (w[i].ctx) libsais_free_ctx(w[i].ctx);
    }
    free(w);
    free(h);
    return el;
}
#endif

int main(int argc, char** argv)
{
    const char* path = argc > 1 ? argv[1] : "gpu/vectors.bin";
    const int rounds = argc > 2 ? atoi(argv[2]) : 3;
    const int threads = argc > 3 ? atoi(argv[3]) : 1;
    /* Time the descriptor sort even when its output is wrong. Only useful with a
     * build that is deliberately wrong -- DSA_ABLATE cuts a phase out of the
     * walk to find what that phase costs, and the answer is not visible any
     * other way, since a phase timer fine enough to see inside the walk would
     * cost more than the walk. The exit status still reports the mismatch. */
    const int force = argc > 4 ? atoi(argv[4]) : 0;

    FILE* f = fopen(path, "rb");
    if (!f) { printf("cannot open %s\n", path); return 1; }

    char magic[8];
    uint32_t count = 0;
    if (fread(magic, 1, 8, f) != 8 ||
        (memcmp(magic, "ABWTVEC1", 8) != 0 && memcmp(magic, "ABWTVEC2", 8) != 0)) {
        printf("%s is not a vector file\n", path); return 1;
    }
    const int v2 = memcmp(magic, "ABWTVEC2", 8) == 0;
    if (fread(&count, 4, 1, f) != 1) { printf("truncated\n"); return 1; }

    Vector* v = (Vector*)calloc(count, sizeof(Vector));
    uint64_t totalBytes = 0;
    for (uint32_t i = 0; i < count; i++) {
        uint32_t n = 0;
        if (fread(&n, 4, 1, f) != 1) { printf("truncated at %u\n", i); return 1; }
        v[i].n = (int32_t)n;
        v[i].text = (uint8_t*)malloc(n);
        v[i].want = (int32_t*)malloc((size_t)n * 4);
        if (fread(v[i].text, 1, n, f) != n) { printf("truncated text\n"); return 1; }
        if (fread(v[i].want, 4, n, f) != n) { printf("truncated sa\n"); return 1; }
        if (v2) fseek(f, 32, SEEK_CUR);
        totalBytes += n;
    }
    fclose(f);

    int32_t* sa = (int32_t*)malloc((size_t)SCRATCH_LEN * 4);
    int32_t* dsa = (int32_t*)malloc((size_t)SCRATCH_LEN * 4);
    void* ctx = libsais_create_ctx();
    if (!sa || !dsa || !ctx) { printf("out of memory\n"); return 1; }

    /* ---- correctness, over every vector, before either is timed ---------- */

    for (uint32_t i = 0; i < count; i++) {
        const int32_t fs = SCRATCH_LEN - v[i].n;
        if (libsais_ctx(ctx, v[i].text, sa, v[i].n, fs, NULL) != 0) {
            printf("  libsais failed on vector %u\n", i); return 1;
        }
        if (memcmp(sa, v[i].want, (size_t)v[i].n * 4) != 0) {
            printf("  libsais MISMATCH on vector %u\n", i); return 1;
        }
    }
    printf("  libsais     correct on all %u\n", count);

    int desc_ok = 1;
    for (uint32_t i = 0; i < count && desc_ok; i++) {
        const int rc = dsa_descriptor_suffix_array(v[i].text, dsa, v[i].n);
        if (rc != 0) {
            printf("  descriptor  FAILED on vector %u (rc %d)\n", i, rc);
            desc_ok = 0;
            break;
        }
        for (int32_t k = 0; k < v[i].n; k++) {
            if (dsa[k] != v[i].want[k]) {
                printf("  descriptor  MISMATCH on vector %u of %u, at %d of %d:"
                       " got %d, want %d\n",
                       i, count, k, v[i].n, dsa[k], v[i].want[k]);
                desc_ok = 0;
                break;
            }
        }
    }
    if (desc_ok || force) printf("  descriptor  correct on all %u\n", count);

    /* ---- timing ---------------------------------------------------------- */

    double best = 1e30;
    double bestDesc = 1e30;

#if defined(_WIN32)
    if (threads > 1) {
        /* Interleaved, not one sort's rounds then the other's, so that a drift
         * in background load lands on both instead of on whichever went last. */
        for (int r = 0; r < rounds; r++) {
            const double a = time_threaded(v, count, threads, 1);
            if (a < 0) { printf("  cannot start %d threads\n", threads); return 1; }
            if (a < best) best = a;
            if (desc_ok || force) {
                const double b = time_threaded(v, count, threads, 0);
                if (b < 0) { printf("  cannot start %d threads\n", threads); return 1; }
                if (b < bestDesc) bestDesc = b;
            }
        }
        /* The work was threads * inner times as much, so the wall clock is
         * divided by that to keep "texts/s" meaning the same as at one thread. */
        best /= threads * THREADED_INNER;
        bestDesc /= threads * THREADED_INNER;
    } else
#endif
    {
        for (int r = 0; r < rounds; r++) {
            const double t0 = now_seconds();
            for (uint32_t i = 0; i < count; i++)
                libsais_ctx(ctx, v[i].text, sa, v[i].n, SCRATCH_LEN - v[i].n, NULL);
            const double el = now_seconds() - t0;
            if (el < best) best = el;
        }

        if (desc_ok || force) {
            for (int r = 0; r < rounds; r++) {
                const double t0 = now_seconds();
                for (uint32_t i = 0; i < count; i++)
                    dsa_descriptor_suffix_array(v[i].text, dsa, v[i].n);
                const double el = now_seconds() - t0;
                if (el < bestDesc) bestDesc = el;
            }
        }
    }

    printf("\n  libsais %s, %u texts, %.1f MB, best of %d, %d thread(s)\n",
           LIBSAIS_VERSION_STRING, count, totalBytes / 1e6, rounds, threads);
    printf("  %-12s %8.3f s %8.0f texts/s %7.1f MB/s\n",
           "libsais", best, count / best, totalBytes / 1e6 / best);
    if (desc_ok || force) {
        printf("  %-12s %8.3f s %8.0f texts/s %7.1f MB/s   %+.1f%%\n",
               "descriptor", bestDesc, count / bestDesc,
               totalBytes / 1e6 / bestDesc, (best / bestDesc - 1) * 100);
    }
    printf("\n");

#ifdef DSA_PROF
    /* The phase table. Built with DSA_PROF, so the timers are themselves in the
     * measurement: absolute cycles are inflated and only the shares matter. */
    {
        extern unsigned long long dsa_prof[5];
        extern unsigned long long dsa_stat[9];
        static const char* ph[5] = {
            "run boundaries", "column walk + emit", "descriptor radix sort",
            "merge (key collisions)", "tail",
        };
        unsigned long long tot = 0;
        for (int i = 0; i < 5; i++) tot += dsa_prof[i];
        if (tot == 0) tot = 1;

        printf("  %-26s %14s %8s\n", "phase", "cycles", "share");
        for (int i = 0; i < 5; i++) {
            printf("  %-26s %14llu %7.1f%%\n", ph[i], dsa_prof[i],
                   100.0 * dsa_prof[i] / tot);
        }
        printf("\n");

        /* Every timed round plus the one correctness pass ran every text. */
        const double per = (double)count * (rounds + 1);
        const double meanN = totalBytes / count;
        printf("  per text: %.0f runs, %.0f descriptors, %.0f key groups,\n",
               dsa_stat[5] / per, dsa_stat[0] / per, dsa_stat[1] / per);
        printf("            %.0f colliding (%.1f%% of groups), %.0f positions merged (%.1f%% of n),\n",
               dsa_stat[2] / per,
               dsa_stat[1] ? 100.0 * dsa_stat[2] / dsa_stat[1] : 0.0,
               dsa_stat[3] / per, 100.0 * (dsa_stat[3] / per) / meanN);
        printf("            %.0f suffix comparisons\n", dsa_stat[4] / per);
        printf("  high water: %llu descriptors, %llu positions in one key group,"
               " %llu lists in one\n\n",
               dsa_stat[6], dsa_stat[7], dsa_stat[8]);
    }
#endif
    return desc_ok ? 0 : 1;
}
