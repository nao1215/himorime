---
title: Cookbook
description: Runnable recipes for yahiko, found by what you want to do. Every recipe is an example suite in the repository, and CI runs each one.
toc: true
filter: true
---

Every recipe here is a suite under [`examples/`](https://github.com/nao1215/yahiko/tree/main/examples) that you can run from a clone of the repository. The end-to-end suite runs each recipe (`test/e2e/atago/cookbook.atago.yaml`, one scenario per heading) on Linux, macOS and Windows, and a test fails if a recipe and its scenario drift apart.

The examples measure two small programs in `examples/tools`: `wordcount`, a word counter with two implementations and a cache, and `sleepy`, a stand-in whose speed and exit status you control.

## Find a recipe

| I want to | Recipes |
|---|---|
| measure one program | [Measure the startup time of one CLI](#measure-the-startup-time-of-one-cli) |
| compare programs or implementations | [Compare several CLIs on the same input](#compare-several-clis-on-the-same-input), [Compare parsers on a stdin fixture](#compare-parsers-on-a-stdin-fixture), [Compare small, medium and large inputs](#compare-small-medium-and-large-inputs), [Understand the geometric mean](#understand-the-geometric-mean) |
| control state between runs | [Reset state before every run](#reset-state-before-every-run), [Clean up after success, failure or interruption](#clean-up-after-success-failure-or-interruption), [Benchmark a warm cache](#benchmark-a-warm-cache) |
| guard CI | [Fail CI on an absolute budget](#fail-ci-on-an-absolute-budget), [Compare main with your working tree](#compare-main-with-your-working-tree), [Fail on a clear regression](#fail-on-a-clear-regression), [Compare a pull request with its base in GitHub Actions](#compare-a-pull-request-with-its-base-in-github-actions), [Handle noisy measurements](#handle-noisy-measurements) |
| produce reports | [Write a Markdown table](#write-a-markdown-table), [Keep raw samples in JSON](#keep-raw-samples-in-json), [Export CSV](#export-csv), [Write a GitHub Actions job summary](#write-a-github-actions-job-summary) |
| choose what runs | [Run only smoke benchmarks](#run-only-smoke-benchmarks) |
| handle failures | [Stop a command that hangs](#stop-a-command-that-hangs), [Treat a non-zero exit as an error](#treat-a-non-zero-exit-as-an-error) |
| use shell features | [Use a shell pipeline](#use-a-shell-pipeline) |

## Measure the startup time of one CLI

You want to know how long a command takes to start and finish, and keep that measurement in the repository so anyone can repeat it.

<!-- example: examples/startup/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipe: measure the startup time of one CLI.
# https://nao1215.github.io/yahiko/cookbook/#measure-the-startup-time-of-one-cli
version: "1"

suite:
  name: startup
  description: How long git takes to start and print its version.

defaults:
  warmup: 2
  runs: 10

benchmarks:
  - name: git version
    tags: [smoke]
    commands:
      git:
        # A list of arguments runs without a shell, the same way on Linux,
        # macOS and Windows.
        command: [git, --version]
```

```console
$ yahiko run examples/startup
```

A table with one row: the median, mean and standard deviation of 10 runs after 2 warmup runs, and `PASS`.

- The command is a list of arguments, so the same file runs on Linux, macOS and Windows without a shell.
- A command this short mostly measures process creation. Use it to catch start-up regressions, not to compare algorithms.

Example: [`examples/startup`](https://github.com/nao1215/yahiko/tree/main/examples/startup)

## Compare several CLIs on the same input

You want to see how different tools, or different modes of one tool, perform on exactly the same input.

<!-- example: examples/compare-clis/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipe: compare several CLIs on the same input.
# https://nao1215.github.io/yahiko/cookbook/#compare-several-clis-on-the-same-input
version: "1"

suite:
  name: compare CLIs
  description: Two word counters and git hashing the same file.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 10

benchmarks:
  - name: count a 20k-line file
    setup:
      - command: ["${artifact}", -gen, "20000", -o, "${workdir}/input.txt"]
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
$ yahiko run examples/compare-clis
```

One row per command. `RELATIVE` is each median divided by the `baseline` command's median, so `scanner` shows `1.00x` and the others show how many times slower or faster they are.

- The commands run interleaved in a seeded random order, so a slow moment on the machine is shared rather than hitting one tool.
- `setup` generates the input once into `${workdir}`; every command reads the same file.

Example: [`examples/compare-clis`](https://github.com/nao1215/yahiko/tree/main/examples/compare-clis)

## Compare parsers on a stdin fixture

Your tools read standard input, and you want every run to see the same committed fixture from its first byte.

<!-- example: examples/stdin-fixture/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipe: compare parsers on a stdin fixture.
# https://nao1215.github.io/yahiko/cookbook/#compare-parsers-on-a-stdin-fixture
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
    # first byte. Relative paths are relative to this file.
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
$ yahiko run examples/stdin-fixture
```

Two benchmarks, each with a `scanner` and a `readall` row, and a geometric mean line across both cases.

- A relative `stdin` path is relative to the suite file. The file is reopened for every run.
- `stdin: {content: ...}` passes inline text instead of a file.

Example: [`examples/stdin-fixture`](https://github.com/nao1215/yahiko/tree/main/examples/stdin-fixture)

## Compare small, medium and large inputs

Performance depends on input size, and you want each size as its own case instead of one averaged number, with fixtures generated rather than committed.

<!-- example: examples/input-sizes/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipes: separate cases per input size, fixtures generated by setup, and a
# geometric mean across cases.
# https://nao1215.github.io/yahiko/cookbook/#compare-small-medium-and-large-inputs
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
$ yahiko run examples/input-sizes --tag smoke
```

Without `--tag` there are three benchmarks, `small`, `medium` and `large`, each with both implementations, followed by the geometric mean of each command's ratio to the baseline across the three cases.

- `setup` runs once per benchmark, before warmup, and writes the fixture into that benchmark's own `${workdir}`.
- Read the per-case rows first. The geometric mean is a summary and hides that an implementation can win on small inputs and lose on large ones.

Example: [`examples/input-sizes`](https://github.com/nao1215/yahiko/tree/main/examples/input-sizes)

## Reset state before every run

The command changes something it depends on (a file, a cache, a database), so every run has to start from the same state.

<!-- example: examples/prepare-each/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipe: reset state before every run with prepare_each.
# https://nao1215.github.io/yahiko/cookbook/#reset-state-before-every-run
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
$ yahiko run examples/prepare-each
```

One `PASS` row. `prepare_each` ran 11 times (1 warmup + 10 runs) and none of that time is in the result.

- `prepare_each` runs before warmup runs too.
- `stdout: last-output.txt` keeps the latest run's output inside `${workdir}` instead of discarding it.

Example: [`examples/prepare-each`](https://github.com/nao1215/yahiko/tree/main/examples/prepare-each)

## Clean up after success, failure or interruption

Your benchmark creates something that must not be left behind, whether the benchmark passes, a command fails, or you press Ctrl+C.

<!-- example: examples/cleanup/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipe: always clean up, even when a run fails or is interrupted.
# https://nao1215.github.io/yahiko/cookbook/#clean-up-after-success-failure-or-interruption
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
$ yahiko run examples/cleanup
```

One `PASS` row. `cleanup` also runs when a command fails, when `setup` fails, and after an interrupt, and `${workdir}` is removed afterwards.

- A failing `cleanup` fails the benchmark with exit status 4.
- After Ctrl+C, cleanup gets up to 30 seconds before yahiko gives up on it.

Example: [`examples/cleanup`](https://github.com/nao1215/yahiko/tree/main/examples/cleanup)

## Benchmark a warm cache

Your program has a cache, and you want to measure both the warm path and a cold start in one suite.

<!-- example: examples/cache/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipes: benchmark with a warm cache, and reproduce a cold cache with
# prepare_each.
# https://nao1215.github.io/yahiko/cookbook/#benchmark-a-warm-cache
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
$ yahiko run examples/cache
```

`warm cache` is much faster than `cold cache`: `setup` primes the cache once for the warm case, while `prepare_each` purges it before every run of the cold case.

- A cold start reproduced this way is "cold" for the program's own cache only; the operating system's file cache is still warm.
- The warmup run of `warm cache` is also a cache hit, because `setup` ran before it.

Example: [`examples/cache`](https://github.com/nao1215/yahiko/tree/main/examples/cache)

## Fail CI on an absolute budget

You have a limit that must hold on every run, independent of the base branch: a start-up time, a response time.

<!-- example: examples/budget/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipe: fail CI when an absolute median budget is exceeded.
# https://nao1215.github.io/yahiko/cookbook/#fail-ci-on-an-absolute-budget
version: "1"

suite:
  name: budget
  description: A contract on how long git may take to start.

defaults:
  warmup: 2
  runs: 10

benchmarks:
  - name: git version
    commands:
      git:
        command: [git, --version]
    # A budget is a contract you chose. When the median is not below the
    # limit, yahiko exits 1 regardless of noise.
    budget:
      git:
        median: "< 2s"
        max: "<= 5s"
```

```console
$ yahiko run examples/budget
```

`PASS` while the median is below 2s and the slowest run at most 5s. When a budget is missed the row says `OVER BUDGET`, a line names the budget and the measured value, and yahiko exits 1.

- Budgets are contracts, not statistics: noise does not excuse a miss. Keep CI budgets generous enough for shared runners.
- Budgets can use `mean`, `median`, `min` and `max`, with `<` or `<=`.

Example: [`examples/budget`](https://github.com/nao1215/yahiko/tree/main/examples/budget)

## Compare main with your working tree

Before pushing, you want to know whether your changes, committed or not, made the program slower than `main`.

<!-- example: examples/git-compare/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipes: compare a Git revision with the working tree, locally or in CI.
# https://nao1215.github.io/yahiko/cookbook/#compare-main-with-your-working-tree
#
#   yahiko compare --against main examples/git-compare
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
    metric: median
    max_percent: 10
    confidence: 0.95

benchmarks:
  - name: count 50k lines
    setup:
      - command: ["${artifact}", -gen, "50000", -o, "${workdir}/input.txt"]
    stdin: "${workdir}/input.txt"
    commands:
      wordcount:
        command: ["${artifact}"]
```

```console
$ yahiko compare --against main examples/git-compare
```

A `BASE`/`HEAD` table with the change, the confidence and `PASS`. The log says whether uncommitted changes were included.

- The base is checked out into a temporary Git worktree and built there. Your working tree, index and branches are never modified, and the worktree is removed on success, failure and Ctrl+C.
- Both builds run on this machine in the same invocation, interleaved; results from different machines are never compared.

Example: [`examples/git-compare`](https://github.com/nao1215/yahiko/tree/main/examples/git-compare)

## Fail on a clear regression

A change made the program clearly slower, and CI must fail instead of letting it merge.

<!-- example: examples/git-compare/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipes: compare a Git revision with the working tree, locally or in CI.
# https://nao1215.github.io/yahiko/cookbook/#compare-main-with-your-working-tree
#
#   yahiko compare --against main examples/git-compare
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
    metric: median
    max_percent: 10
    confidence: 0.95

benchmarks:
  - name: count 50k lines
    setup:
      - command: ["${artifact}", -gen, "50000", -o, "${workdir}/input.txt"]
    stdin: "${workdir}/input.txt"
    commands:
      wordcount:
        command: ["${artifact}"]
```

```console
$ yahiko compare --against main examples/git-compare
```

The row says `REGRESSION` with a large positive `CHANGE` and a confidence above 95%, and yahiko exits 1. The end-to-end suite proves this by shrinking the word counter's read buffer in a scratch repository.

- A regression needs both a change beyond `max_percent` and bootstrap confidence above `confidence`; see [Regression detection](/regression-detection/).
- Use `--format json` to keep the raw samples of both revisions as evidence.

Example: [`examples/git-compare`](https://github.com/nao1215/yahiko/tree/main/examples/git-compare)

## Compare a pull request with its base in GitHub Actions

Every pull request should be compared with its base branch in CI, safely for pull requests from forks.

<!-- example: examples/github-actions/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipe: the suite benchmark.yml runs in GitHub Actions.
# https://nao1215.github.io/yahiko/cookbook/#compare-main-with-your-working-tree
#
#   YAHIKO_BASE_REF=main yahiko ci examples/github-actions
version: "1"

suite:
  name: github actions
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
    metric: median
    max_percent: 10
    confidence: 0.95

benchmarks:
  - name: count 50k lines
    setup:
      - command: ["${artifact}", -gen, "50000", -o, "${workdir}/input.txt"]
    stdin: "${workdir}/input.txt"
    commands:
      wordcount:
        command: ["${artifact}"]
```

The workflow:

<!-- example: examples/github-actions/benchmark.yml -->
```yaml
# Copy this file to .github/workflows/benchmark.yml.
# Recipe: compare a pull request with its base in GitHub Actions.
# https://nao1215.github.io/yahiko/cookbook/#compare-a-pull-request-with-its-base-in-github-actions
name: Benchmark

on:
  pull_request:

# Read-only: yahiko needs no secret and no write token, so pull requests from
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
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version: stable
      - run: go install github.com/nao1215/yahiko@latest
      # Finds the pull request's base commit from the event, compares it with
      # the checked-out head, writes a summary to the job page, and exits 1 on
      # a confirmed regression.
      - run: yahiko ci
```

```console
$ yahiko ci examples/github-actions
```

In the job log, the comparison table; on the job page, a summary headed by a one-line verdict. The job fails on a confirmed regression. Copy `examples/github-actions/benchmark.yml` into `.github/workflows/`.

- The workflow is `pull_request` with `contents: read` and no secrets. yahiko refuses `pull_request_target`.
- Check out with `fetch-depth: 0` so the base commit exists locally.

Example: [`examples/github-actions`](https://github.com/nao1215/yahiko/tree/main/examples/github-actions)

## Handle noisy measurements

Your measurements are too noisy to call, and you want to decide whether that fails CI.

<!-- example: examples/noisy/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipes: see an inconclusive comparison, and decide whether it fails CI.
# https://nao1215.github.io/yahiko/cookbook/#handle-noisy-measurements
#
#   yahiko compare --against main examples/noisy
#   yahiko compare --against main --fail-on-inconclusive examples/noisy
version: "1"

suite:
  name: noisy
  description: A stand-in program whose run time alternates between 10ms and 60ms.

build:
  command: [go, build, -o, "${artifact}", ../tools/sleepy]

defaults:
  warmup: 0
  runs: 12

benchmarks:
  - name: bimodal
    commands:
      sleepy:
        command: ["${artifact}", -ms, "10", -jitter-ms, "50", -state, "${workdir}/calls"]
    regression:
      max_percent: 10
      confidence: 0.95
      # The alternating run time gives a coefficient of variation near 0.7.
      # Above max_cv a comparison is inconclusive instead of pass or fail.
      max_cv: 0.3
```

```console
$ yahiko compare --against main examples/noisy
```

`INCONCLUSIVE`, with the reason `measurements are noisier than max_cv`, and exit status 0. With `--fail-on-inconclusive` the same run exits 1.

- Inconclusive is not a pass: it means yahiko cannot tell. Add runs, measure a heavier workload, or use a quieter machine.
- `sleepy` is a stand-in program whose run time alternates between 10ms and 60ms.

Example: [`examples/noisy`](https://github.com/nao1215/yahiko/tree/main/examples/noisy)

## Write a Markdown table

You want benchmark results you can paste into a README, a pull request, a blog post or release notes.

<!-- example: examples/reports/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipes: Markdown, JSON with raw samples, CSV and a GitHub job summary.
# https://nao1215.github.io/yahiko/cookbook/#write-a-markdown-table
#
#   yahiko run examples/reports --format markdown
#   yahiko run examples/reports --format json --output result.json
#   yahiko run examples/reports --format csv --output result.csv
#   yahiko run examples/reports --summary summary.md
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
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner, "${workdir}/input.txt"]
      readall:
        command: ["${artifact}", -impl, readall, "${workdir}/input.txt"]
```

```console
$ yahiko run examples/reports --format markdown
```

A GitHub-flavored Markdown table with median, mean, standard deviation, min, max, run count, both ratios and the result, followed by a line describing the machine.

- Names are escaped, so a `|` cannot break the table.
- `--output FILE` writes the report to a file instead of standard output.

Example: [`examples/reports`](https://github.com/nao1215/yahiko/tree/main/examples/reports)

## Keep raw samples in JSON

You want every measured duration, unrounded, for your own analysis or as evidence attached to a CI run.

<!-- example: examples/reports/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipes: Markdown, JSON with raw samples, CSV and a GitHub job summary.
# https://nao1215.github.io/yahiko/cookbook/#write-a-markdown-table
#
#   yahiko run examples/reports --format markdown
#   yahiko run examples/reports --format json --output result.json
#   yahiko run examples/reports --format csv --output result.csv
#   yahiko run examples/reports --summary summary.md
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
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner, "${workdir}/input.txt"]
      readall:
        command: ["${artifact}", -impl, readall, "${workdir}/input.txt"]
```

```console
$ yahiko run examples/reports --format json --output result.json
```

`result.json` has `schema_version` `"1"`, integer nanoseconds everywhere, every run in `samples_ns`, the environment, the seed and the exit status.

- The schema is published as `schema/report.schema.json`.
- The report never includes environment variables or host names.

Example: [`examples/reports`](https://github.com/nao1215/yahiko/tree/main/examples/reports)

## Export CSV

You want the results in a spreadsheet or a database.

<!-- example: examples/reports/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipes: Markdown, JSON with raw samples, CSV and a GitHub job summary.
# https://nao1215.github.io/yahiko/cookbook/#write-a-markdown-table
#
#   yahiko run examples/reports --format markdown
#   yahiko run examples/reports --format json --output result.json
#   yahiko run examples/reports --format csv --output result.csv
#   yahiko run examples/reports --summary summary.md
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
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner, "${workdir}/input.txt"]
      readall:
        command: ["${artifact}", -impl, readall, "${workdir}/input.txt"]
```

```console
$ yahiko run examples/reports --format csv --output result.csv
```

A header line and one row per command (per revision in a comparison), with integer nanosecond columns.

- The column list is fixed; new columns are only added at the end.

Example: [`examples/reports`](https://github.com/nao1215/yahiko/tree/main/examples/reports)

## Write a GitHub Actions job summary

You want the results on the job page, even when running `yahiko run` rather than `yahiko ci`.

<!-- example: examples/reports/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipes: Markdown, JSON with raw samples, CSV and a GitHub job summary.
# https://nao1215.github.io/yahiko/cookbook/#write-a-markdown-table
#
#   yahiko run examples/reports --format markdown
#   yahiko run examples/reports --format json --output result.json
#   yahiko run examples/reports --format csv --output result.csv
#   yahiko run examples/reports --summary summary.md
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
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner, "${workdir}/input.txt"]
      readall:
        command: ["${artifact}", -impl, readall, "${workdir}/input.txt"]
```

```console
$ yahiko run examples/reports --summary summary.md
```

`summary.md` gets a verdict line and the Markdown tables appended. In a workflow, pass `--summary "$GITHUB_STEP_SUMMARY"`.

- `yahiko ci` appends to `$GITHUB_STEP_SUMMARY` without the flag.

Example: [`examples/reports`](https://github.com/nao1215/yahiko/tree/main/examples/reports)

## Run only smoke benchmarks

The full suite is slow, and a pull request or a pre-commit check should run only a quick subset.

<!-- example: examples/tags/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipe: run only the smoke benchmarks with tags and filters.
# https://nao1215.github.io/yahiko/cookbook/#run-only-smoke-benchmarks
#
#   yahiko run examples/tags --tag smoke
#   yahiko run examples/tags --skip-tag slow
#   yahiko run examples/tags --filter '^startup'
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
$ yahiko run examples/tags --tag smoke
```

Only the `startup` benchmark runs. `--skip-tag slow` gives the same here, and `--filter` selects by a regular expression on the name.

- A selection that matches nothing exits 3, so a renamed tag cannot silently skip every benchmark.
- `yahiko list --tag smoke` shows what would run.

Example: [`examples/tags`](https://github.com/nao1215/yahiko/tree/main/examples/tags)

## Stop a command that hangs

A command can hang, and a benchmark must not block CI forever or leave processes behind.

<!-- example: examples/timeout/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipe: stop a command that hangs. This example fails on purpose (exit 4).
# https://nao1215.github.io/yahiko/cookbook/#stop-a-command-that-hangs
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
$ yahiko run examples/timeout
```

`ERROR` with `timed out after 1s and was stopped`, and exit status 4. This example fails on purpose.

- The whole process tree is stopped: a process group on Unix, a Job Object on Windows.
- A timeout is an execution error, not a slow sample: the command did not complete.

Example: [`examples/timeout`](https://github.com/nao1215/yahiko/tree/main/examples/timeout)

## Treat a non-zero exit as an error

A benchmark whose command fails is measuring an error path; that must be reported, not averaged in.

<!-- example: examples/failing-command/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipe: treat a failing command as an error, or allow expected exit codes.
# The first benchmark fails on purpose (exit 4).
# https://nao1215.github.io/yahiko/cookbook/#treat-a-non-zero-exit-as-an-error
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
$ yahiko run examples/failing-command
```

`unexpected failure` shows `ERROR` with `exited with status 3` and the last lines of its standard error; `expected exit code` passes because `exit_codes: [0, 1]` allows exit 1. yahiko exits 4.

- Only successful runs become samples, and a failed command is never left out of the table.
- Values of secret-looking environment variables are masked in the reported standard error.

Example: [`examples/failing-command`](https://github.com/nao1215/yahiko/tree/main/examples/failing-command)

## Use a shell pipeline

You need a pipe or a redirection, which an argument list cannot express.

<!-- example: examples/shell/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipes: portable commands without a shell, and an explicit shell pipeline.
# https://nao1215.github.io/yahiko/cookbook/#use-a-shell-pipeline
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
$ yahiko run examples/shell
```

Two rows: `argv` and `pipeline`. The pipeline is slower partly because its time includes starting the shell and a second process.

- `shell: true` uses `/bin/sh -c`, or `cmd.exe` on Windows; the pipeline in this example is valid in both.
- Substituted variables are quoted for the shell. yahiko never subtracts shell start-up time.

Example: [`examples/shell`](https://github.com/nao1215/yahiko/tree/main/examples/shell)

## Understand the geometric mean

You want one overall number across cases, and need to know when yahiko refuses to give one.

<!-- example: examples/missing-case/yahiko.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/yahiko.schema.json
#
# Recipe: why no overall score is printed when a case is missing.
# https://nao1215.github.io/yahiko/cookbook/#understand-the-geometric-mean
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
$ yahiko run examples/missing-case
```

Per-case rows, then `no geometric mean: command "readall" is missing from benchmark "large"`. With every command in every case, as in [Compare small, medium and large inputs](#compare-small-medium-and-large-inputs), the geometric mean is printed.

- The geometric mean is computed only when every command completed every case; a failure or timeout suppresses it rather than being left out.
- yahiko never averages ratios arithmetically.

Example: [`examples/missing-case`](https://github.com/nao1215/yahiko/tree/main/examples/missing-case)
