---
title: Configuration
description: The yahiko.yaml suite format, version 1. Every key, its default, variables, paths, and how validation works.
toc: true
---

A suite is a YAML file, `yahiko.yaml` by default. `yahiko run`, `compare`,
`ci`, `list` and `validate` take files and directories as arguments; a
directory contributes its `yahiko.yaml` and every `*.yahiko.yaml` in it.

## Editor support

The format is described by a JSON Schema. Put this comment on the first line
and editors using the YAML language server (VS Code with the YAML extension,
Neovim, JetBrains IDEs) complete keys and flag mistakes as you type:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/nao1215/yahiko/main/schema/yahiko.schema.json
```

yahiko validates every suite against the same schema, embedded in the
binary, before it looks at anything else. What the editor accepts and what
yahiko accepts cannot drift apart; a test in the repository keeps them equal.

## A complete example

```yaml
version: "1"

suite:
  name: jsonize benchmarks
  description: Performance checks for common conversion paths

defaults:
  warmup: 3
  runs: 20
  timeout: 10s

build:
  command: [go, build, -o, "${artifact}", ./cmd/jz]

benchmarks:
  - name: parse df output
    tags: [parser, smoke]
    stdin: testdata/df-large.txt
    baseline: jsonize
    commands:
      jsonize:
        command: ["${artifact}", --parser, df]
      jc:
        command: [jc, --df]
    budget:
      jsonize:
        median: "< 20ms"
    regression:
      metric: median
      max_percent: 10
      confidence: 0.95
