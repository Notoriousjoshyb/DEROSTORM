# DeroStorm

An AstroBWTv3 miner for DERO. Mines on the CPU and, when an NVIDIA card is
present, on the GPU as well. Full-screen themed console, guided first-run
setup.

The proof-of-work output is bit-for-bit identical to the reference
implementation — every optimisation here is a faster route to the same 32 bytes,
and `astrobwt/difftest` compares the two on every build.

```
      ░▒▒▒▒▒░        ██████  ███████ ██████   ██████  ███████ ████████  ██████  ██████  ███    ███
    ░▒▓█████▓▒░      ██   ██ ██      ██   ██ ██    ██ ██         ██    ██    ██ ██   ██ ████  ████  ┌────────┐
   ░▓█████████▓▒     ██   ██ █████   ██████  ██    ██ ███████    ██    ██    ██ ██████  ██ ████ ██  │ v1.6.1 │
   ▒███████████▒     ██   ██ ██      ██   ██ ██    ██      ██    ██    ██    ██ ██   ██ ██  ██  ██  └────────┘
    ░▒▓▓█▟▙█▓▒░      ██████  ███████ ██   ██  ██████  ███████    ██     ██████  ██   ██ ██      ██
        ▝█▛                                    ASTROBWTv3 MINER FOR DERO
 NODE: dero-node.mysrv.cloud:10100   NETWORK: mainnet   UPTIME: 03:12:08             DERO MINING CONTROL CENTRE
────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 ┌─◈─MINING PERFORMANCE──────────────┐┌─◈─HASHRATE HISTORY─────────5 MIN─┐┌─◈─NETWORK STATUS──────────────────┐
 │  ╻  ┏━┓ ┏━╸     ╻  ╺━┓            ││120K ┤⠤⠤⠤⣀⢄⡠⠤⠤⢄⡠⣀⠤⡠⢄⡠⢄⠔⢄⠤⣀⠤⡠⠢⡠⢄⡠⢄ ││ STATUS                ● CONNECTED │
 │  ┃  ┃ ┃ ┗━┓     ┃   ━┫            ││     │                            ││ NETWORK HASHRATE        5.59 MH/s │
 │  ╹  ┗━┛ ╺━┛ ▄   ╹  ╺━┛  KH/s      ││ 60K ┤                            ││ DIFFICULTY            100,580,000 │
 │ TOTAL HASHRATE                    ││     │                            ││ HEIGHT                  7,541,894 │
 │ CPU 33.04 KH/s  ███▊░░░░░░░░  31% ││   0 ┤                            ││ PEERS                      6 / 12 │
 │ GPU 72.09 KH/s  ████████▎░░░  69% ││     └5m──4m───3m───2m───1m───Now ││ LATENCY                     42 ms │
 └───────────────────────────────────┘└──────────────────────────────────┘└───────────────────────────────────┘
 ┌─◈─CPU PERFORMANCE────────────────┐┌─◈─GPU PERFORMANCE────────────────┐┌─◈─SHARE STATS─────────────ALL TIME─┐
 │   ▄▄████▄▄    THREADS         15 ││   ▄▄████▄▄    TEMP          62°C ││   ▄▄████▄▄    ACCEPTED       1,245 │
 │  ██▀    ▀██   TEMP          56°C ││  ██▀    ▀██        8.2 / 16.0 GB ││  ██▀    ▀██   REJECTED          12 │
 │ ██  96%   ██  FREQ      4.22 GHz ││ ██  99%   ██  CLOCK     2730 MHz ││ ██ 99.0%  ██  STALE             -- │
 │ ██  LOAD  ██  POWER           -- ││ ██  LOAD  ██  POWER        215 W ││ ██ACCEPTED██  INVALID           -- │
 │  ██▄    ▄██                      ││  ██▄    ▄██                      ││  ██▄    ▄██                        │
 │   ▀▀████▀▀                       ││   ▀▀████▀▀                       ││   ▀▀████▀▀    SUBMITTED      1,257 │
 └──────────────────────────────────┘└──────────────────────────────────┘└────────────────────────────────────┘
 ┌─◈─BLOCKCHAIN STATUS──────────┐┌─◈─SYSTEM OVERVIEW────────────┐┌─◈─LIVE MINING LOG──────────────────────────┐
 │ HEIGHT             7,541,894 ││    CPU TEMP      GPU TEMP    ││ 09:18:20 [ACCEPTED] share accepted (12ms)… │
 │ MINI BLOCKS            1,245 ││      56°C          62°C      ││ 09:18:16 [ACCEPTED] share accepted (15ms)… │
 │ BLOCKS FOUND               0 ││  ████▌░░░░░░░  █████▌░░░░░░  ││ 09:18:00 [JOB]      new job received (dif… │
 │ ORPHANED                  -- ││    CPU LOAD       MEMORY     ││ 09:17:40 [ACCEPTED] share accepted (11ms)… │
 │ DIFFICULTY         5,587,798 ││      96%         8.2/32G     ││ 09:17:15 [WARN]     high difficulty detec… │
 │ NET DIFF         100,580,000 ││  ███████████▋  ███▏░░░░░░░░  ││ 09:17:04 [INFO]     mining is running smo… │
 └──────────────────────────────┘└──────────────────────────────┘└────────────────────────────────────────────┘
 ┌─◈─ACTIVE THREADS─────15 CPU─┐┌─◈─MINING STATUS───────────────┐┌─◈─QUICK STATS───────────┐┌─◈─CONNECTION────┐
 │ T01 █████████████████ 100%  ││              ⢀⡀⡀⣀⢀⢀⢀⢀⢀        ││ UPTIME         03:12:08 ││   ● CONNECTED   │
 │ T02 ████████████████▌  97%  ││   ⡰⠂     M I N I N G    ⠁⠂⠢   ││ TOTAL H… 39,874,982,145 ││                 │
 │ T03 ████████████████░  94%  ││   ⠳⢄    IN THE STORM          ││ ACCEPTED          1,245 ││ SIGNAL    ▁▃▄▆█ │
 │ T04 ███████████████▌░  91%  ││     ⠈⠑⠐⠂⠤⠄⠤⠠⠤⠠⠤⠒⠊⠁   ⣀⠴⠃      ││ REJECTED             12 ││ LATENCY   42 ms │
 │ +11 more                    ││              ⠄⠠⠠⠠⠐⠐⠂⠉         ││ STALE                -- ││      GOOD       │
 └─────────────────────────────┘└───────────────────────────────┘└─────────────────────────┘└─────────────────┘
 [M] MINING   [S] STATISTICS   [N] NETWORK   [T] THREADS   [C] CONFIG   [L] LOGS   [P] POOLS   DEROSTORM v1.6.1
```

Eight screens, one key each. The **dashboard** above is the one you leave up;
`M` `S` `N` `T` `C` `L` `P` `H` go to mining, statistics, network, threads,
config, logs, pools and help, `Tab` cycles them and `Esc` comes back here.

The split between **CPU** and **GPU** is the part worth having. A single
combined hashrate cannot tell you that a card stopped contributing an hour ago;
a bar per device can, and **ACTIVE THREADS** does the same job one level down —
a core that has been taken by something else shows up as one short bar rather
than as a total that is quietly 6% low. Each panel carries its device's
temperature, and the GPU one its clock, VRAM and power draw.

