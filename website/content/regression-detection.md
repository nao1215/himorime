---
title: Regression detection
description: How yahiko decides pass, improved, regression and inconclusive with a percentile bootstrap, and why a single slow run cannot fail CI.
toc: true
---

A comparison has to answer one question: is the head slower than the base by
more than you are willing to accept, and is that more than noise? yahiko
answers it from the raw samples of both revisions, measured interleaved on the
same machine in the same job.

## Measuring

`yahiko compare` and `yahiko ci` build the base revision in a temporary Git
worktree and the head in your working tree. Every round runs each compared
command once for each revision, in an order shuffled with the seed. A
background job that slows the machine for a few seconds therefore slows both
revisions, instead of all of one of them.

## The statistic

For each compared command, yahiko computes the observed change of the metric
(`regression.metric`, median by default):

```text
change = (head - base) / base × 100%
```

It then runs a percentile bootstrap: 2,000 times, it draws as many samples as
each side has, with replacement, from that side's samples, and computes the
change of the metric between the two resamples. The resulting distribution of
changes gives:

- the central interval at `regression.confidence`, reported as `ci_low_percent`
  and `ci_high_percent`;
- `probability_regression`, the share of resampled changes above
  `+max_percent`;
- `probability_improvement`, the share below `-max_percent`.

The bootstrap's random generator is seeded from `--seed` and the benchmark and
command names, so the same samples always produce the same verdict. The seed
of every run is printed and stored in the JSON report.

## The verdict

In order:

| Verdict | When |
|---|---|
| `inconclusive` | Either side has fewer than `min_samples` samples. |
| `inconclusive` | Either side's coefficient of variation (stddev / mean) exceeds `max_cv`. |
| `regression` | The observed change exceeds `+max_percent` and `probability_regression` ≥ `confidence`. |
| `improved` | The observed change is below `-max_percent` and `probability_improvement` ≥ `confidence`. |
| `pass` | The probability that the change is within `+max_percent` is at least `confidence`. |
| `inconclusive` | Anything else: the change is too close to the tolerance to call. |

A regression therefore needs three things at once: enough samples, an observed
slowdown beyond the tolerance, and enough evidence that the slowdown is not
noise. One slow run cannot fail a build; a consistently slow head does.

An absolute [budget](/configuration/) is different: it is a limit you chose,
checked on the head's measurements, and exceeding it fails the run whatever
the noise.

## Noise and inconclusive results

Shared CI runners are noisy. Neighboring jobs, CPU frequency scaling and
cold caches all move timings by more than a small regression. yahiko does not
pretend otherwise:

- A result it cannot call is `inconclusive`, with the reason, and exits 0
  unless you pass `--fail-on-inconclusive`.
- `max_cv` guards against distributions so spread out (for example bimodal)
  that a median comparison means little.
- Very short commands are dominated by process creation. Below a millisecond
  or so you are mostly measuring the operating system starting a process, and
  a 10% tolerance may be smaller than the variation of that start-up. Measure
  a representative workload instead of `--version` when you can.

Ways to get conclusive results:

- More samples: raise `runs`, or `min_runs` and `min_time`.
- A wider tolerance for noisy benchmarks, such as `max_percent: 20`.
- A lower `confidence`, such as `0.9`, if an occasional false alarm is
  acceptable.
- A quieter machine: a dedicated or larger runner.

## What yahiko does not claim

yahiko measures end-to-end wall-clock time of whole processes. It does not
isolate CPUs, pin threads, disable frequency scaling, or subtract anything
from what it measured. Its results are evidence for a decision in a pull
request, not a scientific measurement of your program's absolute speed.
