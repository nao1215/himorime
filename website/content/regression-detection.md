---
title: Regression detection
description: How himorime decides pass, improved, regression and inconclusive for latency, throughput, CPU time and peak RSS with a percentile bootstrap, and why a single slow run cannot fail CI.
toc: true
---

A comparison has to answer one question per metric: is the head worse than the base by more than you are willing to accept, and is that more than noise? himorime answers it from the raw samples of both revisions, measured interleaved on the same machine in the same job.

## Measuring

`himorime compare` and `himorime ci` check the base revision out into a temporary Git worktree and build it there, and build the head in your working tree. Commands and hooks run in `${root}` of each revision and every relative path resolves there, so the base runs the base's code even without a build step, such as a script run by an interpreter. A file both revisions must share, such as a fixture, is written with `${head_root}`; see [Paths](/configuration/#paths).

A fresh checkout is not the same as a working tree that has existed for a while: Git cannot trust the cached state of files written in the same second as its index, and checks their content again on every `git status`. Before measuring, himorime waits for that second to pass and refreshes the index of the base worktree, so a command that asks Git about its repository costs the same in both revisions.

Every round runs each compared command once for each revision, in an order shuffled with the seed. A background job that slows the machine for a few seconds therefore slows both revisions, instead of all of one of them. Throughput, CPU time and peak RSS are recorded by the same runs, so the interleaving protects every metric. Adaptive runs continue until each revision has `min_samples` samples, so a comparison is never inconclusive only because measuring stopped early.

## Metrics and directions

| Metric | Compared value | Worse when | Settings |
|---|---|---|---|
| latency | the statistic of the run times | it grows | `regression.latency` |
| throughput | work / the selected latency statistic | it shrinks | derived from `regression.latency` |
| CPU time | the statistic of user + system time | it grows | `regression.cpu_total` |
| peak RSS | the statistic of the runs' peaks | it grows | `regression.peak_rss` |

CPU utilization, user and system time are reported but not compared: they are parts of CPU time, or have no better direction. Latency, CPU and memory each get their own verdict; the command's result is the worst verdict of the gated metrics, so a memory regression fails the check even when latency passed. Throughput derives its verdict from latency and never gates or counts a second time. A throughput budget still fails a run independently.

## The statistic

Revision comparisons support median (the default) or mean, not p95/p99. To guard slow executions, combine the median comparison with an absolute `budget.<command>.latency` percentile limit, such as `{p95: "<= 500ms", p99: "<= 1s"}`. A head-side budget can fail even when the median passes. It checks a latency requirement, not a statistically confirmed change from the base: an unchanged heavy tail can also exceed it.

Percentiles need enough observations of the slow cases. With ten runs, p99 is near the observed maximum; if none hits the rare slow path, no percentile can recover it. Use a larger fixed `runs` count for tail checks; meeting adaptive run-count and duration limits does not establish adequate tail sampling. A relative tail-regression gate would need separate sample-size guidance and false-alarm calibration; changing the statistic name alone would not provide that.

For each compared metric of each command, himorime computes the observed change of the selected statistic:

```text
change = (head - base) / base × 100%
degradation = change        for latency, CPU time and peak RSS
degradation = -change       for throughput
```

For independent metrics, `max_percent` is the tolerated increase. The throughput row converts latency's change and confidence interval through the reciprocal, reversing the interval endpoints, and copies the verdict and probabilities. A +10% latency tolerance is a 9.09% rate drop; the corresponding improvement threshold is an 11.11% rate increase. `max_percent` on the derived row describes the regression threshold only. `min_difference` is applied in latency units, so consult the latency row for that threshold. This remains coherent for both mean and median: the derived value is work / the latency statistic, not the mean or median of per-run rates.

It then runs a percentile bootstrap: 2,000 times, it draws as many samples as each side has, with replacement, from that side's samples, and computes the change of the metric between the two resamples. The resulting distribution of changes gives:

- the central interval at `regression.confidence`, reported as `ci_low_percent` and `ci_high_percent`;
- `probability_regression`, the share of resamples whose degradation exceeds `max_percent`;
- `probability_improvement`, the share whose degradation is below `-max_percent`.

The bootstrap's random generator is seeded from `--seed` and the benchmark, command and metric names, so the same samples always produce the same verdict. The seed of every run is printed and stored in the JSON report.

With `regression.pairing: rounds`, the comparison pairs the samples of one round instead. Round i ran base and head once each, so it takes the ratios head_i / base_i, and the bootstrap draws rounds rather than samples. A slowdown that hits both runs of a round, such as a busy machine, cancels out of the ratio. `change_percent` is then (the statistic of the ratios - 1) × 100, which can differ from the ratio of the two side statistics reported as `base` and `head`. `difference` is that change in the metric's unit, so `min_difference` applies to it, and `max_cv` applies to the spread of the ratios. Every comparison records its pairing in the JSON field `pairing`.

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

