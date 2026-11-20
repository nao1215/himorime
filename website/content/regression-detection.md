---
title: Regression detection
description: How himorime decides pass, improved, regression and inconclusive for latency, throughput, CPU time and peak RSS with a percentile bootstrap, and why a single slow run cannot fail CI.
toc: true
---

For each metric, a comparison asks whether the head is worse than the base by more than the tolerance, and whether that difference is larger than noise. himorime answers from the raw samples of both revisions, measured interleaved in the same job.

## Measuring

`himorime compare` and `himorime ci` build the base in a temporary Git worktree and the head in your working tree. Commands and hooks run in `${root}` of each revision, so the base runs the base's code even without a build step. Write a file both revisions share, such as a fixture, with `${head_root}`; see [Paths](/configuration/#paths).

Git re-checks the content of files written in the same second as its index on every `git status`. himorime waits for that second to pass and refreshes the base worktree's index before measuring, so a command that queries Git costs the same in both revisions.

Each round runs every compared command once per revision, in an order shuffled with the seed, so a background job that slows the machine for a few seconds hits both revisions. All metrics come from the same runs. Adaptive runs continue until each revision has `min_samples` samples.

## Metrics and directions

| Metric | Compared value | Worse when | Settings |
|---|---|---|---|
| latency | the statistic of the run times | it grows | `regression.latency` |
| throughput | work / the selected latency statistic | it shrinks | derived from `regression.latency` |
| CPU time | the statistic of user + system time | it grows | `regression.cpu_total` |
| peak RSS | the statistic of the runs' peaks | it grows | `regression.peak_rss` |

CPU utilization, user and system time are reported but not compared. Latency, CPU time and peak RSS each get a verdict, and the command's result is the worst verdict of the gated metrics. Throughput copies latency's verdict and never gates on its own, though a throughput budget can still fail a run.

## The statistic

Comparisons use the median (the default) or the mean, not p95/p99. To guard slow executions, add an absolute percentile budget such as `budget.<command>.latency: {p95: "<= 500ms", p99: "<= 1s"}`. It checks the head only, so an unchanged heavy tail can fail it too. With ten runs p99 is close to the maximum, so use a larger fixed `runs` for tail budgets.

For each compared metric, himorime computes the change of the statistic:

```text
change = (head - base) / base × 100%
degradation = change        for latency, CPU time and peak RSS
degradation = -change       for throughput
```

`max_percent` is the tolerated increase. The throughput row converts latency's change and interval through the reciprocal: a +10% latency tolerance is a 9.09% rate drop, and the improvement threshold an 11.11% rate increase. Its `min_difference` is in latency units. The derived value is work / the latency statistic, not a statistic of per-run rates.

A percentile bootstrap then resamples each side 2,000 times, with replacement, and computes the change between the resamples. That distribution gives:

- the central interval at `regression.confidence`, reported as `ci_low_percent` and `ci_high_percent`;
- `probability_regression`, the share of resamples whose degradation exceeds `max_percent`;
- `probability_improvement`, the share whose degradation is below `-max_percent`.

The bootstrap is seeded from `--seed` and the benchmark, command and metric names, so the same samples give the same verdict. The seed is printed and stored in the JSON report.

With `regression.pairing: rounds`, the bootstrap draws rounds and uses the ratios head_i / base_i, so a slowdown that hits both runs of a round cancels out. `change_percent` is then (the statistic of the ratios - 1) × 100, which can differ from the ratio of `base` and `head`. `min_difference` applies to that change in the metric's unit, and `max_cv` to the spread of the ratios. The JSON field `pairing` records the choice.

## The verdict

In order:

| Verdict | When |
|---|---|
| `inconclusive` | With `pairing: rounds`, the sides do not have one sample per round, or a base sample is zero so its ratio is undefined; one such sample makes the whole metric inconclusive. |
| `inconclusive` | Either side has fewer than `min_samples` samples. |
| `inconclusive` | Either side's spread exceeds `max_cv` and the observed ranges overlap or touch: the interquartile range divided by 1.349 and by the median, or the coefficient of variation (stddev / mean) when the statistic is `mean`. With `pairing: rounds`, the spread of the per-round ratios exceeds `max_cv`, whatever the ranges. |
| `inconclusive` | A bootstrap draw has a zero base statistic, so its relative change is undefined; confidence and interval estimates are unavailable (their numeric fields remain zero). |
| `pass` | `min_difference` is set and the absolute difference is smaller. |
| `regression` | The observed degradation exceeds `max_percent` and `probability_regression` ≥ `confidence`. |
| `improved` | The observed degradation is below `-max_percent` and `probability_improvement` ≥ `confidence`. |
| `pass` | The probability that the degradation is within `max_percent` is at least `confidence`. |
| `inconclusive` | Anything else: the change is too close to the tolerance to call. |
| `skipped` | The metric was unsupported on one side, both RSS statistics were at their floors, or derived throughput lacked constant equal work; it is not judged. |

