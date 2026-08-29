# DeroStorm

An AstroBWTv3 miner for DERO. Mines on the CPU and, when an NVIDIA card is
present, on the GPU as well. Live themed console, guided first-run setup.

The proof-of-work output is bit-for-bit identical to the reference
implementation — every optimisation here is a faster route to the same 32 bytes,
and `astrobwt/difftest` compares the two on every build.

```
╭─ DEROSTORM ──────────────────────────────────────── AstroBWTv3 · v1.3.0 ─╮
│                                                                          │
│  ◆ MINING                     103.15 KH/s                 15 CPU · 1 GPU │
│       ▁▂▃▄▅▆▇▇▇▇▇▇▇▇▇▇█▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇   60s │
├─ DEVICES ────────────────────────────────────────────────────────────────┤
│  CPU    ████████████████▌░░░░░░░░░░░░░░░░░  33.51 KH/s   32%  71°C       │
│  GPU 0  ████████████████████████████████░░  69.64 KH/s   68%  66°C  215W │
├─ NETWORK ────────────────────────────────────────────────────────────────┤
│  HEIGHT      2,481,903                DIFFICULTY  132,000                │
│  BLOCKS      9                        NETWORK     120.00 KH/s            │
│  MINIBLOCKS  89                       SHARE       12.92% · ~8.5s         │
│  REJECTED    1                        PEAK        104.20 KH/s            │
│  UPTIME      00:12:47                 GPU EFF     324 H/W                │
│  NODE        minernode1.dero.live:10100                                  │
╰──────────────────────────────────────────────────────────────────────────╯
  ▸ 11:04:00  connect    connected to minernode1.dero.live:10100
  ▸ 11:04:17  job        height 2481903 · difficulty 132000
  ▸ 11:04:24  accepted   miniblock 89 at height 2481903
  ▸ 11:04:36  block      block 9 found at height 2481903

  › threads 12▏
```

The **DEVICES** section is the part worth having. A single combined hashrate
cannot tell you that a card stopped contributing an hour ago; a row per device
can, and on a rig it says *which* card rather than only that the total dropped.
Each row carries that device's temperature, and a GPU row its power draw.

`SHARE` is your slice of the network and the mean gap between shares at the
current difficulty, which is what says whether a change to the settings actually
helped. `PEAK` beside the live figure is what says whether the machine is still
as fast as it was, and `GPU EFF` is hashes per watt — on a machine that runs for
months that is the number the electricity bill is denominated in.

