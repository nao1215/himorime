---
title: Fail a GitHub Actions job when a performance budget is violated
---

Every pull request must stay within its budgets and must not regress against its base branch, and the job log has to say whether a failure is about performance or about the measurement.

<!-- example: examples/github-actions/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: the suite .github/workflows/benchmark.yml runs, locally or in CI.
# https://nao1215.github.io/himorime/cookbook/#fail-a-github-actions-job-when-a-performance-budget-is-violated
#
#   himorime run examples/github-actions
#   HIMORIME_BASE_REF=main himorime ci examples/github-actions
version: "1"

name: github actions
description: Budgets and regression checks the pull request workflow enforces.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 2
  runs: 20
  regression:
    confidence: 0.95
    latency:
      max_percent: 15

benchmarks:
  - name: count 50k lines
    setup:
      - command: ["${artifact}", -gen, "50000", -o, "${workdir}/input.txt"]
    stdin: "${workdir}/input.txt"
    metrics:
      throughput:
        work:
          value: 50000
          unit: lines
      cpu: true
      memory: true
    commands:
      wordcount:
        command: ["${artifact}"]
        # Absolute budgets hold on every run, with or without a base revision.
        # Shared runners are slow and noisy, so the limits leave a wide margin.
        budget:
          latency:
            p95: "<= 2s"
          throughput:
            median: ">= 10000 lines/s"
          cpu_total:
            median: "<= 2s"
          peak_rss:
            max: "<= 256MiB"
    regression:
      cpu_total:
        max_percent: 20
        min_difference: 5ms
      peak_rss:
        max_percent: 10
        min_difference: 4MiB
```

Copy the workflow from [GitHub Actions](/github-actions/#a-workflow). `himorime ci` compares with the pull request's base, writes the job summary, annotates every missed budget or regression, and exits 1 for a performance failure, 4 when the measurement did not complete, and 6 when a metric or required budget could not be assessed; see [Why did the job fail?](/github-actions/#why-did-the-job-fail).

- The workflow is `pull_request` with `contents: read` and no secrets. himorime refuses `pull_request_target`.
- Check out with `fetch-depth: 0` so the base commit exists locally. Locally, `himorime run` checks the same budgets.

Example: [`examples/github-actions`](https://github.com/nao1215/himorime/tree/main/examples/github-actions)
