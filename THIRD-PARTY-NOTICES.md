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

## Go module dependencies

`vendor/` also contains the remaining Go module dependencies listed in
`go.mod`. Each keeps the licence in its own directory. Run `go mod vendor` to
refresh them.

## Mining rewards

Rewards go to whatever address you configure and to nobody else. There is no
developer fee.
