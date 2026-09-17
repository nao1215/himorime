---
description: yahiko measures command-line programs from a versioned YAML suite kept in your repository, compares them with each other or with a Git base revision, checks performance budgets and fails CI on confirmed regressions.
---

yahiko keeps a benchmark suite for your command-line program in the
repository, runs it the same way on a laptop and in CI, and tells you
whether a change made things slower. It compares commands with each other,
compares a Git revision with your working tree, checks the budgets you set,
and exits non-zero on a confirmed regression.

```console
$ yahiko compare --against main
suite: jsonize benchmarks (yahiko.yaml)
BENCHMARK     BASE     HEAD  CHANGE  CONFIDENCE  BUDGET  RESULT
df small    1.84ms   1.89ms   +2.7%         low    +10%  PASS
df large   14.20ms  16.41ms  +15.6%       98.1%    +10%  REGRESSION
```

## Where to go

| I want to | Page |
|---|---|
| try it in a minute | [Getting started](/getting-started/) |
| install it | [Installation](/install/) |
| write a suite | [Configuration](/configuration/) |
| look up a command or flag | [Commands](/commands/) |
| choose an output format | [Reports](/reports/) |
| understand PASS, REGRESSION and INCONCLUSIVE | [Regression detection](/regression-detection/) |
| run it on pull requests | [GitHub Actions](/github-actions/) |
| copy a working example | [Cookbook](/cookbook/) |
| decide whether yahiko fits | [Comparison](/comparison/) |
| script around exit statuses | [Exit codes](/exit-codes/) |
| fix a problem | [Troubleshooting](/troubleshooting/) |

## What yahiko measures, and what it does not

yahiko measures the wall-clock time of a whole process, from starting it to
reaping it, exactly as you wrote the command. It does not subtract shell
start-up time, it does not profile, and it cannot remove the noise of a
shared CI runner. It reduces the damage that noise does: commands run
interleaved, a regression needs statistical confidence, and a result that
cannot be told apart from noise is reported as inconclusive rather than
passed or failed. See [Regression detection](/regression-detection/).
