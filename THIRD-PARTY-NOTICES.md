# Third-party notices

This repository vendors its dependencies so it builds standalone. Those
dependencies keep their own licences. The MIT licence in `LICENSE` applies to
the original derostorm code only, never to anything listed below.

## DERO (derohe) — RESEARCH LICENSE 1.1.2

`vendor/github.com/deroproject/derohe/`

Copyright DERO Project. Licensed under the DERO RESEARCH LICENSE, version
1.1.2. Full text: `vendor/github.com/deroproject/derohe/LICENSE`.

**Read this before you use derostorm.** The RESEARCH LICENSE permits research,
evaluation, teaching and personal customisation only. It expressly excludes use
or distribution for direct or indirect commercial gain. Commercial use requires
a separate commercial licence from the DERO Project.

Because derostorm builds on this code, the same restriction reaches any build
of derostorm that includes it.

## libsais — Apache License 2.0

`native/libsais/`

Copyright (c) 2021-2024 Ilya Grebnov. Licensed under the Apache License,
Version 2.0. Full text: `native/libsais/LICENSE`. The source is unmodified.
Only the small C wrapper in `native/derostorm_sa.c` is original derostorm code.

## purego — Apache License 2.0

`vendor/github.com/ebitengine/purego/`

Copyright The Ebitengine Authors. Licensed under the Apache License, Version
2.0. Full text: `vendor/github.com/ebitengine/purego/LICENSE`. The source is
unmodified.

Used by `cmd/derostorm/gpu_cuda.go` to call the embedded CUDA library's C entry
points without cgo — `dlopen` on Linux, and the typed function binding on both
platforms. Avoiding cgo is what keeps the Linux miner a cross-compile from
Windows and keeps a C toolchain out of an ordinary build.

## Dirtybird C Miner — MIT (idea, not code)

https://github.com/Dirtybird99/Dirtybird-C-Miner — Copyright (c) Dirtybird99,
MIT licensed.

No Dirtybird source is included in this repository. It is listed here because
the design of the descriptor suffix sort in `native/descriptor.c` follows an
insight first published in that miner: that AstroBWTv3's stage-1 output has
exploitable structure a general suffix sort discards. The C in
`native/descriptor.c` is original DeroStorm code and is covered by our MIT
licence, but the credit for the approach belongs to Dirtybird99. See
`CREDITS.md`.

## Go module dependencies

`vendor/` also contains the remaining Go module dependencies listed in
`go.mod`. Each keeps the licence in its own directory.

**`go mod vendor` is destructive here.** The vendored derohe is not upstream
derohe: `astrobwt/astrobwtv3/pow.go` is modified and `sha_hook.go` is an
addition, and between them they provide `AstroBWTv3_pair` and `SHA256Pair`,
which `cmd/derostorm/` calls. Neither exists in the `replace` target, so
`go mod vendor` overwrites the first and deletes the second, and the build then
fails with `undefined: astrobwtv3.AstroBWTv3_pair`. If you have run it, put
them back with:

```
git checkout -- vendor/github.com/deroproject/derohe/astrobwt/astrobwtv3/
```

To add a dependency, run `go mod vendor` and then that `git checkout` — the
new module's files survive it, since it only restores the derohe ones.

## Mining rewards

Rewards go to whatever address you configure and to nobody else. There is no
developer fee.
