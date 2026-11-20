---
title: Troubleshooting
description: Common himorime errors and what to do about them.
toc: true
---

## Every comparison is inconclusive

The samples are too noisy or too few for the tolerance. Raise `runs`, widen `max_percent`, measure a heavier workload, or use a quieter machine; see [Regression detection](/regression-detection/#noise-and-inconclusive-results). The reason is printed under the table. "measurements are noisier than max_cv" means the bulk of the samples is spread out: three slow runs of 10 trip it, three of 30 do not. A metric you only want to see can have `gate: false`; see [What fails the run](/regression-detection/#what-fails-the-run).

## A comparison passes although the working tree is clearly slower

Check that both revisions run their own code. A path under `${head_root}`, a program outside the repository, or one on `PATH` is the same for both. Build the program with a `build` section and measure `${artifact}`, or run it by a relative path.

## The base revision fails with "does not exist in the base revision"

A relative path resolves in each revision, and the base does not have that file, usually because this change added it. Write it as `${head_root}/path` so both revisions read the working tree's copy.

## Exit 6: "metrics.memory cannot be measured on this platform"

The platform cannot measure a requested metric and `metrics.unsupported` is `fail` (the default). Remove the metric or set `metrics.unsupported: skip`. On Windows, peak RSS is unsupported for commands that start child processes, including `shell: true`; see [Metrics](/metrics/#process-tree).

## Exit 6: "metric collection failed"

The platform did not report the metric for a run, or the throughput `file_size` is missing or empty when the run starts. Create the file in `setup` or `prepare_each` and check that its path is relative to `${root}`. Collection failures are never skipped.

## "the budget is in bytes/s but the declared work unit is records"

A throughput budget must use the unit of the declared work: `MiB/s` and friends need `unit: bytes` (or `file_size`), `records/s` needs `unit: records`.

## "this metric is better when lower, so a budget is an upper bound"

Latency, CPU time and peak RSS budgets use `<` or `<=`; throughput budgets use `>` or `>=`.

## CPU time reads 0ns

A command of a millisecond or two can finish between scheduler ticks, so the OS reports no CPU time (on Windows a tick is 15.625ms). Measure a heavier workload, or judge latency.

## CPU utilization above 100%

The command used more than one CPU at once. See [Understand CPU utilization above 100%](/cookbook/#understand-cpu-utilization-above-100).

## "runs (5) is lower than regression.min_samples (10)"

`compare` and `ci` refuse settings that can never give a verdict. Raise `runs` (or `max_runs`), or lower `regression.min_samples`.

## "revision ... is not a commit"

The base revision is not in the clone. Fetch it (`git fetch origin main`), or in GitHub Actions check out with `fetch-depth: 0`.

## "cannot determine the base revision"

`himorime ci` runs outside GitHub Actions, or on an event without a base, such as `schedule`. Pass `--against <ref>` or set `HIMORIME_BASE_REF`.

## "new in this revision: ... does not exist in the base revision"

The pull request adds the suite, so only the working tree is built and run, as by `himorime run`. Comparisons start once the suite is in the base branch.

## "the build ... did not write ${artifact}"

The build succeeded but did not create `${artifact}`. Pass it as the output path, such as `go build -o "${artifact}"`.

## A command fails with "executable file not found"

A bare program name is looked up in `PATH`, with the benchmark's `env` applied. Use `${artifact}` for a built program, or a path starting with `${root}`.

## A command works in my shell but not in himorime

List commands run without a shell: no `~`, globbing, pipes or `$VAR`. Use `${env:HOME}` and similar, or a string with `shell: true`.

## "path ... resolves outside the project"

A `cwd` or `stdin` path, after following symbolic links, leaves the repository, the base worktree and `${workdir}`. Keep fixtures in the repository or generate them into `${workdir}`.

## "path ... leaves the project"

A relative path's `..` climbs out of the repository, or out of the suite's directory outside a repository. The suite fails to load with exit 2. Keep the path inside, or write generated files to `${workdir}`.

## "the work differs between the revisions"

The `file_size` fixture has a different size in the two revisions. Point it at `${head_root}` so both read the working tree's copy.

## himorime keeps running after Ctrl+C

The first interrupt stops the commands, runs `cleanup` hooks and removes the temporary worktree. A second interrupt quits at once and skips the rest; a leftover `himorime-*` worktree in the temporary directory is pruned by the next `himorime compare` once you delete it.

## Output of a failing command

The report shows the tail of the failing run's standard error. To keep the full output, set `stdout` or `stderr` to a file in `${workdir}` and copy it in a `cleanup` hook.

## Very short commands show large relative differences

Below about a millisecond, process creation dominates. Measure a real input rather than `--version`, on one machine.
