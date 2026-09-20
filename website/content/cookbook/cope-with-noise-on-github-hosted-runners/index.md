---
title: Cope with noise on GitHub-hosted runners
---

Comparisons on shared runners flip between pass and fail, and you want results you can trust without buying a dedicated machine.

<!-- example: examples/noisy/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: cope with noise on GitHub-hosted runners.
# https://nao1215.github.io/himorime/cookbook/#cope-with-noise-on-github-hosted-runners
#
#   himorime compare --against main examples/noisy
#   himorime compare --against main --fail-on-inconclusive examples/noisy
version: "1"

name: noisy
description: A stand-in program with bimodal run times, and a steady one with noise-tolerant settings.

build:
  command: [go, build, -o, "${artifact}", ../tools/sleepy]

defaults:
  # Warmup runs absorb one-off costs such as a cold file cache.
  warmup: 1
  # More runs make the bootstrap interval narrower on a noisy machine.
  runs: 16

benchmarks:
  - name: bimodal
    commands:
      sleepy:
        command: ["${artifact}", -ms, "10", -jitter-ms, "50", -state, "${workdir}/calls"]
    regression:
      confidence: 0.95
      latency:
        max_percent: 10
      # The alternating run time gives a coefficient of variation near 0.7.
      # Above max_cv a comparison is inconclusive instead of pass or fail.
      max_cv: 0.3

  - name: steady
    metrics:
      cpu: true
    commands:
      sleepy:
        command: ["${artifact}", -ms, "30"]
    regression:
      # A wider tolerance than the default 10%, and an absolute floor: a
      # change of less than 2ms is never a regression on a shared runner,
      # however large it is in percent.
      latency:
        max_percent: 20
        min_difference: 2ms
      cpu_total:
        max_percent: 50
        min_difference: 5ms
```

```console
$ himorime compare --against main examples/noisy
```

`bimodal` is `INCONCLUSIVE` with the reason `measurements are noisier than max_cv`; `steady` passes. himorime exits 0. With `--fail-on-inconclusive` the same run exits 1.

- `INCONCLUSIVE` means the comparison cannot tell; its exit behavior and causes are in [Regression detection](/regression-detection/#the-verdict).
- Interleaved rounds expose both revisions to the same noise; [noise guidance](/regression-detection/#noise-and-inconclusive-results) covers samples and warmups.
- `min_difference` and `max_percent` are the absolute and relative thresholds described in [Regression detection](/regression-detection/#minimum-meaningful-change).
- Prefer an absolute budget with a wide margin for hard limits, and a relative comparison for trends. CPU time and peak RSS are usually steadier than latency on a shared runner.
- A single-digit percentage change of a millisecond command is below what a shared runner can resolve. Measure a representative workload instead.

Example: [`examples/noisy`](https://github.com/nao1215/himorime/tree/main/examples/noisy)
