[![UnitTest](https://github.com/nao1215/himorime/actions/workflows/unit_test.yml/badge.svg)](https://github.com/nao1215/himorime/actions/workflows/unit_test.yml) [![E2E](https://github.com/nao1215/himorime/actions/workflows/e2e.yml/badge.svg)](https://github.com/nao1215/himorime/actions/workflows/e2e.yml) [![tested with atago](https://img.shields.io/badge/tested%20with-atago-7c3aed?logo=data:image/svg%2Bxml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCI%2BPHBhdGggZmlsbD0iI2ZmZiIgZD0iTTMuNiA0LjIgMTEuOSAxMmwtOC4zIDcuOC0xLjktMi4yTDcuOSAxMiAxLjcgNi40eiIvPjxyZWN0IGZpbGw9IiNmZmYiIHg9IjEyLjYiIHk9IjE3LjIiIHdpZHRoPSI5LjciIGhlaWdodD0iMi44IiByeD0iMS40Ii8%2BPC9zdmc%2B&logoColor=white)](https://github.com/nao1215/atago) [![Lint](https://github.com/nao1215/himorime/actions/workflows/lint.yml/badge.svg)](https://github.com/nao1215/himorime/actions/workflows/lint.yml) [![Go Reference](https://pkg.go.dev/badge/github.com/nao1215/himorime.svg)](https://pkg.go.dev/github.com/nao1215/himorime)

[![measured with himorime](https://img.shields.io/badge/measured%20with-himorime-d9480f)](https://github.com/nao1215/himorime)

<p align="center">
  <img src="./doc/images/himorime-logo.jpeg" alt="himorime logo" width="600" />
</p>

himorime measures how fast and how lean a command-line program is: latency, throughput, CPU time and peak memory, from a YAML file kept in your repository. It runs your actual binary, in any language, checks the numbers against budgets, and compares a change with its Git base revision, so a pull request that makes the program slower fails CI. The same file and command work on your laptop and in GitHub Actions.

himorime is the sister project of [atago](https://github.com/nao1215/atago): atago tests what a CLI does, himorime tests how it performs.

Documentation: https://nao1215.github.io/himorime/

## First run

If you have Go and Git, paste this. `init` writes a suite that measures `git --version`, and `run` measures it:

```shell
go run github.com/nao1215/himorime@latest init
go run github.com/nao1215/himorime@latest run
```

```text
suite: my benchmarks (himorime.yaml)
latency
BENCHMARK    COMMAND    MEDIAN      MEAN   STDDEV  RELATIVE  RESULT
git startup  git      529.50µs  548.45µs  75.40µs     1.00x  PASS

budgets
BENCHMARK    COMMAND  METRIC           BUDGET  MEASURED  RESULT
git startup  git      latency median  < 1.00s  529.50µs  PASS

1 passed · 1 benchmark · seed 477717617585165 · exit 0
```

Then point `himorime.yaml` at your own tool. This suite builds it, measures one input on every metric, and sets a budget for each:

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

`himorime run` exits 1 when a budget is missed. `himorime compare --against main` builds `main` in a temporary worktree and your working tree side by side, measures both in interleaved rounds, and exits 1 on a regression it is confident about. Reports are also available as JSON with every sample, CSV, Markdown and a GitHub Actions job summary, and a Markdown report can replace a marked section of a documentation page.

## GitHub Actions

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

Exit status 1 means performance got worse; 4 and 6 mean the measurement itself failed. Shared runners are noisy: himorime interleaves the revisions, needs statistical confidence to call a regression, and reports anything it cannot tell apart from noise as inconclusive.

## Measuring programs you did not write

himorime measures a command, so a third-party tool is measured like your own. The suites in [bench/thirdparty](./bench/thirdparty) do that with real programs, and they are written to be read and copied: [real-world suites](https://nao1215.github.io/himorime/real-world/) says what each one shows and what was made equal between the programs.

| Suite | Programs |
|---|---|
| [json](./bench/thirdparty/json) | jq, gojq, jaq |
| [compression](./bench/thirdparty/compression) | gzip, bzip2, xz |

## Install

```shell
go install github.com/nao1215/himorime@latest
```

On macOS, Homebrew works too:

```shell
brew install --cask nao1215/tap/himorime
```

The [release page](https://github.com/nao1215/himorime/releases) has prebuilt archives for Linux, macOS and Windows (amd64 and arm64) and `.deb`, `.rpm` and `.apk` packages for Linux. In GitHub Actions, [setup-himorime](https://github.com/nao1215/setup-himorime) installs a release. Building from source needs Go 1.26 or later.

Runs on Linux, macOS and Windows. What each metric covers on each OS is on the [metrics](https://nao1215.github.io/himorime/metrics/) page.

## The name

himorime was inspired by atago, whose name is associated with protection from fire. It is named after 火防女 (himorime), the maiden who tends the fire: in Demon's Souls, 黒衣の火防女, the Maiden in Black in English, manages the player's stats through leveling, which fits a tool that measures performance stats and detects regressions.

## License

[MIT](./LICENSE)
