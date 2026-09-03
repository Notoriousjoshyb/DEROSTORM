# GPU kernel libraries

The miner embeds one GPU library per vendor. NVIDIA's lives in
`../gpucuda/` (`windows/derostorm_gpu.dll`, `linux/libderostorm_gpu.so`).
AMD's goes in here:
    windows/derostorm_hip.dll       built by gpu\buildlib_hip.bat
    linux/libderostorm_hip7.so      built by gpu/buildlib_hip.sh  (ROCm 7)
    linux/libderostorm_hip6.so      built by gpu/buildlib_hip.sh  (ROCm 6)
    linux/libderostorm_hip5.so      built by gpu/buildlib_hip.sh  (ROCm 5)

All of them come from the same source as the NVIDIA library —
`gpu/derostorm_gpu.cu` and the headers beside it — through a HIP compiler
instead of `nvcc`. `gpu/gpuapi.cuh` is the only file that knows which compiler
it is talking to.

### Why Linux has one per ROCm generation

A HIP library links to the ROCm runtime by soname: `libamdhip64.so.7` for
ROCm 7, `.so.6` for ROCm 6, `.so.5` for ROCm 5. A rig has one of those
installed, not three, and no single build satisfies each — a `.so.5` library
will not load on a ROCm 6 rig and the reverse is equally true.

So one is built per generation and `dlopen` decides: whichever one the rig can
resolve is the one that runs. The highest number is tried first, because that is
the current line and the newest cards need it.

`buildlib_hip.sh` names its output after the runtime it actually linked
against, read back out of the finished ELF. Building on a machine with only one
ROCm produces one of them, which is a perfectly good library for every rig on
that generation.

`gpu_backend_linux.go` reads the list out of this directory rather than spelling
the names out, and that is not tidiness. Until 1.7.3 it named `hip6` and `hip5`
in Go, so a ROCm 7 rig — Arch ships only `libamdhip64.so.7` — resolved neither
and got the same silence as a machine with no AMD card. ROCm 8 will need a
build here and nothing else.

Windows has one file because Windows has only ever had the ROCm 6 line —
`amdhip64_6.dll`, which ships inside the Adrenalin driver.

### They are not in git

None of these libraries are, on either side: they are build products, and a fat
binary in git is a fat binary in git forever. All of the GPU libraries are
**optional** -- NVIDIA's exactly like these. `go:embed` takes the whole
directory rather than the files, so a tree without them still compiles; the
backend then finds no library and reports no devices -- the same thing, to the
miner, as a machine with no such card. An AMD-only rig has no nvcc and needs
none. See the long note in `cmd/derostorm/gpu_backend.go`.

## Building it

Needs ROCm on Linux, or the AMD **HIP SDK for Windows** — a separate download
from the Adrenalin driver. Neither needs an AMD card to be present: the compiler
targets the `gfx` list in the build script, not the machine it runs on.

```
gpu/buildlib_hip.sh          # Linux   -> gpulib/linux/libderostorm_hip<N>.so
gpu\buildlib_hip.bat         # Windows -> gpulib\windows\derostorm_hip.dll
```

Each script copies its result into place itself. Then build the miner as usual
(`./build.sh` or `.\build.ps1`); it picks the library up from here.

On Linux, ROCm 7 is the current runtime — Arch carries it in `extra`, and AMD's
own repository has it for the distributions it supports. ROCm 6 is the same
story a generation back and also targets RDNA 4. ROCm 5 is three packages from
Ubuntu 24.04's universe if that is all you can get:

```
apt-get install hipcc libamdhip64-dev rocm-device-libs-17
```

`DSG_HIPCC` picks the compiler when a machine has more than one, which is how
the shipped set is built. Some ROCm 7 packagings ship `amdclang++` and no
`hipcc` at all; the script looks for both.

The script offers each `gfx` target to the compiler before using it and builds
only the ones it accepts, so an older ROCm produces a narrower library rather
than a failed build. It prints what it skipped. `DSG_HIP_ARCHS` overrides the
list outright:

```
DSG_HIP_ARCHS="gfx1030 gfx1100" gpu/buildlib_hip.sh
set DSG_HIP_ARCHS=gfx1030 gfx1100
```

There is no PTX equivalent on AMD, so a card whose `gfx` target is not in the
library does not compile at load — it fails to launch and mines on the CPU. Keep
the list ahead of the hardware.

## Running it

No SDK needed, only the runtime:

- **Windows** — the Adrenalin driver ships it (`amdhip64_6.dll`).
- **Linux** — ROCm installed, for `libamdhip64`.

Supported cards are RDNA only: RX 5000, 6000, 7000 and 9000
(`gfx1010`–`gfx1201`). Vega, Polaris and the CDNA MI cards are wave64 and are
refused at compile time, because the block radix sort's shared-memory layout is
32 lanes wide throughout `gpu/blockradix.cuh`.

## Checking it

In order, because each step tells you something different when it fails:

```
derostorm --bench --gpu=all        # does it see the card, and how fast
go test ./cmd/derostorm/ -run GPU  # does it agree with the CPU, byte for byte
```

The test suite is the one that matters. `TestGPUHashMatchesCPU` and
`TestGPUSearchAgreesWithCPU` run the GPU hash and the CPU hash over the same
nonces and compare every byte, through whichever backend is present — so an AMD
card proves itself exactly the way the NVIDIA one did. A share is not trusted
until it has.

**Report what you get either way**, at
<https://github.com/Notoriousjoshyb/DEROSTORM/issues>. No AMD card has ever
mined with this, so "it works, here is the hashrate" is as useful a report as a
failure — and a card that mines correctly but slowly is worth an issue too. The
block count, batch size and radix width were all tuned on NVIDIA hardware.

One known non-bug: an integrated Radeon reports system memory as VRAM, so the
miner sizes a batch the driver will not actually back and `dsg_init` fails with
`create stream: out of memory`. A one-compute-unit iGPU was never going to mine
anyway; a large APU might, and that sizing has not been fixed.

What to report if something goes wrong:

- **"no kernel image is available for execution"** — the card's `gfx` target is
  not in the build. `rocminfo | grep gfx` says which it is.
- **No devices at all, with an AMD card present** — the build had no library
  for this rig's ROCm generation, or the HIP runtime is not installed. From
  1.7.3 `--gpu=all` says which: the error carries what each backend reported,
  and an unresolved `libamdhip64.so.N` names the generation that is missing.
  `ls /opt/rocm/lib/libamdhip64.so.*` says which one the rig has.
- **A wrong hash** — the interesting one. `gpu/gpuapi.cuh` has a
  `DSG_BYTE_PERM_FALLBACK` switch (build with `-DDSG_BYTE_PERM_FALLBACK=1`) that
  replaces the one intrinsic whose AMD semantics were taken on trust with a slow
  but obviously-correct version. If that fixes it, the intrinsic is the bug.
