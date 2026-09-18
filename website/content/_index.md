---
description: himorime measures the latency, throughput, CPU time and peak RSS of a command-line program from a YAML suite kept in your repository, checks budgets and fails CI when a change makes it worse.
---

himorime measures how fast and how lean a command-line program is. You keep the benchmarks, budgets and tolerances in a `himorime.yaml` next to your code. The same file and the same command run on a laptop and in GitHub Actions, and the exit status tells CI whether performance got worse.

```console
$ himorime compare --against main
suite: jsonize benchmarks (himorime.yaml)
latency
BENCHMARK     BASE     HEAD      DIFF  CHANGE  CONFIDENCE  TOLERANCE  RESULT
df small    1.84ms   1.89ms  +50.00µs   +2.7%      100.0%       +10%  PASS
df large   14.20ms  16.41ms   +2.21ms  +15.6%       98.1%       +10%  REGRESSION

peak rss
BENCHMARK       BASE      HEAD      DIFF  CHANGE  CONFIDENCE  TOLERANCE  RESULT
df small     9.82MiB   9.84MiB  +16.00KiB  +0.2%      100.0%        +5%  PASS
df large    31.40MiB  31.52MiB  +128.00KiB  +0.4%      100.0%        +5%  PASS
```

It measures:

- latency: wall-clock time of each run, with percentiles;
- throughput: work you declare (records, bytes of a file) per second;
- CPU time and utilization: user and system time of the process tree;
- peak RSS: the largest resident memory of any process in the tree.

Each metric can have absolute budgets (`p95 <= 100ms`, `>= 50MiB/s`, `<= 64MiB`) and a tolerance against a Git base revision, judged in the direction that is worse for it.

## Where to go

| I want to | Page |
|---|---|
| try it in a minute | [Getting started](/getting-started/) |
| install it | [Installation](/install/) |
| write a suite | [Configuration](/configuration/) |
| understand what each metric includes | [Metrics](/metrics/) |
| look up a command or flag | [Commands](/commands/) |
| choose an output format | [Reports](/reports/) |
| understand PASS, REGRESSION and INCONCLUSIVE | [Regression detection](/regression-detection/) |
| run it on pull requests | [GitHub Actions](/github-actions/) |
| copy a working example | [Cookbook](/cookbook/) |
| decide whether himorime fits | [Comparison](/comparison/) |
| script around exit statuses | [Exit codes](/exit-codes/) |
| fix a problem | [Troubleshooting](/troubleshooting/) |

## What himorime measures, and what it does not

himorime runs the command exactly as you wrote it, measures the whole process from start to reaping, and reads CPU time and peak memory from the operating system when it exits. It does not profile, it does not see inside a language runtime, and it cannot remove the noise of a shared CI runner. It reduces the damage that noise does: revisions run interleaved, a regression needs statistical confidence, and a result that cannot be told apart from noise is reported as inconclusive rather than passed or failed. Its results are evidence for a decision in a pull request, not a guarantee. See [Regression detection](/regression-detection/) and [Metrics](/metrics/).

himorime checks performance. To check that a CLI *behaves* correctly — exit codes, output, files — use [atago](https://github.com/nao1215/atago).
