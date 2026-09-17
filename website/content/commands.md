---
title: Commands
description: Every himorime command and flag, generated from the command table in the source.
toc: true
---

Flags may appear before or after paths: `himorime run bench.yaml --format json`
and `himorime run --format json bench.yaml` are the same. `--` ends flag
parsing. Errors go to standard error; reports go to standard output or to the
file named by `--output`.

`--filter`, `--tag` and `--skip-tag` are shared by `list`, `run`, `compare` and
`ci`. `--tag` keeps benchmarks with any of the given tags; `--skip-tag` drops
benchmarks with any of them and wins over `--tag`; `--filter` is a regular
expression on the benchmark name. A selection that matches nothing exits 3.

<!-- BEGIN GENERATED: commands -->
### init

```text
himorime init [flags] [PATH]
```

Writes a small suite that measures `git --version`, with a JSON Schema comment so editors can complete and validate it. PATH defaults to himorime.yaml. An existing file is never overwritten unless --force is given.

| Flag | Description |
|---|---|
| `--force` | overwrite an existing file |

### validate

```text
himorime validate [PATH...]
```

Checks YAML syntax, the schema (unknown keys, types, durations, byte sizes, rates, percentages, paths) and semantic rules (baselines, budgets and regression settings that refer to real commands and measured metrics, budget directions, throughput units, variables that exist). No command is executed. Each PATH is a suite file or a directory holding himorime.yaml or *.himorime.yaml files; the default is himorime.yaml.

### list

```text
himorime list [flags] [PATH...]
```

Prints one line per command of every selected benchmark: suite file, benchmark, tags, stdin fixture, command name and the command as written. Nothing is executed, and variables are shown unexpanded. --format json prints the same data for tools.

| Flag | Description |
|---|---|
| `--filter REGEXP` | select benchmarks whose name matches this regexp |
| `--format STRING` | output format: text or json (default text) |
| `--skip-tag TAG` | skip benchmarks with this tag (repeatable, or comma-separated); wins over --tag |
| `--tag TAG` | select benchmarks with this tag (repeatable, or comma-separated) |

### run

```text
himorime run [flags] [PATH...]
```

Builds the suite's artifact when a build section exists, then measures every selected benchmark: latency, and the throughput, CPU time and peak RSS the suite asks for. Commands of one benchmark run interleaved in a seeded random order. Exits 1 when a budget is exceeded, 4 when a command fails, and 6 when a requested metric cannot be measured.

| Flag | Description |
|---|---|
| `--filter REGEXP` | select benchmarks whose name matches this regexp |
| `--format FORMAT` | report format written to stdout or --output: table, json, csv, markdown, github, samples-csv (default table) |
| `--no-color` | disable colors in the table (also honored: NO_COLOR) |
| `--output FILE` | write the report to this file instead of stdout |
| `--quiet` | do not print progress to stderr |
| `--runs N` | measure exactly n runs per command, overriding the suite |
| `--section NAME` | update only the name section of the existing --output file, between its himorime:begin and himorime:end comment lines; needs --format markdown |
| `--seed SEED` | seed for the execution order and the bootstrap (default: random, printed in the report) |
| `--skip-tag TAG` | skip benchmarks with this tag (repeatable, or comma-separated); wins over --tag |
| `--summary FILE` | also append a GitHub-flavored Markdown summary to this file |
| `--tag TAG` | select benchmarks with this tag (repeatable, or comma-separated) |
| `--warmup N` | run n warmup runs per command, overriding the suite |

### compare

```text
himorime compare --against REF [flags] [PATH...]
```

Checks REF out into a temporary Git worktree, builds both REF and the current working tree (uncommitted changes included), and measures them interleaved on this machine. Commands run in ${root} of each revision, so each runs its own code; ${head_root} names the working tree's copy for shared fixtures. Every measured metric is compared in the direction that is worse for it; a metric with regression.<metric>.gate: false is reported but never fails. The working tree, index and branches are never modified. Exits 1 on a gated regression or an exceeded budget.

