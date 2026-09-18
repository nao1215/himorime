---
title: Troubleshooting
description: Common himorime errors and what to do about them.
toc: true
---

## Every comparison is inconclusive

The measurements are too noisy, or there are too few of them, for the tolerance you set. See [Regression detection](/regression-detection/#noise-and-inconclusive-results): raise `runs`, widen `max_percent`, measure a heavier workload, or use a quieter machine. The reason is printed under the table. A reason of "measurements are noisier than max_cv" means the bulk of the samples is spread out, such as a command with two modes far apart, not that a few runs were slow: the gate ignores rare outliers when the statistic is the median. How rare is rare depends on `runs`: three slow runs of 10 are 30% of the samples and do trip it, where three of 30 do not. A metric you want to see but not gate on can have `gate: false`; see [What fails the run](/regression-detection/#what-fails-the-run).

## A comparison passes although the working tree is clearly slower

Check that both revisions run their own code. A command reads files relative to `${root}` of each revision; a path that starts with `${head_root}`, or an absolute program outside the repository, is the same for both revisions. A program installed on `PATH` is also the same for both: build it from the tree with a `build` section and measure `${artifact}`, or run it from the tree with a relative path.

## The base revision fails with "does not exist in the base revision"

A relative path, such as a `stdin` fixture or a `file_size`, is relative to `${root}` of each revision, and the base revision does not have that file, usually because this change added it. Write it as `${head_root}/path` to give both revisions the working tree's copy.

## Exit 6: "metrics.memory cannot be measured on this platform"

A benchmark requests a metric this platform cannot measure, and its `metrics.unsupported` is `fail` (the default). Remove the metric, measure it on another platform, or set `metrics.unsupported: skip` to measure everything else and report the metric as unsupported. On Windows, peak RSS is unsupported for commands that start child processes, which includes `shell: true`; see [Metrics](/metrics/#process-tree).

## Exit 6: "metric collection failed"

The platform supports the metric but did not report it for a run, or the throughput `file_size` does not exist or is empty when the run starts. Create the file in `setup` or `prepare_each`, and check its path: it is relative to `${root}`, the suite's directory in the revision being measured. A collection failure is never skipped, because it cannot be told apart from a broken measurement.

## "the budget is in bytes/s but the declared work unit is records"

A throughput budget must use the unit of the declared work: `MiB/s` and friends need `unit: bytes` (or `file_size`), `records/s` needs `unit: records`.

## "this metric is better when lower, so a budget is an upper bound"

Latency, CPU time and peak RSS budgets use `<` or `<=`; throughput budgets use `>` or `>=`. A budget in the other direction would pass for the wrong programs.

## CPU time reads 0ns

A command that runs for a millisecond or two can finish between two scheduler ticks, and the operating system then reports no user or system time. Measure a heavier workload, or judge latency for such commands.

## CPU utilization above 100%

Not a bug: the command used more than one CPU at the same time. See [Understand CPU utilization above 100%](/cookbook/#understand-cpu-utilization-above-100).

## "runs (5) is lower than regression.min_samples (10)"

`himorime compare` and `himorime ci` refuse settings that could never produce a verdict. Raise `runs` (or `max_runs` for adaptive runs), or lower `regression.min_samples`. `himorime run` does not need either.

## "revision ... is not a commit"

The base revision is not in the clone. Fetch it (`git fetch origin main`), or in GitHub Actions check out with `fetch-depth: 0`.

## "cannot determine the base revision"

`himorime ci` runs outside GitHub Actions, or on an event without a base, such as `schedule`. Pass `--against <ref>` or set `HIMORIME_BASE_REF`.

## "new in this revision: ... does not exist in the base revision"

The suite directory was added after the base revision, usually by the pull request that adopts himorime, so there is nothing to compare it with yet. Only the working tree is built and run, and its budgets and failures decide the exit status as in `himorime run`. Comparisons start once the suite is in the base branch.

## "the build ... did not write ${artifact}"

The build command succeeded but never created the file at `${artifact}`. Pass `${artifact}` as the build's output path, such as `go build -o "${artifact}"`.

## A command fails with "executable file not found"

A bare program name is looked up in `PATH` (with the benchmark's `env` applied). Use `${artifact}` for the program the suite builds, or a path relative to the suite starting with `${root}`.

## A command works in my shell but not in himorime

Commands written as lists run without a shell: no `~`, globbing, pipes or `$VAR` expansion. Either pass the expanded values (`${env:HOME}`), or use a string with `shell: true`.

## "path ... resolves outside the project"

A `cwd` or `stdin` path, after following symbolic links, leaves the Git repository, the base worktree and `${workdir}`. Keep fixtures inside the repository, or generate them into `${workdir}` in `setup`.

## "path ... leaves the project"

A relative path's `..` climbs out of the repository holding the suite, or out of the suite's directory when it is not in a repository. This is caught when the suite is loaded, so `run` and `compare` stop with exit 2 before the build. Keep the path inside the repository, or write generated files to `${workdir}`.

## "the work differs between the revisions"

The file named by `metrics.throughput.work.file_size` has a different size in the two revisions, so their throughput describes two different jobs. Point `file_size` at `${head_root}` so both revisions read the working tree's fixture, or leave the fixture unchanged in a pull request you want judged on throughput.

## himorime keeps running after Ctrl+C

The first interrupt stops the measured commands and then runs `cleanup` hooks and removes the temporary worktree, which can take a moment. Interrupt a second time to quit immediately. Cleanup that has not run yet is then skipped: a temporary worktree stays in the system temporary directory (`himorime-*`), and once that directory is deleted, the next `himorime compare` prunes its entry from the repository.

## Output of a failing command

The report shows the last lines of the failing run's standard error. To keep the full output of the latest run, set `stdout` or `stderr` to a file inside `${workdir}` and copy it in a `cleanup` hook.

## Very short commands show large relative differences

Below about a millisecond the time is dominated by process creation. Measure a representative input rather than `--version`, and compare results only on the same machine.