**SHARE STATS** is accepted against rejected, stale and invalid over the whole
session, because those four fail for different reasons and a single "accepted
%" hides which. **NETWORK STATUS** is the node's own view — peers, latency, the
height it is handing out — so a stall shows as a node problem or a miner problem
rather than only as a number that stopped moving.

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

## Runtime keys and commands

While it is mining, one key does one thing. Nothing needs Enter.

| Key | What it does |
|---|---|
| `M` `S` `N` `T` `C` `L` `P` `H` | Mining, statistics, network, threads, config, logs, pools, help. |
| `Esc` or `D` | Back to the dashboard. |
| `Tab` | Cycle the screens. |
| arrows, `PgUp`, `PgDn` | Scroll the event log. `End` returns it to live. |
| `:` | Open the command line. |
| `Q` or `Ctrl-C` | Stop mining and print a session summary. |

Press `:` first for anything that takes an argument, then type and press Enter:

| Command | What it does |
|---|---|
| `threads <n>` | Change the thread count live. Also accepts `+2` or `-4`. |
| `theme <name>` | Switch colour theme. |
| `save` | Write the current settings to the config file. |
| `config` | Show the active settings and where they came from. |
| `quit` | Stop mining and exit. |

Thread changes take effect immediately and are remembered for the session; run
`save` to make them permanent.

The full-screen console is the default on an interactive terminal. Two other
modes exist for the cases it does not suit:

| Flag | What you get |
|---|---|
| `--classic` | The compact in-place panel. It scrolls with the shell, so the scrollback above it survives. |
| `--no-tui` | Plain scrolling lines. No cursor movement at all — for `tmux` logs, `systemd` units and CI. |

Colour and cursor control are switched off automatically when output is not a
terminal or when `NO_COLOR` is set, so piping to a log file or running as a
service produces clean text with no flags at all.

---

## Themes

Six themes ship with it. See any of them without connecting to anything:

```
derostorm --preview
derostorm --preview --theme=copper
derostorm --preview --theme=aurora --screen=network --size=140x44
```

| Theme | Look |
|---|---|
| `cyber` | neon cyan + violet on black — the control-centre look. **The default.** |
| `default` | cyan + violet on near-black. Quieter than `cyber`. |
| `copper` | burnt copper + slate on charcoal. |
| `aurora` | emerald + ice on deep green-black. |
| `ember` | amber + rose on warm black. |
| `mono` | no colour at all — for logs, CI and dumb terminals. |

`--preview` draws one real frame through the same code the live console uses,
with representative data in it. A preview with a rendering path of its own would
eventually be a picture of a program that does not exist, so there is only the
one. `--screen` picks which of the eight to draw and `--size=<cols>x<rows>` a
window you do not have, which is how a layout gets checked at a size before
someone runs it at that size.

### Window size

The console redraws in place and reflows to whatever it is given. Panels drop
out of each row as the window narrows, columns collapse, and labels shorten
before they truncate. It is readable from about **100 columns by 32 rows**, and
the layout above is what 112x40 looks like; the three-across rows want about 150
columns to open up fully.

DeroStorm asks the terminal for a larger window on start-up, and only ever asks
for more than it has, so a window someone has deliberately made large is left
alone. Two mechanisms are tried, because neither covers both consoles: classic
`conhost` honours the Win32 console calls and ignores the ANSI resize, Windows
Terminal is the other way round.

Below the minimum it says so and falls back to plain scrolling output rather
than drawing a frame wider than its window. If a frame ever *does* come out
wider than the window — the one failure that leaves the screen unreadable —
`--termdiag` reports what every source says the size is and rules a line that
wide, which is enough to say which source is lying.

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
  --rpc-address=<host:port>         derod JSON-RPC, for peer count, network
                                    hashrate and block interval. Default: the
                                    getwork host with the port two higher.
  --mining-threads=<n>              Override the saved thread count.
  --gpu=<list>                      Mine on these NVIDIA devices: 0, 0,1,
                                    all or off.
  --gpu-batch=<n>                   Nonces per GPU launch.
  --gpu-blocks=<n>                  Resident blocks in the GPU suffix kernel.
                                    Default: measure it while mining.
  --theme=<name>                    cyber, default, copper, aurora, ember
                                    or mono.
  --tui                             Force the full-screen console. Already the
                                    default on an interactive terminal.
  --classic                         The compact in-place panel instead.
  --no-tui                          Plain scrolling output, no console at all.
                                    --no-dashboard is the same thing.
  --testnet                         Use the DERO testnet.
  --debug                           Verbose logging to the log file.
  --bench                           Benchmark the hash function and exit.
                                    Add --gpu=all to benchmark the GPU too.
  --stats-file=<path>               Write a JSON status document here every
                                    five seconds, for a rig manager to read.
  --run-for=<sec>                   Mine for this long, then print a summary.
  --cpuprofile=<path>               Write a CPU profile. Works with --bench
                                    and with --run-for.
  --preview                         Draw one console frame with sample data
                                    and exit.
  --size=<WxH>                      Size for --preview. Default: this terminal.
  --screen=<name>                   Screen for --preview: dashboard, mining,
                                    stats, network, threads, config, logs,
                                    pools or help.
  --preview-classic                 Preview the compact panel in every theme.
  --termdiag                        Report what every source says the terminal
                                    size is, and rule a line that wide.
