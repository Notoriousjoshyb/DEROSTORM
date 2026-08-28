# GPU port of AstroBWTv3

Status: **the whole hash runs on the GPU, exactly, by two different routes.**
Option B ships in the miner, in every build: the kernels live in a DLL that is
embedded in the executable and bound at run time, so there is one binary and a
machine with no NVIDIA card runs the same file and mines on the CPU.

Measured on an RTX 5080 (sm_120, 84 SMs, 16 GB) beside a Ryzen 7 9800X3D.

**Live mining, 75-second runs against a real node** -- the number that matters:

| | H/s | vs CPU alone |
|---|---:|---:|
| CPU alone, 15 threads | 6,920 | 1.00x |
| GPU alone | ~5,500 | |
| **CPU 15 threads + GPU** | **12,478** | **1.80x** |

Kernel and benchmark figures, which are what the test binaries print:

| | H/s |
|---|---:|
| CPU benchmark, 15 threads | 7,511 |
| option A -- thread per hash, SA-IS | 3,570-3,960 |
| **option B -- block per hash, prefix doubling** | **6,830-6,910** |

Both GPU options are checked against 512 real CPU vectors before any figure is
quoted. Option B is what the miner ships.

Two notes on the numbers, because earlier versions of this file got them wrong:

- **The CPU baseline used to be recorded as 4,622 H/s and that was stale.** The
  same machine benchmarks at 7,511. Every "x times the CPU" figure written
  against 4,622 was overstated by about 60%, which is why they are all restated
  here against a fresh measurement.
- **Ranges are the spread over repeated runs.** These kernels run for seconds at
  a time and the boost clock sags under sustained load, so a single sample
  measures thermals as much as code. Every figure is a best of three.

Thread count is worth as much as any of the GPU work: on this machine 12 CPU
threads plus the GPU gives 11.4 KH/s, 14 gives 12.3, 15 gives 12.5, and 16 falls
back to 11.6. One thread has to be left for the OS and for feeding the GPU.

## Why this is hard

AstroBWTv3 was designed to be GPU-hostile and it succeeds. Two properties do it:

- **A SA-IS suffix sort** over ~68 KB of text, which is ~93% of a *CPU* hash
  (see the comment at `astrobwtv3/pow.go:2461`). SA-IS is an irregular,
  data-dependent, pointer-chasing traversal — the workload GPUs are worst at.
- **A 256-way branch** taken ~276 times per hash (`pow.go:84`, and the source
  calls the divergence deliberate). A warp runs 32 lanes in lockstep, so 256
  branch targets serialise it.

## Two things the first estimates got wrong

Both were corrected by measurement, and both changed the answer.

**Stage 1 was 80% of the GPU hash, not 7%.** The 256-way switch is cheap on a
CPU, so it was assumed cheap here too and the suffix sort was optimised first.
It was not cheap: `step_3` and the RC4 permutation are 256-byte arrays indexed
by runtime values, so as plain locals they land in local memory — global memory
with a per-thread stride, where every access costs its own 32-byte sector.
Moving both into shared memory with a 516-byte stride (4 bytes of padding, so
lanes land on different banks) took stage 1 from 2,231 to 373,117 H/s, and the
whole hash from 1,784 to 3,961 in the same run. **That one change was
worth 2.2x** and nothing
about the suffix sort had to move.

**The text is not RC4 noise.** Stage 1 appends the 256-byte state after every
iteration and most iterations change at most 32 of those bytes, so consecutive
blocks are near copies and long repeats are the rule. Assuming noise would have
predicted prefix doubling finishing in 3 rounds. `gpu/lcpstat` measured the real
figure over 512 texts: mean LCP 97 bytes, worst 2,301, so **13 rounds**. The
same measurement showed that re-sorting only the still-tied groups costs 6n
instead of 13n, which is why option B does that and why it is worth 1.3x.

## The two mappings

Both compute the same function; they differ in what owns a hash.

**Option A, one thread per hash** (`sais.cuh`). Classic SA-IS, ~200 lines. Each
thread owns ~434 KB and chases pointers around it, so 16,000 threads is a 7 GB
working set against a 64 MB L2 and essentially every access is a cold sector
fetch. Measured effective bandwidth: **~90 GB/s, under a tenth of the card**.
It is at its ceiling — throughput rises only 11% between 8,192 and 26,752
threads, and the batch size is set by VRAM, not by the SMs.

**Option B, one block per hash** (`sa_doubling.cuh` + `blockradix.cuh`). Prefix
doubling with a stable block-cooperative LSD radix sort. O(n log n) rather than
O(n), so it does more work — and wins anyway, because every access is coalesced
and only 42–84 hashes are in flight. Measured: **~710 GB/s**. It is bandwidth
bound, so the only thing that makes it faster is moving fewer bytes.

Option B also removes A's memory wall. Its blocks loop over the batch and reuse
their scratch, so resident blocks and batch size are independent: 3 GB total
where A needed 13.

### What moved the needle in B

| change | SA/s |
|---|---:|
| first working version, whole array re-sorted every round | 3,792 |
| re-sort only the still-tied groups (13n → 6n) | 5,059 |
| 1,024 threads per block instead of 256 | 6,598 |
| all pass histograms in one traversal | 6,598 (no change) |
| stage each tile in shared memory so the scatter coalesces | 6,910 |

The last two are the useful part of this table, and both are negative results.

A radix pass only permutes its keys, so every pass's histogram can be counted
once up front; that removes a whole read of the key array per pass and bought
nothing. The obvious reading was that the scattered *writes* were the cost, so
each tile is now staged in shared memory in digit order and copied out, which
makes same-digit lanes write consecutive addresses. Expected 20-30%. Got 5%.