A regression needs enough samples, a degradation beyond the tolerance, and bootstrap confidence that it is not noise. When the sample ranges do not overlap, a wide spread does not trip `max_cv`, but the other checks still apply. The same samples, settings and seed always give the same verdict and exit status.

## Minimum meaningful change

10% of a 2ms command is 200µs, less than a shared runner's jitter. `min_difference` sets the smallest absolute change that counts, in the metric's unit:

```yaml
regression:
  latency: {max_percent: 10, min_difference: 2ms}
  cpu_total: {max_percent: 15, min_difference: 5ms}
  peak_rss: {max_percent: 5, min_difference: 1MiB}
```

A smaller change passes, whatever its percentage.

An absolute [budget](/configuration/#metrics-budgets-and-tolerances) is checked on the head only and fails the run whatever the noise. A comparison catches a 15% slowdown well inside the budget; a budget catches slow drift across many pull requests and needs no base revision.

## What fails the run

Every command gets one result, and the exit status follows from all of them.

| Check | Outcome | Command result | Exit status |
|---|---|---|---|
| Budget | exceeded | `over_budget` | 1 |
| Budget | its metric is unsupported and `metrics.unsupported: skip` | unchanged, budget `skipped` | 0 |
| Budget | its measured value cannot be assessed, such as an RSS bound below the floor | `metric_error`, budget `skipped` | 6 |
| Gated comparison (`gate: true`, the default) | `regression` | `regression` | 1 |
| Gated comparison | `inconclusive` | `inconclusive` | 0, or 1 with `--fail-on-inconclusive` |
| Gated comparison | `pass`, `improved` | `pass`, `improved` | 0 |
| Comparison with `gate: false` | any verdict | unchanged; shown as `REGRESSION (NOT GATED)` and so on, counted in `summary.not_gated` | 0 |
| Comparison | its metric is unsupported and `metrics.unsupported: skip` | unchanged, comparison `skipped`, counted in `summary.skipped` | 0 |
| Any metric | unsupported and `metrics.unsupported: fail` (the default) | nothing runs | 6 |
| Any metric | the platform supports it but reading it failed | `metric_error` | 6 |
| The command | failed, timed out, or a hook failed | `error` | 4 |
| The suite | its file does not exist in the base revision | the working tree is measured and judged as by `himorime run`; `new_in_head`, counted in `summary.new_suites` | as by `himorime run` |

Budgets are always enforced. A command excluded by `regression.commands` still runs on the head when it has a budget, with `base` and `comparisons` set to `null` in JSON. A comparison with `gate: false` is marked `(NOT GATED)` in tables, `"gate": false` in JSON and a `gate` column in CSV, becomes a GitHub Actions notice, and is ignored by `--fail-on-inconclusive`; see [Gate on CPU time and memory, report latency only](/cookbook/gate-on-cpu-time-and-memory-report-latency-only/).

## Noise and inconclusive results

On shared CI runners, neighboring jobs, frequency scaling and cold caches move timings by more than a small regression. CPU time moves less than latency because waiting does not count; peak RSS is usually steady. A result himorime cannot call is `inconclusive`, with the reason, and exits 0 unless you pass `--fail-on-inconclusive`. `max_cv` measures the bulk of the samples, so a few slow runs do not trip it, but a distribution with two modes far apart does. Below about a millisecond you mostly measure process creation, so benchmark a real workload rather than `--version`.

To get conclusive results:

- Raise `runs`, or `min_runs` and `min_time`.
- Widen the tolerance (`max_percent: 20`) and set `min_difference`.
- Gate on CPU time or peak RSS with `latency: {gate: false}`.
- Lower `confidence` to `0.9` if an occasional false alarm is acceptable.
- Use `pairing: rounds` on a machine whose load changes during the run.
- Use a dedicated or larger runner.

## How reliable the verdict is

`internal/stats/calibration_test.go` runs the classifier on seeded synthetic samples with the default settings (median, `max_percent` 10, `confidence` 0.95, `min_samples` 10, `max_cv` 0.5), once per pairing, as part of `go test`. `make calibration` runs 1,000 trials per scenario and prints the table. With independent pairing unless stated:

- Identical distributions were called a regression in at most 0.4% of trials (10 samples, 25% coefficient of variation), and an improvement in at most 0.3%. An improvement was never called a regression, and throughput's direction was never confused.
- Degradations of 25% and 50% were called regressions in 99.6 to 100% of trials with 10 or 30 samples and up to 10% noise.
- Changes near the tolerance mostly come out `inconclusive`: +8% was a regression in 0.1% of trials, +10% in 3.8%, +12% in 25.5% with 30 samples and 63.2% with 100. None was confidently called in the wrong direction.
- `inconclusive` grows with noise and shrinks with samples: at 10% noise, 46.7% of identical comparisons with 10 samples, 8.9% with 30 and none with 100.
- With one slow run of 30, or three of 30, or three on each side (3 to 10 times slower), unchanged pairs passed in 100% of trials and a +20% change was a regression in 99.9 to 100%. With two of 10, 33.8% of unchanged trials were inconclusive, none a false alarm.
- Three slow runs of 10 are 30% of the samples and tripped the gate in every trial. Raise `runs` for short benchmarks on noisy machines.
- With half the samples in a mode five times slower, every trial was inconclusive, with or without a +25% change. With `pairing: rounds`, 27.3% of unchanged pairs passed and 20.6% of +25% pairs were a regression, none in the wrong direction.
- A 30% step in the machine's speed mid-run made every independent comparison inconclusive, a +20% change included. With `pairing: rounds` that +20% was a regression in 94.1% of trials with 10 samples and 99.9% with 30, and unchanged pairs passed in 96.4 to 100%.
- With a 25% slowdown shared by both runs of a round, on top of 3% per-run noise, independent pairing was inconclusive in 94 to 98% of unchanged pairs and found a +20% in 1.6 to 3.8%; rounds passed every unchanged pair and found the +20% in 99.9 to 100%.
- Otherwise the pairings agree within a few points. On bimodal samples rounds passed 90.3% of unchanged pairs against 75.0%, and caught 97.5% of +25% pairs against 85.3%. With two slow head runs of 10, rounds passed 46.2% of unchanged pairs against 66.2%, none a false alarm. At 25% noise and 10 samples, rounds gave a false regression in 1.2% of trials against 0.4%. The default stays independent.

`ci_low_percent` and `ci_high_percent` are the central bootstrap interval. The verdict uses one-sided probabilities, so a regression's interval can reach below the tolerance.

Synthetic data does not model real runners' heavy tails, autocorrelated noise and thermal limits. Before relying on a gate, compare a revision with itself on that runner (an A/A test) several times, and check that a known slowdown is caught.

### One measurement on GitHub-hosted runners

On 2026-11-13 a CPU-bound program that sorts numbers for about 110 ms was compared with itself and with builds 2 to 20% slower, each in a fresh `ubuntu-latest` job, with himorime v0.5.3, the default settings and 20 runs. The workflow and the program are in [ci-bench-noise](https://github.com/nao1215/ci-bench-noise), and a write-up in Japanese with every sample is on [debimate](https://debimate.jp/notes/software-testing/ci-benchmark/).

- In the same job, all 30 A/A comparisons passed, with changes from -0.56% to +0.39%. Slowdowns of 12, 15 and 20% were regressions in all 30 jobs, and 10% was half pass, half inconclusive.
- The 90 jobs ran on seven CPU models, and the median time of the same build ranged from 99 to 143 ms, 29% apart within one model. Comparing the bases of two different jobs gave a regression or improvement in 18% of pairs with the same CPU model and 60% with different ones, which is why `compare` and `ci` measure both revisions in one job.
- Within a job the median coefficient of variation was 0.6%. Re-judged at `max_percent: 3`, the samples still passed every A/A comparison and caught every 5% slowdown. Most real benchmarks are noisier because of process start-up, I/O and allocation.
- On a busy desktop with an 18% median coefficient of variation, 10 A/A comparisons and 10 with a 20% slowdown were judged with each pairing. Independent caught 2 or 3 slowdowns, depending on the seed, and was inconclusive in 5 A/A comparisons; rounds caught 8 and passed all 10. Neither raised a false alarm. On the hosted runners, where a round's base and head were weakly correlated (median 0.19), the pairings nearly agreed.

## What himorime does not claim

himorime measures the wall-clock time of whole processes and the CPU time and peak memory the operating system reports. It does not isolate CPUs, pin threads, disable frequency scaling, or subtract overhead. Treat the results as evidence for a pull request decision, not an absolute measurement.
