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

`yahiko compare` and `yahiko ci` check the base revision out into a temporary
Git worktree and build it there, and build the head in your working tree.
Commands and hooks run in `${root}` of each revision and every relative path
resolves there, so the base runs the base's code even without a build step,
such as a script run by an interpreter. A file both revisions must share,
such as a fixture, is written with `${head_root}`; see
[Paths](/configuration/#paths).

Every round runs each compared command once for each revision, in an order
shuffled with the seed. A background job that slows the machine for a few
seconds therefore slows both revisions, instead of all of one of them.
Throughput, CPU time and peak RSS are recorded by the same runs, so the
interleaving protects every metric. Adaptive runs continue until each
revision has `min_samples` samples, so a comparison is never inconclusive
only because measuring stopped early.

## Metrics and directions

| Metric | Compared value | Worse when | Settings |
|---|---|---|---|
| latency | the statistic of the run times | it grows | `regression.latency` |
| throughput | the statistic of work / latency per run | it shrinks | `regression.throughput` |
| CPU time | the statistic of user + system time | it grows | `regression.cpu` |
| peak RSS | the statistic of the runs' peaks | it grows | `regression.memory` |

CPU utilization, user and system time are reported but not compared: they
are parts of CPU time, or have no better direction. Each compared metric gets
its own verdict; the command's result is the worst verdict of the gated
metrics, so a memory regression fails the check even when latency passed.

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

The bootstrap's random generator is seeded from `--seed` and the benchmark,
command and metric names, so the same samples always produce the same verdict. The seed
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
  latency: {max_percent: 10, min_difference: 2ms}
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

## What fails the run

A benchmark can carry absolute budgets and compare several metrics, and not
every check has to fail CI. Every command gets one result; the exit status
follows from the results of all commands.

| Check | Outcome | Command result | Exit status |
|---|---|---|---|
| Budget | exceeded | `over_budget` | 1 |
| Budget | its metric is unsupported and `metrics.unsupported: skip` | unchanged, budget `skipped` | 0 |
| Gated comparison (`gate: true`, the default) | `regression` | `regression` | 1 |
| Gated comparison | `inconclusive` | `inconclusive` | 0, or 1 with `--fail-on-inconclusive` |
| Gated comparison | `pass`, `improved` | `pass`, `improved` | 0 |
| Comparison with `gate: false` | any verdict | unchanged; shown as `REGRESSION (NOT GATED)` and so on, counted in `summary.not_gated` | 0 |
| Comparison | its metric is unsupported and `metrics.unsupported: skip` | unchanged, comparison `skipped`, counted in `summary.skipped` | 0 |
| Any metric | unsupported and `metrics.unsupported: fail` (the default) | nothing runs | 6 |
| Any metric | the platform supports it but reading it failed | `metric_error` | 6 |
| The command | failed, timed out, or a hook failed | `error` | 4 |

Budgets are always enforced: a budget you do not want to fail on is a budget
to remove. `gate` exists for comparisons, where a metric can be worth seeing
without being worth failing on. To gate on CPU time and memory and only
report latency:

```yaml
regression:
  latency: {gate: false}
  cpu: {max_percent: 15, min_difference: 5ms}
  memory: {max_percent: 10, min_difference: 4MiB}
```

The latency comparison is still computed with the same bootstrap and shown
with its verdict, so a slowdown is visible. It is marked `(NOT GATED)` in
tables, has `"gate": false` in JSON and a `gate` column in CSV, becomes a
GitHub Actions notice instead of an error, and is counted in
`summary.not_gated` rather than in `summary.regression`. A not-gated
comparison is never shown as a plain `PASS`, and `--fail-on-inconclusive`
ignores it.

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
- CPU time or peak RSS as the gate, with `latency: {gate: false}`, when
  waiting time is not what you care about.
- A lower `confidence`, such as `0.9`, if an occasional false alarm is
  acceptable.
- A quieter machine: a dedicated or larger runner.

## How reliable the verdict is

`internal/stats/calibration_test.go` runs the classifier on synthetic samples
from fixed seeds, many trials per scenario, with the default settings
(median, `max_percent` 10, `confidence` 0.95, `min_samples` 10, `max_cv` 0.5).
It is deterministic and part of `go test`; `make calibration` runs 1,000
trials per scenario and prints the table. On that data:

- Identical distributions were called a regression in at most 0.4% of trials
  (10 samples, 25% coefficient of variation), and an improvement in at most
  0.3%. An improvement was never called a regression, and throughput's
  direction was never confused.
- Degradations of 25% and 50% were called regressions in 99.6 to 100% of
  trials with 10 or 30 samples and up to 10% noise.
- Changes near the tolerance mostly come out `inconclusive`: +8% was a
  regression in 0.1% of trials, +10% in 3.8%, +12% in 25.5% with 30 samples
  and 63.2% with 100. None was confidently called in the wrong direction.
- `inconclusive` grows with noise and shrinks with samples: at 10% noise,
  46.7% of identical comparisons with 10 samples, 8.9% with 30 and none with
  100.
- `max_cv` uses the standard deviation, so a few outliers make a comparison
  inconclusive even when the median is barely affected: one sample 3 to 10
  times slower among 30 did so in 85% of trials, three in all of them, hiding
  a real +20% change too. With `max_cv: 0` the median comparison called that
  change a regression in 99.9% of trials. Consider `max_cv: 0` when rare
  outliers are expected and the metric is the median.
- The bootstrap resamples base and head independently, although the runner
  pairs them by round. When the machine drifted during the run by the same
  factor for both revisions, the independent bootstrap produced no false
  regression, but lost power: a step of 30% in the middle of the run made
  every comparison inconclusive, a real +20% change included, where a paired
  resampling of rounds, evaluated in the test for comparison, detected it in
  82 to 95% of trials. Without drift both behaved the same within the Monte
  Carlo error. yahiko keeps the independent bootstrap: it errs toward
  `inconclusive`, and whether pairing is worth its assumptions depends on how
  much real runners drift, which synthetic data cannot tell.

`ci_low_percent` and `ci_high_percent` describe the central interval of the
bootstrap distribution. The verdict uses one-sided probabilities, so the
interval of a regression can reach below the tolerance, and that of a pass
above it; the probabilities, not the interval, decide.

None of this calibrates yahiko for a particular CI provider. Real runners
have heavier tails, autocorrelated noise, CPU frequency scaling and thermal
limits that synthetic data does not model. Before relying on a gate there,
compare a revision with itself on that runner (an A/A test) a number of
times to see how often it is inconclusive or, rarely, a regression, and
compare a change with a known slowdown to see that it is caught.

## What yahiko does not claim

yahiko measures end-to-end wall-clock time of whole processes, and the CPU
time and peak memory the operating system reports for them. It does not
isolate CPUs, pin threads, disable frequency scaling, or subtract anything
from what it measured. Its results are evidence for a decision in a pull
request, not a guarantee or a scientific measurement of your program's
absolute performance.
