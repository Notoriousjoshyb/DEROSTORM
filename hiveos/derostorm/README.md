# DeroStorm on HiveOS

A custom miner package. DeroStorm mines AstroBWTv3 for DERO on the CPU and on
NVIDIA GPUs at the same time.

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
| Installation URL | wherever you host `derostorm-1.5.9.tar.gz` |
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

Leaving the box empty mines on every CPU thread and every NVIDIA card.

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

## Requirements

- linux/amd64. There is no arm64 GPU build; see the main README.
- An NVIDIA driver, for GPU mining. The CUDA runtime is linked in statically, so
  nothing else has to be installed. Cards from Turing (RTX 20xx) up are covered,
  including Blackwell; CUDA 13 dropped Pascal and Volta.
- `jq`, which HiveOS already has.
- CPU-only mining works with no GPU present: add `--gpu=off`.

## Checking it by hand

```
cd /hive/miners/custom/derostorm
./derostorm --bench --gpu=all --no-tui
```

That needs no node and no wallet, and it verifies each card against the CPU
before reporting a number for it.
