---
title: Cookbook
description: Runnable recipes for himorime, found by what you want to do. Every recipe is an example suite in the repository, and CI runs each one.
toc: true
filter: true
---

Every recipe here is a suite under [`examples/`](https://github.com/nao1215/himorime/tree/main/examples) that you can run from a clone of the repository. The end-to-end suite runs each recipe (`test/e2e/atago/cookbook.atago.yaml`, one scenario per heading) on Linux, macOS and Windows, and a test fails if a recipe and its scenario drift apart. Recipes that reference other CLIs use `git`, which CI already has; nothing is downloaded.

The examples measure two small programs in `examples/tools`: `wordcount`, a word counter with a streaming, an in-memory and a parallel implementation, and `sleepy`, a stand-in whose run time, CPU use, child processes and exit status you control.

## Find a recipe

| I want to | Recipes |
|---|---|
| keep a CLI within a budget | [Protect the startup latency of a CLI](#protect-the-startup-latency-of-a-cli), [Protect the throughput of large CSV and JSON processing](#protect-the-throughput-of-large-csv-and-json-processing) |
| catch regressions against a base revision | [Detect a CPU time regression](#detect-a-cpu-time-regression), [Detect a peak RSS regression](#detect-a-peak-rss-regression), [Compare the Git base and head on every metric](#compare-the-git-base-and-head-on-every-metric), [Compare a script-based CLI without a build step](#compare-a-script-based-cli-without-a-build-step), [Gate on CPU time and memory, report latency only](#gate-on-cpu-time-and-memory-report-latency-only) |
| run it in CI | [Fail a GitHub Actions job when a performance budget is violated](#fail-a-github-actions-job-when-a-performance-budget-is-violated), [Cope with noise on GitHub-hosted runners](#cope-with-noise-on-github-hosted-runners) |
| compare programs or inputs | [Compare similar CLIs on the same input](#compare-similar-clis-on-the-same-input), [Compare parsers on a stdin fixture](#compare-parsers-on-a-stdin-fixture), [Compare small, medium and large inputs](#compare-small-medium-and-large-inputs), [Understand the geometric mean](#understand-the-geometric-mean) |
| understand the numbers | [Understand CPU utilization above 100%](#understand-cpu-utilization-above-100), [Understand process tree measurement on each OS](#understand-process-tree-measurement-on-each-os), [Handle a metric this platform cannot measure](#handle-a-metric-this-platform-cannot-measure) |
| keep results | [Save JSON, CSV and Markdown reports locally](#save-json-csv-and-markdown-reports-locally) |
| control state between runs | [Reset state before every run](#reset-state-before-every-run), [Clean up after success, failure or interruption](#clean-up-after-success-failure-or-interruption), [Benchmark a warm cache](#benchmark-a-warm-cache) |
| choose what runs | [Run only smoke benchmarks](#run-only-smoke-benchmarks) |
| handle failures | [Stop a command that hangs](#stop-a-command-that-hangs), [Treat a non-zero exit as an error](#treat-a-non-zero-exit-as-an-error), [Use a shell pipeline](#use-a-shell-pipeline) |

## Protect the startup latency of a CLI

Your CLI must start quickly, and you want CI to fail when a change makes it slower than a limit you chose.

<!-- example: examples/startup/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: protect the startup latency of a CLI.
# https://nao1215.github.io/himorime/cookbook/#protect-the-startup-latency-of-a-cli
version: "1"

suite:
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
    # A budget is a limit you chose. When it is missed himorime exits 1,
    # whatever the base branch does. Keep CI limits generous: shared runners
    # start processes several times slower than a laptop.
    budget:
      wordcount:
        median: "< 250ms"
        latency:
          p95: "<= 500ms"
```

```console
$ himorime run examples/startup
```

A latency table, a budgets table with `PASS` for the median and the 95th percentile, and exit status 0. When a budget is missed its row says `FAIL`, the latency row says `OVER BUDGET`, a note names the budget and the measured value, and himorime exits 1.

- `median: "< 250ms"` is a shorthand for `latency: {median: "< 250ms"}`. Percentiles such as `p95` or `p99.9` need the `latency:` form.
- A command this short mostly measures process creation. It catches start-up regressions, not algorithmic ones.
- A budget does not need a base revision, so the same file works in `himorime run` on a laptop and in CI.

Example: [`examples/startup`](https://github.com/nao1215/himorime/tree/main/examples/startup)

## Protect the throughput of large CSV and JSON processing

Your tool processes large files, and what matters is how much it gets through per second, not how long one file takes.

<!-- example: examples/throughput/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: protect the throughput of large CSV and JSON processing.
# https://nao1215.github.io/himorime/cookbook/#protect-the-throughput-of-large-csv-and-json-processing
version: "1"

suite:
  name: throughput
  description: Bytes and records per second of the word counter on large CSV and JSON Lines inputs.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 10

benchmarks:
  - name: csv bytes
    setup:
      - command: ["${artifact}", -gen, "200000", -gen-format, csv, -o, "${workdir}/input.csv"]
    metrics:
      throughput:
        # Throughput is work divided by latency, and himorime never guesses the
        # work: here it is the size of the input file, read before every run.
        work:
          file_size: "${workdir}/input.csv"
          unit: bytes
    commands:
      wordcount:
        command: ["${artifact}", "${workdir}/input.csv"]
    budget:
      wordcount:
        # Higher is better, so a throughput budget is a floor.
        throughput:
          median: ">= 5MiB/s"

  - name: jsonl records
    setup:
      - command: ["${artifact}", -gen, "200000", -gen-format, jsonl, -o, "${workdir}/input.jsonl"]
    metrics:
      throughput:
        # A fixed amount of work in your own unit: records, lines, files.
        work:
          value: 200000
          unit: records
    commands:
      wordcount:
        command: ["${artifact}", "${workdir}/input.jsonl"]
    budget:
      wordcount:
        throughput:
          median: ">= 50000 records/s"
          min: ">= 20000 records/s"
```

```console
$ himorime run examples/throughput
```

A throughput table in `MiB/s` for the CSV case and `records/s` for the JSON Lines case, and the budgets table. Throughput is computed per run, as the declared work divided by that run's latency.

- himorime never guesses the work. Declare either `value` (a fixed amount in your own `unit`) or `file_size` (the size of a file in bytes, read before every run, outside the measured time).
- Throughput is better when higher, so a budget is a floor (`>=` or `>`) and a comparison calls a drop a regression. himorime refuses `<=` on throughput.
- The budget's unit must match the work: `MiB/s` needs `unit: bytes`, `records/s` needs `unit: records`.
- `min` of throughput is the slowest run. A percentile such as `p95` is the 95th percentile of the throughput values, which is the fast end.

Example: [`examples/throughput`](https://github.com/nao1215/himorime/tree/main/examples/throughput)

## Detect a CPU time regression

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

suite:
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
      wordcount:
        cpu:
          total:
            median: "<= 5s"
    regression:
      # Latency and CPU time get separate tolerances. CPU time ignores time
      # spent waiting, so it moves less when the runner is busy.
      latency:
        max_percent: 25
      cpu:
        max_percent: 20
        # A difference smaller than this is never a regression, however large
        # it is in percent.
        min_difference: 5ms
```

```console
$ himorime compare --against main examples/cpu-regression
```

Two comparison tables, `latency` and `cpu total`, each with `BASE`, `HEAD`, `DIFF`, `CHANGE`, `CONFIDENCE`, `TOLERANCE` and `RESULT`. When the head uses clearly more CPU time the `cpu total` row says `REGRESSION` and himorime exits 1. The end-to-end suite proves it by shrinking the word counter's read buffer in a scratch repository, which multiplies its read calls.

- CPU time is user plus system time of the process tree, reported by the operating system when each run exits. It does not include time spent waiting, so it is often steadier than latency on a busy machine, but frequency scaling and a busy host still move it.
- `cpu.max_percent` and `cpu.min_difference` are separate from the latency tolerance. `min_difference` keeps a large percentage of a tiny amount from failing CI.
- CPU utilization is reported but never judged as a regression: more CPU per second can mean better parallelism.

Example: [`examples/cpu-regression`](https://github.com/nao1215/himorime/tree/main/examples/cpu-regression)

## Detect a peak RSS regression

A change made the program hold much more memory at its peak, and you want the pull request to fail.

<!-- example: examples/memory-regression/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: detect a peak RSS regression.
# https://nao1215.github.io/himorime/cookbook/#detect-a-peak-rss-regression
#
#   himorime compare --against main examples/memory-regression
version: "1"

suite:
  name: memory regression
  description: Peak resident set size of the word counter, compared between two revisions.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 12

benchmarks:
  - name: count 200k lines
    setup:
      - command: ["${artifact}", -gen, "200000", -o, "${workdir}/input.txt"]
    metrics:
      memory: true
    commands:
      wordcount:
        command: ["${artifact}", "${workdir}/input.txt"]
    budget:
      wordcount:
        memory:
          peak_rss:
            max: "<= 512MiB"
    regression:
      latency:
        max_percent: 50
      memory:
        max_percent: 20
        min_difference: 4MiB
```

```console
$ himorime compare --against main examples/memory-regression
```

A `peak rss` comparison table in MiB. When the head holds clearly more memory the row says `REGRESSION` and himorime exits 1. The end-to-end suite proves it by switching the word counter's default implementation to one that reads the whole input into memory.

- Peak RSS is the largest resident set size of any single process in the tree, as the operating system recorded it. It is not the heap size or the allocation count of a language runtime, and it is not the sum of processes running at the same time.
- Memory samples are often identical run to run, so the comparison is usually decisive. `min_difference` stops a few hundred KiB of allocator noise from failing CI.
- A budget on `max` is a hard ceiling for the worst run; a budget on `median` tolerates an outlier.
- On Unix a peak RSS cannot be measured below a floor of a few MiB, the memory of the process that starts the command. A streaming base at the floor still shows a regression when the head clearly exceeds the floor; see [The floor](/metrics/#the-floor).

Example: [`examples/memory-regression`](https://github.com/nao1215/himorime/tree/main/examples/memory-regression)

## Compare similar CLIs on the same input

You want to see how different tools, or different modes of one tool, perform on exactly the same input, including how much CPU and memory each uses.

<!-- example: examples/compare-clis/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: compare similar CLIs on the same input.
# https://nao1215.github.io/himorime/cookbook/#compare-similar-clis-on-the-same-input
version: "1"

suite:
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

- The commands run interleaved in a seeded random order, so a slow moment on the machine is shared rather than hitting one tool.
- `setup` generates the input once into `${workdir}`; every command reads the same file.
- `unsupported: skip` keeps the recipe running on Windows, where `git` may start a helper process whose peak RSS Windows does not record.

Example: [`examples/compare-clis`](https://github.com/nao1215/himorime/tree/main/examples/compare-clis)

## Compare the Git base and head on every metric

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

suite:
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
      metric: median
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
      cpu:
        max_percent: 15
        min_difference: 2ms
      memory:
        max_percent: 10
        min_difference: 2MiB
```

```console
$ himorime compare --against main examples/git-compare
```

One comparison table per metric: `latency`, `throughput`, `cpu total` and `peak rss`. The tolerance column shows the direction that counts as worse: `+10%` for latency, `-10%` for throughput. The log says whether uncommitted changes were included.

- The base is checked out into a temporary Git worktree and built there. Your working tree, index and branches are never modified, and the worktree is removed on success, failure and Ctrl+C.
- Both builds are measured on this machine in the same invocation, interleaved round by round, so every metric sees the same noise. Results from different machines are never compared.
- A regression needs a change beyond the tolerance and bootstrap confidence above `confidence`; see [Regression detection](/regression-detection/). Use `--format json` to keep the raw samples of both revisions.

Example: [`examples/git-compare`](https://github.com/nao1215/himorime/tree/main/examples/git-compare)

## Compare a script-based CLI without a build step

Your CLI is a script run by an interpreter, so there is nothing to build, and you still want `main` and your working tree compared, each running its own script on the same input.

<!-- example: examples/script-compare/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: compare a script-based CLI without a build step.
# https://nao1215.github.io/himorime/cookbook/#compare-a-script-based-cli-without-a-build-step
#
#   himorime compare --against main examples/script-compare
version: "1"

suite:
  name: script compare
  description: A shell script measured as the base revision has it and as the working tree has it, with no build step.

defaults:
  warmup: 1
  runs: 15

benchmarks:
  - name: count records
    # Commands and hooks run in ${root}, this directory inside the revision
    # being measured, and relative paths are relative to it. The base
    # revision therefore runs the base's work.sh, and the head the working
    # tree's.
    #
    # ${head_root} is this directory inside the working tree for both
    # revisions: both read exactly the same records, even when the fixture
    # changed or is new in the working tree.
    stdin: "${head_root}/testdata/records.txt"
    commands:
      script:
        command: [sh, work.sh]
    regression:
      latency:
        max_percent: 20
        min_difference: 20ms
```

```console
$ himorime compare --against main examples/script-compare
```

One `count records` row. A slower `work.sh` in the working tree is a `REGRESSION`; an unchanged one passes.

- Commands and hooks run in `${root}` of each revision, and relative paths such as `work.sh` resolve there, so the base worktree runs the base's script. A command that ran from the working tree in both revisions would compare your changes with themselves.
- `${head_root}` is the suite directory in the working tree for both revisions. Use it for a fixture both must read, as `stdin` does here, or for a tool you do not want compared.
- A relative path the base revision does not have, such as a fixture added in this change, fails the base with a hint to use `${head_root}`.
- The script needs `sh`; on Windows, point `command` at an interpreter that exists there.

Example: [`examples/script-compare`](https://github.com/nao1215/himorime/tree/main/examples/script-compare)

## Gate on CPU time and memory, report latency only

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

suite:
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
      cpu:
        max_percent: 25
        # Two Windows scheduler ticks: a difference smaller than this cannot
        # be told from the accounting on the coarsest platform.
        min_difference: 32ms
      memory:
        max_percent: 25
        min_difference: 4MiB
```

```console
$ himorime compare --against main examples/gate-cpu-memory
```

Three comparison tables. The `latency` row reads `PASS (NOT GATED)`, or `REGRESSION (NOT GATED)` when the program got slower; `cpu total` and `peak rss` decide the exit status.

- `gate: false` keeps a metric measured, compared and reported, verdict included; it only stops that verdict from failing the run. No extreme `max_percent` is needed to switch a metric off.
- The summary line counts what was not gated, such as `not gated: 1 regressed`, the JSON report has `gate: false` on the comparison and `summary.not_gated`, and GitHub Actions shows a notice instead of an error.
- `--fail-on-inconclusive` applies to gated comparisons only. Budgets are always enforced; leave out a budget you do not want to fail on.
- The command is given work to do and memory to hold so that both gated metrics are measurable on every platform. Gate on a metric your program moves by more than the resolution of its measurement: see [CPU time](/metrics/#cpu-time-and-utilization) and [The floor](/metrics/#the-floor).

Example: [`examples/gate-cpu-memory`](https://github.com/nao1215/himorime/tree/main/examples/gate-cpu-memory)

## Fail a GitHub Actions job when a performance budget is violated

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

suite:
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
      wordcount:
        latency:
          p95: "<= 2s"
        throughput:
          median: ">= 10000 lines/s"
        cpu:
          total:
            median: "<= 2s"
        memory:
          peak_rss:
            max: "<= 256MiB"
    regression:
      cpu:
        max_percent: 20
        min_difference: 5ms
      memory:
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

## Save JSON, CSV and Markdown reports locally

You want results you can archive, load into a spreadsheet, paste into a pull request, or analyze yourself.

<!-- example: examples/reports/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: save JSON, CSV and Markdown reports locally.
# https://nao1215.github.io/himorime/cookbook/#save-json-csv-and-markdown-reports-locally
#
#   himorime run examples/reports --format json --output result.json
#   himorime run examples/reports --format csv --output result.csv
#   himorime run examples/reports --format samples-csv --output samples.csv
#   himorime run examples/reports --format markdown --output result.md
#   himorime run examples/reports --summary summary.md
version: "1"

suite:
  name: reports
  description: Two ways to count the same file, for report examples.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 10

benchmarks:
  - name: count 20k lines
    setup:
      - command: ["${artifact}", -gen, "20000", -o, "${workdir}/input.txt"]
    metrics:
      throughput:
        work:
          value: 20000
          unit: lines
      cpu: true
      memory: true
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner, "${workdir}/input.txt"]
      readall:
        command: ["${artifact}", -impl, readall, "${workdir}/input.txt"]
```

```console
$ himorime run examples/reports --format json --output result.json
$ himorime run examples/reports --format csv --output result.csv
$ himorime run examples/reports --format samples-csv --output samples.csv
$ himorime run examples/reports --format markdown --output result.md
```

- `result.json` has `schema_version` `"1"` and, for every command, `head.metrics` with each metric's unit, direction, status, statistics, percentiles and every raw sample. It validates against `schema/report.schema.json`.
- `result.csv` is long: one row per statistic, budget or comparison of one metric, with `record`, `metric`, `unit`, `statistic` and `value` columns and no structured data inside a cell.
- `samples.csv` has one row per measured value of every run, keyed by `run`, so the metrics of one run can be joined.
- `result.md` has one table per metric group and a budgets table, headed by the suite name.

To write several reports on every run, list them under `report.outputs` in the suite, or append a job summary with `--summary FILE`. Reports never include environment variables or host names.

Example: [`examples/reports`](https://github.com/nao1215/himorime/tree/main/examples/reports)

## Understand CPU utilization above 100%

The CPU table says `246%` and you want to know whether that is a bug.

<!-- example: examples/cpu-utilization/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: understand CPU utilization above 100%.
# https://nao1215.github.io/himorime/cookbook/#understand-cpu-utilization-above-100
version: "1"

suite:
  name: cpu utilization
  description: A single-threaded and a multi-threaded word count of the same file.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 8

benchmarks:
  - name: count 500k lines
    setup:
      - command: ["${artifact}", -gen, "500000", -o, "${workdir}/input.txt"]
    metrics:
      cpu: true
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner, "${workdir}/input.txt"]
      parallel:
        command: ["${artifact}", -impl, parallel, -workers, "4", "${workdir}/input.txt"]
    budget:
      scanner:
        cpu:
          # Utilization is CPU time divided by wall-clock time. A
          # single-threaded program stays near or below 100%.
          utilization:
            median: "<= 150%"
```

```console
$ himorime run examples/cpu-utilization
```

`scanner` shows a utilization near 100%, `parallel` well above it on a machine with several CPUs, while its latency is lower.

- Utilization is total CPU time divided by wall-clock time, times 100. One CPU busy for the whole run is 100%; four CPUs busy for the whole run is 400%. It is not a share of the machine, and it is not divided by the number of CPUs.
- A Go, Java or .NET program runs garbage collection and runtime threads next to your code, so even a single-threaded program can exceed 100% slightly.
- A value far below 100% means the program waited: for I/O, a lock, a child process or a sleep.
- Utilization has no better direction, so it can carry a budget in either direction but is never judged as a regression.

Example: [`examples/cpu-utilization`](https://github.com/nao1215/himorime/tree/main/examples/cpu-utilization)

## Understand process tree measurement on each OS

Your command starts other processes, a shell, `make`, a compiler, and you need to know what the CPU and memory numbers include.

<!-- example: examples/process-tree/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: understand process tree measurement on each OS.
# https://nao1215.github.io/himorime/cookbook/#understand-process-tree-measurement-on-each-os
version: "1"

suite:
  name: process tree
  description: CPU time of a program that does its work itself, and of one that delegates it to two child processes.

build:
  command: [go, build, -o, "${artifact}", ../tools/sleepy]

defaults:
  warmup: 1
  runs: 6

benchmarks:
  - name: spin 100ms
    metrics:
      cpu:
        # The started process and its descendants. Only process_tree exists.
        scope: process_tree
    commands:
      alone:
        command: ["${artifact}", -busy, -ms, "100"]
      two-children:
        # The parent only waits; each child spins for 100ms.
        command: ["${artifact}", -busy, -ms, "100", -children, "2"]
```

```console
$ himorime run examples/process-tree
```

`alone` shows about 100ms of CPU time, `two-children` about 200ms: the CPU time of the two children counts, although the parent itself only waits.

| | Linux, macOS, BSD | Windows |
|---|---|---|
| CPU time | the process and every descendant its parents waited for (`wait4` usage) | every process that was part of the command's Job Object |
| Peak RSS | the largest peak of the process or any descendant its parents waited for | the peak working set of the started process, reported only when it started no child processes |
| Not included | a descendant still running when the command exits, or orphaned before it exits | a child started in the microseconds before the process joined the job |

- Neither platform adds up processes that ran at the same time: peak RSS is the largest single process.
- himorime reads these values from the operating system after each run exits, without polling, so collecting them adds nothing to the measured time. The peak is the kernel's own high-water mark, so a short spike is not missed the way a sampling profiler can miss it; memory that was reserved but never touched, or swapped out, never counted as resident.
- `shell: true` adds the shell process to the tree: its start-up time, CPU time and memory count too.

Example: [`examples/process-tree`](https://github.com/nao1215/himorime/tree/main/examples/process-tree)

## Cope with noise on GitHub-hosted runners

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

suite:
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
      cpu:
        max_percent: 50
        min_difference: 5ms
```

```console
$ himorime compare --against main examples/noisy
```

`bimodal` is `INCONCLUSIVE` with the reason `measurements are noisier than max_cv`; `steady` passes. himorime exits 0. With `--fail-on-inconclusive` the same run exits 1.

- Inconclusive is not a pass: it means himorime cannot tell. It exits 0 by default so noise does not teach people to ignore the check.
- Revisions are measured interleaved, round by round, so a noisy neighbor slows both. More `runs` narrow the bootstrap interval; `warmup` absorbs one-off costs.
- `min_difference` sets the smallest change worth failing CI for, per metric; `max_percent` sets the relative tolerance.
- Prefer an absolute budget with a wide margin for hard limits, and a relative comparison for trends. CPU time and peak RSS are usually steadier than latency on a shared runner.
- A single-digit percentage change of a millisecond command is below what a shared runner can resolve. Measure a representative workload instead.

Example: [`examples/noisy`](https://github.com/nao1215/himorime/tree/main/examples/noisy)

## Handle a metric this platform cannot measure

The same suite runs on Linux and Windows, and one metric cannot be measured on one of them.

<!-- example: examples/unsupported-metrics/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: handle a metric this platform cannot measure.
# https://nao1215.github.io/himorime/cookbook/#handle-a-metric-this-platform-cannot-measure
version: "1"

suite:
  name: unsupported metrics
  description: Peak RSS of a command that starts a child process, which Windows cannot report.

build:
  command: [go, build, -o, "${artifact}", ../tools/sleepy]

defaults:
  warmup: 0
  runs: 5
  metrics:
    cpu: true
    memory: true
    # The default, fail, stops before measuring when a requested metric
    # cannot be measured here. skip measures everything else and reports the
    # metric as unsupported, with its budgets and comparisons skipped.
    unsupported: skip

benchmarks:
  - name: parent and child
    commands:
      sleepy:
        command: ["${artifact}", -ms, "20", -children, "1"]
    budget:
      sleepy:
        memory:
          peak_rss:
            max: "<= 256MiB"
```

```console
$ himorime run examples/unsupported-metrics
```

On Linux and macOS every metric is measured and the budget passes. On Windows the command starts a child process, so peak RSS is reported as `unsupported`, its budget as `SKIPPED` with the reason, CPU time is still measured, and himorime exits 0.

- Without `unsupported: skip`, the default `fail` stops before building or running anything when a platform cannot measure a requested metric at all, and exits 6 when it turns out at run time. A budget that silently disappeared would read as a pass.
- An unsupported metric is never reported as zero: its `stats` are `null`, its status says `unsupported`, and the reason says why.
- A metric the platform supports but failed to report is a `metric_collection_failed` error with exit status 6 under either policy, because it cannot be told apart from a broken measurement.

Example: [`examples/unsupported-metrics`](https://github.com/nao1215/himorime/tree/main/examples/unsupported-metrics)

## Compare parsers on a stdin fixture

Your tools read standard input, and you want every run to see the same committed fixture from its first byte.

<!-- example: examples/stdin-fixture/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: compare parsers on a stdin fixture.
# https://nao1215.github.io/himorime/cookbook/#compare-parsers-on-a-stdin-fixture
version: "1"

suite:
  name: stdin fixture
  description: Both implementations read the same access log on standard input.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 10

benchmarks:
  - name: access log on stdin
    # The fixture is reopened for every run, so each run reads it from the
    # first byte. Relative paths are relative to this file's directory, ${root}.
    stdin: testdata/access.log
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner]
      readall:
        command: ["${artifact}", -impl, readall]

  - name: inline stdin
    stdin:
      content: "a short inline input\nwith two lines\n"
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner]
      readall:
        command: ["${artifact}", -impl, readall]
```

```console
$ himorime run examples/stdin-fixture
```

Two benchmarks, each with a `scanner` and a `readall` row, and a geometric mean line across both cases.

- A relative `stdin` path is relative to the suite file. The file is reopened for every run.
- `stdin: {content: ...}` passes inline text instead of a file.

Example: [`examples/stdin-fixture`](https://github.com/nao1215/himorime/tree/main/examples/stdin-fixture)

## Compare small, medium and large inputs

Performance depends on input size, and you want each size as its own case instead of one averaged number, with fixtures generated rather than committed.

<!-- example: examples/input-sizes/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipes: separate cases per input size, fixtures generated by setup, and a
# geometric mean across cases.
# https://nao1215.github.io/himorime/cookbook/#compare-small-medium-and-large-inputs
version: "1"

suite:
  name: input sizes
  description: The same two implementations on small, medium and large inputs.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 10

benchmarks:
  - name: small
    tags: [smoke]
    setup:
      - command: ["${artifact}", -gen, "100", -o, "${workdir}/input.txt"]
    stdin: "${workdir}/input.txt"
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner]
      readall:
        command: ["${artifact}", -impl, readall]

  - name: medium
    setup:
      - command: ["${artifact}", -gen, "10000", -o, "${workdir}/input.txt"]
    stdin: "${workdir}/input.txt"
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner]
      readall:
        command: ["${artifact}", -impl, readall]

  - name: large
    setup:
      - command: ["${artifact}", -gen, "200000", -o, "${workdir}/input.txt"]
    stdin: "${workdir}/input.txt"
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner]
      readall:
        command: ["${artifact}", -impl, readall]
```

```console
$ himorime run examples/input-sizes --tag smoke
```

Without `--tag` there are three benchmarks, `small`, `medium` and `large`, each with both implementations, followed by the geometric mean of each command's ratio to the baseline across the three cases.

- `setup` runs once per benchmark, before warmup, and writes the fixture into that benchmark's own `${workdir}`.
- Read the per-case rows first. The geometric mean is a summary and hides that an implementation can win on small inputs and lose on large ones.

Example: [`examples/input-sizes`](https://github.com/nao1215/himorime/tree/main/examples/input-sizes)

## Reset state before every run

The command changes something it depends on (a file, a cache, a database), so every run has to start from the same state.

<!-- example: examples/prepare-each/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: reset state before every run with prepare_each.
# https://nao1215.github.io/himorime/cookbook/#reset-state-before-every-run
version: "1"

suite:
  name: prepare each
  description: Every run starts from a freshly generated output file.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 10

benchmarks:
  - name: regenerate then count
    # prepare_each is not measured. It runs before every warmup and every
    # measured run, in the same working directory as the command.
    prepare_each:
      - command: ["${artifact}", -gen, "5000", -o, "${workdir}/input.txt"]
    commands:
      wordcount:
        command: ["${artifact}", "${workdir}/input.txt"]
        # Keep the latest run's output for inspection instead of discarding it.
        stdout: last-output.txt
```

```console
$ himorime run examples/prepare-each
```

One `PASS` row. `prepare_each` ran 11 times (1 warmup + 10 runs) and none of that time is in the result.

- `prepare_each` runs before warmup runs too.
- `stdout: last-output.txt` keeps the latest run's output inside `${workdir}` instead of discarding it.

Example: [`examples/prepare-each`](https://github.com/nao1215/himorime/tree/main/examples/prepare-each)

## Clean up after success, failure or interruption

Your benchmark creates something that must not be left behind, whether the benchmark passes, a command fails, or you press Ctrl+C.

<!-- example: examples/cleanup/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: always clean up, even when a run fails or is interrupted.
# https://nao1215.github.io/himorime/cookbook/#clean-up-after-success-failure-or-interruption
version: "1"

suite:
  name: cleanup
  description: setup creates a cache, cleanup purges it however the benchmark ends.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 5

benchmarks:
  - name: with cleanup
    setup:
      - command: ["${artifact}", -gen, "1000", -o, "${workdir}/input.txt"]
      - command: ["${artifact}", -cache, "${workdir}/cache", "${workdir}/input.txt"]
    # cleanup runs after the benchmark whether it passed, failed or was
    # interrupted with Ctrl+C. ${workdir} itself is removed afterwards.
    cleanup:
      - command: ["${artifact}", -cache, "${workdir}/cache", -purge]
    commands:
      wordcount:
        command: ["${artifact}", -cache, "${workdir}/cache", "${workdir}/input.txt"]
```

```console
$ himorime run examples/cleanup
```

One `PASS` row. `cleanup` also runs when a command fails, when `setup` fails, and after an interrupt, and `${workdir}` is removed afterwards.

- A failing `cleanup` fails the benchmark with exit status 4.
- After Ctrl+C, cleanup gets up to 30 seconds before himorime gives up on it.

Example: [`examples/cleanup`](https://github.com/nao1215/himorime/tree/main/examples/cleanup)

## Benchmark a warm cache

Your program has a cache, and you want to measure both the warm path and a cold start in one suite.

<!-- example: examples/cache/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipes: benchmark with a warm cache, and reproduce a cold cache with
# prepare_each.
# https://nao1215.github.io/himorime/cookbook/#benchmark-a-warm-cache
version: "1"

suite:
  name: cache
  description: The same count with a primed cache and with a cache wiped before every run.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 10

benchmarks:
  - name: warm cache
    setup:
      - command: ["${artifact}", -gen, "100000", -o, "${workdir}/input.txt"]
      # Prime the cache once; every measured run then hits it.
      - command: ["${artifact}", -cache, "${workdir}/cache", "${workdir}/input.txt"]
    commands:
      wordcount:
        command: ["${artifact}", -cache, "${workdir}/cache", "${workdir}/input.txt"]

  - name: cold cache
    setup:
      - command: ["${artifact}", -gen, "100000", -o, "${workdir}/input.txt"]
    # prepare_each runs before every warmup and measured run, outside the
    # measured time. Removing the cache makes every run a cold one.
    prepare_each:
      - command: ["${artifact}", -cache, "${workdir}/cache", -purge]
    commands:
      wordcount:
        command: ["${artifact}", -cache, "${workdir}/cache", "${workdir}/input.txt"]
```

```console
$ himorime run examples/cache
```

`warm cache` is much faster than `cold cache`: `setup` primes the cache once for the warm case, while `prepare_each` purges it before every run of the cold case.

- A cold start reproduced this way is "cold" for the program's own cache only; the operating system's file cache is still warm.
- The warmup run of `warm cache` is also a cache hit, because `setup` ran before it.

Example: [`examples/cache`](https://github.com/nao1215/himorime/tree/main/examples/cache)

## Run only smoke benchmarks

The full suite is slow, and a pull request or a pre-commit check should run only a quick subset.

<!-- example: examples/tags/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: run only the smoke benchmarks with tags and filters.
# https://nao1215.github.io/himorime/cookbook/#run-only-smoke-benchmarks
#
#   himorime run examples/tags --tag smoke
#   himorime run examples/tags --skip-tag slow
#   himorime run examples/tags --filter '^startup'
version: "1"

suite:
  name: tags
  description: A quick smoke benchmark next to a slower one.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 5

benchmarks:
  - name: startup
    tags: [smoke]
    commands:
      wordcount:
        command: ["${artifact}", -version]

  - name: large input
    tags: [slow]
    setup:
      - command: ["${artifact}", -gen, "500000", -o, "${workdir}/input.txt"]
    commands:
      wordcount:
        command: ["${artifact}", "${workdir}/input.txt"]
```

```console
$ himorime run examples/tags --tag smoke
```

Only the `startup` benchmark runs. `--skip-tag slow` gives the same here, and `--filter` selects by a regular expression on the name.

- A selection that matches nothing exits 3, so a renamed tag cannot silently skip every benchmark.
- `himorime list --tag smoke` shows what would run.

Example: [`examples/tags`](https://github.com/nao1215/himorime/tree/main/examples/tags)

## Stop a command that hangs

A command can hang, and a benchmark must not block CI forever or leave processes behind.

<!-- example: examples/timeout/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: stop a command that hangs. This example fails on purpose (exit 4).
# https://nao1215.github.io/himorime/cookbook/#stop-a-command-that-hangs
version: "1"

suite:
  name: timeout
  description: A command that would take 30 seconds is stopped after one.

build:
  command: [go, build, -o, "${artifact}", ../tools/sleepy]

defaults:
  warmup: 0
  runs: 3

benchmarks:
  - name: hangs
    commands:
      sleepy:
        command: ["${artifact}", -ms, "30000"]
        # The run and every process it started are stopped when the limit
        # passes: a process group on Unix, a Job Object on Windows.
        timeout: 1s
```

```console
$ himorime run examples/timeout
```

`ERROR` with `timed out after 1s and was stopped`, and exit status 4. This example fails on purpose.

- The whole process tree is stopped: a process group on Unix, a Job Object on Windows.
- A timeout is an execution error, not a slow sample: the command did not complete.

Example: [`examples/timeout`](https://github.com/nao1215/himorime/tree/main/examples/timeout)

## Treat a non-zero exit as an error

A benchmark whose command fails is measuring an error path; that must be reported, not averaged in.

<!-- example: examples/failing-command/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: treat a failing command as an error, or allow expected exit codes.
# The first benchmark fails on purpose (exit 4).
# https://nao1215.github.io/himorime/cookbook/#treat-a-non-zero-exit-as-an-error
version: "1"

suite:
  name: failing command
  description: A command that exits 3, and one whose exit 1 is expected.

build:
  command: [go, build, -o, "${artifact}", ../tools/sleepy]

defaults:
  warmup: 0
  runs: 3

benchmarks:
  - name: unexpected failure
    commands:
      sleepy:
        command: ["${artifact}", -ms, "1", -exit, "3"]

  - name: expected exit code
    commands:
      sleepy:
        command: ["${artifact}", -ms, "1", -exit, "1"]
        # Like grep without a match: exit 1 is part of normal operation.
        exit_codes: [0, 1]
```

```console
$ himorime run examples/failing-command
```

`unexpected failure` shows `ERROR` with `exited with status 3` and the last lines of its standard error; `expected exit code` passes because `exit_codes: [0, 1]` allows exit 1. himorime exits 4.

- Only successful runs become samples, and a failed command is never left out of the table.
- Values of secret-looking environment variables are masked in the reported standard error.

Example: [`examples/failing-command`](https://github.com/nao1215/himorime/tree/main/examples/failing-command)

## Use a shell pipeline

You need a pipe or a redirection, which an argument list cannot express.

<!-- example: examples/shell/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipes: portable commands without a shell, and an explicit shell pipeline.
# https://nao1215.github.io/himorime/cookbook/#use-a-shell-pipeline
version: "1"

suite:
  name: shell
  description: The same count through argv and through a shell pipeline.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 5

benchmarks:
  - name: pipeline
    setup:
      - command: ["${artifact}", -gen, "10000", -o, "${workdir}/input.txt"]
    baseline: argv
    commands:
      argv:
        command: ["${artifact}", "${workdir}/input.txt"]
      pipeline:
        # shell: true runs the string through /bin/sh -c, or cmd.exe on
        # Windows. Variables are quoted for that shell before substitution.
        # The measured time includes starting the shell.
        command: "${artifact} < ${workdir}/input.txt | ${artifact}"
        shell: true
```

```console
$ himorime run examples/shell
```

Two rows: `argv` and `pipeline`. The pipeline is slower partly because its time includes starting the shell and a second process.

- `shell: true` uses `/bin/sh -c`, or `cmd.exe` on Windows; the pipeline in this example is valid in both.
- Substituted variables are quoted for the shell. himorime never subtracts shell start-up time.

Example: [`examples/shell`](https://github.com/nao1215/himorime/tree/main/examples/shell)

## Understand the geometric mean

You want one overall number across cases, and need to know when himorime refuses to give one.

<!-- example: examples/missing-case/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: why no overall score is printed when a case is missing.
# https://nao1215.github.io/himorime/cookbook/#understand-the-geometric-mean
version: "1"

suite:
  name: missing case
  description: The readall implementation has no measurement for the large case.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 5

benchmarks:
  - name: small
    setup:
      - command: ["${artifact}", -gen, "100", -o, "${workdir}/input.txt"]
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner, "${workdir}/input.txt"]
      readall:
        command: ["${artifact}", -impl, readall, "${workdir}/input.txt"]

  - name: large
    setup:
      - command: ["${artifact}", -gen, "100000", -o, "${workdir}/input.txt"]
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner, "${workdir}/input.txt"]
```

```console
$ himorime run examples/missing-case
```

Per-case rows, then `no geometric mean: command "readall" is missing from benchmark "large"`. With every command in every case, as in [Compare small, medium and large inputs](#compare-small-medium-and-large-inputs), the geometric mean is printed.

- The geometric mean is computed only when every command completed every case; a failure or timeout suppresses it rather than being left out.
- A suite whose benchmarks share no command name, such as a regression suite with a one-command start-up benchmark, is not a comparison: it gets neither a geometric mean nor a note, whatever order its benchmarks are in.
- himorime never averages ratios arithmetically.

Example: [`examples/missing-case`](https://github.com/nao1215/himorime/tree/main/examples/missing-case)
