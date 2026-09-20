---
title: Gate on CPU time and memory, report latency only
---

Your program spends most of its time waiting, so its latency follows the runner's load, and you want CI to fail only when it uses more CPU time or memory while still seeing how latency moved.

<!-- example: examples/gate-cpu-memory/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: gate on CPU time and memory, report latency only.
# https://nao1215.github.io/himorime/cookbook/#gate-on-cpu-time-and-memory-report-latency-only
#
#   himorime compare --against main examples/gate-cpu-memory
version: "1"

name: gate cpu and memory
description: A program that does a fixed amount of work and then waits, compared on CPU time and peak RSS; its latency is shown but decides nothing.

build:
  command: [go, build, -o, "${artifact}", ../tools/sleepy]

defaults:
  warmup: 1
  runs: 15

benchmarks:
  - name: work then wait
    metrics:
      cpu: true
      memory: true
    commands:
      sleepy:
        # -busy-ms 150 keeps a CPU busy for 150ms and -alloc-mb 32 holds
        # 32MiB before the program waits. Both are chosen so the gated
        # metrics are measurable everywhere: Windows accounts CPU time in
        # scheduler ticks of 15.625ms, and a peak RSS at or below the
        # measurement floor of a few mebibytes can only be compared
        # conservatively. See CPU time and The floor on the Metrics page.
        command: ["${artifact}", -busy-ms, "150", -alloc-mb, "32"]
    regression:
      # Latency is still measured, compared and reported with its verdict,
      # marked NOT GATED, but a latency regression or an inconclusive latency
      # comparison never fails the run.
      latency:
        gate: false
      # CPU time and peak RSS decide the result and the exit status.
      cpu_total:
        max_percent: 25
        # Two Windows scheduler ticks: a difference smaller than this cannot
        # be told from the accounting on the coarsest platform.
        min_difference: 32ms
      peak_rss:
        max_percent: 25
        min_difference: 4MiB
```

```console
$ himorime compare --against main examples/gate-cpu-memory
```

Three comparison tables. The `latency` row reads `PASS (NOT GATED)`, or `REGRESSION (NOT GATED)` when the program got slower; `cpu total` and `peak rss` decide the exit status.

- `gate: false` keeps a metric measured, compared and reported without letting its verdict fail the run; see [Regression detection](/regression-detection/#what-fails-the-run).
- The report marks not-gated comparisons in tables, JSON and GitHub Actions; budgets remain enforced as described in [Configuration](/configuration/#metrics-budgets-and-tolerances).
- The command is given work to do and memory to hold so that both gated metrics are measurable on every platform. Gate on a metric your program moves by more than the resolution of its measurement: see [CPU time](/metrics/#cpu-time-and-utilization) and [The floor](/metrics/#the-floor).

Example: [`examples/gate-cpu-memory`](https://github.com/nao1215/himorime/tree/main/examples/gate-cpu-memory)
