// prof.cuh -- cycle attribution for the suffix kernel, compiled out by default.
//
// Nsight Compute cannot read this machine's performance counters without a
// driver permission that needs admin, so the kernel times itself instead:
// thread 0 samples clock64() at each phase boundary -- almost all of which are
// existing __syncthreads() -- and the deltas accumulate per phase.
//
// This lives in the real headers rather than in instrumented copies of them.
// gpu/prof/ used to keep its own blockradix.cuh and sa_doubling.cuh, and they
// went stale the moment the originals changed, which is exactly when a profile
// is most wanted. With DS_PROF undefined every macro below is empty and the
// generated code is identical, so the shipped kernel pays nothing for this.
//
//   gpu\prof\build.bat            (defines DS_PROF)
//   gpu\prof\prof.exe gpu\vectors.bin
//
// Reading it: the absolute cycles mean little. The marks add barriers where the
// code had none, the atomicAdd per phase per tile is itself contended across
// blocks, and two blocks sharing an SM each measure elapsed cycles that include
// the other's work. Two builds of the same code have measured 78 ms and 97 ms.
// The *shares* are the point, and they have been reliable.

#pragma once

#ifdef DS_PROF

// Phases, in the order they run. Keep phName[] in gpu/prof/prof.cu in step.
enum {
    // prefix doubling (sa_doubling.cuh, blockradix.cuh)
    PH_HIST, PH_OFFSCAN,
    PH_RANK, PH_LEADER, PH_SCANBINS, PH_STAGE, PH_COPYOUT, PH_CURSOR,
    PH_KEYBUILD, PH_SAWRITE, PH_GROUPS, PH_RANKUPD, PH_FLAGS, PH_COMPACT,
    // the descriptor sort (desc.cuh), which is the path the miner takes
    PH_D_RUNS, PH_D_WALK, PH_D_TAIL, PH_D_RADIX, PH_D_SCAN,
    PH_D_EXPAND, PH_D_SCATTER, PH_D_MERGE,
    PH_N
};

__device__ unsigned long long g_prof[PH_N];
__device__ unsigned long long g_tiles;
__device__ unsigned long long g_rounds;

// Run-length histogram of the column walk's tasks, and the longest run seen.
// The walk gives one task to one thread and the task costs the run's length, so
// the block waits for the longest one; this is what says how much of the walk
// is threads doing nothing.
// Per-thread cost of the column walk. The walk gives one task to one thread and
// the block waits at the barrier after it, so the phase costs the slowest
// thread, not the average one. max against mean is how much a perfectly
// balanced walk would save.
__device__ unsigned long long g_wmax;
__device__ unsigned long long g_wsum;
__device__ unsigned long long g_wcnt;
__device__ unsigned long long g_wblk;   // summed per-block maxima

// Per-task cost of the column walk, bucketed by the run's length, plus the
// descriptors each task emitted. The walk's imbalance is 1.97x while the run
// lengths spread 1..45, so cost is not proportional to length: this says how
// much of a task is fixed per column and how much scales with the run.
__device__ unsigned long long g_scyc[65];   // the chunk seed alone
__device__ unsigned long long g_ccyc[65];   // the col_same precompute alone
__device__ unsigned long long g_tcyc[65];
__device__ unsigned long long g_tcnt[65];
__device__ unsigned long long g_tdsc[65];

#define PROF_TASK_DECL      long long _tt0 = 0, _ts0 = 0; int _tnd = 0
#define PROF_SEED_BEG()     do { _ts0 = clock64(); } while (0)
#define PROF_SEED_END(len)  do { atomicAdd(&g_scyc[(len) < 64 ? (len) : 64], (unsigned long long)(clock64() - _ts0)); } while (0)
#define PROF_COLS_BEG()     do { _ts0 = clock64(); } while (0)
#define PROF_COLS_END(len)  do { atomicAdd(&g_ccyc[(len) < 64 ? (len) : 64], (unsigned long long)(clock64() - _ts0)); } while (0)
#define PROF_TASK_BEG()     do { _tt0 = clock64(); _tnd = 0; } while (0)
#define PROF_TASK_EMIT()    do { _tnd++; } while (0)
#define PROF_TASK_END(len)  do {         const int _L = (len) < 64 ? (len) : 64;         atomicAdd(&g_tcyc[_L], (unsigned long long)(clock64() - _tt0));         atomicAdd(&g_tcnt[_L], 1ull);         atomicAdd(&g_tdsc[_L], (unsigned long long)_tnd);     } while (0)

__device__ unsigned long long g_runlen[65];
__device__ unsigned long long g_runmax;
__device__ unsigned long long g_runsum;
__device__ unsigned long long g_runcnt;
#define PROF_RUNS(start, nruns) do {         for (int _r = threadIdx.x; _r < (nruns); _r += BR_BLOCK) {             const int _l = (start)[_r + 1] - (start)[_r];             atomicAdd(&g_runlen[_l < 64 ? _l : 64], 1ull);             atomicMax(&g_runmax, (unsigned long long)_l);             atomicAdd(&g_runsum, (unsigned long long)_l);             atomicAdd(&g_runcnt, 1ull);         }     } while (0)

#define PROF_DECL long long _pt = 0
#define PROF_MARK() do { if (threadIdx.x == 0) _pt = clock64(); } while (0)
#define PROF_ADD(ph) do { \
        if (threadIdx.x == 0) { long long _n = clock64(); \
            atomicAdd(&g_prof[ph], (unsigned long long)(_n - _pt)); _pt = _n; } \
    } while (0)
#define PROF_COUNT(c) do { if (threadIdx.x == 0) atomicAdd(&(c), 1ull); } while (0)
#define PROF_WALK_BEG() const long long _wt0 = clock64()
#define PROF_WALK_END() do { \
        __shared__ unsigned long long _wmx; \
        if (threadIdx.x == 0) _wmx = 0; \
        __syncthreads(); \
        const unsigned long long _w = (unsigned long long)(clock64() - _wt0); \
        atomicMax(&_wmx, _w); \
        atomicAdd(&g_wsum, _w); \
        atomicAdd(&g_wcnt, 1ull); \
        __syncthreads(); \
        if (threadIdx.x == 0) { atomicAdd(&g_wblk, _wmx); atomicMax(&g_wmax, _wmx); } \
    } while (0)

#else

#define PROF_DECL       ((void)0)
#define PROF_MARK()     ((void)0)
#define PROF_ADD(ph)    ((void)0)
#define PROF_COUNT(c)   ((void)0)
#define PROF_RUNS(s, n) ((void)0)
#define PROF_TASK_DECL      ((void)0)
#define PROF_SEED_BEG()     ((void)0)
#define PROF_SEED_END(len)  ((void)0)
#define PROF_COLS_BEG()     ((void)0)
#define PROF_COLS_END(len)  ((void)0)
#define PROF_TASK_BEG()     ((void)0)
#define PROF_TASK_EMIT()    ((void)0)
#define PROF_TASK_END(len)  ((void)0)

#define PROF_WALK_BEG() ((void)0)
#define PROF_WALK_END() ((void)0)

#endif