```

## Commands

`command` is either a list of arguments or, with `shell: true`, a string.

- A list runs the program directly with those arguments: no shell, no word
  splitting, no globbing, the same on every operating system. This is the
  default and the recommended form.
- A string with `shell: true` runs through `/bin/sh -c` on Unix and
  `cmd.exe /d /s /c` on Windows. Every variable substituted into the string
  is quoted for that shell first, so a path or an environment value cannot
  become shell syntax. On Windows, a value holding `"`, `%` or a line break is
  refused, because `cmd.exe` cannot quote it, and so is a value ending in `\`.
Write variables without quotes of your own, such as `cat ${workdir}/in.txt`:
a variable inside quotes you wrote is a validation error, because two layers
of quoting would change what the inner quotes mean. The measured time includes
  starting the shell; yahiko does not subtract it.

A string without `shell: true`, or a list with it, is rejected.

## Runs

Each benchmark runs a number of unmeasured warmup runs, then measured runs.

- With `runs`, every command runs exactly that many times.
- Without `runs`, yahiko measures adaptively: it keeps going until every
  command has at least `min_runs` samples and at least `min_time` of total
  measured time, and stops at `max_runs` regardless.

Commands of one benchmark run interleaved: each round runs every command
once, in an order shuffled with the seed (`--seed`, printed in every report).
In a revision comparison the base and head builds of a command are
interleaved the same way. A slow period on the machine therefore lands on
all commands instead of on whichever ran last.

`--runs` and `--warmup` on the command line override the suite, which is
handy for a quick smoke run.

## Hooks

- `setup` runs once per benchmark, before warmup. In a comparison it runs
  once for each revision, with that revision's `${artifact}`, `${root}` and
  its own `${workdir}`.
- `prepare_each` runs before every warmup and measured run. It is not
  measured. Use it to reset state, such as deleting a cache to measure a cold
  start.
- `cleanup` runs after the benchmark, whether it passed, a command failed,
  setup failed, or the run was interrupted with Ctrl+C.

After Ctrl+C, running commands are stopped and cleanup runs; interrupt a
second time to quit at once, skipping the remaining cleanup.

A process a command or hook leaves running in the background is stopped when
that command or hook exits, on every platform. yahiko measures commands, it
does not manage services: a hook cannot start a server that outlives it.

A failing `setup` or `cleanup` fails the benchmark. A failing `prepare_each`
fails the command it prepared. Hook output is not shown unless the hook fails,
in which case the tail of its standard error is.

## Standard input and output

`stdin` is a fixture path (`stdin: testdata/input.txt`), `stdin: {file: ...}`,
or inline text (`stdin: {content: "..."}`). The file is reopened for every
run, so every run reads it from the first byte.

A command's standard output and standard error are discarded by default and
never mixed with yahiko's own report. Set `stdout` or `stderr` to a path
inside `${workdir}` to keep the latest run's output. When a run fails, the
last lines of its standard error are shown in the report either way, with
secrets masked.

## Variables

Commands, `cwd`, `env` values, `stdin` and hooks may use these variables:

<!-- BEGIN GENERATED: variables -->
| Variable | Expands to |
|---|---|
| `${artifact}` | The file the build step writes. In a comparison each revision has its own. Only available when the suite has a build section. On Windows it ends in .exe. |
| `${root}` | The directory holding the suite file, inside the tree being measured: the working tree, or the temporary worktree of the base revision. |
| `${workdir}` | A fresh, empty directory created for each benchmark (and each revision in a comparison) and removed after cleanup. Not available in build. |
| `${exe}` | `.exe` on Windows, empty elsewhere. |
| `${env:NAME}` | The environment variable NAME. A variable that is not set is an execution error. |
| `$${` | A literal `${`. |
<!-- END GENERATED: variables -->

An unknown variable is a validation error. `${artifact}` without a build and
`${workdir}` inside the build are validation errors too.

## Paths

- Relative `cwd` and `stdin` paths are relative to the suite file. In a
  comparison both revisions read the same fixtures from your working tree;
  only the build and `${root}` differ.
- A relative build `cwd` is relative to `${root}` of the revision being built.
- Paths may start with `${root}` or `${workdir}`. Absolute paths, `~`, and
  `..` after a variable are rejected.
- After symbolic links are resolved, a path must stay inside the Git
  repository (or the suite's directory outside Git), the base worktree, or
  `${workdir}`. `stdout`, `stderr` and report paths may not climb out of their
  base directory at all.

## Environment

Commands inherit yahiko's environment, with `env` from `defaults`, the
benchmark and the command layered on top, in that order. Reports never
contain the environment, and command lines are shown as written, before
`${env:NAME}` is substituted.

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
| `timeout (build)` | `1` |
| `stdout, stderr` | `discard` |
| `exit_codes` | `[0]` |
| `regression.metric` | `median` |
| `regression.max_percent` | `10` |
| `regression.confidence` | `0.95` |
| `regression.min_samples` | `10` |
| `regression.max_cv` | `0.5` |
<!-- END GENERATED: defaults -->

A benchmark inherits `defaults`; a command inherits its benchmark. The most
specific value wins.

## Durations and percentages

Durations are a number with a unit: `ns`, `us` (or `µs`), `ms`, `s`, `m`, `h`,
such as `500ms`, `1.5s` or `1m30s`. A bare number is rejected, because `10`
could mean ten of anything. The maximum is `24h`.

A budget is `<` or `<=` followed by a duration, such as `"< 20ms"`. Quote it:
YAML reads a leading `<` fine, but the quotes make the intent obvious.

A percentage is a number (`10`) or a string with a percent sign (`"10%"`),
greater than 0 and at most 1000.

## Validation

`yahiko validate` checks, without running anything:

1. YAML syntax and duplicate keys.
2. The schema: unknown keys, types, required keys, durations, budgets,
   percentages, names and path shapes. Every problem is listed at once.
3. Semantic rules: unique benchmark names, baselines, budgets and
   `regression.commands` that name real commands, `max_runs` not below
   `min_runs`, known variables used where they exist.

`yahiko compare` and `yahiko ci` additionally refuse a benchmark whose `runs`
(or `max_runs`) is below `regression.min_samples`, because such a comparison
could never be conclusive.

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
| `cwd` |  | Working directory: relative to the suite file, or starting with ${root} or ${workdir}. Default: the suite file's directory. |
| `env` |  | Environment variables added to the inherited environment. Values may use variables. |
| `stdout` |  | "discard" (default), or a path inside ${workdir} that receives the standard output of the latest run. |
| `stderr` |  | "discard" (default), or a path inside ${workdir} that receives the standard error of the latest run. A failing run's stderr tail is reported either way. |
| `exit_codes` |  | Exit statuses that count as success. Default [0]. |
| `regression` |  | How a base revision and the working tree are compared (yahiko compare / yahiko ci). |

### build, setup, prepare_each, cleanup

| Key | Required | Description |
|---|---|---|
| `command` | yes | The command: a list of arguments (no shell), or a string when shell is true. |
| `shell` |  | Run the command string through /bin/sh -c (cmd.exe /d /s /c on Windows). Variables substituted into the string are quoted for that shell. |
| `cwd` |  | Working directory. For build, a relative path is relative to ${root} of the revision being built (the default); for hooks, to the suite file (the default). May start with ${root}, or ${workdir} in hooks. |
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
| `cwd` |  | Working directory: relative to the suite file, or starting with ${root} or ${workdir}. Default: the suite file's directory. |
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
| `budget` |  | Absolute budgets keyed by command name. A violation fails the run with exit status 1. |
| `regression` |  | How a base revision and the working tree are compared (yahiko compare / yahiko ci). |

### benchmarks[].commands.NAME

| Key | Required | Description |
|---|---|---|
| `command` | yes | The command: a list of arguments (no shell), or a string when shell is true. |
| `shell` |  | Run the command string through /bin/sh -c (cmd.exe /d /s /c on Windows). Variables substituted into the string are quoted for that shell. |
| `cwd` |  | Working directory: relative to the suite file, or starting with ${root} or ${workdir}. Default: the suite file's directory. |
| `env` |  | Environment variables added to the inherited environment. Values may use variables. |
| `timeout` |  | Per-run time limit. A run that exceeds it is stopped with its whole process tree and fails the benchmark. Default 1m. |
| `stdout` |  | "discard" (default), or a path inside ${workdir} that receives the standard output of the latest run. |
| `stderr` |  | "discard" (default), or a path inside ${workdir} that receives the standard error of the latest run. A failing run's stderr tail is reported either way. |
| `exit_codes` |  | Exit statuses that count as success. Default [0]. |

### benchmarks[].budget.NAME

| Key | Required | Description |
|---|---|---|
| `mean` |  | Budget on the mean duration. |
| `median` |  | Budget on the median duration. |
| `min` |  | Budget on the min duration. |
| `max` |  | Budget on the max duration. |

### regression

| Key | Required | Description |
|---|---|---|
| `metric` |  | Statistic compared: median (default) or mean. |
| `max_percent` |  | Tolerated slowdown in percent: a number such as 10, or a string such as "10%". Default 10. |
| `confidence` |  | Bootstrap probability required to call a regression, an improvement or a pass. Default 0.95. |
| `min_samples` |  | Fewest samples per side for a verdict; fewer is inconclusive. Default 10. |
| `max_cv` |  | A side whose coefficient of variation exceeds this is inconclusive; 0 disables the check. Default 0.5. |
| `commands` |  | Compare only these commands. Default: every command. |

### report

| Key | Required | Description |
|---|---|---|
| `outputs` |  | Report files. |

<!-- END GENERATED: config-reference -->
