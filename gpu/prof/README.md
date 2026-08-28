# Cycle attribution for the suffix kernel

Nsight Compute cannot read GPU performance counters without a driver permission
that needs admin (NVIDIA Control Panel → Desktop → Developer settings → *Manage
GPU Performance Counters* → all users). Where that is not available, this gives
the next most useful thing: how the suffix sort's time divides between its
phases.

Thread 0 of each block samples `clock64()` at every phase boundary — almost all
of which are existing `__syncthreads()` — and the deltas accumulate into a
per-phase table. It checks its suffix arrays against `gpu/vectors.bin` before
reporting anything, because a kernel computing the wrong answer is profiling the
wrong thing.

The marks live in the **real** headers, behind `gpu/prof.cuh`, which compiles
every one of them to nothing unless `DS_PROF` is defined. This directory used to
keep instrumented *copies* of `blockradix.cuh` and `sa_doubling.cuh`, and they
went stale the moment the originals changed — which is exactly when a profile is
wanted. There is nothing to keep in step now.

Nested timers need care. Each function has its own accumulator, so an outer
timer left running across an inner one bills for both: an early run of this
table put "write sa back" at 35.9%, which is absurd for a single `int32` store
per element, because the round's timer was still running through the whole sort.
There is a `PROF_MARK()` immediately after `blockRadixSort` for that reason.

```
prof\build.bat
gpu\prof\prof.exe gpu\vectors.bin
```

## Reading it

Absolute cycles mean little. The marks add barriers where the code had none, the
`atomicAdd` per phase per tile is itself contended across blocks, and two blocks
sharing an SM each measure elapsed cycles that include the other's work. Two
builds of the *same* code have measured 78 ms and 97 ms.

The **shares** are the point, and they have been reliable: the leader-walk phase
measured 16.5% twice on separate runs, and dropped to 5.8% when it was fixed —
which the hashrate then confirmed, 7.01 to 7.73 KH/s.

So: use this to decide *where* to look, then confirm any change with
`derostorm --bench --gpu=all`, which measures the real thing.

## The copies in this directory

`blockradix.cuh` and `sa_doubling.cuh` here are **instrumented copies** of the
headers in `gpu/`, not the originals. They will drift the moment the real ones
change, and a profile of stale code is worse than no profile — it once produced a
run that looked like a fix had done nothing, because the copy had not been
updated.

There is no build step that regenerates them, so before trusting a run: copy
`gpu/blockradix.cuh` and `gpu/sa_doubling.cuh` over the ones here, then re-add
the `PROF_DECL` / `PROF_MARK()` / `PROF_ADD(PH_*)` marks and the `PH_*` enum and
`g_prof` / `g_tiles` / `g_rounds` declarations at the top of `blockradix.cuh`.
The phase names in `prof.cu` say where each mark belongs.

Nothing here is compiled into the miner.