| Flag | Description |
|---|---|
| `--against REVISION` | the Git revision to compare the working tree with (required) |
| `--fail-on-inconclusive` | exit 1 when a gated comparison is inconclusive |
| `--filter REGEXP` | select benchmarks whose name matches this regexp |
| `--format FORMAT` | report format written to stdout or --output: table, json, csv, markdown, github, samples-csv (default table) |
| `--no-color` | disable colors in the table (also honored: NO_COLOR) |
| `--output FILE` | write the report to this file instead of stdout |
| `--quiet` | do not print progress to stderr |
| `--runs N` | measure exactly n runs per command, overriding the suite |
| `--section NAME` | update only the name section of the existing --output file, between its himorime:begin and himorime:end comment lines; needs --format markdown |
| `--seed SEED` | seed for the execution order and the bootstrap (default: random, printed in the report) |
| `--skip-tag TAG` | skip benchmarks with this tag (repeatable, or comma-separated); wins over --tag |
| `--summary FILE` | also append a GitHub-flavored Markdown summary to this file |
| `--tag TAG` | select benchmarks with this tag (repeatable, or comma-separated) |
| `--warmup N` | run n warmup runs per command, overriding the suite |

### ci

```text
himorime ci [flags] [PATH...]
```

Like compare, with CI defaults: no colors, and a Markdown summary appended to $GITHUB_STEP_SUMMARY when it is set. The base revision is --against, else $HIMORIME_BASE_REF, else the base commit of the GitHub Actions pull_request, merge_group or push event. pull_request_target is refused. In GitHub Actions, every missed budget, regression and failure is also printed as an annotation.

| Flag | Description |
|---|---|
| `--against REVISION` | the base revision; defaults to $HIMORIME_BASE_REF, then the GitHub Actions event |
| `--fail-on-inconclusive` | exit 1 when a gated comparison is inconclusive |
| `--filter REGEXP` | select benchmarks whose name matches this regexp |
| `--format FORMAT` | report format written to stdout or --output: table, json, csv, markdown, github, samples-csv (default table) |
| `--no-color` | disable colors in the table (also honored: NO_COLOR) |
| `--output FILE` | write the report to this file instead of stdout |
| `--quiet` | do not print progress to stderr |
| `--runs N` | measure exactly n runs per command, overriding the suite |
| `--section NAME` | update only the name section of the existing --output file, between its himorime:begin and himorime:end comment lines; needs --format markdown |
| `--seed SEED` | seed for the execution order and the bootstrap (default: random, printed in the report) |
| `--skip-tag TAG` | skip benchmarks with this tag (repeatable, or comma-separated); wins over --tag |
| `--summary FILE` | also append a GitHub-flavored Markdown summary to this file |
| `--tag TAG` | select benchmarks with this tag (repeatable, or comma-separated) |
| `--warmup N` | run n warmup runs per command, overriding the suite |

### version

```text
himorime version
```

Prints the version, the Go version and the platform.

### completion

```text
himorime completion <bash|zsh|fish|powershell>
```

Prints a completion script for the named shell to standard output.

### help

```text
himorime help [COMMAND]
```

Shows the command list, or the usage and flags of one command.

<!-- END GENERATED: commands -->

## Environment variables

| Variable | Used by | Effect |
|---|---|---|
| `HIMORIME_BASE_REF` | `ci` | Base revision when `--against` is not given. |
| `GITHUB_ACTIONS`, `GITHUB_EVENT_NAME`, `GITHUB_EVENT_PATH` | `ci` | Find the base commit of a pull request, merge queue or push event. |
| `GITHUB_STEP_SUMMARY` | `ci` | File the Markdown job summary is appended to. |
| `NO_COLOR` | `run`, `compare` | Disables colors in the table, as does `--no-color`. |
