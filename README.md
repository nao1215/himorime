# yahiko

[![UnitTest](https://github.com/nao1215/yahiko/actions/workflows/unit_test.yml/badge.svg)](https://github.com/nao1215/yahiko/actions/workflows/unit_test.yml)
[![E2E](https://github.com/nao1215/yahiko/actions/workflows/e2e.yml/badge.svg)](https://github.com/nao1215/yahiko/actions/workflows/e2e.yml)
[![Lint](https://github.com/nao1215/yahiko/actions/workflows/lint.yml/badge.svg)](https://github.com/nao1215/yahiko/actions/workflows/lint.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/nao1215/yahiko.svg)](https://pkg.go.dev/github.com/nao1215/yahiko)

yahiko runs the command-line benchmarks you keep in your repository, locally
and in CI, and fails the build when a change makes them measurably slower.

## Try it in 30 seconds

```console
$ go install github.com/nao1215/yahiko@latest
$ yahiko init && yahiko run
```

`yahiko init` writes a `yahiko.yaml` that measures `git --version`.

```text
suite: my benchmarks (yahiko.yaml)
BENCHMARK    COMMAND    MEDIAN      MEAN   STDDEV  RELATIVE  RESULT
git startup  git      521.34µs  528.99µs  45.55µs     1.00x  PASS
```

## How it differs from hyperfine

[hyperfine](https://github.com/sharkdp/hyperfine) is the tool to reach for when
you want to measure a few commands in your terminal right now. yahiko is for
the benchmarks that should guard every pull request: the commands, inputs,
hooks, budgets and tolerances live in a reviewed YAML file, `compare` builds a
Git base revision next to your working tree and measures both interleaved, and
the exit status tells CI whether a regression is confirmed, inconclusive or
absent. See the [comparison](https://nao1215.github.io/yahiko/comparison/) for
Bencher, CodSpeed and github-action-benchmark as well.

## Install

```console
$ go install github.com/nao1215/yahiko@latest
```

Prebuilt archives and `.deb`, `.rpm` and `.apk` packages for Linux, macOS and
Windows are on [GitHub Releases](https://github.com/nao1215/yahiko/releases),
signed and with SBOMs; see [Installation](https://nao1215.github.io/yahiko/install/).

## A minimal suite

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/nao1215/yahiko/main/schema/yahiko.schema.json
version: "1"

suite:
  name: mytool benchmarks

build:
  command: [go, build, -o, "${artifact}", ./cmd/mytool]

benchmarks:
  - name: parse large input
    stdin: testdata/large.json
    commands:
      mytool:
        command: ["${artifact}", parse]
    budget:
      mytool:
        median: "< 50ms"
```

## Run it

```console
$ yahiko run
```

yahiko builds `${artifact}`, runs each command until it has enough samples, and
exits 1 when a budget is exceeded. Reports can be a table, JSON with raw
samples, CSV or Markdown.

## Compare with main

```console
$ yahiko compare --against main
```

`main` is checked out into a temporary Git worktree and built next to your
working tree, uncommitted changes included. Both are measured interleaved on
the same machine; yahiko exits 1 only when a slowdown beyond the tolerance is
confirmed by a bootstrap confidence test, and never touches your branch or
working tree.

## GitHub Actions

<!-- example: examples/github-actions/benchmark.yml -->
```yaml
# Copy this file to .github/workflows/benchmark.yml.
# Recipe: fail a GitHub Actions job when a performance budget is violated.
# https://nao1215.github.io/yahiko/cookbook/#fail-a-github-actions-job-when-a-performance-budget-is-violated
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
      # the checked-out head on every metric the suite measures, writes a
      # summary to the job page and annotations to the pull request, and
      # exits 1 on a missed budget or a confirmed regression. Exit 4 or 6
      # means the measurement itself failed, not the performance.
      - run: yahiko ci
```

## Supported platforms

Linux, macOS and Windows, on amd64 and arm64. CI runs the unit and end-to-end
tests on all three.

## Documentation

https://nao1215.github.io/yahiko/ — getting started, the configuration
reference, commands, reports, regression detection, a cookbook of runnable
examples, exit codes and troubleshooting.

## The name

`yahiko` is named after Yahiko Shrine in Niigata, whose deity is revered for
bringing vitality and restoring troubled lands. Local legend also tells of the
deity subduing thunder. The name reflects the tool's purpose: measure command
performance, detect regressions, and keep software healthy.

## License

[MIT](./LICENSE)