```

Command-line flags override the config file for that run; they do not rewrite it unless you run `save`.

Logs go to `derostorm.exe.log` beside the executable — never to the console, so
nothing scribbles over the frame.

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

Four of them are embedded in the executable and bound at run time: the CUDA
kernels for Windows, the same kernels for Linux, and the descriptor suffix sort
for each. They are build products, not source, so they are not in git — build them
once and the copies under `cmd/derostorm/` are what `go:embed` picks up from
then on. After that an ordinary build needs neither a C toolchain nor CUDA, and
they only need rebuilding when their sources change.

**Which ones you need depends on the target, not on your machine**, and for some
targets the answer is none:

| Target | Needs |
| --- | --- |
| `windows/amd64` | `derostorm_gpu.dll` and `derostorm_sa.dll` |
| `linux/amd64` | `libderostorm_gpu.so` and `libderostorm_sa.so` |
| `linux/arm64`, `darwin/amd64`, `darwin/arm64` | **nothing** |

So building for macOS needs no GPU, no CUDA and no libraries: `./build.sh` on a
Mac produces a working CPU miner from a clean clone. `build.sh` checks only what
the target it is building actually embeds.

```
.\build.ps1 -Native      # all four, then the miner
```

or one at a time:

```
gpu\buildlib.bat         # CUDA kernels  -> cmd\derostorm\derostorm_gpu.dll
gpu/buildlib.sh          # CUDA kernels  -> cmd\derostorm\libderostorm_gpu.so   (run under Linux)
native\build.bat         # suffix sort   -> cmd\derostorm\derostorm_sa.dll
native/buildlib.sh       # suffix sort   -> cmd\derostorm\libderostorm_sa.so    (run under Linux)
```

Each script copies its result into `cmd\derostorm\` itself, because doing that
by hand is how a stale library gets shipped. Both build scripts refuse to build
if a copy is missing, since `go:embed` fails with no hint as to the cause.

The CUDA halves need the toolkit and a host compiler — MSVC on Windows, gcc on
Linux; the suffix-sort halves need only the host compiler, MSVC or gcc.

The two `.sh` scripts are the odd ones, because a compiler targets the host it
runs on: a Linux `.so` cannot be produced from the Windows toolkit, whatever
flags you pass. WSL is enough, and `gpu/buildlib.sh` needs no GPU of its own —
the build wants `nvcc`, not
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

Headline, all of it at once, re-measured on 2026-08-31 at 1.5.8:

| | H/s | at 1.5.6 | at 1.5.5 | at 1.5.0 | at 1.4.0 |
|---|---:|---:|---:|---:|---:|
| CPU, 15 threads | 33,000 | 32,940 | 32,640 | 34,293 | 33,700 – 34,500 |
| RTX 5080 | **142,400** | 128,000 | 119,463 | 100,640 | 91,870 |
| together, `--bench` sum | **175,400** | 160,900 | | | |
| **together, real mining path** | not re-measured | **159,000 – 159,500** | 153,900 – 154,700 | 130,400 – 131,800 | 124,700 – 125,400 |

Two notes on that table, because the columns are not all the same measurement.

The 1.5.8 and 1.5.6 GPU figures were taken **on the same day, interleaved, on
the same machine** — 142.18 / 142.55 against 128.06 / 127.91, +11.2% — so the
comparison between those two is sound. The 1.5.6 column's own headline, 127,930, was
measured a day earlier; the card reads about 1% higher today and ~8% higher warm
than on the first run of a session, which is why nothing here is carried forward
between days.

The real mining path was **not** re-measured at 1.5.8. It needs a node and it
submits shares, and this session had neither reason nor permission to do that,
so the `--bench` sum is given instead and labelled as such. The sum overstates
the real path slightly — mining shares a memory system and a job feed that the
benchmark does not — so read 175,400 as an upper bound and 1.5.6's 159.0 – 159.5
as the last honest real-path number.

1.5.2 onwards no longer sweeps the block count — it sits at four blocks per SM
(336 on a 5080), so no figure here is diluted by a tuning sweep in the first
twelve seconds.

The CPU row is 1.3k lower than at 1.5.0 and the CPU did not get slower: nothing
on that path has changed since the descriptor compare at 1.5.3, which measured
faster, and 1.5.8 is GPU-only. Four `--bench` runs across this session
read 32.88, 33.00, 33.17 and 32.95 KH/s, so it is not scatter either — it is this
machine on this day, and it is left as measured rather than quietly carried
forward from a better one. Every row above was taken in the same session, so the
comparisons between them hold.

The GPU is where the gain keeps coming from. 1.4.0 was +28% on it in one
session, all of it the same mistake in different files — see [The GPU was
reading memory one byte at a
time](#the-gpu-was-reading-memory-one-byte-at-a-time). 1.5.0 is +9.5% more, from
giving the few big colliding groups the whole block instead of one thread; the
average group was never the cost.

**1.5.0 is three things.** The console was rebuilt as a full-screen, eight-screen
one, which is what most of the diff is and none of the hashrate. The GPU merge
above is the speed. And Linux got the native suffix sort it had never had: those
builds were quietly running the Go sort, so the same hardware read about a
quarter of its Windows hashrate — see [the suffix sort](#cpu) for the numbers and
why macOS and arm64 still could not have it then. 1.5.1 is that port.

**1.5.1 is the suffix sort on Mac.** Windows and Linux already had the descriptor
sort; macOS and arm64 were still on the Go SA-IS, about a quarter of the same
silicon. They now use the same algorithm. GitHub archives are cross-compiled
with cgo off, so they ship it in Go plus hardware SHA pairing. `./build.sh` on
the Mac compiles the C sort and ARMv8 SHA-2 pairing into the binary. Darwin
mining threads request user-interactive QoS so they prefer P-cores.

DERO difficulty is already hashes per second. The console was dividing it by
the eighteen-second block target and showing the network eighteen times slower
than it was.

GPU kernels are unchanged from 1.5.0.

**1.5.2 fills the GPU.** `--gpu-batch=0` already claimed to size the launch from
free VRAM; the library still launched 8,192 hashes, which left stage 1 and SHA
on half-empty SMs. It now fills the card, capped at 32,768 so job latency stays
under about half a second. On a 5080 that is 30,016 hashes a batch, ~350 ms,
and **85.87 KH/s at 336 suffix blocks** against 74.9k at 8,192. Stage 1 writes
each 256-byte append as sixteen-byte stores. Mining defaults to four suffix
blocks per SM (the occupancy this kernel actually reaches) instead of sweeping
from a few KH/s up through 1,252, which measured slower under a display.

**The current descriptor arena moves half as many bytes.** Every position emitted
by one descriptor has the same column within a 256-byte block. The arena now
stores only the 16-bit block index and keeps that shared eight-bit column in the
descriptor's unused fourth key byte; scatter reconstructs
`(block << 8) | column`. Descriptor size and radix passes are unchanged, while
both the column walk's stores and the scatter's loads are halved.

Two baseline/compact `--bench --gpu=0` pairs on the RTX 5080 measured
142.27/146.50 and 142.00/146.76 KH/s at 336 blocks: **+3.2% whole-hash
throughput**, with all 512 suffix arrays and whole hashes bit-identical to the
CPU. The same representation was built for the CPU and rejected: widening the
indices during scatter cost more than the saved traffic there.

**1.6.1 stops writing the arena when the order has not moved, +10.2% on the
GPU.** Halving an entry was the first half of the same idea; not writing it at
all is the second. A constant column prepends the same byte to every suffix in
the run, so the order does not change -- that is the premise the whole file rests
on, and `col_same` already knows which columns those are. The walk still wrote
the order out again for each of them: 256 x `len` arena entries per run, the same
handful of block indices repeated for as long as the order held.

It does not have to. Every column writes exactly `ord[0..len)` into one
contiguous slot, in order, whatever way the keys happen to split it into groups,
and a column's slot is never touched again. So a column whose order has not
moved can hand its descriptors the earlier slot and write nothing; a group
`[i, j)` reads from `awBase + i` either way, and the descriptor word does not
change shape at all. The store loop leaves the walk's inner loop entirely, which
is the shape the register ceiling asked for -- something *removed*, not something
added.

Three interleaved `--bench --gpu=0` pairs on the RTX 5080, at 336 blocks:

| | GPU |
|---|---:|
| write every column | 145.59 / 145.57 / 145.64 KH/s |
| **write on change** | **160.61 / 160.24 / 160.29 KH/s** |

**+10.2%, with no overlap between the two sets.** The profiler puts it in two
places: the column walk falls from 922.2M cycles to 774.9M (-16%) because the
stores are gone, and *expand to sa* falls from 340.0M to 274.9M (-19%) because
what it reads back is now the slots that were actually written -- a smaller and
hotter footprint for the same answer. The radix sort does not move, as expected:
descriptor count and width are untouched. `gpu\desc_test.exe` is correct on all
512 suffix arrays, `gpu\hash_parallel_test.exe` reports all 512 whole hashes
bit-identical to the CPU, and `go test ./cmd/derostorm/` is green.

**1.5.8 is two changes to the descriptor sort, +11.2% on the GPU together.**

*The collision scan was reading the descriptor array twice.* Step 5a places
every position and reads every descriptor to do it; step 5 then read all ~20,000
of them again — three global words each — only to ask which key groups collide,
and ~98.7% of them do not. The question is now asked in step 5a out of the window
it has already staged in shared memory, and step 5 is handed the ~250 groups that
answer yes. **+3.7%.**

*And the sort was ordering a byte it did not need to.* The key is four text
bytes, so the radix sort ordered 32 bits in five passes. Three bytes is enough:
it orders 24 bits in four passes, and it hands the sort fewer descriptors as
well, because a coarser key merges neighbouring groups into one. What it costs is
collisions, and the merge that resolves them was 1.6% of the kernel against the
radix's 20.7%. **+6.4%** on top, and two bytes is a clear loss, so three is the
peak and not just a direction. This is the same trade the CPU sort settled a year
of measurements ago and the GPU had never been asked.

The rest of the session is in *What does not help*, and it is the more useful
half: with the GPU performance counters finally unlocked, `suffix_kernel` turns
out to be a latency kernel that is **register-saturated at exactly 64** — the
number that fits four blocks on an SM — so anything that adds a live value
spills, and it will not trade text locality for coalescing, balance or cache
residency either.

**1.5.6 runs stage 1 underneath the suffix sort.** The three kernels ran one
after another because they shared one set of texts and suffix arrays, so the
card did one thing at a time. Stage 1 is 14.7% of GPU time and the SHA check
7.5%, and both of those were the sort standing still.

They need not be. `gpu/overlap.cu` puts two of them on two streams over separate
storage and times the pair: **80% of stage 1 disappears into the sort**, and 80%
of the SHA check with it. They are short of different things — stage 1 of shared
memory (516 bytes a thread caps it at ~193 threads an SM, and no block size
changes that), the sort of memory latency, the SHA check of bandwidth — so an SM
hosting two of them is not paying twice.

So the storage is now two banks, a chunk each, with a stream apiece. A chunk owns
its bank from stage 1 through to the SHA check, and the bank's own stream orders
everything that reuses it; the only thing needing an event is the sort itself,
whose scratch pool is shared by every block and must never host two kernels at
once. The banks together hold what one bank held, so the overlap is paid for in
chunk size, not VRAM.

Half of it does not pay, and that is the interesting half. Letting the SHA check
run under the *next* chunk's sort makes both slower — it reads back the suffix
arrays the sort has just written, so it fights the sort for exactly what the sort
is short of, and its own elapsed time nearly trebles:

| | H/s |
|---|---:|
| one bank | 123,265 |
| two banks, SHA overlapping too | 116,116 |
| two banks, SHA kept out of the way | **127,532** |

So the sort's turn is released after the SHA check rather than before it.
`--bench --gpu=all` reads **127,930 H/s against 119,463**, and the real mining
path with the CPU beside it **159.0 – 159.5 KH/s against 153.9 – 154.7**.
Hashes are unchanged and still bit-identical to the CPU.

Two things measured and not kept. Stage 1's probabilistic hashes read shared
memory through `ld32le`, which builds a word out of four byte loads — the same
mistake the suffix kernel's `descKey32` had. Fixing it is exactly neutral: stage
1 is not short of load slots, it is short of threads. And ablating stage 1 to
find where its time goes does not work at all, because every part of it feeds
`lhash`, which picks the next operation — remove any of it and the loop takes a
different path through a different number of iterations. The numbers that come
back are real and mean nothing.

**1.5.5 turns the scatter inside out.** Placing the sorted positions gave a
whole descriptor -- a run of ~5.7 positions -- to one thread, which copied it a
word at a time. Neighbouring threads therefore wrote addresses ~23 bytes apart
and read arena slots with no relation to each other, so a warp's 32 words cost
~28 memory transactions where 4 would do. It was **26.5% of the suffix kernel**,
the largest single phase in it.

Driving the loop by output position instead fixes the writes completely: output
position q always lands at `sa[q]`, so a warp writes 32 consecutive words. What
it costs is finding which descriptor owns q, and the answer is to walk
descriptors rather than output tiles — a window of 256 descriptors is read once,
coalesced, into shared memory, and the ~1,460 output positions it covers are
written from it with an eight-step binary search in shared memory each. Driving
by output would reread the offset array once per tile of 256 outputs, 5.7 times
the loads for the same answer and 5.6 times the barriers to do it.

| | suffix ms |
|---|---:|
| before | 140.9 |
| one thread per output position | 136.7 |
| the same, two barriers a tile not three | 135.1 |
| by descriptor window, not output tile | **129.9** |

Both merges lose their gather loop as well — the positions are already where
they want them, so all either one still needs is where each list starts. End to
end on a 5080 with every core busy: **123,148 H/s against 114,614**, +7.4%, and
the run-to-run spread fell from ±5% to ±0.3%. `--bench --gpu=all` reads
119,463 H/s against 1.5.2's 100,640, and the real mining path with the CPU
beside it reads 153.9 – 154.7 KH/s against 130.4 – 131.8. `suffix_kernel` is 226.4 → 207.2 ms
a batch. Hashes stay bit-identical to the CPU — `gpu/hash_parallel_test.exe`
matches all 512 vectors.

`BR_BITS`, `DESC_CHUNKS` and `DESC_MERGE_WIDE` were re-swept afterwards in case
the rewrite had moved their peaks. It had not: 7, 4 and 64 stand, and 8 bits or
8 chunks both cost enough shared memory to lose a block per SM.

**What is left, and what did not work.** With the scatter fixed, the column walk
is the largest phase again — and most of it is threads doing nothing.
`gpu/prof/prof.exe` now times each thread's own tasks and reports the block's
wait: **the slowest thread takes 1.98× the average**, so a perfectly balanced
walk would cost half of what this one does. The walk is ~32% of the kernel, so
that is ~16% of it and ~12% of hashrate sitting there.

Two ways to collect it were built and measured, and neither is in the tree.

*Cutting the runs shorter* — `DESC_RUN_MAX`, a knob that already existed — buys
threads and is **four times slower at every cap**: 130 ms uncapped against 188
at 64 blocks, 338 at 16 and 797 at 4. A run split in two emits its descriptors
twice, and the radix sort and the merge carry every one of them.

*Splitting the columns unevenly* avoids that completely, because chunking a run
changes no descriptor at all, and it was built in full: a chunk count per run,
scans for the task and storage offsets, a binary search from task back to run,
and the order table packed to a block index — two bytes instead of four — to pay
for the extra entries. It is exact and it is not faster. Swept over five
threshold pairs, from boosting runs past 4 blocks to past 16, every one landed
within noise of the flat split, and the imbalance barely moved: 1.98× → 1.92×.
The reason is that every chunk of a run repeats that run's seed insertion sort
and its full key read, and the seed is quadratic in the run's length — so the
chunks a long run needs in order to come down to average cost add back about
what the balance saves. Mean per-thread cost rose 9% to improve balance by 3%.

What the walk needs is a cheaper per-chunk seed, not another way to divide the
columns. `DESC_CMP_WORDS` and `DESC_SPLIT` were swept in the same session and
are both already at their best.

**1.5.4 stops the card waiting for a CPU.** The GPU worker enqueued a batch and
then blocked until it came back, so between batches the card was idle for as
long as the one host thread took to wake up — and on a machine mining on every
core, that is a scheduler quantum, not microseconds. It now keeps a second batch
queued behind the running one (`dsg_submit` / `dsg_collect`), so the card starts
the next the instant the current one ends. On a 5080 with all sixteen threads
busy: **112,548 H/s against 106,429**, +5.7%, and the GPU rate no longer moves
when the CPU load does. Batches shorter than the default gain far more — +31% at
4,096 nonces, +78% at 1,024 — because the gap is a fixed cost per batch. Kernels
are unchanged and hashes stay bit-identical to the CPU.

**1.5.3 is the next sort step on that 1.5.2 shape.** The CPU descriptor compare
starts with an eight-byte word, then an AVX2 32-byte tail; radix histogram and
scatter are four-way. On this 5080 + 9800X3D that is about **+2.3%** on the
sort at 15 threads (~37.8k texts/s against 1.5.2's ~37,005). The GPU does the
same eight-byte-first compare, 64-bit text loads, a 32-byte wide tail,
`DESC_MERGE_WIDE` 64, and an L2 prefetch of the text at stride 64: about
**+4–5%** in the suffix harness at 336 blocks (~81.5–82.5 KH/s against
1.5.2's ~78.5). Hashes stay bit-identical to the CPU.

**1.4.1 exists for one reason.** The 1.4.0 Linux archives shipped the *previous*
CUDA kernels: `libderostorm_gpu.so` is built by `nvcc` under Linux, which was
not available on the machine that cut the release, so Linux GPU mining ran at
1.3.0 speed while Windows had everything. The `.so` is rebuilt here with
`gpu/buildlib.sh` under WSL (Ubuntu 24.04, CUDA 13.0) and the `linux-amd64`
binary relinked — Linux now mines the same kernels as Windows, verified
end-to-end on the reference 5080 from the new binary under WSL. No kernel source
changed between 1.4.0 and 1.4.1; Windows binaries are unchanged in behaviour,
and CPU mining is untouched on every platform.

### CPU

| | Before | Now | |
|---|---:|---:|---:|
| 1 thread | 700 H/s | 3,280 H/s | +369% |
| 15 threads | 8.09 KH/s | 32,640 H/s | +303% |

*Before* is stock derohe, measured once on this machine at the start and not
re-run since — it is upstream code and does not move. *Now* is `--bench` on
1.5.5. The intermediate figures this table used to carry (1,875 H/s and
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

Linux gets the same library and the same margin. It was Windows-only for a
while — the sort was packaged as a DLL and nothing had been built for anything
else — and that showed up as Linux rigs reporting a quarter of the hashrate the
same hardware managed on Windows. It was never a tuning problem: those builds
were running the Go sort. `native/buildlib.sh` now builds the `.so`, and
`--bench` on linux/amd64 reads +248.9% at 8 threads against the Windows
+260.0% on the same row. macOS and arm64 use the same algorithm from 1.5.1:
NEON in the C sort when the binary is built on the machine, the portable Go
descriptor otherwise, and ARMv8 SHA-2 pairing either way.

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
| RTX 5080 | 7.45 KH/s | 142,400 H/s | +1811% |

*Before* is the GPU on its own on the real mining path
(`--mining-threads=1 --gpu=0 --run-for=90`), measured when GPU support first
worked. *Now* is `--bench --gpu=all`, which runs the same kernels over the same
batch size without needing a node. The intermediate 12.28 KH/s this table used
to carry was the gain from the packed-key change described below; the rest came
from the block-count sweep, the stage-1 work after it, the byte-load session at
1.4.0, the merge described next, the batch pipeline and coalesced scatter at
1.5.4 and 1.5.5, and the overlapped stage 1 at 1.5.6.

**The collision scan was a second pass over words the scatter had already
read.** Step 5a places every position: it walks the sorted descriptors in
windows of 256, reads each window once into shared memory, and writes the
output. Step 5 then walked *all ~20,000 descriptors again* to ask which key
groups collide -- reading three global words per descriptor to do it, the
descriptor, its predecessor and its successor -- when ~98.7% of them are
singletons the answer is "no" for.

All three words are in the window step 5a has already read. So the question is
asked there, out of shared memory, and the ~250 groups that answer yes go into a
compact list; step 5 iterates that list instead of sifting the whole array. The
list grows down from the top of the `dead` array while the merge's bump
allocator grows up from the bottom, so it costs no memory and the overflow guard
that was already there covers both.

| | before | after |
|---|---:|---:|
| desc: expand to sa | 368.2M cycles | 392.2M |
| desc: find groups | 326.8M cycles | 137.8M |
| the pair | 695.0M | **530.0M** |

Three interleaved rounds of `--bench --gpu=all`, warm, with no overlap between
the two sets: **129.97 / 130.55 / 130.21 KH/s becomes 135.63 / 135.84 / 135.61**,
+4.2%, and combined with the CPU 163.4 becomes 168.8, +3.3%. Hashes are
unchanged and still bit-identical to the CPU.

This is the same mistake 1.5.0 fixed once already, re-introduced by the 1.5.5
scatter rewrite: two loops over all `nd` descriptors, both opening by asking the
same question of the same words. It is worth checking for after any change that
splits a pass in two.

**Two things measured and not kept.** Handing the runs out longest-first, so a
warp's lanes all draw runs of about the same length, is exact and **9% slower**
-- 102-104k SA/s against 113-114k. It buys balance with locality: in run order a
warp's eight runs are eight adjacent stretches of the text and of the arena, and
ranked they are eight stretches from anywhere in 68 KB and 274 KB. The block
waits for the same longest task either way, because 247 tasks over 256 threads
means no thread ever gets a second one, so all the ranking can win is issue slots
for the other blocks on the SM -- and that does not pay for the misses. Locality
beats balance here, which is the `DESC_RUN_MAX` lesson from the other direction.

`DESC_CHUNKS`, `BR_BITS` and `DESC_MERGE_WIDE` were all re-swept afterwards,
because the three-byte key changes what the sort is doing. None of them moved:
`DESC_CHUNKS` 4 reads 121.2k SA/s against 117.4k at 2 and 114.9k at 8, and
`BR_BITS` 6 and `DESC_MERGE_WIDE` 128 each looked like a 2% win over three rounds
and then gave it all back over three more — 122.3k and 121.4k against 121.2k,
which is this harness's noise floor and not a result.

**A warning about the sweeps, because it cost this session two wrong answers.**
`cmd.exe` splits a batch argument on `=` as well as on spaces, so a build script
taking its defines as `%2` receives `-DDESC_CHUNKS=8` as `-DDESC_CHUNKS` and
turns it into 1. Every sweep run that way measures the same build twice and
reports it as two settings that happen to agree. It produced a confident
"`DESC_CHUNKS` 2 and 8 are 10% worse" that was one build of `DESC_CHUNKS=1`
measured twice, and it nearly killed the three-byte key by making the four-byte
control 5x slower than it is. Pass defines through an environment variable the
script expands, and sanity-check any sweep where two settings land on the same
number.

**What the hardware counters say the kernel is.** With *Manage GPU Performance
Counters* allowed (see [Profiling the GPU
further](#profiling-the-gpu-further)), Nsight Compute answers in one run what
build-and-measure had been circling. Nothing is saturated -- DRAM 34%, SM 31% --
so `suffix_kernel` is a latency kernel, and its scheduler finds an eligible warp
on 23.8% of cycles. The 27.8 cycles between two issues divide almost evenly
between **waiting at a barrier for sibling warps (35%)** and **waiting on an
L1TEX load (33%)**, the L1 hit rate is 55.6%, and **57% of every global sector
fetched is excessive** -- global loads use 8.3 of each 32 bytes.

By source line, the excessive sectors are 43% `descLoadBE64` (the text gather),
20% the arena write in the walk, 10% the radix scatter. The largest single stall
site is the first instruction after the barrier that ends the column walk, which
is the walk's imbalance wearing a load's clothing.

Four changes were built against that and all four lost, which is the useful
part: an arena laid out column-major so a warp's runs write adjacent spans
(+0.5%, noise -- the lanes are too divergent to issue the store together);
warp-uniform chunks so all 32 lanes walk one column (-8%); L2 persistence pinned
on the texts (-8%); and `col_same` as a 64-bit register mask instead of a
64-byte local array (-1.4%). **This kernel will not trade text locality for
anything** -- not for coalescing, not for balance, not for cache residency. The
losses are 8-9% where the gains are noise.

**And three more at the text gather, which is where the counters point.**
`descLoadBE64` builds eight bytes from three four-byte loads, and four calls in a
row -- the thirty-two-byte comparison step -- issue twelve, three of which re-read
a word the call before already had. Two rewrites of it lost: five `uint2` loads
for a whole thirty-two-byte step (-1.8%) and a two-load eight-byte form (-4.6%).
Seeding the column walk by its four-byte key and full-comparing only on ties lost
too (-4.2%), which is the second time that idea has been measured and the second
time it has failed.

All three failed the same way and it is worth knowing before trying a fourth:
**`suffix_kernel` sits at exactly 64 registers with zero spills, and 64 is the
number that fits four blocks on an SM.** ptxas will not go to 65 -- it spills
instead. Each of those three added live state, each one spilled (the batched
loader 404 bytes of stores and 1,428 of loads), and each paid more for the spill
than it saved. The first rewrite was also simply wrong on its own terms: reading
sixteen bytes to extract eight touches *more* memory than reading twelve, so it
cut instructions and raised traffic. An optimisation here has to be register-
neutral or register-negative before anything else about it matters.

**The card was waiting for a CPU.** Taking two CPU mining threads away made the
GPU faster, which should not happen: the CPU threads and the card share nothing
but a job. Measured on a 5080 with every logical CPU busy, the GPU ran at
**106,429 H/s against 111,247 with the CPU idle** — a 4.3% tax for mining on the
processor at the same time — and the run-to-run spread went from 1% to 4.5%.

Two guesses were wrong before the measurement was right. It is not the clocks:
`nvidia-smi` reports 2,865 MHz either way, at the same temperature. It is not
the kernels: profiled with `nsys`, `suffix_kernel` takes 227 ms a batch whether
the CPU is idle or saturated. `gpu/gapbench.cu` records CUDA events either side
of each batch, so the GPU's own timeline can be compared with the wall clock,
and that is what found it — the card was idle between batches, waiting for the
one host thread that enqueues the next one to get a scheduler slot behind
sixteen pinned mining threads that never yield.

The fix is not to make that thread faster. It is to make the card not care how
slow it is: `dsg_submit` and `dsg_collect` replace the blocking `dsg_search` on
the mining path, and the miner keeps a second batch queued behind the running
one. The card starts it the instant the first ends, and the host's wake-up
happens with a whole batch of slack in hand. Two batches share the same scratch
— they are on one stream, so batch N's last kernel is done before batch N+1's
first one starts — and all the second slot costs is a few hundred bytes for its
own work, target and result buffers.

| nonces per batch | before | after | |
|---|---:|---:|---:|
| 32,768 (default) | 106,429 H/s | 112,548 H/s | +5.7% |
| 4,096 | 55,504 H/s | 72,728 H/s | +31% |
| 1,024 | 17,740 H/s | 31,604 H/s | +78% |

All with sixteen busy CPU threads, which is the case that was broken. The gap is
a fixed cost per batch, so it is a rounding error against a batch that takes
290 ms and it is most of the time against one that takes 30 — which is why
`--gpu-batch`, sold as a job-latency knob, was quietly also a throughput one.
It is not any more. With the pipeline the GPU rate no longer depends on what the
CPU is doing at all: 112,548 H/s with the machine saturated against 109,992 with
it idle, the difference being noise.

Raising the feeder thread's priority was the obvious other half and is
deliberately not there. It is not one trade but two opposite ones: +4.8% at
1,024 nonces a batch, where the wake-up is a real share of the batch, and -4.0%
at the default 32,768, where it is not and the preemption costs more than it
buys. The default is the case that matters.

**One thread was the phase.** The merge that resolves colliding descriptor
groups gave each group to one thread. The average group holds seven positions,
which is exactly why the code was written that way and exactly why the average
was the wrong thing to look at: every thread waits at the barrier afterwards, so
the phase costs whatever the *busiest* thread costs. Counted over the 512
reference vectors, that thread does **43.5% of the whole text's merge
comparisons**.

The distribution says why. Of ~247 colliding groups in a text, the five or so
holding more than 32 positions carry 62% of the comparisons:

| positions in group | groups per text | share of comparisons |
|---|---:|---:|
| 1 – 32 | 241.6 | 38% |
| 33 – 64 | 3.1 | 12% |
| 65 – 128 | 0.7 | 6% |
| 129 – 256 | 0.4 | 11% |
| 257+ | 0.5 | 32% |

So the big ones now go through a **block-wide** merge instead, one group at a
time with all 256 threads on it, and the per-thread loop keeps the ~242 small
ones it was always good at. The threshold (`DESC_MERGE_WIDE`, 128 positions) is
a trade: a block-wide merge costs barriers, and below that size the barriers
cost more than the serial tail they remove. `DESC_BIG_MAX` caps how many groups
a text may hand over at 48 — there are about five, and a group past the cap is
merged by one thread as before, slower and never wrong.

The scatter was folded into the same pass while the code was open. It and the
merge were two loops over all `nd` descriptors, and both opened by asking the
same question of the same words — is this descriptor the first of its key group,
and does the group hold anything else. Two global reads a descriptor, asked
twice: ~50,000 loads a text spent re-deriving what the previous loop had already
worked out.

Together: **91,870 → 100,640 H/s, +9.5%** on the 5080, output bit-identical —
`gpu/hash_parallel_test.exe` still matches all 512 CPU vectors and the miner
still re-verifies the device against the CPU before it will submit anything.

**The GPU was reading memory one byte at a time.** The largest single session of
gains on this card, +28% end to end, and every part of it was the same mistake in
a different file: code that gathered or scattered bytes individually on data that
was already aligned. A byte load is not four times the DRAM traffic of a word
load — the coalescer merges adjacent sectors — but it is four times the
load/store-unit trips, four times the L1 tag lookups and four times the
outstanding-load budget, in a kernel whose whole problem is waiting on memory.

Found by dumping the SASS and counting, which is worth doing before believing any
comment about what the compiler will do:

```
cuobjdump -sass ds.cubin | grep -c LDG.E.U8
```

*The text load.* `descKey32` and every suffix comparison built a four-byte
big-endian word from four byte loads and a shift-or chain. The comment above it
claimed nvcc folded that into one 32-bit load. It does not and it cannot: a
32-bit load must be four-byte aligned, `p` is arbitrary, and the compiler has no
way to prove anything. `suffix_kernel` carried **98 `LDG.E.U8`**.

Reading the two aligned words that straddle the offset and picking the bytes out
with one `PRMT` is two loads instead of four, and the byte reversal comes free in
the same instruction rather than costing three shifts and three ORs. Byte loads
fell to 34, and the miner went 71.65 → 73.37 KH/s.

*The run boundary test.* Deciding whether two 256-byte blocks differ enough to
end a run compared them **one byte at a time** — 512 loads per pair, and it
almost always ran all 256 iterations because the early exit only fires on a
rekey. Sixteen bytes at a time with `__vsetne4` took the phase from **4.2% of the
kernel to 0.6%**, and the miner to 76.63 KH/s. The CPU had done this with AVX2
since the beginning; only the GPU was still counting bytes.

*SHA-256.* The message schedule built sixteen big-endian words from **64 byte
loads per 64-byte block**. The input is a suffix array cast to bytes, so it is
sixteen-byte aligned and always was. Four `uint4` loads and sixteen `PRMT`s
instead: **SHA time halved, 15.0 ms to 7.4 ms**, and the miner went to 85.5 KH/s.

*The stage-1 output.* Every one of ~270 iterations appended the 256-byte state to
the text with a byte loop — about **69,000 single-byte global stores per hash**,
the largest source of memory instructions in that kernel. Sixty-four word stores
instead took **stage 1 from 22.0 ms to 13.4 ms**, and the miner to 93.9 KH/s.

*And then the knobs were all wrong.* Every tuning constant in `gpu/desc.cuh` had
been swept against the old load. `DESC_CMP_WORDS` sits on the trade between
reading further ahead and reading past the answer; with a byte gather an extra
word cost four more loads and four was a measured null, and with a wide load it
costs two and four is a clear win:

| DESC_CMP_WORDS | 1 | 2 | 4 | 6 | 8 |
|---|---:|---:|---:|---:|---:|
| KH/s | 74.1 | 77.2 | **79.5** | 77.6 | 76.3 |

That is the general lesson and it is worth more than the number: **a swept
constant is only valid against the code it was swept on.** The others were
re-swept too and held — `DESC_CHUNKS` 4, `BR_BITS` 7, `BR_BLOCK` 256.

Two preconditions came out of this and are worth knowing before touching that
file. The `uint4` boundary test needs sixteen-byte-aligned texts, which the miner
has by construction and the test harnesses did not — `gpu/vectors_host.h` now
rounds its layout stride, and the kernel branches to a byte fallback rather than
faulting on a caller that gets it wrong. And `descLoadBE32` reads up to four
bytes past the text, so every allocation of texts carries eight bytes of tail.

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

**Everything else tried on the GPU this session.** Recorded because each one
looked reasonable and each one measured worse, interleaved against the build
beside it:

| | result |
|---|---|
| `DESC_RUN_MAX` 8 / 16 / 32 / 64 — cap run length to even out the walk | **31.7 / 42.4 / 54.3 / 63.6 KH/s** against 79.7 uncapped |
| `DESC_CHUNKS` 2 / 8 / 16 | 73.7 / 71.8 / 47.3 against 79.5 |
| `DESC_SPLIT` 100 / 130 / 190 / 220 | flat, 79.2–79.5, all inside the noise |
| `BR_BITS` 6 / 8 | 79.0 / 72.1 against 79.6; 8 doubles the histogram and costs occupancy |
| `BR_BLOCK` 128 / 512 | 64.9 / 68.5 against 79.6 |
| seed the column walk by key first, full compare only on ties | null, 79.1 against 79.5 |

The run-length cap is the interesting one. Capping runs shortens the longest
column walk, which is what the block waits on at the barrier — the profile bills
11.2% of the kernel to threads idling there. It still loses catastrophically,
because a shorter run shares fewer constant columns and hands the global sort a
smaller pre-ordered group. **Long runs are worth far more than balance.**

Occupancy is not the limit either and there is nothing to win by forcing it:
`suffix_kernel` is 64 registers with zero spills and 12,256 bytes of shared
memory, which is exactly 4 blocks per SM on this card, and 4 × 84 SMs = 336 is
where the block-count plateau starts.

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

**The GPU's wider comparison, on the CPU.** `DESC_CMP_WORDS` = 4 was worth 3.2%
on the GPU by putting more loads in flight before a branch, and the CPU sort is
latency bound, so the same shape should pay. It does not. Swept 1 / 2 / 3 / 4
eight-byte words per iteration of `suffix_less`, three rounds each at 15 threads,
every result landed between 44.5k and 47.0k texts/s with no ordering — pure
noise. An out-of-order core already issues the next iteration's loads
speculatively past the branch; the GPU has no speculation, which is exactly why
it needed the unrolling and the CPU does not.

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
- **Four GPU suffix blocks per SM** is the default (336 on a 5080). Pin it with
  `--gpu-blocks=<n>` if you would rather a different count; past that the curve
  is flat or down.
- Threads are pinned to CPUs automatically, spreading over physical cores before
  using SMT siblings.
- **Faster RAM is not worth buying for this.** See *What does not help* above:
  running the CPU hash entirely out of DRAM costs 8.8%, and at its natural
  footprint it never leaves L3.

## Rig managers, and HiveOS

`--stats-file=<path>` writes a JSON document every five seconds and nothing
else: hashrate, the split by device, temperatures, fans, power, miniblocks and
rejects. It is the machine-readable half of the console, and it exists because
the other half is not one -- parsing a panel that was laid out for a person
breaks the first time a column is widened.

```
derostorm --no-tui --stats-file=/run/derostorm.json
```

The file is written whole and renamed into place, so a reader polling it sees
the previous document or the next one and never half of one, and it is deleted
when the miner exits, so a monitor can tell a stopped miner from one running at
zero.

`hiveos/` is a HiveOS custom miner package built on it -- `h-manifest.conf`,
`h-config.sh`, `h-run.sh`, `h-stats.sh` and a README, packaged as
`derostorm-1.6.1.tar.gz` and attached to the release. Point a flight sheet's
*Installation URL* at it, set the miner name to `derostorm`, and put a **derod
node address in the Pool URL field** -- which is the one thing worth saying
twice, because this is a solo miner and there is no pool. Accepted counts
miniblocks and blocks.

Its `h-stats.sh` reports the rig total including the CPU, and per-card figures
for the GPUs only, so on a machine mining on both the cards do not add up to the
total. The difference is the processor.

### Profiling the GPU further

`gpu/prof/` gives phase shares without any special permission — see the GPU
section above. What it cannot give is *why* a phase is slow, and for that Nsight
Compute needs a driver permission:

> NVIDIA Control Panel → Desktop → Developer settings → *Manage GPU Performance
> Counters* → allow access to all users. Needs admin, and takes effect on the
> next launch -- of the process, not of the machine. No reboot.

It is worth the trip. `gpu/prof/` says *which phase*; this says *why*, and the
GPU section above is the record of it answering in one run what a week of
build-and-measure had been circling.

Then:

```
ncu --section SpeedOfLight --section WarpStateStats ^
    --kernel-name suffix_kernel --launch-count 1 --clock-control none ^
    gpu\hash_parallel_test.exe gpu\vectors.bin
