---
title: Configuration
description: The himorime.yaml suite format, version 1. Every key, its default, variables, paths, and how validation works.
toc: true
---

A suite is a YAML file, `himorime.yaml` by default. `himorime run`, `compare`, `ci`, `list` and `validate` take files and directories as arguments; a directory contributes its `himorime.yaml` and every `*.himorime.yaml` in it.

## Editor support

The format is described by a JSON Schema. Put this comment on the first line and editors using the YAML language server (VS Code with the YAML extension, Neovim, JetBrains IDEs) complete keys and flag mistakes as you type:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/nao1215/himorime/main/schema/himorime.schema.json
```

himorime validates every suite against the same schema, embedded in the binary, before it looks at anything else. What the editor accepts and what himorime accepts cannot drift apart; a test in the repository keeps them equal.

## A complete example

```yaml
version: "1"

suite:
  name: jsonize benchmarks
  description: Performance budgets for common conversion paths

defaults:
  warmup: 3
  runs: 20
  timeout: 10s
  metrics:
    cpu: true
    memory: true

build:
  command: [go, build, -o, "${artifact}", ./cmd/jz]

benchmarks:
  - name: parse df output
    tags: [parser, smoke]
    stdin: testdata/df-large.txt
    metrics:
      throughput:
        work:
          file_size: testdata/df-large.txt
    baseline: jsonize
    commands:
      jsonize:
        command: ["${artifact}", --parser, df]
      jc:
        command: [jc, --df]
    budget:
      jsonize:
        median: "< 20ms"
        latency: {p95: "<= 30ms"}
        throughput: {median: ">= 50MiB/s"}
        cpu: {total: {median: "<= 25ms"}}
        memory: {peak_rss: {max: "<= 64MiB"}}
    regression:
      confidence: 0.95
      latency: {metric: median, max_percent: 10}
      throughput: {max_percent: 8}
      cpu: {max_percent: 10, min_difference: 2ms}
      memory: {max_percent: 5, min_difference: 1MiB}
```

## Commands

`command` is either a list of arguments or, with `shell: true`, a string.

- A list runs the program directly with those arguments: no shell, no word splitting, no globbing, the same on every operating system. This is the default and the recommended form.
- A string with `shell: true` runs through `/bin/sh -c` on Unix and `cmd.exe /d /s /c` on Windows. Every variable substituted into the string is quoted for that shell first, so a path or an environment value cannot become shell syntax. On Windows, a value holding `"`, `%` or a line break is refused, because `cmd.exe` cannot quote it, and so is a value ending in `\`. Write variables without quotes of your own, such as `cat ${workdir}/in.txt`: a variable inside quotes you wrote is a validation error, because two layers of quoting would change what the inner quotes mean. The measured time includes starting the shell; himorime does not subtract it.

A string without `shell: true`, or a list with it, is rejected.

## Runs

Each benchmark runs a number of unmeasured warmup runs, then measured runs.

- With `runs`, every command runs exactly that many times.
- Without `runs`, himorime measures adaptively: it keeps going until every command has at least `min_runs` samples and at least `min_time` of total measured time, and stops at `max_runs` regardless. In a revision comparison it also keeps going until every compared command has `regression.min_samples` samples on each revision, because fewer could only be inconclusive; a plain `himorime run` ignores `min_samples`.

Commands of one benchmark run interleaved: each round runs every command once, in an order shuffled with the seed (`--seed`, printed in every report). In a revision comparison the base and head builds of a command are interleaved the same way. A slow period on the machine therefore lands on all commands instead of on whichever ran last.

`--runs` and `--warmup` on the command line override the suite, which is handy for a quick smoke run.

## Metrics, budgets and tolerances

Latency is always measured. `metrics` switches on throughput, CPU time and peak RSS; see [Metrics](/metrics/) for what each one means and covers on each platform.

```yaml
metrics:
  latency: true            # always on; allowed for readability
  throughput:
    work: {value: 1000, unit: records}   # or {file_size: path}
  cpu: true                # or {scope: process_tree}
  memory: true             # or {scope: process_tree}
  unsupported: fail        # or skip
```

`cpu`, `memory` and `unsupported` can be set under `defaults.metrics` and overridden per benchmark (`cpu: false` turns an inherited setting off). Throughput is declared per benchmark, because its work belongs to the benchmark's input.