A regression therefore needs three things at once: enough samples, an observed degradation beyond the tolerance, and enough evidence that it is not noise. When every head sample exceeds every base sample (or vice versa), within-side dispersion does not erase the observed ordering. Such samples pass the noise gate but still need all the remaining checks, including bootstrap confidence beyond the tolerance. This is evidence about the observed samples, not proof that future executions cannot overlap. The verdict is deterministic: the same samples, settings and seed always give the same verdict and exit status.

## Minimum meaningful change

A percentage alone misleads for small values: 10% of a 2ms command is 200µs, less than a shared runner's jitter, and 10% of a 6MiB peak is a few pages of allocator noise. `min_difference` sets the smallest absolute change that can count, in the metric's own unit:

```yaml
regression:
  latency: {max_percent: 10, min_difference: 2ms}
  cpu_total: {max_percent: 15, min_difference: 5ms}
  peak_rss: {max_percent: 5, min_difference: 1MiB}
```

A change smaller than `min_difference` passes, whatever its percentage.

An absolute [budget](/configuration/#metrics-budgets-and-tolerances) is different: it is a limit you chose, checked on the head's measurements, and exceeding it fails the run whatever the noise. The two complement each other. A comparison catches a 15% slowdown that is still far inside the budget; a budget catches a program that has been getting a little slower in every pull request, each change too small to call, and it works without a base revision.

## What fails the run

A benchmark can carry absolute budgets and compare several metrics, and not every check has to fail CI. Every command gets one result; the exit status follows from the results of all commands.

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

On the pull request that adds a suite, the base revision has no suite file, even when the suite sits in a directory the base already has such as the repository root, so there is nothing to compare: the suite is reported as new in this revision, and only the working tree is built and run. Its budgets and failures count as in `himorime run`, so a broken build exits 4 and an exceeded budget exits 1 on the pull request that adds the suite. Other suites of the same run are compared as usual.

Budgets are always enforced: a budget you do not want to fail on is a budget to remove. A command excluded by `regression.commands` still runs on the head when it has a budget; its JSON `base` and `comparisons` fields are `null`. `gate` exists for comparisons, where a metric can be worth seeing without being worth failing on. To gate on CPU time and memory and only report latency:

```yaml
regression:
  latency: {gate: false}
  cpu_total: {max_percent: 15, min_difference: 5ms}
  peak_rss: {max_percent: 10, min_difference: 4MiB}
```

The latency comparison is still computed with the same bootstrap and shown with its verdict, so a slowdown is visible. It is marked `(NOT GATED)` in tables, has `"gate": false` in JSON and a `gate` column in CSV, becomes a GitHub Actions notice instead of an error, and is counted in `summary.not_gated` rather than in `summary.regression`. A not-gated comparison is never shown as a plain `PASS`, and `--fail-on-inconclusive` ignores it.

## Noise and inconclusive results

Shared CI runners are noisy. Neighboring jobs, CPU frequency scaling, the host's load and cold caches all move timings by more than a small regression. CPU time moves less than latency, because waiting does not count, but frequency scaling and cache contention still change it. Peak RSS is usually steady, but depends on the allocator, the runtime and the input. himorime does not pretend otherwise:

- A result it cannot call is `inconclusive`, with the reason, and exits 0 unless you pass `--fail-on-inconclusive`.
- `max_cv` guards against distributions so spread out that a comparison of them means little, such as one with two modes far apart. It measures the bulk of the samples, so a few slow runs do not trip it.
- Very short commands are dominated by process creation. Below a millisecond or so you are mostly measuring the operating system starting a process, and a 10% tolerance may be smaller than the variation of that start-up. Measure a representative workload instead of `--version` when you can.

Ways to get conclusive results:

- More samples: raise `runs`, or `min_runs` and `min_time`.
- A wider tolerance for noisy benchmarks, such as `max_percent: 20`, and a `min_difference` below which changes do not matter.
- CPU time or peak RSS as the gate, with `latency: {gate: false}`, when waiting time is not what you care about.
- A lower `confidence`, such as `0.9`, if an occasional false alarm is acceptable.
- On a machine whose load changes during the run, such as a busy desktop or a self-hosted runner shared with other work, `pairing: rounds`.
- A quieter machine: a dedicated or larger runner.

## How reliable the verdict is

`internal/stats/calibration_test.go` runs the classifier on synthetic samples from fixed seeds, many trials per scenario, with the default settings (median, `max_percent` 10, `confidence` 0.95, `min_samples` 10, `max_cv` 0.5), once with each pairing. It is deterministic and part of `go test`; `make calibration` runs 1,000 trials per scenario and prints the table. On that data, with the default independent pairing unless stated:

- Identical distributions were called a regression in at most 0.4% of trials (10 samples, 25% coefficient of variation), and an improvement in at most 0.3%. An improvement was never called a regression, and throughput's direction was never confused.
- Degradations of 25% and 50% were called regressions in 99.6 to 100% of trials with 10 or 30 samples and up to 10% noise.
- Changes near the tolerance mostly come out `inconclusive`: +8% was a regression in 0.1% of trials, +10% in 3.8%, +12% in 25.5% with 30 samples and 63.2% with 100. None was confidently called in the wrong direction.
- `inconclusive` grows with noise and shrinks with samples: at 10% noise, 46.7% of identical comparisons with 10 samples, 8.9% with 30 and none with 100.
- Rare slow runs do not hide a verdict. With one, three of 30 or three on each side made 3 to 10 times slower, an unchanged pair passed in 100% of trials, and a real +20% change was called a regression in 99.9 to 100%, with the same numbers as with the gate turned off. With two of 10 the outliers left too few samples for the bootstrap and 33.8% of unchanged trials were inconclusive, none a false alarm.
- How rare is rare depends on `runs`. Three slow runs of 10 are 30% of the samples, they move the quartiles, and the gate fired in every trial. Raise `runs` when a benchmark is short enough that a noisy machine slows a large share of its runs.
- The gate still fires on a distribution that is genuinely wide. With half the samples in a mode five times slower, every trial was inconclusive on the gate, with and without a real +25% change underneath. `pairing: rounds` applies the gate to the per-round ratios instead, so it judged some of these: 27.3% of unchanged pairs passed and 20.6% of the +25% pairs were a regression, never in the wrong direction.
- The independent bootstrap ignores that the runner pairs base and head by round. When the machine drifted during the run by the same factor for both revisions, it produced no false regression but lost power: a step of 30% in the middle of the run made every comparison inconclusive, a real +20% change included. With `pairing: rounds` the same +20% was a regression in 94.1% of trials with 10 samples and 99.9% with 30, and unchanged pairs passed in 96.4 to 100%.
- A slowdown shared by both runs of a round, 25% on top of 3% of each run's own, is what a busy machine does. The independent bootstrap was inconclusive in 94 to 98% of unchanged pairs and found a real +20% in 1.6 to 3.8% of trials; `pairing: rounds` passed every unchanged pair and found the +20% in 99.9 to 100%.
- Without shared noise or drift the two pairings mostly agree within a few points. Bimodal samples are the exception, where rounds was more decisive: 90.3% of unchanged pairs passed against 75.0% with independent, and 97.5% of +25% pairs were a regression against 85.3%. Rounds is less sure where only one side has outliers: with two slow head runs of 10, 46.2% of unchanged pairs passed against 66.2% independent, none a false alarm. At 25% noise and 10 samples, unchanged pairs were a regression in 1.2% of trials with rounds against 0.4% independent. The default stays independent, so existing gates keep their verdicts.

`ci_low_percent` and `ci_high_percent` describe the central interval of the bootstrap distribution. The verdict uses one-sided probabilities, so the interval of a regression can reach below the tolerance, and that of a pass above it; the probabilities, not the interval, decide.

None of this calibrates himorime for a particular CI provider. Real runners have heavier tails, autocorrelated noise, CPU frequency scaling and thermal limits that synthetic data does not model. Before relying on a gate there, compare a revision with itself on that runner (an A/A test) a number of times to see how often it is inconclusive or, rarely, a regression, and compare a change with a known slowdown to see that it is caught.

### One measurement on GitHub-hosted runners

The numbers below are one such A/A test, not a calibration of every runner. On 2026-11-13 a CPU-bound program that sorts numbers for about 110 ms was compared with itself and with builds made 2 to 20% slower, each comparison in a fresh `ubuntu-latest` job, with himorime v0.5.3, the default settings and 20 runs. The workflow and the program are in [ci-bench-noise](https://github.com/nao1215/ci-bench-noise), and a write-up in Japanese with every sample is on [debimate](https://debimate.jp/notes/software-testing/ci-benchmark/).

- In the same job, all 30 A/A comparisons passed, with changes from -0.56% to +0.39%. Slowdowns of 12, 15 and 20% were regressions in all 30 jobs, and 10% was half pass, half inconclusive.
- Across jobs the hardware changes. The 90 jobs ran on seven CPU models, and the median time of the same build ranged from 99 to 143 ms, 29% apart within one model. Comparing the base of one A/A job with the base of another, as a gate against a result stored by an earlier run does, was called a regression or an improvement in 18% of pairs with the same CPU model and 60% with different ones. `compare` and `ci` measure base and head in the same job for this reason.
- Within a job the runners were quiet, with a median coefficient of variation of 0.6%. Re-judged at `max_percent: 3`, the same samples still passed every A/A comparison and caught every 5% slowdown. A CPU-bound suite on these runners can afford a tighter tolerance than the default 10%, after an A/A test of its own; process start-up, I/O and allocation make most real benchmarks noisier.
- On a busy desktop with an 18% median coefficient of variation, 10 A/A comparisons and 10 with a 20% slowdown were measured with `himorime compare` and their samples judged again with each pairing. Independent caught 2 or 3 of the slowdowns, depending on the seed, and was inconclusive in 5 A/A comparisons; rounds caught 8 and passed all 10. Neither raised a false alarm. On the hosted runners, where base and head of a round were only weakly correlated (median 0.19), the two pairings gave nearly the same verdicts.

## What himorime does not claim

himorime measures end-to-end wall-clock time of whole processes, and the CPU time and peak memory the operating system reports for them. It does not isolate CPUs, pin threads, disable frequency scaling, or subtract anything from what it measured. Its results are evidence for a decision in a pull request, not a guarantee or a scientific measurement of your program's absolute performance.
