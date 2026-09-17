---
title: Regression detection
description: How yahiko decides pass, improved, regression and inconclusive for latency, throughput, CPU time and peak RSS with a percentile bootstrap, and why a single slow run cannot fail CI.
toc: true
---

A comparison has to answer one question per metric: is the head worse than
the base by more than you are willing to accept, and is that more than noise?
yahiko answers it from the raw samples of both revisions, measured interleaved
on the same machine in the same job.

## Measuring

`yahiko compare` and `yahiko ci` build the base revision in a temporary Git
worktree and the head in your working tree. Every round runs each compared
command once for each revision, in an order shuffled with the seed. A
background job that slows the machine for a few seconds therefore slows both
revisions, instead of all of one of them. Throughput, CPU time and peak RSS
are recorded by the same runs, so the interleaving protects every metric.

## Metrics and directions

| Metric | Compared value | Worse when | Settings |
|---|---|---|---|
| latency | `regression.metric` of the run times | it grows | `max_percent`, `min_difference` |
| throughput | the statistic of work / latency per run | it shrinks | `regression.throughput` |
| CPU time | the statistic of user + system time | it grows | `regression.cpu` |
| peak RSS | the statistic of the runs' peaks | it grows | `regression.memory` |

CPU utilization is reported but not compared: it has no better direction.
Each compared metric gets its own verdict; the command's result is the worst
of them, so a memory regression fails the check even when latency passed.

## The statistic

For each compared metric of each command, yahiko computes the observed
change of the statistic (median by default, or mean):

```text
change = (head - base) / base × 100%
degradation = change        for latency, CPU time and peak RSS
degradation = -change       for throughput
```

The same `max_percent` therefore means the same thing for every metric: how
much worse it may get. A throughput drop of 8% and a latency increase of 8%
are both a degradation of 8%; a throughput increase is an improvement.

It then runs a percentile bootstrap: 2,000 times, it draws as many samples as
each side has, with replacement, from that side's samples, and computes the
change of the metric between the two resamples. The resulting distribution of
changes gives:

- the central interval at `regression.confidence`, reported as `ci_low_percent`
  and `ci_high_percent`;
- `probability_regression`, the share of resamples whose degradation exceeds
  `max_percent`;
- `probability_improvement`, the share whose degradation is below
  `-max_percent`.

The bootstrap's random generator is seeded from `--seed` and the benchmark and
command names, so the same samples always produce the same verdict. The seed
of every run is printed and stored in the JSON report.

## The verdict

In order:

| Verdict | When |
|---|---|
| `inconclusive` | Either side has fewer than `min_samples` samples. |
| `inconclusive` | Either side's coefficient of variation (stddev / mean) exceeds `max_cv`. |
| `pass` | `min_difference` is set and the absolute difference is smaller. |
| `regression` | The observed degradation exceeds `max_percent` and `probability_regression` ≥ `confidence`. |
| `improved` | The observed degradation is below `-max_percent` and `probability_improvement` ≥ `confidence`. |
| `pass` | The probability that the degradation is within `max_percent` is at least `confidence`. |
| `inconclusive` | Anything else: the change is too close to the tolerance to call. |
| `skipped` | The metric was unsupported on one side (`metrics.unsupported: skip`); it is not judged. |

A regression therefore needs three things at once: enough samples, an observed
degradation beyond the tolerance, and enough evidence that it is not noise.
One slow run cannot fail a build; a consistently slow head does. The verdict is
deterministic: the same samples, settings and seed always give the same verdict
and exit status.

## Minimum meaningful change

A percentage alone misleads for small values: 10% of a 2ms command is 200µs,
less than a shared runner's jitter, and 10% of a 6MiB peak is a few pages of
allocator noise. `min_difference` sets the smallest absolute change that can
count, in the metric's own unit:

```yaml
regression:
  max_percent: 10
  min_difference: 2ms
  throughput: {max_percent: 10, min_difference: "1000 records/s"}
  cpu: {max_percent: 15, min_difference: 5ms}
  memory: {max_percent: 5, min_difference: 1MiB}
```

A change smaller than `min_difference` passes, whatever its percentage.

An absolute [budget](/configuration/#metrics-budgets-and-tolerances) is
different: it is a limit you chose, checked on the head's measurements, and
exceeding it fails the run whatever the noise. The two complement each other.
A comparison catches a 15% slowdown that is still far inside the budget; a
budget catches a program that has been getting a little slower in every pull
request, each change too small to call, and it works without a base revision.

## Noise and inconclusive results

Shared CI runners are noisy. Neighboring jobs, CPU frequency scaling, the
host's load and cold caches all move timings by more than a small regression.
CPU time moves less than latency, because waiting does not count, but
frequency scaling and cache contention still change it. Peak RSS is usually
steady, but depends on the allocator, the runtime and the input. yahiko does
not pretend otherwise:

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
- A wider tolerance for noisy benchmarks, such as `max_percent: 20`, and a
  `min_difference` below which changes do not matter.
- CPU time or peak RSS as the gate, with latency as information, when waiting
  time is not what you care about.
- A lower `confidence`, such as `0.9`, if an occasional false alarm is
  acceptable.
- A quieter machine: a dedicated or larger runner.

## What yahiko does not claim

yahiko measures end-to-end wall-clock time of whole processes, and the CPU
time and peak memory the operating system reports for them. It does not
isolate CPUs, pin threads, disable frequency scaling, or subtract anything
from what it measured. Its results are evidence for a decision in a pull
request, not a guarantee or a scientific measurement of your program's
absolute performance.