A budget is an absolute limit on one aggregation of one metric of one command, checked on every run of `run`, `compare` and `ci`. It is keyed by command name, then metric, then aggregation: `min`, `max`, `mean`, `median`, or a percentile from `p1` to `p99.9`.

```yaml
budget:
  mytool:
    median: "< 20ms"                    # shorthand for latency.median
    latency: {p95: "<= 30ms"}
    throughput: {median: ">= 50MiB/s"}
    cpu:
      user: {median: "<= 20ms"}
      system: {p95: "<= 5ms"}
      total: {median: "<= 25ms"}
      utilization: {median: "<= 150%"}
    memory:
      peak_rss: {max: "<= 64MiB"}
```

The operator follows the metric's direction. Latency, CPU time and peak RSS are better when lower, so their budgets are upper bounds (`<`, `<=`). Throughput is better when higher, so its budgets are lower bounds (`>`, `>=`). CPU utilization has no better direction and takes either. A budget on a metric the benchmark does not measure is a validation error.

`regression` sets how a comparison judges each metric. `confidence`, `min_samples`, `max_cv` and `commands` apply to every metric. `latency`, `throughput`, `cpu` (total CPU time) and `memory` (peak RSS) each take the same four keys, and each metric is compared whenever the benchmark measures it:

```yaml
regression:
  confidence: 0.95
  latency: {gate: false}                  # compared and reported, never fails
  cpu: {max_percent: 15, min_difference: 5ms}
  memory: {max_percent: 5, min_difference: 1MiB}
```

- `metric` is the compared statistic, `median` or `mean`.
- `max_percent` is the tolerated degradation in the metric's worse direction: an increase for latency, CPU time and peak RSS, a decrease for throughput.
- `min_difference` is the smallest absolute change that can count at all.
- `gate` (default `true`) says whether the metric's verdict decides the result and the exit status. With `gate: false` the metric is still compared and reported with its verdict, marked as not gated, but a regression or an inconclusive result never fails the run.

See [Regression detection](/regression-detection/) for how budgets, gates, `--fail-on-inconclusive` and unsupported metrics decide the exit status.

## Hooks

- `setup` runs once per benchmark, before warmup. In a comparison it runs once for each revision, with that revision's `${artifact}`, `${root}` and its own `${workdir}`.
- `prepare_each` runs before every warmup and measured run. It is not measured. Use it to reset state, such as deleting a cache to measure a cold start.
- `cleanup` runs after the benchmark, whether it passed, a command failed, setup failed, or the run was interrupted with Ctrl+C.

After Ctrl+C, running commands are stopped and cleanup runs; interrupt a second time to quit at once, skipping the remaining cleanup.

A process a command or hook leaves running in the background is stopped when that command or hook exits, on every platform. himorime measures commands, it does not manage services: a hook cannot start a server that outlives it.

A failing `setup` or `cleanup` fails the benchmark. A failing `prepare_each` fails the command it prepared. Hook output is not shown unless the hook fails, in which case the tail of its standard error is.

## Reports

`report.outputs` lists report files written after every run, relative to the suite file. A Markdown output with `section` does not replace the file: it updates the section of that name in an existing page, between the lines `<!-- himorime:begin NAME -->` and `<!-- himorime:end NAME -->`, and keeps the rest of the page. `report.versions` names the tools whose versions every report records:

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

- A section name uses lowercase letters, digits and `-`. `section` is only allowed with `format: markdown`.
- Each version command is a list of arguments, run once without a shell in the suite directory of the working tree, before anything is built or measured. It inherits himorime's environment with `LC_ALL=C`, so the version line does not change language with the locale. It may use `${root}`, `${head_root}`, `${exe}` and `${env:NAME}`, and has 30 seconds. The first non-empty line it prints on standard output, or else on standard error, is recorded.
- A version command that fails, times out or prints nothing fails the suite like a failed setup, and the run exits 4.

