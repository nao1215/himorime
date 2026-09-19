---
title: Configuration
description: The himorime.yaml suite format, version 1. Every key, its default, variables, paths, and how validation works.
toc: true
---

A suite is a YAML file, `himorime.yaml` by default. `run`, `compare`, `ci`, `list` and `validate` accept suite files or directories containing `himorime.yaml` and `*.himorime.yaml` files.

## Editor support

Put this comment on the first line for completion and validation in editors that support the YAML language server:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/nao1215/himorime/main/schema/himorime.schema.json
```

himorime embeds the same schema and validates it before running a suite.

For a runnable first suite, use [Getting started](/getting-started/). The [Cookbook](/cookbook/) has examples for budgets, regression checks, fixtures and CI.

## Commands

Use a list to run a program directly: `command: [jq, -c, ., input.json]`. Use a string only with `shell: true` when shell syntax is required. Variables in shell strings are quoted by himorime and must not be quoted again. On Windows, substituted values cannot contain quotes, percent signs, line breaks or trailing backslashes. Shell startup is part of the measured time.

## Runs

`runs` fixes the sample count. Without it, himorime runs until `min_runs` and `min_time` are satisfied, up to `max_runs`; comparisons also require `regression.min_samples`. Commands and revisions are interleaved in a seeded order. `--runs` and `--warmup` override the suite for quick checks.

## Metrics, budgets and tolerances

Latency is always measured. `metrics` enables throughput, CPU time and peak RSS; [Metrics](/metrics/) defines their scope and platform support.

```yaml
metrics:
  latency: true            # always on; allowed for readability
  throughput:
    work: {value: 1000, unit: records}   # or {file_size: path}
  cpu: true                # or {scope: process_tree}
  memory: true             # or {scope: process_tree}
  unsupported: fail        # or skip
```

`cpu`, `memory` and `unsupported` may be defaults or benchmark overrides. Throughput belongs to one benchmark because it needs that benchmark's work value or input file.

A budget is an absolute limit for one command, metric and aggregation. Available aggregations are `min`, `max`, `mean`, `median` and percentiles from `p1` to `p99.9`.

```yaml
budget:
  mytool:
    median: "< 20ms"                    # shorthand for latency.median
    latency: {p95: "<= 30ms"}
    throughput: {median: ">= 50MiB/s"}
    cpu: {total: {median: "<= 25ms"}}
    memory: {peak_rss: {max: "<= 64MiB"}}
```

Latency, CPU time and peak RSS use upper bounds; throughput uses lower bounds. A budget for an unmeasured metric is invalid.

`regression` configures latency, total CPU time and peak RSS comparisons. Throughput comparisons are derived from latency; use a throughput budget for an absolute rate limit.

```yaml
regression:
  confidence: 0.95
  latency: {gate: false}                  # compared and reported, never fails
  cpu: {max_percent: 15, min_difference: 5ms}
  memory: {max_percent: 5, min_difference: 1MiB}
```

`metric` selects `median` or `mean`, `max_percent` sets the relative tolerance, `min_difference` ignores smaller absolute changes, and `gate: false` reports a verdict without changing the exit status. See [Regression detection](/regression-detection/) for the decision rules.

## Hooks

- `setup` runs once per benchmark and revision before warmup.
- `prepare_each` runs outside the measured time before every warmup and measured run.
- `cleanup` runs after the benchmark, including after failure or interruption.

Background processes are stopped when their command or hook exits, so hooks cannot host a resident service. A failed hook fails the benchmark or prepared command; its standard-error tail is included in the report.

## Reports

`report.outputs` writes reports after every run. Paths are relative to the suite file. A Markdown output with `section` updates matching `himorime:begin` and `himorime:end` markers instead of replacing the file. `report.versions` records the first non-empty output line from each version command.

```yaml
report:
  outputs:
    - format: json
      path: bench/result.json
    - format: markdown
      path: docs/compare.md
      section: speed
  versions:
    jc: [jc, --version]
    jo: [jo, -v]