```

The note at the top of `gpu/blockradix.cuh` carries the full record: what the
phase shares were, what was changed, and what was tried and thrown away.

A different question — is the card *working*, or waiting for the host? — needs a
different tool, because throughput on this GPU swings 10% run to run and will
not settle an argument about a few percent. `gpu/gapbench.cu` records CUDA
events either side of every batch, so the GPU's own timeline can be held against
the wall clock, and it loads the CPU the way mining does so the answer is for
the machine as it is actually run:

```
gpu\gapbench.bat
gpu\gapbench.exe serial   16 25
gpu\gapbench.exe pipeline 16 25
```

That is what found the idle gap the batch pipeline removes, described above.

## Layout

```
derostorm/
├── bin/                    built binaries
├── cmd/derostorm/          the miner
│   ├── main.go             flags and startup order
│   ├── runloop.go          the run loop, and which console it drives
│   ├── setup.go            first-run wizard
│   ├── config.go           the derostorm.json file
│   ├── engine.go           getwork + the mining threads
│   ├── target.go           allocation-free difficulty check
│   ├── tui.go              the full-screen console: input, frame, resize
│   ├── tui_layout.go       which panels fit, at this window size
│   ├── tui_panels.go       every panel on the dashboard
│   ├── tui_screens.go      the other seven screens
│   ├── tui_preview.go      --preview: one frame, sample data, no node
│   ├── dashboard.go        the compact in-place panel (--classic)
│   ├── theme.go            palette lookup, over internal/ui
│   ├── commands.go         runtime command line
│   ├── nodeinfo.go         derod JSON-RPC: peers, net hashrate, block time
│   ├── sysinfo*.go         CPU load, frequency and memory, per platform
│   ├── sensors.go          temperatures, fan, power
│   ├── statsfile.go        --stats-file: the JSON a rig manager reads
│   ├── termdiag*.go        --termdiag: what each source says the size is
│   ├── termprobe*.go       asks the terminal for a bigger window
│   ├── affinity.go         CPU-slot → logical-CPU map
│   ├── gpu_cuda.go         binds the CUDA library and drives it
│   ├── gpu_cuda_windows.go   embeds the .dll, finds symbols with LoadLibrary
│   ├── gpu_cuda_linux.go     embeds the .so, finds symbols with dlopen
│   ├── gpu_other.go        the no-GPU build: macOS, and Linux off amd64
│   ├── gpu_worker.go       the GPU mining worker
│   ├── gpu_tune.go         measures the suffix kernel's block count
│   ├── gpu_bench.go        --bench for the GPU
│   ├── sa_lib.go           the shared suffix-sort binding
│   ├── sa_windows.go       embeds the .dll, proves it, installs the hook
│   ├── sa_linux.go         embeds the .so, same three steps
│   ├── sa_other.go         darwin / linux-arm64: portable descriptor, then cgo
│   ├── sa_cgo.go           installs the native sort when built with cgo
│   ├── sa_test.go          332 inputs through both sorts, hashes compared
│   ├── sa_bench.go         --bench: the two sorts, interleaved
│   ├── thread_darwin.go    P-core QoS on macOS
│   └── default.pgo         profile for -pgo=auto
├── internal/dsa/           portable descriptor sort (Mac, and the fallback)
├── internal/sacgo/         cgo bundle of the native sort
├── internal/shapair/      paired SHA-256 without the native library
├── internal/ui/            the widgets the console is drawn from
│   ├── canvas.go           the cell grid everything writes into
│   ├── panel.go            bordered boxes and their titles
│   ├── chart.go            sparklines, bars, the braille history plot
│   ├── gauge.go            rings, meters and dials
│   ├── art.go              the wordmark, the cloud, the block glyphs
│   ├── text.go             wrapping, truncation, alignment
│   ├── format.go           hashrates, counts, durations
│   └── theme.go            the six palettes
├── native/                 the C suffix sort
│   ├── derostorm_sa.c      the C API: sort, version, self-test
│   ├── descriptor.c        the structure-exploiting suffix sort
│   ├── sabench.c           checks and times both sorts on the real texts
│   ├── build.bat           builds the Windows .dll
│   ├── buildlib.sh         builds the Linux .so
│   └── libsais/            upstream libsais, unmodified (Apache-2.0)
├── gpu/                    the CUDA kernels and their test harnesses
│   ├── derostorm_gpu.cu    the three kernels and the C API
│   ├── stage1.cuh          the 256-way state machine, thread per hash
│   ├── desc.cuh            the descriptor suffix sort, block per hash
│   ├── sa_doubling.cuh     suffix array by prefix doubling, block per hash
│   ├── blockradix.cuh      the block-wide radix sort under it
│   ├── hash_parallel_test.cu  whole hash against 512 real CPU vectors
│   ├── prof.cuh            phase timers, compiled out unless DS_PROF
│   └── prof/               cycle attribution for the suffix kernel
├── hiveos/                 the HiveOS custom miner package
│   └── derostorm/          h-manifest, h-config, h-run, h-stats, the binary
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

## No developer fee

**Every hash this miner finds pays the address you configure, and nobody else.**

There is no developer fee, no fee period, no fee-off switch that quietly turns
itself back on, and no second address anywhere in the source. The wallet you set
is read once from your config and handed to the engine, and it is the only
address the miner has: `grep -rn "dero1" cmd/ internal/` returns the placeholder
in the setup prompt and nothing else. It is a short read and it is worth doing
yourself rather than believing a README.

That is not a promise about the future either -- the licence is MIT, so it stays
checkable in every version.

### Donations

If DeroStorm is earning for you and you would like to send some back, this is
where:

```
dero1qypj3sctlt7mefhvdhrvrygj55m40ugl7ml2dukzypxdtd2agpgsjqq2v3n6h
```

Entirely optional, and it changes nothing about how the miner behaves. Copy the
address from this file rather than typing it -- DERO addresses carry a checksum,
so a typo is rejected rather than lost, but the miner will simply refuse to
start and it is not obvious why.