See [Reports](/reports/#publish-results-in-documentation) for what a section looks like.

## Standard input and output

`stdin` is a fixture path (`stdin: testdata/input.txt`), `stdin: {file: ...}`, or inline text (`stdin: {content: "..."}`). The file is reopened for every run, so every run reads it from the first byte.

A command's standard output and standard error are discarded by default and never mixed with himorime's own report. Set `stdout` or `stderr` to a path inside `${workdir}` to keep the latest run's output. When a run fails, the last lines of its standard error are shown in the report either way, with secrets masked.

## Variables

Commands, `cwd`, `env` values, `stdin` and hooks may use these variables:

<!-- BEGIN GENERATED: variables -->
| Variable | Expands to |
|---|---|
| `${artifact}` | The file the build step writes. In a comparison each revision has its own. Only available when the suite has a build section. On Windows it ends in .exe. |
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
| `cwd` of commands, `setup`, `prepare_each`, `cleanup` | `${root}` of the revision measured | `${root}` |
| `cwd` of `build` | `${root}` of the revision built | `${root}` |
| `stdin` file | `${root}` of the revision measured | |
| `metrics.throughput.work.file_size` | `${root}` of the revision measured | |
| a relative program or argument, such as `[sh, work.sh]` | the command's working directory | |
| `stdout`, `stderr` | `${workdir}` | discarded |
| `report.outputs[].path` | the suite file in the working tree | |
| a relative program of `report.versions` | the directory of the suite file in the working tree | |

`${head_root}` is the same directory inside the working tree, for both revisions. Use it when both revisions must read the same file, such as a fixture that changed or was added in the working tree, or run the same helper:

```yaml
benchmarks:
  - name: parse the large log
    stdin: "${head_root}/testdata/large.log"   # the same input for base and head
    commands:
      parser:
        command: [sh, parse.sh]                # each revision's own parse.sh
```

A relative path that exists only in the working tree fails the base revision, with a hint to use `${head_root}`. The suite directory itself must exist in the base revision. A suite with a `build` measures `${artifact}`, which each revision builds from its own tree.

- Paths may start with `${root}`, `${head_root}` or `${workdir}`. Absolute paths, `~`, and `..` after a variable are rejected.
- After symbolic links are resolved, a path must stay inside the Git repository (or the suite's directory outside Git), the base worktree, or `${workdir}`. `stdout`, `stderr` and report paths may not climb out of their base directory at all.
- A relative path whose `..` leaves the repository (or the suite's directory outside Git) is rejected when the suite is loaded, before the build. A `..` that stays inside, such as `cwd: ..` in a `bench` directory, is allowed.

## Environment

Commands inherit himorime's environment, with `env` from `defaults`, the benchmark and the command layered on top, in that order. Reports never contain the environment, and command lines are shown as written, before `${env:NAME}` is substituted.

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
| `regression.latency, throughput, cpu, memory: metric` | `median` |
| `regression.latency, throughput, cpu, memory: max_percent` | `10` |
| `regression.latency, throughput, cpu, memory: min_difference` | `unset (none)` |
| `regression.latency, throughput, cpu, memory: gate` | `true` |
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

`himorime validate` checks, without running anything:

1. YAML syntax and duplicate keys.
2. The schema: unknown keys, types, required keys, durations, budgets, percentages, names and path shapes. Every problem is listed at once.
3. Semantic rules: unique benchmark names, baselines, budgets and `regression.commands` that name real commands, `max_runs` not below `min_runs`, known variables used where they exist, budgets and tolerances only for metrics the benchmark measures, operators in the metric's direction, and throughput units that match the declared work.

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
| `max_cv` |  | A side whose spread exceeds this is inconclusive: the interquartile range divided by 1.349 and by the median, or the coefficient of variation (standard deviation / mean) when the statistic is mean; 0 disables the check. Default 0.5. |
| `commands` |  | Compare only these commands. Default: every command. |
| `latency` |  | How latency (wall-clock time; lower is better) is compared. |
| `throughput` |  | How throughput (higher is better, so a drop is a degradation) is compared. |
| `cpu` |  | How total CPU time (lower is better) is compared. |
| `memory` |  | How peak RSS (lower is better) is compared. |

### regression.latency, regression.throughput, regression.cpu, regression.memory

| Key | Required | Description |
|---|---|---|
| `metric` |  | Statistic compared: median (default) or mean. |
| `max_percent` |  | Tolerated degradation in percent: a number such as 10, or a string such as "10%". Default 10. |
| `min_difference` |  | The smallest absolute difference that can be a regression or an improvement; smaller differences pass. A duration such as 1ms for latency and cpu, a byte size such as 1MiB for memory, a rate in the work unit such as "1000 records/s" for throughput. Default: none. |
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
