# Credits

DeroStorm is not the first miner to do any of this. What follows is who got
there first, and what DeroStorm took from them.

## Dirtybird C Miner — the structure-exploiting suffix sort

**https://github.com/Dirtybird99/Dirtybird-C-Miner** — by Dirtybird99, MIT.

The single largest speed-up in DeroStorm's CPU path is the descriptor suffix
sort in `native/descriptor.c`, and **the idea behind it is Dirtybird's, not
ours.** Dirtybird99 worked out the thing everyone else had missed: the text
AstroBWTv3 hands to the suffix sort is not arbitrary bytes. Stage 1 writes its
whole 256-byte state after each iteration and an iteration rewrites at most 32
of them, so consecutive blocks are near-copies, and a sort that knows that can
inherit most of its ordering instead of computing it.

Every general-purpose suffix sort — libsais included, and it is an excellent one
— throws that structure away before it starts. Seeing that it is there, and that
it is worth exploiting, is the hard part. Dirtybird did that first.

DeroStorm's implementation is its own code. The run splitting at the RC4 rekey
boundaries, the four-byte descriptor grouping, the merge-not-sort on collided
descriptors and the AVX2 scatter were written here and measured here. But they
are worked-out details of somebody else's insight, and the miner would be about
a quarter of its current CPU speed without it.

Thanks, Dirtybird.

## Wolf9466 and tnn-miner — the 256 operations are sixteen

**https://github.com/Tritonn204/tnn-miner** — by Tritonn204, and the file this
came from, `src/crypto/astrobwtv3/wolfbranching.cpp`, credits @Wolf9466.

AstroBWTv3's stage 1 is written as a 256-way switch, and everyone including us
implemented it as one: 2,300 lines of Go in `pow.go`, and 2,572 lines of
generated CUDA to match. Wolf noticed that it is not 256 of anything. Every one
of the operations is exactly **four instructions drawn from one set of sixteen**,
so the entire switch is a 512-byte table and a four-step loop.

DeroStorm's table is generated from `pow.go` by `gpu/gencases` rather than
copied, and it came out identical to Wolf's, entry for entry, all 256 — which is
the check that the reading is right, in both directions. No tnn-miner code is
included here.

On the GPU it is worth about 6% of stage 1, and it shrinks the embedded CUDA
library from 5,840 KB to 2,124 KB. On the CPU the same table is **4.7x slower**
than the switch it would replace, so the CPU keeps its switch — see the README.
That is not a criticism of the idea: tnn-miner pairs it with AVX2, which is what
makes it pay there, and DeroStorm's stage 1 is Go.

Thanks, Wolf.

## libsais — Ilya Grebnov

**https://github.com/IlyaGrebnov/libsais** — Apache-2.0.

The fallback suffix sort, and the reference every faster path is checked
against. Vendored unmodified in `native/libsais/`. Before the descriptor sort
existed, libsais alone was the fast path and worth about +18% over the Go
SA-IS it replaced.

## DERO Project

**https://github.com/deroproject/derohe** — RESEARCH LICENSE.

The chain, the AstroBWTv3 proof-of-work, and the getwork protocol. DeroStorm is
a miner for their network and vendors their code. See `THIRD-PARTY-NOTICES.md`
for what that licence permits — it is non-commercial.

## Everyone who published a negative result

The `What does not help` section of the README exists because measurements that
came back "no" are worth as much as the ones that came back "yes", and are
published far less often.
