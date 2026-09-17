# himorime

[![UnitTest](https://github.com/nao1215/himorime/actions/workflows/unit_test.yml/badge.svg)](https://github.com/nao1215/himorime/actions/workflows/unit_test.yml)
[![E2E](https://github.com/nao1215/himorime/actions/workflows/e2e.yml/badge.svg)](https://github.com/nao1215/himorime/actions/workflows/e2e.yml)
[![tested with atago](https://img.shields.io/badge/tested%20with-atago-7c3aed?logo=data:image/svg%2Bxml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCI%2BPHBhdGggZmlsbD0iI2ZmZiIgZD0iTTMuNiA0LjIgMTEuOSAxMmwtOC4zIDcuOC0xLjktMi4yTDcuOSAxMiAxLjcgNi40eiIvPjxyZWN0IGZpbGw9IiNmZmYiIHg9IjEyLjYiIHk9IjE3LjIiIHdpZHRoPSI5LjciIGhlaWdodD0iMi44IiByeD0iMS40Ii8%2BPC9zdmc%2B&logoColor=white)](https://github.com/nao1215/atago)
[![Lint](https://github.com/nao1215/himorime/actions/workflows/lint.yml/badge.svg)](https://github.com/nao1215/himorime/actions/workflows/lint.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/nao1215/himorime.svg)](https://pkg.go.dev/github.com/nao1215/himorime)

himorime answers one question for a command-line program: **does this CLI stay
within its performance budget?** It measures latency, throughput, CPU time and
peak memory from a `himorime.yaml` kept in your repository, checks your budgets,
compares against a Git base revision, and fails CI when performance gets
worse. The same file and the same command work on your laptop and in GitHub
Actions.

himorime is a sister project of [atago](https://github.com/nao1215/atago):
atago tests what a CLI does, himorime tests how fast and how lean it does it.

## Install

```console
$ go install github.com/nao1215/himorime@latest
```

On macOS, `brew install --cask nao1215/tap/himorime`. Prebuilt archives and
`.deb`, `.rpm` and `.apk` packages for Linux, macOS and Windows are on
[GitHub Releases](https://github.com/nao1215/himorime/releases), and
[setup-himorime](https://github.com/nao1215/setup-himorime) installs one in
GitHub Actions; see [Installation](https://nao1215.github.io/himorime/install/).

## A suite

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/nao1215/himorime/main/schema/himorime.schema.json
version: "1"

suite:
  name: mytool

build:
  command: [go, build, -o, "${artifact}", ./cmd/mytool]

benchmarks:
  - name: parse large input
    metrics:
      throughput:
        work:
          file_size: testdata/large.jsonl
      cpu: true
      memory: true
    commands:
      mytool:
        command: ["${artifact}", testdata/large.jsonl]
    budget:
      mytool:
        latency: {p95: "<= 100ms"}
        throughput: {median: ">= 50MiB/s"}
        cpu: {total: {median: "<= 80ms"}}
        memory: {peak_rss: {max: "<= 64MiB"}}
```

## Run it

```console
$ himorime run
```

```text
suite: mytool (himorime.yaml)
latency
BENCHMARK          COMMAND   MEDIAN     MEAN  STDDEV  RELATIVE  RESULT
parse large input  mytool   81.88ms  82.53ms  4.45ms     1.00x  PASS

throughput
BENCHMARK          COMMAND       MEDIAN         MEAN          MIN  RESULT
parse large input  mytool   189.22MiB/s  188.19MiB/s  165.48MiB/s  PASS

cpu
BENCHMARK          COMMAND     USER  SYSTEM    TOTAL  UTILIZATION  RESULT
parse large input  mytool   79.03ms  3.03ms  82.07ms       100.4%  OVER BUDGET

memory
BENCHMARK          COMMAND  PEAK RSS       MAX  RESULT
parse large input  mytool   10.57MiB  10.57MiB  PASS

budgets
BENCHMARK          COMMAND  METRIC                    BUDGET     MEASURED  RESULT
parse large input  mytool   latency p95          <= 100.00ms      89.64ms  PASS
parse large input  mytool   throughput median  >= 50.00MiB/s  189.22MiB/s  PASS
parse large input  mytool   cpu total median      <= 80.00ms      82.07ms  FAIL
parse large input  mytool   peak rss max         <= 64.00MiB     10.57MiB  PASS
  parse large input / mytool: budget cpu total median <= 80.00ms not met (measured 82.07ms)

0 passed, 1 over budget · 1 benchmark · seed 6177791292910221 · exit 1
himorime: exit 1: performance check failed: a budget or regression threshold was violated (the measurement itself succeeded)
```

(Explanatory lines under each table are omitted here.) Reports are also
available as JSON with every raw sample, long-format CSV, Markdown and a
GitHub Actions job summary.

To compare your working tree, uncommitted changes included, with `main` on
every metric:

```console
$ himorime compare --against main
```

Each revision runs its own build or scripts, and each metric can gate the
result or, with `regression.<metric>.gate: false`, only be reported.

## The same suite in GitHub Actions

<!-- example: examples/github-actions/benchmark.yml -->
```yaml
# Copy this file to .github/workflows/benchmark.yml.
# Recipe: fail a GitHub Actions job when a performance budget is violated.
# https://nao1215.github.io/himorime/cookbook/#fail-a-github-actions-job-when-a-performance-budget-is-violated
name: Benchmark

on:
  pull_request:

# Read-only: himorime needs no secret and no write token, so pull requests from
# forks run it safely. Do not use pull_request_target, which would run the
# pull request's code with the base repository's secrets.
permissions:
  contents: read

jobs:
  benchmark:
    runs-on: ubuntu-latest
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
      - uses: nao1215/setup-himorime@b2896dee554ef31ea979f0388d9be22779ce73a2 # v0.1.1
      # Finds the pull request's base commit from the event, compares it with
      # the checked-out head on every metric the suite measures, writes a
      # summary to the job page and annotations to the pull request, and
      # exits 1 on a missed budget or a confirmed regression. Exit 4 or 6
      # means the measurement itself failed, not the performance.
      - run: himorime ci
```

Exit status `1` means performance got worse; `4` and `6` mean the measurement
itself failed. Shared runners are noisy: himorime interleaves the revisions,
needs statistical confidence to call a regression, and reports anything it
cannot tell apart from noise as inconclusive.

## himorime and atago

[atago](https://github.com/nao1215/atago) checks that a CLI **behaves** as
expected: exit codes, output, files. himorime checks that it **performs** as
expected: latency, throughput, CPU time and memory within budget. They are
meant to be used side by side.

## Documentation

https://nao1215.github.io/himorime/ — [getting started](https://nao1215.github.io/himorime/getting-started/),
[configuration](https://nao1215.github.io/himorime/configuration/),
[metrics](https://nao1215.github.io/himorime/metrics/),
[reports](https://nao1215.github.io/himorime/reports/),
[regression detection](https://nao1215.github.io/himorime/regression-detection/),
[cookbook](https://nao1215.github.io/himorime/cookbook/),
[exit codes](https://nao1215.github.io/himorime/exit-codes/). Linux, macOS and
Windows on amd64 and arm64 are supported; what each metric covers per OS is on
the [metrics](https://nao1215.github.io/himorime/metrics/#process-tree) page.

## The name

`himorime` was inspired by `atago`, whose name is associated with protection
from fire. It is named after the Maiden in Black (*Hi no Bōjo*) from
*Demon's Souls*, who manages the player's stats through leveling.
The name also fits the tool's role of measuring performance stats and
detecting regressions.

## License

[MIT](./LICENSE)
