---
title: Reference
description: Commands, flags, environment variables and exit codes.
toc: true
aliases: ["/commands/", "/exit-codes/"]
---

Flags may appear before or after paths: `himorime run bench.yaml --format json` and `himorime run --format json bench.yaml` are the same. `--` ends flag parsing. Errors go to standard error; reports go to standard output or to the file named by `--output`.

`--filter`, `--tag` and `--skip-tag` are shared by `list`, `run`, `compare` and `ci`. `--tag` keeps benchmarks with any of the given tags; `--skip-tag` drops benchmarks with any of them and wins over `--tag`; `--filter` is a regular expression on the benchmark name, matched anywhere in it: `--filter 'run 10'` also selects `run 1000`, and `--filter '^run 10$'` selects that one benchmark. A selection that matches nothing exits 3.

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

Builds the suite's artifact when a build section exists, then measures every selected benchmark: latency, and the throughput, CPU time and peak RSS the suite asks for. Commands of one benchmark run interleaved in a seeded random order. Exits 1 when a budget is exceeded, 4 when a command fails, and 6 when a requested metric cannot be measured or a required budget cannot be assessed.

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

Checks REF out into a temporary Git worktree, builds both REF and the current working tree (uncommitted changes included), and measures them interleaved on this machine. Commands run in ${root} of each revision, so each runs its own code; ${head_root} names the working tree's copy for shared fixtures. Every measured metric is compared in the direction that is worse for it; a metric with `regression.<metric>.gate: false` is reported but never fails. The working tree, index and branches are never modified. Exits 1 on a gated regression or an exceeded budget, and 6 when a requested metric cannot be measured or a required budget cannot be assessed.

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

Like compare, with CI defaults: no colors, and a Markdown summary appended to $GITHUB_STEP_SUMMARY when it is set. The base revision is --against, else $HIMORIME_BASE_REF, else the base commit of the GitHub Actions pull_request, merge_group or push event. pull_request_target is refused. In GitHub Actions, every missed budget, regression and failure is also printed as an annotation. Exits 6 when a requested metric cannot be measured or a required budget cannot be assessed.

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

## Exit codes

<!-- BEGIN GENERATED: exit-codes -->
| Code | Name | Meaning |
|---|---|---|
| `0` | ok | Every selected benchmark completed without a failing required check. Explicitly skipped unsupported metrics are allowed; inconclusive comparisons exit 0 unless --fail-on-inconclusive is given. |
| `1` | failed | A performance budget was exceeded, a regression was confirmed on any metric, or --fail-on-inconclusive was given and a comparison was inconclusive. The measurement itself succeeded. |
| `2` | config | A suite file is not valid YAML, does not match the schema, or fails semantic validation. Nothing was executed. |
| `3` | usage | The command line is invalid: an unknown flag or command, a missing argument, or no benchmark matched the selection. |
| `4` | execution | A measured command, hook or build failed or timed out, a Git operation failed, the base revision could not be resolved, or the run was interrupted. |
| `5` | internal | himorime hit an unexpected internal error. Please report it. |
| `6` | metric | A requested metric (throughput, cpu or memory) could not be measured, or a required budget could not be assessed. Depending on the cause, the commands may not have run. |
<!-- END GENERATED: exit-codes -->

Execution failures take precedence over metric failures, which take precedence over performance failures. An inconclusive comparison exits 0 unless `--fail-on-inconclusive` is set. JSON reports record the chosen status in `summary.exit_code`.

## Diagnostic codes

Tool errors printed to standard error carry an `HMR` code identifying the error category and cause. Use the exit status for scripts and the diagnostic code when reporting a problem. Measurement-result lines for success and performance-result exit `1`, including `--fail-on-inconclusive`, have no diagnostic prefix; an auxiliary operation such as writing an annotation can still report a separate error.

<!-- BEGIN GENERATED: error-codes -->
| Code | Name | Meaning |
|---|---|---|
| `HMR2001` | invalid input | A suite or saved report is missing, malformed, or invalid. |
| `HMR3001` | invalid command line | The command name, flag, or command-line argument is invalid. |
| `HMR4001` | command, hook, or build failed | A measured command, hook, or build failed or timed out. |
| `HMR4002` | Git operation failed | A Git repository, revision, or worktree operation failed. |
| `HMR4003` | output failed | CLI output, a generated suite, a report, or a job summary could not be written. |
| `HMR4004` | interrupted | The run was interrupted and cleanup was performed. |
| `HMR5001` | internal error | himorime encountered an unexpected internal error. |
| `HMR6001` | metric unavailable | A requested metric is unsupported or could not be collected. |
<!-- END GENERATED: error-codes -->
