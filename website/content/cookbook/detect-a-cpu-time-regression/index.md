---
title: Detect a CPU time regression
---

A change made the program burn more CPU, even if the wall-clock time on your machine barely moved.

<!-- example: examples/cpu-regression/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: detect a CPU time regression.
# https://nao1215.github.io/himorime/cookbook/#detect-a-cpu-time-regression
#
#   himorime compare --against main examples/cpu-regression
version: "1"

name: cpu regression
description: CPU time of the word counter, compared between a base revision and the working tree.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 2
  runs: 20

benchmarks:
  - name: count 50k lines
    setup:
      - command: ["${artifact}", -gen, "50000", -o, "${workdir}/input.txt"]
    metrics:
      # User and system CPU time of the process tree, read from the operating
      # system when each run exits.
      cpu: true
    commands:
      wordcount:
        command: ["${artifact}", "${workdir}/input.txt"]
        budget:
          cpu_total:
            median: "<= 5s"
    regression:
      # Latency and CPU time get separate tolerances. CPU time ignores time
      # spent waiting, so it moves less when the runner is busy.
      latency:
        max_percent: 25
      cpu_total:
        max_percent: 20
        # A difference smaller than this is never a regression, however large
        # it is in percent.
        min_difference: 5ms
```

```console
$ himorime compare --against main examples/cpu-regression
```

Two comparison tables, `latency` and `cpu total`, each with `BASE`, `HEAD`, `DIFF`, `CHANGE`, `CONFIDENCE`, `TOLERANCE` and `RESULT`. When the head uses clearly more CPU time the `cpu total` row says `REGRESSION` and himorime exits 1. The end-to-end suite proves it by shrinking the word counter's read buffer in a scratch repository, which multiplies its read calls.

- [CPU time](/metrics/#cpu-time-and-utilization) is user plus system time for the process tree, reported when each run exits; waiting is excluded.
- `cpu.max_percent` and `cpu.min_difference` are separate from latency; [minimum meaningful change](/regression-detection/#minimum-meaningful-change) explains the absolute threshold.
- CPU utilization is reported but not judged as a regression because higher use can mean better parallelism.

Example: [`examples/cpu-regression`](https://github.com/nao1215/himorime/tree/main/examples/cpu-regression)
