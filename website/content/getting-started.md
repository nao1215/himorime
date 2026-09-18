---
title: Getting started
description: Install himorime, write a first suite with himorime init, add budgets for latency, CPU and memory, run it, and compare your working tree with main.
---

## 1. Install

```console
$ go install github.com/nao1215/himorime@latest
```

Other ways are on the [Installation](/install/) page.

## 2. Write a suite

```console
$ himorime init
wrote himorime.yaml
next: himorime validate himorime.yaml && himorime run himorime.yaml
```

`himorime init` writes a small suite that measures `git --version`. The first line points editors at the JSON Schema, so completion and validation work while you type. Replace the command with the program you care about:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/nao1215/himorime/main/schema/himorime.schema.json
version: "1"

suite:
  name: my benchmarks

benchmarks:
  - name: help output
    commands:
      mytool:
        command: [mytool, --help]
```

A command is a list of arguments. It runs without a shell, so it behaves the same on Linux, macOS and Windows. Use a string with `shell: true` only when you need pipes or redirection.

## 3. Check it without running anything

```console
$ himorime validate
himorime.yaml: ok (1 benchmark, 1 command)
```

Mistakes are all reported at once, each with its file, line, column, field and, where there is one, a fix. Nothing runs, and the exit status is 2. A typo in `command` looks like this:

```text
himorime.yaml:10:7: benchmarks[0].commands.mytool.command: command is required
himorime.yaml:11:9: benchmarks[0].commands.mytool.comand: unknown key "comand"
    hint: check the spelling against https://nao1215.github.io/himorime/configuration/; unknown keys are rejected so a typo cannot silently change a benchmark
himorime: the suite is invalid; nothing was run
```

## 4. Run it

```console
$ himorime run
suite: my benchmarks (himorime.yaml)
BENCHMARK    COMMAND  MEDIAN    MEAN    STDDEV  RELATIVE  RESULT
help output  mytool   2.10ms  2.14ms  110.30µs     1.00x  PASS
RELATIVE is the median divided by the baseline command's median, or by the fastest command's when no baseline is set.

1 passed · 1 benchmark · seed 5398001677524849 · exit 0
```

Without `runs`, himorime measures adaptively: at least 10 runs and 2 seconds per command, at most 100 runs. Progress goes to standard error and the report to standard output, so `himorime run --format json > result.json` stays clean.

## 5. Add budgets

A budget is a limit the command must stay within on every run. Latency is always measured; switch on the other metrics you care about:

```yaml
version: "1"

suite:
  name: my benchmarks

benchmarks:
  - name: help output
    metrics:
      cpu: true
      memory: true
    commands:
      mytool:
        command: [mytool, --help]
    budget:
      mytool:
        latency: {p95: "<= 50ms"}
        cpu: {total: {median: "<= 30ms"}}
        memory: {peak_rss: {max: "<= 32MiB"}}
```

```console
$ himorime run
```

The report now has a latency, a CPU, a memory and a budgets table. A missed budget says `FAIL`, and himorime exits 1. To declare throughput, tell himorime how much work one run does; see [Metrics](/metrics/#throughput).

## 6. Compare with main

To compare revisions, let the suite build the program and measure the build:

```yaml
build:
  command: [go, build, -o, "${artifact}", ./cmd/mytool]

benchmarks:
  - name: help output
    commands:
      mytool:
        command: ["${artifact}", --help]
```

```console
$ himorime compare --against main
```

himorime checks `main` out into a temporary Git worktree, builds both `main` and your working tree (uncommitted changes included), measures them interleaved on this machine, and exits 1 only when a degradation beyond the tolerance is statistically confirmed, on latency or on any other metric the suite measures. Your working tree, index and branches are not touched, and the worktree is removed even when you press Ctrl+C.

## 7. Run it on pull requests

See [GitHub Actions](/github-actions/) for a read-only workflow that runs `himorime ci` on every pull request.
