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
    PH_D_SCATTER, PH_D_MERGE,
    PH_N
};

__device__ unsigned long long g_prof[PH_N];
__device__ unsigned long long g_tiles;
__device__ unsigned long long g_rounds;

#define PROF_DECL long long _pt = 0
#define PROF_MARK() do { if (threadIdx.x == 0) _pt = clock64(); } while (0)
#define PROF_ADD(ph) do { \
        if (threadIdx.x == 0) { long long _n = clock64(); \
            atomicAdd(&g_prof[ph], (unsigned long long)(_n - _pt)); _pt = _n; } \
    } while (0)
#define PROF_COUNT(c) do { if (threadIdx.x == 0) atomicAdd(&(c), 1ull); } while (0)

#else

#define PROF_DECL       ((void)0)
#define PROF_MARK()     ((void)0)
#define PROF_ADD(ph)    ((void)0)
#define PROF_COUNT(c)   ((void)0)

#endif
