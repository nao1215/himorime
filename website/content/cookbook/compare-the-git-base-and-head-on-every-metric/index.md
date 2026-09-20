---
title: Compare the Git base and head on every metric
---

Before pushing, you want to know whether your changes, committed or not, made the program slower, less productive, hungrier for CPU or for memory than `main`.

<!-- example: examples/git-compare/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: compare the Git base and head on every metric.
# https://nao1215.github.io/himorime/cookbook/#compare-the-git-base-and-head-on-every-metric
#
#   himorime compare --against main examples/git-compare
version: "1"

name: git compare
description: The word counter built from a base revision and from the working tree.

# In a comparison the build runs twice: once in a temporary worktree of the
# base revision and once in your working tree, uncommitted changes included.
# ${root} is this directory inside the tree being built.
build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 2
  runs: 20
  regression:
    confidence: 0.95
    latency:
      statistic: median
      max_percent: 10

benchmarks:
  - name: count 50k lines
    setup:
      - command: ["${artifact}", -gen, "50000", -o, "${workdir}/input.txt"]
    stdin: "${workdir}/input.txt"
    metrics:
      throughput:
        work:
          file_size: "${workdir}/input.txt"
      cpu: true
      memory: true
    commands:
      wordcount:
        command: ["${artifact}"]
    regression:
      # Throughput derives its verdict from latency. CPU and memory are
      # independent measurements with their own tolerances.
      cpu_total:
        max_percent: 15
        min_difference: 2ms
      peak_rss:
        max_percent: 10
        min_difference: 2MiB
```

```console
$ himorime compare --against main examples/git-compare
```

One comparison table per metric: `latency`, `throughput`, `cpu total` and `peak rss`. The tolerance column shows the direction that counts as worse: `+10%` for latency, `-10%` for throughput. The log says whether uncommitted changes were included.

- The base is checked out into a temporary Git worktree and built there. Your working tree, index and branches are never modified, and the worktree is removed on success, failure and Ctrl+C.
- Both builds are measured on this machine in interleaved rounds, so every metric sees the same noise; [Regression detection](/regression-detection/#measuring) describes the comparison.
- A regression needs a change beyond the tolerance and bootstrap confidence; use `--format json` to keep raw samples.

Example: [`examples/git-compare`](https://github.com/nao1215/himorime/tree/main/examples/git-compare)
