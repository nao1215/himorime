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

The workflow:

<!-- example: examples/github-actions/benchmark.yml -->
```yaml
# Copy this file to .github/workflows/benchmark.yml.
# Recipe: fail a GitHub Actions job when a performance budget is violated.
# https://nao1215.github.io/himorime/cookbook/#fail-a-github-actions-job-when-a-performance-budget-is-violated
name: Benchmark

on:
  pull_request:

# Fork PRs run with read-only permissions and receive no comment.
permissions:
  contents: read

jobs:
  benchmark:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          # The base commit must exist locally for the temporary worktree.
          fetch-depth: 0
          persist-credentials: false
      # Go builds the suite's program; drop it if your suite needs no Go.
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version: stable
      # Installs a prebuilt, checksum-verified himorime release.
      - uses: nao1215/setup-himorime@30710690b8b2dc8c61df8bf289f5bb899af3fbfa # v0.1.2 + automatic comments
      # Compares the pull request base and head, writes a job summary and
      # annotations, and saves JSON for setup-himorime's automatic comment.
      - run: himorime ci --format json --output "$RUNNER_TEMP/himorime.json"
      # Keep raw samples available from the run page.
      - uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1
        if: always()
        with:
          name: himorime-report
          path: ${{ runner.temp }}/himorime.json
          retention-days: 7
```

```console
$ himorime run examples/github-actions
```

Locally, `himorime run` checks the same budgets without a base revision. In the workflow, `himorime ci` compares with the pull request's base, writes the tables to the job summary, adds an annotation for every missed budget or regression, and exits:

| Exit | The job failed because |
|---|---|
| `1` | a budget was missed or a regression was confirmed: the code got slower, hungrier or less productive |
| `4` | a command, hook, build or Git operation failed: the measurement did not complete |
| `6` | a requested metric could not be measured or a required budget could not be assessed |

The last line of the log says the same in words, and annotations are titled `performance budget exceeded`, `performance regression`, `benchmark could not run`, `metric could not be measured` or `required budget could not be assessed`.

- The workflow is `pull_request` with `contents: read` and no secrets. himorime refuses `pull_request_target`.
- Check out with `fetch-depth: 0` so the base commit exists locally.
- The same suite file runs unchanged on a laptop and in CI; only the command differs.

Example: [`examples/github-actions`](https://github.com/nao1215/himorime/tree/main/examples/github-actions)