Put together they say the sort was never being held up by *how* it touches
memory. At 6,900 suffix arrays a second it is moving roughly 100 MB per hash at
about three quarters of the card's peak bandwidth, so the hardware was already
coalescing most of those writes on its own. What is left is *how much* memory it
touches, and the 64-bit sort key is the obvious target.

Staging did buy one thing that is not throughput: the kernel no longer cares how
many blocks it is given. Before it peaked sharply at 42 blocks and fell away;
now it is flat from 84 to 1,344, which is one less thing to tune per card.

## The three kernels

A hash has three stages and they want three different mappings, so the miner
runs three kernels. Timings for a batch of 8,192:

| kernel | mapping | ms | share |
|---|---|---:|---:|
| stage 1 | thread per hash, state in shared memory | 24.2 | 2.0% |
| suffix array | block per hash, 1,024 threads | 1,171.0 | 96.7% |
| SHA-256 + difficulty check | thread per hash | 15.5 | 1.3% |

Only winning nonces cross PCIe.

## Correctness

Nothing here is quoted for speed until it matches the CPU. `gpu/vectors.bin`
holds 512 real stage-1 texts, the suffix arrays Go computed for them, and the
final AstroBWTv3 hashes, so a clean run proves the whole pipeline and not just
one boundary.

| test | checks |
|---|---|
| `gpu/hash_test.exe` | option A: stage 1 text, then the final hash, all 512 |
| `gpu/sa_parallel_test.exe` | option B's suffix arrays vs the CPU, and B vs A |
| `gpu/hash_parallel_test.exe` | option B end to end: all 512 final hashes |
| `go test -tags cuda -run GPU ./cmd/derostorm` | the cgo bridge, and that the GPU's difficulty check picks exactly the nonces the CPU would |

On top of that the miner re-hashes every winning nonce on the CPU before
submitting it, and refuses to mine on a device that disagrees with the CPU on
four probe nonces at start-up. A fast miner that is wrong submits rejected
shares, which is worse than not mining.

## Layout

| file | what |
|---|---|
| `stage1.cuh` | the 256-way op switch and the loop around it |
| `stage1_cases.inc` | **generated** — the 256 case bodies |
| `gencases/` | the generator that reads `pow.go` and writes the above |
| `crypto.cuh` | sha256, salsa20, rc4, fnv1a64, xxhash64, siphash |
| `sais.cuh` | option A: SA-IS, thread per hash |
| `blockradix.cuh` | stable block-cooperative radix sort and scans |
| `sa_doubling.cuh` | option B: prefix doubling, block per hash |
| `derostorm_gpu.cu/.h` | the miner library: three kernels and a C API |
| `vectors/` | dumps real texts, suffix arrays and hashes from Go |
| `lcpstat/` | measures how many doubling rounds the real texts need |
| `*_test.cu` | the checks in the table above |
| `sweep.bat` | rebuilds option B at 128/256/512/1024 threads per block |
| `spike.cu` | the pre-port feasibility model, kept for the comparison |

The 256 case bodies are generated rather than hand-written. Transcribing 2,300
lines of near-identical Go is an error machine; generating makes the port
auditable, because every source line has to match one of 18 known op forms and
an unrecognised one is a hard error rather than a quietly wrong hash. Re-run it
if `pow.go` ever moves:

```
go run ./gpu/gencases -pow ../derohe-main/astrobwt/astrobwtv3/pow.go -out gpu/stage1_cases.inc
```

The capture hook the vector dumper needs lives in
`../../derohe-main/astrobwt/astrobwtv3/gpu_capture.go`, plus a three-line call
in that package's `pow.go`. It costs one nil check per hash and is nil in every
normal build.

## Build

nvcc needs MSVC on PATH, which `vcvars64.bat` provides. Both scripts run from
the repository root.

```
gpu\buildlib.bat          builds gpu\derostorm_gpu.dll
gpu\build.bat             builds the test binaries
gpu\build.bat hash_test   builds just one
gpu\sweep.bat             rebuilds option B at several radix widths
```

After changing the kernels, copy the DLL to `cmd\derostorm\derostorm_gpu.dll`,
which is the copy `go:embed` picks up.

Then the miner:

```
.\build.ps1
bin\derostorm-windows-amd64.exe
```

There is no separate GPU build and no C toolchain in the loop. GPU support used
to go through cgo, which hands the final link to MinGW; the TDM-GCC 10.3.0 on
this machine writes debug sections Windows refuses to load, so every cgo build
died with *"This app can't run on your PC"* before reaching `main()` -- a
trivial cgo hello-world failed the same way, so it was the toolchain and not
this code. Binding the DLL with LoadLibrary instead removed cgo entirely, and
with it that whole class of problem.

Regenerate the vectors with `go run ./gpu/vectors -n 512 -out gpu/vectors.bin`.

## What is left

1. **Coalesce the radix scatter.** The negative result above points straight at
   it: buffer each tile's output in shared memory in digit order, then write it
   out. This is the one change with a clear argument behind it.
2. **Shrink option B's working set.** 48 bytes per text byte is 3.2 MB per
   block. Sorting in two 32-bit stages instead of one 64-bit stage would drop
   the key buffers by half and cut sort traffic ~25%, at the cost of one extra
   pass.
3. **Multi-GPU.** The engine already keys workers by device index and gives each
   one a disjoint nonce range; it has only ever been run on one card.
4. **Show the GPU hashrate separately in the dashboard.** `Engine.GPUHashes()`
   and `Engine.CPUHashes()` exist and are not yet displayed.
