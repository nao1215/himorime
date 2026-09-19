---
title: Compare similar CLIs on the same input
---

You want to see how different tools, or different modes of one tool, perform on exactly the same input, including how much CPU and memory each uses.

<!-- example: examples/compare-clis/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: compare similar CLIs on the same input.
# https://nao1215.github.io/himorime/cookbook/#compare-similar-clis-on-the-same-input
version: "1"

name: compare CLIs
description: Two word counter implementations and git hashing the same file.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 10

benchmarks:
  - name: count a 20k-line file
    setup:
      - command: ["${artifact}", -gen, "20000", -o, "${workdir}/input.txt"]
    metrics:
      cpu: true
      memory: true
      # git on Windows runs through a launcher process; measure everything
      # else there instead of stopping.
      unsupported: skip
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner, "${workdir}/input.txt"]
      readall:
        command: ["${artifact}", -impl, readall, "${workdir}/input.txt"]
      git-hash:
        command: [git, hash-object, "${workdir}/input.txt"]
```

```console
$ himorime run examples/compare-clis
```

One row per command in each of the latency, CPU and memory tables. `RELATIVE` is each median latency divided by the `baseline` command's, so `scanner` shows `1.00x`. The `readall` row holds noticeably more memory than `scanner`, because it reads the whole file at once.

- Commands run interleaved in a seeded order; see [Regression detection](/regression-detection/#measuring).
- `setup` generates the input once into `${workdir}`; every command reads the same file.
- `unsupported: skip` keeps the recipe running when a platform cannot measure a requested metric; see [Metrics](/metrics/#unsupported-failed-and-not-requested).

Example: [`examples/compare-clis`](https://github.com/nao1215/himorime/tree/main/examples/compare-clis)
