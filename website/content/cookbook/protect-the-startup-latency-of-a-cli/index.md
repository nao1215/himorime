---
title: Protect the startup latency of a CLI
---

Your CLI must start quickly, and you want CI to fail when a change makes it slower than a limit you chose.

<!-- example: examples/startup/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: protect the startup latency of a CLI.
# https://nao1215.github.io/himorime/cookbook/#protect-the-startup-latency-of-a-cli
version: "1"

name: startup
description: How long the word counter takes to start, print its version and exit.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 2
  runs: 20

benchmarks:
  - name: version
    tags: [smoke]
    commands:
      wordcount:
        # A list of arguments runs without a shell, the same way on Linux,
        # macOS and Windows.
        command: ["${artifact}", -version]
        budget:
          latency:
            median: "< 250ms"
            p95: "<= 500ms"
    # A budget is a limit you chose. When it is missed himorime exits 1,
    # whatever the base branch does. Keep CI limits generous: shared runners
    # start processes several times slower than a laptop.
```

```console
$ himorime run examples/startup
```

A latency table, a budgets table with `PASS` for the median and the 95th percentile, and exit status 0. When a budget is missed its row says `FAIL`, the latency row says `OVER BUDGET`, a note names the budget and the measured value, and himorime exits 1.

- Use the budget syntax in [Configuration](/configuration/#metrics-budgets-and-tolerances); percentiles belong under `latency`.
- A command this short mostly measures process creation, so use a representative workload for algorithmic regressions; see [Regression detection](/regression-detection/#noise-and-inconclusive-results).
- A budget does not need a base revision, so the same file works in `himorime run` on a laptop and in CI.

Example: [`examples/startup`](https://github.com/nao1215/himorime/tree/main/examples/startup)