See [Temperatures](#temperatures) for where the two numbers come from, and why
the CPU one is harder than the GPU one.

---

## Quick start

Run it. There is nothing to configure first.

```
derostorm
```

On the first run it asks five questions — network, wallet address, node, threads, theme — and saves the answers to `derostorm.json` **next to the executable**. If an NVIDIA card is present it names it and asks a sixth: whether to mine on it as well. Every run after that starts straight into mining.

To change any of it later:

```
derostorm --setup
```

---

## Runtime commands

Type a command and press Enter while it is mining.

| Command | What it does |
|---|---|
| `threads <n>` | Change the thread count live. Also accepts `+2` or `-4`. |
| `theme <name>` | Switch colour theme: `default`, `copper`, `mono`. |
| `save` | Write the current settings to the config file. |
| `config` | Show the active settings and where they came from. |
| `help` | List the commands. |
| `quit` | Stop mining and print a session summary. |

Thread changes take effect immediately and are remembered for the session; run `save` to make them permanent.

`Ctrl-C` also quits cleanly.

---

## Themes

Five themes ship with it. See them side by side without connecting to anything:

```
derostorm --preview
derostorm --preview --theme=copper
```

| Theme | Look |
|---|---|
| `default` | cyan accent, violet secondary, near-black ground. The default. |
| `copper` | burnt copper accent, slate secondary, charcoal ground. |
| `aurora` | emerald and ice on a deep green-black. |
| `ember` | amber and rose on a warm black. |
| `mono` | no colour at all. |

Colour is switched off automatically when output is not a terminal or when `NO_COLOR` is set, so piping to a log file or running as a service produces clean text. A `theme` command cannot override that.

### Window size

The panel needs about **98 columns by 38 rows** — the banner, the panel, the
event log and the command line. It is two rows taller with a GPU than without,
and one row taller again for every extra card, because every device gets its own
row; DeroStorm sizes the window from the device count in the config before
anything is drawn.

DeroStorm asks the terminal for that on start-up, and only ever asks for more
than it has, so a window someone has deliberately made large is left alone. Two
mechanisms are tried, because neither covers both consoles: classic `conhost`
honours the Win32 console calls and ignores the ANSI resize, Windows Terminal is
the other way round.

If the terminal refuses, or the screen is too small to grow into, the event log
is trimmed until the panel fits. This is not cosmetic — the panel is redrawn by
moving the cursor up over its own height, so one that is taller than the window
would walk down the screen leaving a copy of itself behind on every frame. If it
cannot fit even a two-line log, DeroStorm says so and prints plain scrolling
output instead.

---

## Temperatures

The **DEVICES** rows carry a temperature per source, coloured green below 65°C,
amber to 80°C and red above it. Nothing is ever throttled, clocked or fan-curved
in response: the miner reports the number and leaves the decision to you. A
program that quietly backs off on a reading it half-trusts is worse than one
that shows you the reading.

### GPU — always works

Read from **NVML**, the management library inside the NVIDIA display driver. It
is loaded at run time by name, exactly as the CUDA kernels are, so a machine
with a working card always has it and a machine without one simply gets no
telemetry instead of a miner that will not start. Only the read-only queries are
bound — nothing here can change a clock, a fan or a power limit.

That gives temperature, power draw against the enforced limit, fan speed,
utilisation, memory use and core clock. The panel shows temperature and watts;
the watts are also what `GPU EFF` divides by.

On a rig with unlike cards, CUDA numbers devices fastest-first while NVML numbers
them by PCI bus, so the two would disagree about which card is "GPU 1".
DeroStorm sets `CUDA_DEVICE_ORDER=PCI_BUS_ID` before opening anything, which
makes the two the same numbering. Set it yourself to override.

### CPU — reads whatever monitor you already have

**Linux** is straightforward and needs nothing. **Windows needs one of these to
be running**, and most machines with any tuning history already have one
installed:

| | How | Read from |
|---|---|---|
| **Core Temp** | just run it | `CoreTempMappingObjectEx` |
| **HWiNFO** | Settings → Shared Memory Support | `Global\HWiNFO_SENS_SM2` |
| **MSI Afterburner** | Monitoring → tick *CPU temperature* | `MAHMSharedMemory` |
| **LibreHardwareMonitor** | Options → Remote Web Server | `http://127.0.0.1:8085` |

All four are read-only and need no privileges — a section an elevated process
published is still readable from a normal one. Nothing is installed, started or
configured by DeroStorm; it looks, and if it finds nothing it says so.

The first three are shared memory and cost a few microseconds. The fourth is
HTTP and is there because LibreHardwareMonitor has no shared-memory interface.
Point it elsewhere — a different port, or a monitor on another machine in the
rig — with `DEROSTORM_CPU_TEMP_URL`.

### Why Windows needs any of that

**Linux** is straightforward: the kernel has already read the register and
published it under `/sys/class/hwmon`. `k10temp` or `zenpower` on AMD,
`coretemp` on Intel, with `/sys/class/thermal` as a fallback. This is the same
number `sensors` prints, and needs no privileges.

**Windows has no user-mode API for a CPU package temperature.** The value lives
behind an MSR on Intel or the SMU mailbox on AMD, both ring-0. That is why
HWiNFO, Core Temp, Afterburner, LibreHardwareMonitor and Ryzen Master all
install a kernel driver of their own.

DeroStorm will not install one. A miner is not a thing that should be putting
code in your kernel, and a driver installed to display a number is a permanent
increase in a machine's attack surface for a cosmetic gain. So instead it reads
the monitor you already chose to trust, through the interface that monitor
already publishes. That is the whole design: the table above, tried in order of
how much the reading can be trusted.

After those four it falls back to the **ACPI thermal zone**, through the
performance counter Windows publishes for it. That needs no privileges and no
other software at all. On laptops and most servers it tracks the CPU closely; on
many desktop boards it is a chipset zone reporting a fixed, obviously wrong
value — this machine's publishes a constant 290 K, or 17°C — so anything outside
25–125°C is discarded.

When nothing answers, the panel shows `--` and the event log says once what would
fix it. It never guesses. A confidently wrong temperature is worse than no
temperature, because it is the number someone would act on.

**macOS and the BSDs** report nothing. macOS needs a private IOKit interface
whose sensor keys change between Mac models; the BSDs each have their own
sysctl. Both are real options, neither is one line.

Sensors are polled on their own goroutine every two seconds, not on the 200ms
render tick: a temperature does not move five times a second and every read
crosses into a driver.

---

## Options

```
derostorm [options]

  --setup                           Re-run the guided setup.
  --config=<path>                   Use a different config file.
  --wallet-address=<addr>           Override the saved wallet.
  --daemon-rpc-address=<host:port>  Override the saved node.
  --mining-threads=<n>              Override the saved thread count.
  --gpu=<list>                      Mine on these NVIDIA devices: 0, 0,1,
                                    all or off.
  --gpu-batch=<n>                   Nonces per GPU launch.
  --gpu-blocks=<n>                  Resident blocks in the GPU suffix kernel.
                                    Default: measure it while mining.
  --theme=<name>                    default, copper, aurora, ember or mono.
  --no-dashboard                    Plain scrolling output, no live panel.
  --testnet                         Use the DERO testnet.
  --debug                           Verbose logging to the log file.
  --bench                           Benchmark the hash function and exit.
                                    Add --gpu=all to benchmark the GPU too.
  --run-for=<sec>                   Mine for this long, then print a summary.
  --preview                         Show the console with sample data and exit.
```

Command-line flags override the config file for that run; they do not rewrite it unless you run `save`.

Logs go to `derostorm.exe.log` beside the executable — never to the console, so nothing scribbles over the live panel.

---

## Building

Requires **Go 1.22 or newer**. Everything is vendored, so no network access is needed to build.

> **Do not run `go mod tidy` or `go mod vendor` here.** Both ignore `vendor/` and
> go looking for the real modules, and `go.mod` carries
> `replace github.com/deroproject/derohe => ../derohe-main`, a locally patched
> derohe that a clone does not have. `go mod tidy` therefore fails with
> `replacement directory ../derohe-main does not exist`, and `go mod vendor`
> succeeds and quietly deletes the patches — see `THIRD-PARTY-NOTICES.md`.
>
> Nothing else needs them. `go build`, `go test` and both build scripts use
> `vendor/` automatically and never look at the replace target, so a clean clone
> builds with no network and no derohe checkout.

**Windows**

```powershell
.\build.ps1              # build for this machine into .\bin
.\build.ps1 -All         # cross-compile every supported platform
```

**Linux / macOS / Git Bash**

```bash
./build.sh               # build for this machine into ./bin
./build.sh --all         # cross-compile every supported platform
```

Targets built by `--all`: `windows/amd64`, `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.

### Which builds mine on the GPU

| Target | GPU mining | Why |
| --- | --- | --- |
| `windows/amd64` | yes | |
| `linux/amd64` | yes | |
| `linux/arm64` | no | no `.so` is built for it — see below; the kernels are portable, so this is a missing build rather than a missing port |
| `darwin/*` | no | Apple dropped NVIDIA driver support in macOS 10.14 and Apple Silicon never had it, so there is no CUDA on any Mac made since 2018 |

Every other build mines on the CPU and says so; nothing fails.

GPU mining is CUDA, so it means NVIDIA. The library carries a cubin for every
architecture CUDA 13 still supports — `sm_75` (RTX 20xx) through `sm_120`
(RTX 50xx) — plus PTX, so a card newer than the toolkit compiles at load
instead of failing. Pascal and older were dropped by CUDA 13 itself and mine on
the CPU. AMD and Intel cards would need a port to HIP or Vulkan: the kernels
assume a 32-thread warp throughout `gpu/blockradix.cuh`, so that is a rewrite
of the sort, not a recompile.

Only the NVIDIA display driver is needed at run time. The CUDA runtime is
linked into the embedded library, so there is no toolkit to install.

#### What Linux arm64 would take

Left undone deliberately, and recorded here so the next person does not have to
find it out again. "arm64 with an NVIDIA GPU" is two unrelated machines:

- **An arm64 server with a plug-in card** — GH200, or Ampere Altra with an RTX.
  NVIDIA calls this target SBSA, and it cross-compiles from an x86-64 Linux
  host: `cuda-nvcc-cross-sbsa-13-3`, from
  `developer.download.nvidia.com/compute/cuda/repos/ubuntu2404/cross-linux-sbsa`.
  The architecture list is the same as x86-64's, since the cards are the same
  cards. This is a day's work, and `gpu/buildlib.sh` is most of it already.
- **A Jetson** — Orin and friends, where the GPU is part of the SoC. A different
  toolkit (JetPack/L4T), a different driver model, and `sm_87` alone. Not
  covered by an SBSA build, and a separate job.

Neither is built here for one reason: there is no arm64 card to test on, and a
GPU binary nobody has run is worse than an honest CPU-only one. If you have the
hardware, the first case is the easy one.

### What the build flags do

The build scripts pass three non-default flags — two to the Go compiler and one
to MSVC. All three are deliberate and all three matter.

**`-gcflags '…/astrobwtv3=-B'` — bounds checks off, in one package only.**

The suffix-sort package is ~90% of mining CPU time and its inner loops carry two or three bounds checks each. Removing them is worth **+7.8%** hashrate.

This is safe *only because it is tested*. `AstroBWTv3` wraps its body in `recover()` and returns a falsified hash on panic, so an out-of-range index would be silent rather than a crash. The package therefore counts recovered panics, and the test suite asserts that counter stays at zero across millions of hashes built with this same flag. **The build scripts run the tests before building — do not use `--skip-tests` for a build you intend to mine with.**

**It is passed on amd64 and nowhere else.** On arm64 it does not produce a wrong
hash, it produces a crash:

```
unexpected fault address 0x7b681b88b333
fatal error: fault
```

Reported from a Mac and reproduced on `linux/arm64` under qemu, in the miner and
in a bare hashing loop. The same loop is clean on amd64 with `-B`, and clean on
arm64 *without* it — 16,000 hashes each way, `astrobwtv3.RecoveredPanics` zero on
both. So this is not an out-of-range index the checks were hiding: the algorithm
is sound on arm64, and what is not sound is turning the checks off there.

This is the failure the flag was always going to have. It is licensed by a test
suite that only ever ran on the machine doing the building, and the four
cross-compiled targets inherited a guarantee nothing had checked for them. amd64
keeps it because amd64 is what the tests run on; arm64 gives up 7.8% it never
safely had.

**`-pgo=auto` — profile-guided optimisation.**

Uses `cmd/derostorm/default.pgo`. Regenerate it with
`--cpuprofile=cmd/derostorm/default.pgo --run-for=90` if you change the Go hot
path. Note that it only reaches the Go half of a hash — stage 1, the final
SHA-256 and the mining loop. The suffix sort is in the native library, where
Go's profile cannot see it, which is why refreshing the profile after a change
to `native/descriptor.c` is a null (measured 2026-08-29: 34.24/34.25/34.26 KH/s
with a fresh profile against 34.13/34.31 with the shipped one — no difference
outside the noise).

**`/GL` with `/LTCG` — whole-program optimisation, on the native library.**

`native/build.bat` compiles `derostorm_sa.c`, `descriptor.c`, `sha256ni.c` and
`libsais.c` in one command, but without `/GL` each is still its own translation
unit: the descriptor merge cannot inline `suffix_less` across a file boundary
and libsais cannot be specialised for the one way this program calls it. `/GL`
defers code generation to link time, where the optimiser can see all of it.

Measured on `native/sabench.exe` at 15 threads, four interleaved rounds:

| | texts/s |
|---|---:|
| without `/GL` | 44,054 – 45,032 |
| with `/GL /LTCG` | **46,410 – 46,963** |

About **+4.7%** on the sort, every round, with no overlap between the two sets.
End to end that is **+1.8%** on the CPU hashrate — 33.6–34.0 KH/s before,
34.2–34.5 KH/s after, three interleaved `--bench` rounds at 15 threads — because
a whole hash is not only the sort. The output is bit-identical; the 512 vectors
still pass. It costs a slower link and nothing else.

### Building the native libraries

Three of them are embedded in the executable and bound at run time: the CUDA
kernels for Windows, the same kernels for Linux, and libsais for the suffix
sort. They are build products, not source, so they are not in git — build them
once and the copies under `cmd/derostorm/` are what `go:embed` picks up from
then on. After that an ordinary build needs neither a C toolchain nor CUDA, and
they only need rebuilding when their sources change.

**Which ones you need depends on the target, not on your machine**, and for some
targets the answer is none:

| Target | Needs |
| --- | --- |
| `windows/amd64` | `derostorm_gpu.dll` and `derostorm_sa.dll` |
| `linux/amd64` | `libderostorm_gpu.so` |
| `linux/arm64`, `darwin/amd64`, `darwin/arm64` | **nothing** |

So building for macOS needs no GPU, no CUDA and no libraries: `./build.sh` on a
Mac produces a working CPU miner from a clean clone. `build.sh` checks only what
the target it is building actually embeds.

```
.\build.ps1 -Native      # both, then the miner
```

or one at a time:

```
gpu\buildlib.bat         # CUDA kernels  -> cmd\derostorm\derostorm_gpu.dll
gpu/buildlib.sh          # CUDA kernels  -> cmd\derostorm\libderostorm_gpu.so   (run under Linux)
native\build.bat         # libsais       -> cmd\derostorm\derostorm_sa.dll
```

Each script copies its result into `cmd\derostorm\` itself, because doing that
by hand is how a stale library gets shipped. Both build scripts refuse to build
if a copy is missing, since `go:embed` fails with no hint as to the cause.

The CUDA halves need the toolkit and a host compiler — MSVC on Windows, gcc on
Linux; the libsais half needs only MSVC.

`gpu/buildlib.sh` is the odd one, because `nvcc` targets the host it runs on: a
Linux `.so` cannot be produced from the Windows toolkit, whatever flags you
pass. WSL is enough, and needs no GPU of its own — the build wants `nvcc`, not
a card:

```powershell
wsl --install -d Ubuntu-24.04
# then, inside it, NVIDIA's cuda-toolkit package, and:
wsl -- bash -lc "cd /mnt/c/path/to/derostorm && sh gpu/buildlib.sh"
```

`.\build.ps1 -Native` does all of that for you. Once the `.so` exists it is
just a file, so cross-compiling the Linux miner from Windows works normally —
which is the whole reason the GPU binding avoids cgo.

`gpu\build.bat` builds the test harnesses instead. `gpu\hash_parallel_test.exe
gpu\vectors.bin` is the one that matters: it runs 512 real vectors through the
whole GPU hash and compares every byte against the CPU, then reports the block
count curve. Run it after any kernel change.

### Running the tests

```bash
go test ./cmd/derostorm/
```

These cover the console (box geometry, height stability, redraw cleanup when the
frame changes height, colour suppression), the command parser, the CPU-pinning
map, and — most importantly — that the fast difficulty check is bit-for-bit
identical to the reference `big.Int` one. The three `TestGPU*` cases run the card
through the same API the miner uses and check it agrees with the CPU on both the
hash and the difficulty comparison; they skip when there is no CUDA device.

The hash itself is covered in the derohe tree, and this is the suite to run
after touching anything under `astrobwt/`:

```bash
cd ../derohe-main
go test -gcflags='github.com/deroproject/derohe/astrobwt/astrobwtv3=-B' ./astrobwt/...
```

`astrobwt/difftest` compares the optimised package against an untouched copy of
the reference implementation, and `astrobwt/difftest/soak_test.go` asserts the
recovered-panic counter stays at zero across millions of hashes built with bounds
checks off — which is what makes that flag sound.

---

## Where the speed comes from

The hash itself is unchanged. `AstroBWTv3` produces exactly the same 32 bytes it
always did; the suffix array of a string is unique, so a faster way of computing
it cannot change the result. `astrobwt/difftest` holds that line: it compares
every output against an untouched copy of the reference implementation over known
answers, random inputs, nonce sweeps, every length, and boundary bytes. The GPU
is checked the same way — `gpu/hash_parallel_test.exe` against 512 real CPU
vectors, and the miner re-verifies each device against the CPU at start-up before
it will submit anything from it.

Most numbers below were measured on 2026-08-28 against DeroStorm 1.1.0, on a
Ryzen 7 9800X3D (8C/16T, DDR5-6000 CL30) and an RTX 5080, unless a section says
otherwise. Everything in this section except the *Before* columns and the
*What does not help* experiments comes from `derostorm --bench`, which needs no
node and no wallet, so you can reproduce it on your own machine in a minute.

Headline, all of it at once, re-measured on 2026-08-29 at 1.3.0:

| | H/s |
|---|---:|
| CPU, 15 threads | 34,180 – 34,500 |
| RTX 5080 | 71,550 – 71,750 |
| **together, real mining path** | **~103,100** |

The combined figure is `--run-for=60 --gpu=all --gpu-blocks=672`, not the sum of
the two above: on the real path the two share a memory system and a job feed,
and the sum overstates it by two or three percent. The 1.1.0 figures this table
used to carry were 33,506 / 63,400 / 85,437 — the GPU is where almost all of the
gain since has been.

### CPU

| | Before | Now | |
|---|---:|---:|---:|
| 1 thread | 700 H/s | 3,402 H/s | +386% |
| 15 threads | 8.09 KH/s | 33,506 H/s | +314% |

*Before* is stock derohe, measured once on this machine at the start and not
re-run since — it is upstream code and does not move. *Now* is `--bench` on
1.1.0. The intermediate figures this table used to carry (1,875 H/s and
18.03 KH/s) were the state before the descriptor suffix sort landed.

Three changes.

**A suffix sort that knows how the text was made.** This is the large one, and
it replaces libsais on the fast path rather than tuning it.

libsais treats the stage-1 text as arbitrary bytes. It is not arbitrary: stage 1
writes out its whole 256-byte state after each of ~277 iterations and an
iteration rewrites at most 32 of those bytes, so consecutive blocks are
near-copies. Take a run of blocks as 256 columns and walk them right to left,
keeping the run's blocks ordered by the suffixes starting at the current column.
Stepping one column left prepends one byte to each of those suffixes, so the new
order is the old one re-sorted by that byte, stably — and if the column is
constant across the run, the sort is the identity and there is nothing to do.
Roughly 70% of columns are constant across two blocks, so most of the ordering
is inherited rather than computed.

That gives, per run and column, a small list of suffixes already in true order.
Those are grouped by their leading four bytes into descriptors, radix sorted on
that key, and the groups that collide on all four bytes are merged — a merge and
not a sort, because each descriptor's list is already ordered.

Against libsais on the same 512 texts (`native\sabench.exe`):

```
  libsais 2.10.4, 512 texts, 35.2 MB, best of 3, 1 thread

  libsais       0.416 s    1231 texts/s    84.6 MB/s
  descriptor    0.095 s    5381 texts/s   370.0 MB/s   +337.2%
```

That +337% is up from +112% when the sort first landed. The gap widened as the
run-splitting and the descriptor merge were tuned; the libsais column has not
moved, which is the point of keeping it in the same run.

Two things decide whether it pays, and both are measurements rather than
arguments. Runs must be long — 4 blocks is 47% *slower* than libsais, 32 blocks
is 68% faster — because what saves the global sort is the size of the
pre-ordered group, not the column skips. And runs must be cut where stage 1's
RC4 rekey rewrites all 256 bytes: carrying a run through one makes almost every
column non-constant and the per-column insertion sort quadratic in an unbounded
length, which measured 443 texts/s against 2,139.

**Credit where it is due: the idea is not ours.** It comes from the
[Dirtybird C miner](https://github.com/Dirtybird99/Dirtybird-C-Miner) by
Dirtybird99 (MIT), which worked out that stage 1's output is not arbitrary text
and that a suffix sort can exploit it. DeroStorm's implementation is its own
code, written in C against our own run-splitting and descriptor merge, but the
insight that makes it worth writing at all is Dirtybird's. See `CREDITS.md`.

It is checked against libsais over all 512 texts before any timing is reported
— a suffix array is unique, so that is the whole correctness question.

**libsais replaces the Go suffix sort**, and is now the fallback behind the
descriptor sort above rather than the fast path. The suffix array is ~90% of a hash, and
the Go SA-IS in `astrobwtv3` is not the fastest way to build one. libsais is,
on this data, by a consistent margin:

| threads | built-in (Go SA-IS) | descriptor + libsais | |
|---:|---:|---:|---:|
| 1 | 845 H/s | 2,849 H/s | +237.3% |
| 4 | 2,649 H/s | 9,324 H/s | +252.0% |
| 8 | 4,986 H/s | 17,950 H/s | +260.0% |
| 15 | 8,450 H/s | 29,707 H/s | +251.6% |

These are whole hashes, not sorts, which is why they sit below the plain
`--bench` throughput on the same thread count: the two sorts run interleaved so
neither gets the quiet half of the machine. Run-to-run spread on the 15-thread
row is about ±7 points (+251.6% and +258.4% on two runs a minute apart).

When libsais alone replaced the Go sort — before the descriptor sort existed —
the same table read +19.6% / +23.3% / +16.2% / +18.4%. An earlier figure of +30%
for that change was measured while another miner was running on the same
machine, which flatters it: the Go sort degrades further under contention than
libsais does, so the gap widens for reasons that have nothing to do with mining
alone. `--bench` prints this comparison on your own machine, and
it interleaves the two sorts rather than running one after the other, because on
a desktop doing anything else a sequential A-then-B mostly measures which one
ran while the machine was quieter.

Correctness is not a judgement call here. The suffix array of a string is
unique, so a faster one that is *correct* produces the same array and therefore
the same hash; one that is *wrong* changes every hash it touches, and because
AstroBWTv3 swallows panics the symptom would be a miner at full hashrate that
never finds a share. So the library is proved before it is trusted: it runs a
self-test on load, and `sa_test.go` puts 332 inputs through both sorts — nonce
sweeps, random data, every length from 1 to 80, all-zero, all-0xff — and
compares the final 32 bytes. Anything at all going wrong, from a missing DLL to
a failed self-test, falls back to the Go sort and says so on the console.

**The four SA-IS induction scans no longer cache the bucket cursor in a
register.** This one is small.

The stock code kept a copy of `bucket[c]` and only touched the table when the
character changed, on the reasoning that suffixes arrive in sorted order so the
character has good locality. It does — but the character here is derived from RC4
output, and "good locality" still leaves the branch mispredicting often enough
that it costs more than the L1 access it was avoiding. The table is 256 entries.
Reading and writing it every iteration is unconditional and pipelines; the branch
did not.

Why +21% on one thread and only +3% on sixteen: SMT was already hiding those
mispredicts. With two threads per core, one thread's stall is the other thread's
opportunity, so the core was near capacity either way. The win is real but it is
a latency win, and at 16 threads this machine is not latency bound.

Two things that looked promising and were not, both measured and both discarded:

- **Branchless type detection** in the same loops, the trick that
  `placeLMSrec` already uses. 1-2% *slower*: the mask arithmetic lands on the
  critical path, and unlike the bucket test this branch predicts well.
- **Software-pipelining the random reads in `assignID`**, which is 12% of the
  hash and every read of it is an L2 or L3 hit. No measurable change — the
  out-of-order engine was already running those loads ahead.

Everything before this release still applies and is the larger part of the total:
removing the stock suffix sort's three redundant text walks (+17.7%), bounds
checks off in that package (+7.8%), gathering the recursion's subproblem instead
of scanning for it (+3.2%), branchless LMS detection in `placeLMS` (+2.6%), and
profile-guided optimisation (+1.2%).

### GPU

| | Before | Now | |
|---|---:|---:|---:|
| RTX 5080 | 7.45 KH/s | 63,400 H/s | +751% |

*Before* is the GPU on its own on the real mining path
(`--mining-threads=1 --gpu=0 --run-for=90`), measured when GPU support first
worked. *Now* is `--bench --gpu=all`, which runs the same kernels over the same
batch size without needing a node. The intermediate 12.28 KH/s this table used
to carry was the gain from the packed-key change described below; the rest came
from the block-count sweep and the stage-1 work after it.

**The column walk runs on four threads a run, not one.** The largest single gain
the GPU has had since the descriptor sort itself.

The walk inherits: the order at column `rel-1` is the order at column `rel`
re-sorted by one byte. That is the saving, and it is also why the phase ran on
~62 of `BR_BLOCK` threads — one per run, 256 steps each, and the steps are a
chain.

Three probes, each adding one operation and reading the cost off the delta with
the output still correct, say the chain is the cost and the work is not:

```
  + one atomic per descriptor      -0.5%
  + one scattered word per descriptor  -2.1%
  + one arena word per position    -4.6%
  + one text read per position      free
```

Those sum to nothing like the walk's 37%. What is left is threads standing idle
and a 256-long dependency chain.

The chain can be cut, because **inheritance is an optimisation, not a
definition**. The order at a column is the run's blocks sorted by the suffixes
starting there — a function of the text and nothing else. So a thread can start
anywhere: sort directly at the top of its own piece, then inherit down through
it. Four pieces is four seed sorts a run instead of one, against a chain of 64
instead of 256 and four times the threads.

The arena offsets fell out of it. Every column of a run emits every one of its
blocks exactly once, so column `rel` writes at `(255 - rel) * len` — no running
total, which is exactly what let the pieces write into one arena without
meeting.

Swept at the shipped `BR_BLOCK=256`, whole hash, all correct:

```
  1 piece    53,889 H/s        4 pieces   60,451 H/s   <- default
  2 pieces   59,728 H/s        8 pieces   54,209 H/s
```

**The count has to divide 256**, and this is why there is a `static_assert` on
it: 3 and 6 were measured producing a *wrong* suffix array, because 3x85 and
6x42 leave a column nobody walks. The 512-vector check caught both.

The best count is not a property of the algorithm. At `BR_BLOCK=1024` it is 8
and the sort measures 59,455 SA/s against 41,643; at the shipped 256 it is 4 and
8 is *slower than 1*. The order and key arrays are per piece, so the count buys
threads with shared memory, and how much of that is spare depends on the block
size. Sweep it against the configuration you ship, not the harness.

**The column walk's keys slide instead of being re-read.** The gain before that
one, and it came from reading the profiler rather than from an algorithm.

`gpu\prof\prof.exe` attributes cycles by phase. It put the column walk at **51%
of the suffix sort**, which is itself 85% of a GPU hash — so a third of the
kernel was one loop, and that loop runs on about 62 of `BR_BLOCK` threads,
because the runs are the only parallelism in it. Its latency is the kernel's.

What it spent them on was loads. A descriptor key is the four bytes at
`ord[x]+rel`, and the walk re-read all four at every column. Three of them are
the ones it read at the column before:

```
  K(q-1) = t[q-1]<<24 | t[q]<<16 | t[q+1]<<8 | t[q+2]
         = t[q-1]<<24 | (K(q) >> 8)
```

No end-of-text case is needed — the shift drops exactly the byte the zero
padding would have had to invent. And the one byte it does read is the same byte
the constant-column test compares and the same byte the insertion sort orders
by, so all three share one load. A column went from about `6 * len` scattered
byte reads to `len`, the grouping scan moved from text to shared memory, and the
insertion sort stopped touching text at all. The keys live in `s_keep`, which
phase 1 has finished with, so it cost no shared memory.

Measured on 512 real texts, the same binary either way:

```
  descriptor sort      33,272 -> 41,468 SA/s     +25%
  whole GPU hash       30,568 -> 36,753 H/s      +20%
  miner, RTX 5080      45.42 -> 56.09 KH/s       +23%   (Linux 44.28 -> 55.67)
```

The profile after it: the walk is down to 37% and the collision merge is now the
larger remaining phase at 28%. The obvious next step does not fit — knowing a
column is constant *before* loading it would cut most of the remaining loads,
since a constant column slides every key by the same byte, but the mask is
278x256 bits and there is no shared memory left beside the radix sort's tile.

**Stage 1 as a table, not a switch.** AstroBWTv3 picks one of 256 byte
operations per iteration, and a warp whose 32 lanes pick 32 different ones runs
them one after another. That looked like a fixed cost of the algorithm.

It is not. Every one of the 256 operations is exactly **four instructions from
one set of sixteen** — @Wolf9466's observation, published in tnn-miner, see
`CREDITS.md` — so the op can choose data instead of code. `gpu/gencases` emits a
512-byte table and its decoder from `pow.go`, and fails the build if any case
stops being four instructions; `gpu/stage1.cuh` applies the four statements that
are not instructions by op number.

Measured with `gpu\hash_parallel_test.exe`, which times stage 1 separately, three
runs each, the same binary switched with `-DSTAGE1_SWITCH`:

```
  stage 1        switch  24.4  24.4  24.4 ms
                 table   22.8  23.1  22.6 ms      -6%
  whole hash     switch  30422 30563 30399 H/s
                 table   30870 30772 30596 H/s    +0.9%
```

Six percent of stage 1 and about one of the hash, because stage 1 is only 8.6%
of GPU hash time to begin with — the 256-way branch was never the expensive part
of it; the per-iteration hashing and the 256-byte append are. The size is the
larger result: 2,572 lines of generated switch, once per architecture in a fat
binary, became one table, and the embedded CUDA library went from 5,840 KB to
2,124 KB.

Nesting the loops the other way — instruction outside, bytes inside, so the
branch is taken four times per op rather than four times per byte — is also
correct and measured 23.4 ms, slightly worse: it reads and writes shared memory
four times per byte instead of once. The same swap on the CPU goes the other way
by a factor of 4.7, which is why stage 1 in Go still uses its switch. See below.

The kernel is checked before it is timed: `--bench` verifies the card against
the CPU, and `gpu\hash_parallel_test.exe gpu\vectors.bin` reports
`CORRECT: all 512 hashes match the CPU exactly`.

Resident blocks matter more than anything else, and the curve is not flat:

```
    blocks            H/s       ms/batch
        21           4794           1709
        42           9219            889
        84          18069            453
       168          29199            281
       336          45485            180
       672          48636            168
      1252          51931            158
```

The top of that curve is within noise of itself — a second run picked 672 blocks
at 50,736 H/s — so the miner measures it while mining rather than shipping a
constant. Pin it with `--gpu-blocks=<n>` if you would rather it did not.

The suffix sort is ~95% of GPU hash time, so everything below is about it.

**The sort carried a 64-bit key beside a 32-bit value; both fit in one word.**
A rank and a suffix index are each a position in [0, n), which for a 71 KB text
is 17 bits. The key is two ranks and the payload is one index — 51 bits
together. So the pass now moves one 8-byte word per element instead of a
12-byte pair, and the radix sort is told to order by a bit *range* and leave the
bits below it alone, which carries the index along for nothing. Two whole
n-sized arrays disappeared with it.

**The doubling started at one byte; it now starts at four.** Prefix doubling has
to seed somewhere, and the first two rounds after a one-byte seed are the most
expensive in the run, because nothing has been resolved and the active set is
still the whole array — measured over 512 real texts, 100% of suffixes are still
tied going into k=1 and 93% into k=2. A four-byte first sort replaces both. In
passes over n: 2 + 5 + 4.65 = 11.65 becomes 6.

| | suffix arrays/s |
|---|---:|
| before | 6,910 |
| one packed word | 9,881 |
| four-byte seed | 13,390 |

Five bytes was tried and is worse (10.8k): it needs a seventh pass and the extra
resolution does not pay for it. Radix widths of 6 and 8 bits were tried and are
both slightly worse than 7.

One near miss worth recording, because it was caught by the vector check and
would not have been caught by anything else. The seed packs each byte into
*nine* bits, so that "ran off the end of the text" is a value below every real
byte. Eight bits and a zero pad looks sufficient — a tie at round 0 is harmless,
since the doubling loop resolves whatever groups it is handed. It is not. Two
suffixes that both run off the end within k get the same past-the-end marker for
their second rank too, so the doubling cannot separate them either, and the tie
survives to the end. It broke 3 of 512 texts, in the first entry of the array
only. A miner would have looked completely healthy and produced a wrong hash
about half a percent of the time.

### Memory

`ScratchData`, one per mining thread, carried 768 KB of buffers for a counting
sort AstroBWTv3 does not call — allocated, zeroed by the runtime, never read.
They are gone, with the dead sort itself. Per thread it is 2.06 MB down to
1.27 MB; at sixteen threads, 12 MB less resident and 12 MB fewer pages to fault
in at start-up. It did not change the hashrate, and it was never going to: a
buffer nothing touches is never in cache.

### What does not help

**Every nvcc flag worth trying.** The suffix kernel is memory-saturated, so the
temptation is to look for a compiler switch that moves it. Three were built as
single-architecture `sm_120` libraries and A/B'd through the real miner
(`--bench --gpu=all`), twice each, interleaved:

| | best H/s |
|---|---:|
| control (`-O3`, as shipped) | 71.88 / 71.87 K |
| `--extra-device-vectorization` | 71.30 / 71.43 K |
| `-Xptxas -dlcm=cg` | **35.37 / 35.34 K** |

`-dlcm=cg` bypasses L1 for global loads, which halves the rate: the descriptor
walk re-reads the same 68 KB of text constantly and lives on L1 hits. The
vectorisation flag is a small consistent loss. There is nothing here.

A plain rebuild is also a null, which is worth knowing before anyone suspects a
stale artefact: the shipped fat binary against a fresh single-architecture build
of the same source, three interleaved rounds, is 71.55–71.70 K against
71.62–71.75 K.

**More mining threads than the machine has logical CPUs.** The suffix sort has
real headroom left inside each core, and this is the measurement that proves it.
`native/sabench.exe` oversubscribed, three rounds each:

| threads | texts/s | vs 16 |
|---:|---:|---:|
| 16 | 46,043 | — |
| 24 | 47,398 | +2.9% |
| 32 | 49,399 | +7.3% |
| 48 | 51,224 | +11.3% |
| 64 | 51,433 | +11.7% |

Sixteen threads already fill every logical CPU, so a 17th cannot add a core --
it can only add another independent instruction stream to a core that was
stalling. Gaining 11.7% while *also* paying for context switches means the cores
still have idle issue slots with two SMT threads on them. See
[Where the speed comes from](#where-the-speed-comes-from) for what that does and
does not imply.

It does not translate. The whole hash at 20 threads is 34.67 KH/s against 33.73
at 15, and flat after that -- stage 1 and the final SHA-256 are not stalling, so
they dilute it. And with the GPU running it reverses completely, because the GPU
worker needs a thread to feed it and an oversubscribed machine starves it:

| threads | combined |
|---:|---:|
| 15 | **103.16 / 103.36 KH/s** |
| 18 | 101.00 / 99.74 KH/s |
| 20 | 100.03 / 99.53 KH/s |
| 24 | 95.87 / 97.53 KH/s |

The GPU is two thirds of the total, so starving it costs more than the CPU can
win. The headroom is real and adding threads is not how to reach it.

**Fewer CPU threads, once the GPU is running.** The GPU worker needs a
thread to feed it, so 15 of 16 might be one too many. Measured on the real
mining path, `--run-for=45 --gpu-blocks=672`, two rounds each:

| threads | combined |
|---:|---:|
| 13 | 102.46 / 101.19 KH/s |
| 14 | 103.26 / 102.75 KH/s |
| 15 | **103.71 / 101.64 KH/s** |
| 16 | 101.30 / 97.10 KH/s |

14 and 15 tie inside the noise, 16 is clearly worse — the GPU worker and the
16th miner fight over the same core. The shipped default of *cores × 2 − 1* is
already the right answer, from both directions.

**Both obvious attacks on the CPU merge.** `native\saprof.exe` puts the phases
at merge 43.6%, column walk 37.6%, radix sort 17.0% — so the merge is the
largest, and it is resolving only **1.8% of key groups and 3.2% of positions**.
Spending 44% of the sort on 3% of the data looks like an error. It is not; both
ways of fixing it lose.

*A longer descriptor key.* The key is three bytes (`DSA_KEY_BYTES`), so wider
keys mean fewer collisions and less merging. Four bytes does exactly that, and
still loses, because the radix sort needs a third pass:

```
  3 bytes   329 colliding groups   merge 43.7%  radix 17.0%   5403 texts/s
  4 bytes   247 colliding groups   merge 38.5%  radix 24.2%   5267 texts/s
```

The merge gave up 73M cycles and the sort paid 140M for them.

*Pre-reading the comparison's first eight bytes.* Every `suffix_less` loads two
eight-byte windows at unrelated offsets, and a position is compared more than
once, so reading each position's `head8` into an array carried through the merge
should turn scattered text loads into sequential ones. It is bit-exact and it is
slower — 5,270 against 5,455.

The reason is the group size. A pairwise merge of L lists compares each position
about log2(L) times, and the average group here is **seven** positions in a few
lists: its text is in L1 after the first comparison, so there is nothing left to
save and the pre-pass is pure cost. Gating it on group size, so only the tail
(930 positions in 279 lists) pays for it, recovers most of the loss but not all
of it — 5,385 — because the branch that chooses costs about what it saves.

The finding is that the merge is not latency bound the way the phase share
suggests. It is 5,229 comparisons over data that is already hot.

**The stage-1 instruction table, on the CPU.** It is a clear win on the GPU
(above) and the obvious next move is to do the same in `pow.go`, replacing 2,300
lines of switch with a 512-byte table. Both forms generated from `pow.go`, proved
identical on all 256 ops first, then timed:

```
  switch    25.1 ns/op
  table    118.0 ns/op        4.7x slower
```

The reason is the loop nesting, and it is the exact opposite of the GPU's. The
switch chooses the operation **once** and then runs a window of up to 32 bytes
with no branch in sight. The table branches four times **per byte**. A CPU
predicts the one outer branch almost perfectly and a GPU cannot, which is the
whole difference between the two answers.

tnn-miner does use the table on the CPU, and is right to: it pairs it with AVX2,
doing 32 bytes per instruction, which is what makes the shape pay. DeroStorm's
stage 1 is Go, where that is not expressible without assembly, and the ceiling
would not justify it — the operation loop is under 6% of a CPU hash, so a
perfect result is worth about +0.1 KH/s of the machine's ~45.

The three below are recorded for the same reason, and all three are wrong.
Measured on a 9800X3D with DDR5-6000 CL30 and an RTX 5080.

**These three were measured on an earlier build and have not been re-run.** The
absolute H/s in them are therefore low against the numbers above — read them as
ratios, not as throughput. The conclusions are about the shape of the workload,
which the later work did not change: it is still L3-resident and still not
DRAM-bound.

**Faster system RAM.** The CPU hash is not memory bound, and it is not close.
Testing it takes multiplying the footprint without changing the work: give each
thread N scratch buffers instead of one and rotate through them, one per hash.
Same inputs, same instruction stream, more memory.

```
 buffers   footprint         H/s   vs one
       1       18 MB      7115.7     0.0%
       2       37 MB      6819.1    -4.2%
       4       73 MB      6518.3    -8.4%
       8      146 MB      6686.0    -6.0%
      16      293 MB      6579.0    -7.5%
      32      585 MB      6568.1    -7.7%
      64     1170 MB      6492.7    -8.8%
```

Running the whole thing out of DRAM — 1.17 GB, sixty-six times past this chip's
96 MB of L3 — costs **8.8%**. That is the entire distance between "every access
is a cache hit" and "every access is a DRAM round trip". At its natural
footprint, 15 threads share 18 MB and sit comfortably inside L3, which is the top
row of that table.

So faster RAM cannot buy 8.8%; it can only buy some fraction of the gap between
DDR5-6000 and the next kit up, on a workload that has already shown it barely
notices a 66× working-set increase. The reason is the access pattern: the
induction scans walk `sa` linearly and the bucket scatter has only 256
destinations, and streams like that prefetch about as well from DRAM as from
cache.

The thing that *does* matter is the 3D V-Cache, and it is already doing its job.

Two later measurements, taken a different way, agree. The first is the cleanest
evidence in this file, because it does not measure the miner at all — it
measures what the miner leaves for everything else:

```
a 4-thread DRAM streamer, buffers far past L3, read-modify-write

  running alone                27.5 GB/s
  running beside 12 mining threads   27.5 GB/s
```

Mining takes **no measurable bandwidth away from it**. The bus saturates at
about 28 GB/s counted, which is ~56 GB/s of real traffic once each cache line is
counted as a fill plus a writeback, and it saturates from two threads onward.
A workload competing for that would show up here. This one does not appear at
all.

The second separates cache pressure from bandwidth pressure by giving the miner
a control to be measured against — a load that takes the same four cores and
touches almost nothing:

| 12 mining threads, plus | H/s | vs alone | vs the control |
|---|---:|---:|---:|
| nothing | 8,796 | | |
| 4 threads of pure compute (control) | 7,869 | −10.5% | — |
| 4 threads streaming DRAM | 7,206 | −18.1% | −8.4% |
| 4 threads thrashing L3 | 7,705 | −12.4% | −2.1% |

Most of every drop is simply the four cores taken. What is left over after the
control is the memory effect, and the DRAM streamer's extra 8.4% is not it
buying bandwidth the miner wanted — the first measurement rules that out. It is
the streamer walking 2 GB through L3 and evicting the miner's resident working
set, which then has to be fetched back. The L3 thrasher costs less because 96 MB
of stride-walk evicts less than 2 GB of streaming does.

Put together: **AstroBWT on this CPU is L3-resident, not DRAM-bound.** Faster or
larger DDR5 cannot help a workload that is not waiting on DDR5. What can hurt it
is anything that evicts it from L3, which is an argument for not running a
second memory-heavy program beside the miner, and not an argument for a memory
kit.

**Host RAM as GPU scratch.** Unified or pinned host memory would put the suffix
scratch on the far side of PCIe 5.0 x16 — about 64 GB/s against roughly 960 GB/s
of VRAM. Fifteen times slower for the array that every round of the sort streams
through. It would also buy nothing even if it were free: the block-count curve is
flat past 84 blocks, so more hashes in flight, which is what more memory buys, is
not what the card is short of.

**A bigger batch.** `--gpu-batch` is a latency knob, not a throughput one. The
whole batch is already one kernel launch, and a batch that takes longer only
means longer before the miner notices a new job.

**Compiler flags on libsais.** It is 84% of CPU hash time and contains no SIMD
intrinsics at all, so a wider instruction set for the auto-vectoriser looked
like free money. `native\sabench.exe` times it on the real texts:

```
  /arch:AVX2      1234 texts/s      /GL (whole program)   1227 texts/s
  /arch:AVX512    1235 texts/s      no /arch              1228 texts/s
```

Nothing, in either direction. The sort is pointer chasing and unpredictable
branches, and there is nothing in its inner loops for a vector unit to do.

**Hashing two nonces' suffix arrays as interleaved SHA-256 chains.** SHA-256 is
9.3% of a CPU hash and `sha256rnds2` is latency bound, so one chain leaves the
unit part idle and two would fill it. Worth about +3%, on paper.

The paper is wrong, and one measurement says why. SHA-256 throughput on this
machine, one thread per core against two:

```
   8 threads    16,798 MB/s
  16 threads    27,532 MB/s     +64%
```

If a single chain were saturating the unit, the second thread on each core would
add nothing. It adds 64%, because the two mining threads sharing a core are
*already* two independent SHA-256 chains interleaved on that unit — SMT is doing
the trick by hand. What is left is about 1% of total hashrate, for a second
scratch buffer per thread, hand-written SHA-NI intrinsics, and a change to the
consensus-critical path. Left alone.

## Tuning

- **Use every logical CPU.** SMT genuinely helps here, because the sort is
  latency bound. Sixteen threads beat eight by 69% on the test machine.
- **Do not oversubscribe.** Past the logical CPU count it gets slower.
- **The default leaves one CPU free**, which the benchmark disagrees with — it
  measures 16 threads faster than 15. The benchmark has nothing else to run;
  mining has the getwork socket, the console, and the thread that feeds a GPU.
  Ask for the last one with `--mining-threads=16` if the machine is doing
  nothing else.
- **Let the GPU tune itself once**, then pin the result with `--gpu-blocks=<n>`
  so later starts skip the sweep. The console prints the value it chose.
- Threads are pinned to CPUs automatically, spreading over physical cores before
  using SMT siblings.
- **Faster RAM is not worth buying for this.** See *What does not help* above:
  running the CPU hash entirely out of DRAM costs 8.8%, and at its natural
  footprint it never leaves L3.

### Profiling the GPU further

`gpu/prof/` gives phase shares without any special permission — see the GPU
section above. What it cannot give is *why* a phase is slow, and for that Nsight
Compute needs a driver permission:

> NVIDIA Control Panel → Desktop → Developer settings → *Manage GPU Performance
> Counters* → allow access to all users. Needs admin, and takes effect on the
> next launch.

Then:

```
ncu --section SpeedOfLight --section WarpStateStats ^
    --kernel-name suffix_kernel --launch-count 1 --clock-control none ^
    gpu\hash_parallel_test.exe gpu\vectors.bin
```

The note at the top of `gpu/blockradix.cuh` carries the full record: what the
phase shares were, what was changed, and what was tried and thrown away.

## Layout

```
derostorm/
├── bin/                    built binaries
├── cmd/derostorm/          the miner
│   ├── main.go             flags, startup order, run loop, benchmark, preview
│   ├── setup.go            first-run wizard
│   ├── config.go           the derostorm.json file
│   ├── engine.go           getwork + the mining threads
│   ├── target.go           allocation-free difficulty check
│   ├── dashboard.go        the live console
│   ├── theme.go            palettes
│   ├── commands.go         runtime command line
│   ├── affinity.go         CPU-slot → logical-CPU map
│   ├── gpu_cuda.go         binds the CUDA library and drives it
│   ├── gpu_cuda_windows.go   embeds the .dll, finds symbols with LoadLibrary
│   ├── gpu_cuda_linux.go     embeds the .so, finds symbols with dlopen
│   ├── gpu_other.go        the no-GPU build: macOS, and Linux off amd64
│   ├── gpu_worker.go       the GPU mining worker
│   ├── gpu_tune.go         measures the suffix kernel's block count
│   ├── gpu_bench.go        --bench for the GPU
│   ├── sa_windows.go       loads libsais, proves it, installs the hook
│   ├── sa_test.go          332 inputs through both sorts, hashes compared
│   ├── sa_bench.go         --bench: the two sorts, interleaved
│   └── default.pgo         profile for -pgo=auto
├── native/                 the C suffix sort
│   ├── derostorm_sa.c      the C API: sort, version, self-test
│   ├── descriptor.c        the structure-exploiting suffix sort
│   ├── sabench.c           checks and times both sorts on the real texts
│   └── libsais/            upstream libsais, unmodified (Apache-2.0)
├── gpu/                    the CUDA kernels and their test harnesses
│   ├── derostorm_gpu.cu    the three kernels and the C API
│   ├── stage1.cuh          the 256-way state machine, thread per hash
│   ├── sa_doubling.cuh     suffix array by prefix doubling, block per hash
│   ├── blockradix.cuh      the block-wide radix sort under it
│   ├── hash_parallel_test.cu  whole hash against 512 real CPU vectors
│   ├── prof.cuh            phase timers, compiled out unless DS_PROF
│   └── prof/               cycle attribution for the suffix kernel
├── vendor/                 all dependencies, including the optimised derohe
├── build.ps1 / build.sh
├── README.md
├── CREDITS.md               who got there first
├── LICENSE                  MIT, for DeroStorm's own code
└── THIRD-PARTY-NOTICES.md   the licences that are not MIT
```

`vendor/` contains the full optimised `derohe` source, so this folder builds standalone. If you re-point `go.mod`'s `replace` at a newer derohe checkout, re-run `go mod vendor`.

---

## Licence

derostorm's own code — `cmd/derostorm/`, `gpu/`, the C files directly under
`native/`, the build scripts and this README — is MIT. See `LICENSE`.

Everything it is built on is not, and the restrictions are real:

**DERO (`vendor/github.com/deroproject/derohe/`) is under the DERO Project's
RESEARCH licence.** That licence covers research, evaluation, teaching and
personal use, and expressly excludes commercial use or distribution. Commercial
use needs a separate licence from the DERO Project. Since derostorm builds on
that code, the restriction reaches any build that includes it.

`native/libsais/` is libsais by Ilya Grebnov, under Apache-2.0 and unmodified —
see `native/libsais/LICENSE`. Only the small C wrapper around it in
`native/derostorm_sa.c` is ours.

Full detail in `THIRD-PARTY-NOTICES.md`.

## Credits

The descriptor suffix sort — the single biggest CPU win in this miner — follows
an idea first published in the [Dirtybird C
miner](https://github.com/Dirtybird99/Dirtybird-C-Miner) by Dirtybird99 (MIT).
The implementation here is ours; the insight is theirs. `CREDITS.md` has the
full list.

Mining rewards go to whatever address you configure and to nobody else; there is
no developer fee.
