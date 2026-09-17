---
title: Getting started
description: Install yahiko, write a first suite with yahiko init, run it, and compare your working tree with main.
---

## 1. Install

```console
$ go install github.com/nao1215/yahiko@latest
```

Other ways are on the [Installation](/install/) page.

## 2. Write a suite

```console
$ yahiko init
wrote yahiko.yaml
next: yahiko validate yahiko.yaml && yahiko run yahiko.yaml
```

`yahiko init` writes a small suite that measures `git --version`. The first
line points editors at the JSON Schema, so completion and validation work
while you type. Replace the command with the program you care about:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/nao1215/yahiko/main/schema/yahiko.schema.json
version: "1"

suite:
  name: my benchmarks

benchmarks:
  - name: help output
    commands:
      mytool:
        command: [mytool, --help]
```

A command is a list of arguments. It runs without a shell, so it behaves the
same on Linux, macOS and Windows. Use a string with `shell: true` only when
you need pipes or redirection.

## 3. Check it without running anything

```console
$ yahiko validate
yahiko.yaml: ok (1 benchmark, 1 command)
```

Mistakes are all reported at once, each with its file, line, column, field
and, where there is one, a fix. Nothing runs, and the exit status is 2. A typo
in `command` looks like this:

```text
yahiko.yaml:10:7: benchmarks[0].commands.mytool.command: command is required
yahiko.yaml:11:9: benchmarks[0].commands.mytool.comand: unknown key "comand"
    hint: check the spelling against https://nao1215.github.io/yahiko/configuration/; unknown keys are rejected so a typo cannot silently change a benchmark
yahiko: the suite is invalid; nothing was run
```

## 4. Run it

```console
$ yahiko run
suite: my benchmarks (yahiko.yaml)
BENCHMARK    COMMAND   MEDIAN    MEAN    STDDEV  RELATIVE  RESULT
help output  mytool    2.10ms  2.14ms  110.30µs     1.00x  PASS
```

Without `runs`, yahiko measures adaptively: at least 10 runs and 2 seconds
per command, at most 100 runs. Progress goes to standard error and the report
to standard output, so `yahiko run --format json > result.json` stays clean.

## 5. Compare with main

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
$ yahiko compare --against main
```

yahiko checks `main` out into a temporary Git worktree, builds both `main` and
your working tree (uncommitted changes included), measures them interleaved
on this machine, and exits 1 only when a slowdown beyond the tolerance is
statistically confirmed. Your working tree, index and branches are not
touched, and the worktree is removed even when you press Ctrl+C.

## 6. Run it on pull requests

See [GitHub Actions](/github-actions/) for a read-only workflow that runs
`yahiko ci` on every pull request.
