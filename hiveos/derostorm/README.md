# DeroStorm on HiveOS

A custom miner package. DeroStorm mines AstroBWTv3 for DERO on the CPU and on
NVIDIA or AMD GPUs at the same time.

> **AMD support ships in the binaries from 1.7.1, and has never mined.** The
> hash it produces has been checked against the CPU on a real AMD GPU and
> matches exactly; a batch has never run, and there is no AMD hashrate figure
> anywhere. If you run an AMD rig, please report what happens — working or not,
> fast or slow — at <https://github.com/Notoriousjoshyb/DEROSTORM/issues>.
> NVIDIA rigs are unaffected by this release.

## The one thing to know first

**DeroStorm is a solo miner.** It speaks `derod`'s getwork, so it needs a DERO
node, not a pool. In the flight sheet the "Pool URL" field is the address of
that node — your own `derod`, or a public one — and the wallet is the address a
found block pays into. There is no pool account, no worker name, and no pool
share accounting, so HiveOS's "accepted" column counts miniblocks and blocks
instead.

If you want pool mining, this is the wrong miner.

## Flight sheet

| field | value |
|---|---|
| Miner | Custom |
| Miner name | `derostorm` |
| Installation URL | wherever you host `derostorm-1.8.7.tar.gz` |
| Wallet and worker template | your DERO address, e.g. `dero1qy...` |
| Pool URL | your derod getwork address, e.g. `10.0.0.5:10100` |
| Extra config arguments | anything below |

A URL with a scheme (`stratum+tcp://host:10100`) or a trailing path is accepted
— `h-config.sh` reduces it to `host:port`. A bare host gets `:10100`.

## Extra config arguments

Passed to the miner verbatim, so every flag it has is reachable:

```
--mining-threads=12       CPU threads. Default: logical CPUs x 2 - 1
--gpu=all                 or 0,1  or off. Default: all
--gpu-blocks=336          pin the suffix kernel's block count
--gpu-batch=32768         nonces per GPU launch
--testnet                 mine testnet
--debug                   verbose logging to derostorm.log
```

Leaving the box empty mines on every CPU thread and every card, of either
vendor. Cards are numbered NVIDIA first and then AMD, so on a mixed rig
`--gpu=0` is the first NVIDIA card.

## What HiveOS shows

`h-stats.sh` reads a JSON document the miner writes every five seconds
(`--stats-file`), not the log.

- **Rig hashrate** is the total, CPU and GPUs together.
- **Per-card hashrates** are the GPUs only. On a rig that is also mining on its
  processor the cards will not add up to the total, and that is not a bug: the
  difference is the CPU.
- **Accepted** is miniblocks plus blocks. **Rejected** is rejects.
- Card temperatures and fans come from the miner's own sensor poll, and are
  lined up with HiveOS's cards by PCI bus number.

If the miner stops, the stats file goes with it and HiveOS shows nothing rather
than the last hashrate it managed — a frozen number looks like a working rig.

## Files

```
derostorm/
├── h-manifest.conf   name, version, where the log and config go
├── h-config.sh       flight sheet -> derostorm.conf
├── h-run.sh          starts the miner
├── h-stats.sh        stats file -> khs and stats
└── derostorm         the miner, linux/amd64
```

## Tuning

**CPU threads are chosen for you.** The miner's own default is "logical CPUs
minus one", which suits a desktop and is wrong for a rig: every card needs a
host thread to feed it, and DeroStorm's own measurements show a CPU miner
competing with that feeder costs more than it earns. `h-config.sh` therefore
sets `--mining-threads` to *CPUs − cards fed − 1*, never below one. Four threads
and six cards gets one CPU miner, not three. Put `--mining-threads=N` in the
extra arguments to override it, and it will honour `--gpu=off` and
`--gpu=0,1` when working out how many cards are actually being fed.

**Up to 16 cards.** The nonce tagging gives GPUs the byte range 0xf0..0xff, so
16 is the hard ceiling. Beyond that the extra cards are refused with a log line
rather than counted and left idle, which is what 1.5.9 and earlier did.

**Block count needs no tuning.** The suffix kernel plateaus at four resident
blocks per SM and stays flat above it — measured 134,209 H/s at 336 blocks
against 134,214 at 672 and 133,597 at 1,344 on a 5080. The default is already
the plateau. `--gpu-blocks=N` is there if you want to pin it.

**What has not been measured, honestly.** Every Linux figure here was taken
under WSL, which virtualises the GPU. Four runs on the same binary read
136.4, 122.2, 117.1 and 137.6 KH/s — a 15% spread, against 5% for the same
binary on Windows. That noise is the virtualisation, not Linux, and it is too
loud to tune against. HiveOS runs on bare metal and should sit closer to the
Windows figure; the historical gap when it was last measured properly was about
2.5%. If you have numbers from a real rig they are worth more than anything in
this paragraph.

Two things worth checking on the rig itself, neither of which this package can
do for you: the CPU governor (HiveOS may boot in `powersave`) and persistence
mode on the cards.

## Requirements

- linux/amd64, up to 16 cards. There is no arm64 GPU build; see the main README.
- **NVIDIA:** the display driver and nothing else. The CUDA runtime is linked in
  statically. Cards from Turing (RTX 20xx) up are covered, including Blackwell;
  CUDA 13 dropped Pascal and Volta.
- **AMD:** ROCm installed, for `libamdhip64`, and an RDNA card — RX 5000, 6000,
  7000 or 9000. Vega, Polaris and the MI cards are wave64 and are not supported.
  The kernels are in the binary from 1.7.1 on, for ROCm 5, ROCm 6 and — from
  1.7.3 — ROCm 7; the right one is picked at load. **A ROCm 7 rig needs 1.7.3**:
  before it, only ROCm 6 and ROCm 5 were looked for, so the card was not found.
- `jq`, which HiveOS already has.
- CPU-only mining works with no GPU present: add `--gpu=off`.

## Checking it by hand

```
cd /hive/miners/custom/derostorm
./derostorm --bench --gpu=all --no-tui
```

That needs no node and no wallet, and it verifies each card against the CPU
before reporting a number for it.