```

Version commands run once before the build, without a shell, with `LC_ALL=C` and a 30-second timeout. A failed command fails the suite. See [Reports](/reports/) for formats and section updates.

## Standard input and output

`stdin` is a fixture path (`stdin: testdata/input.txt`), `stdin: {file: ...}`, or inline text (`stdin: {content: "..."}`). The file is reopened for every run, so every run reads it from the first byte.

A command's standard output and standard error are discarded by default and never mixed with himorime's own report. Set `stdout` or `stderr` to a path inside `${workdir}` to keep the latest run's output. When a run fails, the last lines of its standard error are shown in the report either way, with secrets masked.

Set `terminal: true` for an interactive shell, line editor or TUI. On Linux and macOS, himorime runs the command on an 80 by 24 pseudo-terminal and types `stdin` one key at a time:

```yaml
- name: shell completion
  terminal: true
  stdin: {content: "SELECT * FROM us\t\n.exit\n"}
  commands:
    sqly:
      command: ["${artifact}", "${workdir}/users.csv"]
```

The input must end the program, for example with `.exit`, `q` or Ctrl-D (`\u0004`); otherwise `timeout` stops it. Key waits count toward latency. Terminal input and output are outside the measured process tree, and `stderr` cannot be captured separately.

## Variables

Commands, `cwd`, `env` values, `stdin` and hooks may use these variables:

<!-- BEGIN GENERATED: variables -->
| Variable | Expands to |
|---|---|
| `${artifact}` | The file the build step writes. In a comparison each revision has its own. Only available when the suite has a build section. Its name is artifact (artifact.exe on Windows): a program that acts on the name it was started as, such as a multi-call binary, needs a link under its own name, made in setup. |
| `${root}` | The directory holding the suite file, inside the tree being measured: the working tree, or the temporary worktree of the base revision. Relative paths are relative to it, so each revision runs its own scripts and reads its own files. |
| `${head_root}` | The directory holding the suite file inside the working tree, the same for every revision. Use it for a fixture or tool both revisions must share. Equal to ${root} in a plain run. |
| `${workdir}` | A fresh, empty directory created for each benchmark (and each revision in a comparison) and removed after cleanup. Not available in build. |
| `${exe}` | `.exe` on Windows, empty elsewhere. |
| `${env:NAME}` | The environment variable NAME. A variable that is not set is an execution error. |
| `$${` | A literal `${`. |
<!-- END GENERATED: variables -->

An unknown variable is a validation error. `${artifact}` without a build and `${workdir}` inside the build are validation errors too.

## Paths

Every relative path of a suite is relative to `${root}`, the directory holding the suite file inside the tree being measured. In a plain run that is the directory of the suite file. In a comparison it is that directory in the base revision's temporary worktree for the base, and in your working tree for the head, so each revision runs its own code:

| Setting | Relative to | Default |
|---|---|---|
| `cwd` of commands, `setup`, `prepare_each`, `cleanup` | `${root}` of the revision measured | the benchmark's `cwd`, else `${root}` |
| `cwd` of `build` | `${root}` of the revision built | `${root}` |
| `stdin` file | `${root}` of the revision measured | |
| `metrics.throughput.work.file_size` | `${root}` of the revision measured | |
| a relative program or argument, such as `[sh, work.sh]` | the command's working directory | |
| `stdout`, `stderr` | `${workdir}` | discarded |
| `report.outputs[].path` | the suite file in the working tree | |
| a relative program of `report.versions` | the directory of the suite file in the working tree | |

`${head_root}` names the working tree for both revisions. Use it when both revisions must read the same fixture:

```yaml
benchmarks:
  - name: parse the large log
    stdin: "${head_root}/testdata/large.log"   # the same input for base and head
    commands:
      parser:
        command: [sh, parse.sh]                # each revision's own parse.sh
```

Absolute paths and `~` are rejected. Relative paths and resolved symbolic links must stay inside the repository, the base worktree or `${workdir}`. Output and report paths cannot climb above their base directory. A suite that does not exist in the base revision is measured only in the working tree; see [Regression detection](/regression-detection/).

## Environment

Commands and hooks inherit himorime's environment. `env` from `defaults`, the benchmark and the command or hook is applied in that order. Reports omit the environment and show command lines before `${env:NAME}` substitution.

## Defaults

<!-- BEGIN GENERATED: defaults -->
| Setting | Default |
|---|---|
| `warmup` | `1` |
| `runs` | `unset (adaptive)` |
| `min_runs` | `10` |
| `max_runs` | `100` |
| `min_time` | `2s` |
| `timeout (commands)` | `1m` |
| `timeout (setup, prepare_each, cleanup)` | `5m` |
| `timeout (build)` | `10m` |
| `stdout, stderr` | `discard` |
| `exit_codes` | `[0]` |
| `regression.confidence` | `0.95` |
| `regression.min_samples` | `10` |
| `regression.max_cv` | `0.5` |
| `regression.latency, cpu, memory: metric` | `median` |
| `regression.latency, cpu, memory: max_percent` | `10` |
| `regression.latency, cpu, memory: min_difference` | `unset (none)` |
| `regression.latency, cpu, memory: gate` | `true` |
| `metrics.cpu, metrics.memory` | `false` |
| `metrics.unsupported` | `fail` |
| `metrics.throughput.work.unit` | `operations (value), bytes (file_size)` |
<!-- END GENERATED: defaults -->

A benchmark inherits `defaults`; a command inherits its benchmark. The most specific value wins.

## Values and units

Every value is typed; himorime never compares a formatted string.

| Kind | Written as | Used by |
|---|---|---|
| Duration | a number with a unit: `ns`, `us` (or `µs`), `ms`, `s`, `m`, `h`, such as `500ms`, `1.5s` or `1m30s` | timeouts, latency and CPU time budgets, `min_difference` of latency and CPU |
| Byte size | a number with `B`, `KB`, `MB`, `GB`, `TB` (powers of 1000) or `KiB`, `MiB`, `GiB`, `TiB` (powers of 1024), such as `64MiB` | peak RSS budgets, `memory.min_difference` |
| Rate | a number, the work unit and `/s`, such as `50MiB/s` or `"1000 records/s"` | throughput budgets, `throughput.min_difference` |
| Percentage | a number (`10`) or a string with a percent sign (`"10%"`) | `max_percent`, CPU utilization budgets |

A bare number is rejected where a unit is expected, because `10` could mean ten of anything. Units are case-sensitive: `64mib` is an error. Durations may not exceed `24h`; a budget's limit and a work value must be greater than zero.

A budget is an operator followed by a value, such as `"< 20ms"`, `"<= 64MiB"` or `">= 50MiB/s"`. Quote it: YAML reads a leading `<` fine, but a leading `>` starts a folded block.

## Validation

`himorime validate` checks the following without running commands:

1. YAML syntax and duplicate keys.
2. The schema: unknown keys, types, required keys, durations, budgets, percentages, names and path shapes. Every problem is listed at once.
3. Semantic rules, including unique names, valid references, run bounds, variable availability, metric directions and throughput units.

`himorime compare` and `himorime ci` additionally refuse a benchmark whose `runs`, `max_runs` or `--runs` is below `regression.min_samples`, because such a comparison could never be conclusive; adaptive runs of a comparison continue until `min_samples`.

## Field reference

<!-- BEGIN GENERATED: config-reference -->
### Top level

| Key | Required | Description |
|---|---|---|
| `version` | yes | Suite format version. Only "1" exists. |
| `suite` | yes | Names the suite. |
| `defaults` |  | Settings every benchmark inherits. |
| `build` |  | Builds ${artifact} before measuring; in a comparison it runs once in each revision, with ${root} pointing into that revision. |
| `benchmarks` | yes | The benchmark cases. |
| `report` |  | Report files written after every run, relative to the suite file. |

### suite

| Key | Required | Description |
|---|---|---|
| `name` | yes | Suite name shown in reports. |
| `description` |  | What the suite is for. |

### defaults

| Key | Required | Description |
|---|---|---|
| `warmup` |  | Unmeasured runs of every command before measuring starts. Default 1. |
| `runs` |  | Measure exactly this many runs per command. When absent, runs are adaptive: at least min_runs and min_time, at most max_runs. |
| `min_runs` |  | Adaptive runs: the fewest runs per command. Default 10. |
| `max_runs` |  | Adaptive runs: the most runs per command. Default 100. |
| `min_time` |  | Adaptive runs: keep measuring until each command has run for at least this long in total. Default 2s. |
| `timeout` |  | Per-run time limit. A run that exceeds it is stopped with its whole process tree and fails the benchmark. Default 1m. |
| `cwd` |  | Working directory: relative to ${root} (the suite directory in the revision being measured), or starting with ${root}, ${head_root} or ${workdir}. Default: ${root}, so each revision runs its own files. |
| `env` |  | Environment variables added to the inherited environment. Values may use variables. |
| `stdout` |  | "discard" (default), or a path inside ${workdir} that receives the standard output of the latest run. |
| `stderr` |  | "discard" (default), or a path inside ${workdir} that receives the standard error of the latest run. A failing run's stderr tail is reported either way. |
| `exit_codes` |  | Exit statuses that count as success. Default [0]. |
| `metrics` |  | Metrics every benchmark measures besides latency. Throughput is declared per benchmark. |
| `regression` |  | How a base revision and the working tree are compared (himorime compare / himorime ci). |

### build, setup, prepare_each, cleanup

| Key | Required | Description |
|---|---|---|
| `command` | yes | The command: a list of arguments (no shell), or a string when shell is true. |
| `shell` |  | Run the command string through /bin/sh -c (cmd.exe /d /s /c on Windows). Variables substituted into the string are quoted for that shell. |
| `cwd` |  | Working directory: relative to ${root} of the revision being built or measured (the default), or starting with ${root}, ${head_root}, or ${workdir} in hooks. |
| `env` |  | Environment variables added to the inherited environment. Values may use variables. |
| `timeout` |  | Time limit for this process. Default 10m for build, 5m for hooks. |

### benchmarks[]

| Key | Required | Description |
|---|---|---|
| `name` | yes | Unique benchmark name shown in reports. |
| `description` |  | What the benchmark measures. |
| `tags` |  | Labels for --tag and --skip-tag. |
| `warmup` |  | Unmeasured runs of every command before measuring starts. Default 1. |
| `runs` |  | Measure exactly this many runs per command. When absent, runs are adaptive: at least min_runs and min_time, at most max_runs. |
| `min_runs` |  | Adaptive runs: the fewest runs per command. Default 10. |
| `max_runs` |  | Adaptive runs: the most runs per command. Default 100. |
| `min_time` |  | Adaptive runs: keep measuring until each command has run for at least this long in total. Default 2s. |
| `timeout` |  | Per-run time limit. A run that exceeds it is stopped with its whole process tree and fails the benchmark. Default 1m. |
| `cwd` |  | Working directory: relative to ${root} (the suite directory in the revision being measured), or starting with ${root}, ${head_root} or ${workdir}. Default: ${root}, so each revision runs its own files. |
| `env` |  | Environment variables added to the inherited environment. Values may use variables. |
| `stdout` |  | "discard" (default), or a path inside ${workdir} that receives the standard output of the latest run. |
| `stderr` |  | "discard" (default), or a path inside ${workdir} that receives the standard error of the latest run. A failing run's stderr tail is reported either way. |
| `exit_codes` |  | Exit statuses that count as success. Default [0]. |
| `stdin` |  | Standard input of every run: a fixture path (reopened for each run), or a mapping with file or content. |
| `terminal` |  | Run every command on a pseudo-terminal of 80 columns and 24 rows (Linux and macOS): standard input, output and error are the terminal, stdin is typed into it one key at a time once the command is ready and has read the key before, and what the command writes goes to stdout. The input must end the program. stderr cannot be set. |
| `setup` |  | Processes run once per benchmark (per revision in a comparison) before any warmup. A failure fails the benchmark. |
| `prepare_each` |  | Processes run before every warmup and measured run, outside the measured time. |
| `cleanup` |  | Processes run after the benchmark whether it passed, failed or was interrupted. |
| `baseline` |  | Command name that RELATIVE and vs_baseline are computed against. |
| `commands` | yes | Named commands measured side by side. Names use letters, digits, '.', '_' and '-'. |
| `metrics` |  | What the benchmark measures besides latency. |
| `budget` |  | Absolute budgets keyed by command name. A violation fails the run with exit status 1. |
| `regression` |  | How a base revision and the working tree are compared (himorime compare / himorime ci). |

### benchmarks[].commands.NAME

| Key | Required | Description |
|---|---|---|
| `command` | yes | The command: a list of arguments (no shell), or a string when shell is true. |
| `shell` |  | Run the command string through /bin/sh -c (cmd.exe /d /s /c on Windows). Variables substituted into the string are quoted for that shell. |
| `cwd` |  | Working directory: relative to ${root} (the suite directory in the revision being measured), or starting with ${root}, ${head_root} or ${workdir}. Default: ${root}, so each revision runs its own files. |
| `env` |  | Environment variables added to the inherited environment. Values may use variables. |
| `timeout` |  | Per-run time limit. A run that exceeds it is stopped with its whole process tree and fails the benchmark. Default 1m. |
| `stdout` |  | "discard" (default), or a path inside ${workdir} that receives the standard output of the latest run. |
| `stderr` |  | "discard" (default), or a path inside ${workdir} that receives the standard error of the latest run. A failing run's stderr tail is reported either way. |
| `exit_codes` |  | Exit statuses that count as success. Default [0]. |

### benchmarks[].metrics

| Key | Required | Description |
|---|---|---|
| `latency` |  | Wall-clock time of each run. Always measured; true only says so. |
| `throughput` |  | Compute throughput from declared work. |
| `cpu` |  | Measure user, system and total CPU time and CPU utilization of the process tree. |
| `memory` |  | Measure the peak resident set size (RSS) of the process tree. |
| `unsupported` |  | What to do when this platform cannot measure a requested metric: fail (default) stops before measuring, skip reports the metric as unsupported and skips its budgets and comparisons. |

### benchmarks[].metrics.throughput.work

| Key | Required | Description |
|---|---|---|
| `value` |  | A fixed amount of work per run, greater than zero, such as 100000. |
| `file_size` |  | A file whose size in bytes is the work of each run: relative to ${root} of the revision being measured, or starting with ${root}, ${head_root} or ${workdir}. Read before every run, outside the measured time. |
| `unit` |  | The unit of work, such as records, lines or bytes. Default: operations for value, bytes for file_size. |

### benchmarks[].budget.NAME

| Key | Required | Description |
|---|---|---|
| `mean` |  | Budget on the mean latency (shorthand for latency.mean). |
| `median` |  | Budget on the median latency (shorthand for latency.median). |
| `min` |  | Budget on the min latency (shorthand for latency.min). |
| `max` |  | Budget on the max latency (shorthand for latency.max). |
| `latency` |  | Latency budgets keyed by aggregation, such as {p95: "<= 100ms"}. |
| `throughput` |  | Throughput budgets keyed by aggregation, such as {median: ">= 50MiB/s"}. Needs metrics.throughput. |
| `cpu` |  | CPU budgets. Needs metrics.cpu. |
| `memory` |  | Memory budgets. Needs metrics.memory. |

### benchmarks[].budget.NAME.cpu

| Key | Required | Description |
|---|---|---|
| `user` |  | User CPU time budgets keyed by aggregation. |
| `system` |  | System CPU time budgets keyed by aggregation. |
| `total` |  | Total (user + system) CPU time budgets keyed by aggregation. |
| `utilization` |  | CPU utilization budgets keyed by aggregation, in percent of one CPU; above 100% means more than one CPU was busy. |

### benchmarks[].budget.NAME.memory

| Key | Required | Description |
|---|---|---|
| `peak_rss` |  | Peak resident set size budgets keyed by aggregation. |

### regression

| Key | Required | Description |
|---|---|---|
| `confidence` |  | Bootstrap probability required to call a regression, an improvement or a pass. Default 0.95. |
| `min_samples` |  | Fewest samples per side for a verdict. In a comparison, adaptive runs keep measuring until every compared command has this many; fewer is inconclusive. Default 10. |
| `max_cv` |  | Overlapping or touching sample ranges are inconclusive when either side exceeds this dispersion: IQR / 1.349 / median, or standard deviation / mean for the mean. Completely separated ranges still need the sample count, tolerance and bootstrap confidence checks. 0 disables the noise check. Default 0.5. |
| `commands` |  | Compare only these commands. Default: every command. |
| `latency` |  | How latency (wall-clock time; lower is better) is compared. |
| `cpu` |  | How total CPU time (lower is better) is compared. |
| `memory` |  | How peak RSS (lower is better) is compared. |

### regression.latency, regression.cpu, regression.memory

| Key | Required | Description |
|---|---|---|
| `metric` |  | Statistic compared: median (default) or mean. |
| `max_percent` |  | Tolerated degradation in percent: a number such as 10, or a string such as "10%". Default 10. |
| `min_difference` |  | The smallest absolute difference that can be a regression or an improvement; smaller differences pass. A duration such as 1ms for latency and cpu, a byte size such as 1MiB for memory. Default: none. |
| `gate` |  | Whether this metric's verdict decides the result and the exit status (default true). With false the metric is still compared and reported, marked as not gated, but a regression or an inconclusive result never fails the run. |

### report

| Key | Required | Description |
|---|---|---|
| `outputs` |  | Report files. |
| `versions` |  | Tools whose versions every report records, such as jc: [jc, --version]. Each command runs once in the suite directory before measuring, with a 30s time limit; the first non-empty line it prints is recorded, and a failure fails the run. |

### report.outputs[]

| Key | Required | Description |
|---|---|---|
| `format` | yes | Report format. |
| `path` | yes | Relative path of the file. |
| `section` |  | Markdown only: replace the section of this name in the existing file, between its himorime:begin and himorime:end comment lines, and keep the rest of the file. Lowercase letters, digits and '-'. |

<!-- END GENERATED: config-reference -->
