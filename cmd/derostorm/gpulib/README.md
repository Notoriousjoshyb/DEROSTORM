# AMD (HIP) kernel libraries

The miner embeds one GPU library per vendor. NVIDIA's sits beside this
directory (`derostorm_gpu.dll`, `libderostorm_gpu.so`). AMD's goes in here:

    windows/derostorm_hip.dll      built by gpu\buildlib_hip.bat
    linux/libderostorm_hip.so      built by gpu/buildlib_hip.sh

Both come from the same source as the NVIDIA library — `gpu/derostorm_gpu.cu`
and the headers beside it — through `hipcc` instead of `nvcc`.
`gpu/gpuapi.cuh` is the only file that knows which compiler it is talking to.

None of these libraries are in git: they are build products, and a fat binary in
git is a fat binary in git forever. The difference is that the NVIDIA ones are
**required** and these are **optional**. `go:embed` takes this whole directory
rather than the files, so a tree without them still compiles; the HIP backend
then finds no library and reports no devices — the same thing, to the miner, as
a machine with no AMD card. See the long note in `cmd/derostorm/gpu_backend.go`.

## Building it

Needs ROCm on Linux, or the AMD **HIP SDK for Windows** — a separate download
from the Adrenalin driver. Neither needs an AMD card to be present: the compiler
targets the `gfx` list in the build script, not the machine it runs on.

```
gpu/buildlib_hip.sh          # Linux   -> gpulib/linux/libderostorm_hip.so
gpu\buildlib_hip.bat         # Windows -> gpulib\windows\derostorm_hip.dll
```

Each script copies its result into place itself. Then build the miner as usual
(`./build.sh` or `.\build.ps1`); it picks the library up from here.

If the compiler rejects a `gfx` target it has never heard of — an older ROCm and
a newer card, usually — trim the list rather than giving up:

```
DSG_HIP_ARCHS="gfx1030 gfx1100" gpu/buildlib_hip.sh
set DSG_HIP_ARCHS=gfx1030 gfx1100
```

There is no PTX equivalent on AMD, so a card whose `gfx` target is not in the
list does not compile at load — it fails to launch and mines on the CPU. Keep
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
<https://github.com/Notoriousjoshyb/DEROSTORM/issues>. This has never been run
on an AMD card, so "it works, here is the hashrate" is as useful a report as a
failure — and a card that mines correctly but slowly is worth an issue too. The
block count, batch size and radix width were all tuned on NVIDIA hardware.

What to report if something goes wrong:

- **"no kernel image is available for execution"** — the card's `gfx` target is
  not in the build. `rocminfo | grep gfx` says which it is.
- **No devices at all, with an AMD card present** — either this directory had no
  library when the miner was built, or the HIP runtime is not installed.
- **A wrong hash** — the interesting one. `gpu/gpuapi.cuh` has a
  `DSG_BYTE_PERM_FALLBACK` switch (build with `-DDSG_BYTE_PERM_FALLBACK=1`) that
  replaces the one intrinsic whose AMD semantics were taken on trust with a slow
  but obviously-correct version. If that fixes it, the intrinsic is the bug.
