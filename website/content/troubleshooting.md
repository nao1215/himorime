---
title: Troubleshooting
description: Common yahiko errors and what to do about them.
toc: true
---

## Every comparison is inconclusive

The measurements are too noisy, or there are too few of them, for the
tolerance you set. See [Regression detection](/regression-detection/#noise-and-inconclusive-results):
raise `runs`, widen `max_percent`, measure a heavier workload, or use a quieter
machine. The reason is printed under the table.

## "runs (5) is lower than regression.min_samples (10)"

`yahiko compare` and `yahiko ci` refuse settings that could never produce a
verdict. Raise `runs` (or `max_runs` for adaptive runs), or lower
`regression.min_samples`. `yahiko run` does not need either.

## "revision ... is not a commit"

The base revision is not in the clone. Fetch it (`git fetch origin main`), or
in GitHub Actions check out with `fetch-depth: 0`.

## "cannot determine the base revision"

`yahiko ci` runs outside GitHub Actions, or on an event without a base, such
as `schedule`. Pass `--against <ref>` or set `YAHIKO_BASE_REF`.

## "the suite directory does not exist in the base revision"

The suite file was added after the base revision, so the base cannot be built
from it. Commit the suite to the base branch first, or compare against a newer
revision.

## "the build ... did not write ${artifact}"

The build command succeeded but never created the file at `${artifact}`. Pass
`${artifact}` as the build's output path, such as `go build -o "${artifact}"`.

## A command fails with "executable file not found"

A bare program name is looked up in `PATH` (with the benchmark's `env`
applied). Use `${artifact}` for the program the suite builds, or a path
relative to the suite starting with `${root}`.

## A command works in my shell but not in yahiko

Commands written as lists run without a shell: no `~`, globbing, pipes or
`$VAR` expansion. Either pass the expanded values (`${env:HOME}`), or use a
string with `shell: true`.

## "path ... resolves outside the project"

A `cwd` or `stdin` path, after following symbolic links, leaves the Git
repository, the base worktree and `${workdir}`. Keep fixtures inside the
repository, or generate them into `${workdir}` in `setup`.

## yahiko keeps running after Ctrl+C

The first interrupt stops the measured commands and then runs `cleanup` hooks
and removes the temporary worktree, which can take a moment. Interrupt a second
time to quit immediately. Cleanup that has not run yet is then skipped: a
temporary worktree stays in the system temporary directory (`yahiko-*`), and
once that directory is deleted, the next `yahiko compare` prunes its entry
from the repository.

## Output of a failing command

The report shows the last lines of the failing run's standard error. To keep
the full output of the latest run, set `stdout` or `stderr` to a file inside
`${workdir}` and copy it in a `cleanup` hook.

## Very short commands show large relative differences

Below about a millisecond the time is dominated by process creation. Measure a
representative input rather than `--version`, and compare results only on the
same machine.
