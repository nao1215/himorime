# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- A tool comparison page covering himorime, hyperfine, benchstat, Bencher and CodSpeed, including their different uses and limitations.

### Changed

- The version 1 suite format has a breaking layout change: move `suite` fields to the top level, put budgets under each command, use flat CPU and memory metric names, and rename regression `metric` to `statistic`; migrate existing suites before using the new parser.

### Fixed

- Diagnostics distinguish Git failures, output failures and interruptions without changing exit statuses or JSON reports.
- The dogfood version benchmark skips RSS collection while all other dogfood benchmarks retain CPU and memory metrics.
- All paths after `--` are treated as operands, including multiple paths beginning with a dash.
- Inconclusive-only comparison failures no longer claim that a performance threshold was violated.
- Corrected stale configuration and exit-status documentation and the rendering of metric gate names in the reference.
- A required absolute budget that cannot be assessed, such as a peak RSS bound below the collector floor, now reports `metric_error` and exits 6 instead of silently passing. The budget remains `skipped` with its concrete reason; an explicit `metrics.unsupported: skip` waiver still exits 0.
- Exit 6 explanations now distinguish an unassessed required budget from a metric collection failure.

## [0.3.0] - 2026-09-19

### Added

- Tool errors include an `HMR` diagnostic code on stderr, documented in the reference; exit statuses and report formats are unchanged.

### Changed

- PR benchmark comments show the overall status, problem rows and a full-report link. Complete results, judgement settings and metadata appear as tables in Actions logs and Job Summaries; measurement, verdict, JSON and comment replacement contracts are unchanged.
- setup-himorime posts comments automatically for same-repository PRs from `$RUNNER_TEMP/himorime.json`; grant the benchmark job `pull-requests: write`. Fork PRs receive no comment.

### Removed

- Remove the local `comment` command and `himorime-comment` helper. Use the pinned setup-himorime action and remove the separate `workflow_run` reporter.

### Fixed

- Suite report outputs cannot follow symbolic links outside the suite directory, and conflicting report destinations are rejected before measuring.
- Failed builds and hooks include a redacted stdout tail in their error message as well as the existing stderr diagnostics.
- Revision comparisons enforce head-side budgets even for commands excluded by `regression.commands`.
- Benchmark filters skip builds, version commands and report outputs of suites with no selected benchmarks.
- New suites measured only in the working tree accept run counts below the comparison's `min_samples` requirement.

## [0.2.2] - 2026-09-19

### Added

- `himorime comment REPORT.json` creates or updates a compact benchmark report on the source pull request after a trusted `workflow_run`. The documented two-workflow setup keeps benchmark commands read-only, preserves the JSON report as `himorime-report`, and can comment on fork pull requests without checking out their code. Comments include a small himorime footer by default; `--hide-footer` omits it.
- `bench/thirdparty/compression` measures gzip, bzip2 and xz over the same generated input, verifying decompressed output before measuring.

## [0.2.1] - 2026-09-19

### Changed

- Throughput comparisons derive their verdict and confidence from latency, with reciprocal values and intervals, instead of classifying the same executions twice. `regression.throughput` is no longer accepted: use `regression.latency` for relative changes and throughput budgets for absolute rate limits. Reports identify the derived comparison with `derived_from: latency` (also a CSV column), `gate: false` and `(FROM LATENCY)` in tables; it is not counted again in `summary.not_gated`. Per-run throughput samples, statistics and budgets are unchanged. A comparison requires constant, equal work on both revisions; otherwise the derived row is skipped.
- The `max_cv` noise gate permits completely separated sample ranges to proceed to the existing tolerance and bootstrap confidence checks. It still rejects overlapping noisy distributions, and sample-count and absolute-difference checks still apply.

### Fixed

- Comparisons whose bootstrap encounters a zero base statistic are inconclusive instead of discarding those draws and overstating confidence in an improvement or pass. This can affect zero-valued CPU samples with a raised or disabled noise gate; the report explains why inference is unavailable.

- Memory comparisons with both statistics at or below their RSS floors are skipped without computing a change or confidence. They do not count as inconclusive or fail `--fail-on-inconclusive`.
- E2E runs preserve the complete CPU-regression JSON report, including reasons and samples, outside the disposable scenario directory. CI uploads these diagnostics on success or failure; the original intermittent runner failure remains unconfirmed.
- Documented that revision comparisons belong to the suite repository and other tools are installed commands. Service-dependent commands remain outside the v0.x scope.

## [0.2.0] - 2026-09-18

### Added

- `terminal: true` on a benchmark runs its commands on a pseudo-terminal of 80 columns and 24 rows, on Linux and macOS, so a program that only does its work on a terminal (an interactive shell, a line editor, a TUI) can be measured. Standard input, output and error are the terminal, `stdin` is typed into it one key at a time once the command is ready and has read the key before, as a person types, and what the command writes goes to `stdout`; `stderr` cannot be set with it. On Windows and the BSDs such a benchmark fails with exit 4. See Standard input and output on the Configuration page.

### Changed

- A `..` after `${root}` or `${head_root}` in a path setting (`cwd`, `stdin`, `file_size`) is accepted when the path stays inside the project, as a relative path's `..` already was, so a fixture committed outside the suite's directory can be read from the working tree by both revisions. It used to be rejected wherever it led. `..` after `${workdir}` is still rejected.
- On the pull request that adds a suite, `compare` and `ci` build and run the suite in the working tree and judge it as `himorime run` does, instead of skipping it. A broken build or command exits 4 and an exceeded budget exits 1 on that pull request, where they used to pass and fail the next pull request instead. The suite is still reported as new in this revision (`new_in_head`, `summary.new_suites`, the notice annotation); in JSON its commands have a null `base` and null `comparisons`, and CSV has its plain-run rows after the `new_in_head` row.
- The `CONFIDENCE` column of comparison tables in the terminal, Markdown and job summaries shows the probability that decided `RESULT`: that the change stays within the tolerance for `PASS`, as before for `REGRESSION` and `IMPROVED`, and the highest of these for a change too close to call. A pass used to show `low`, which read as an unsure pass, and a comparison made inconclusive by noise or too few samples could show `99.0%`; a result the probabilities did not decide now shows `-`. JSON and CSV are unchanged.

### Fixed

- Whether a plain run printed `no geometric mean: command ... is missing from benchmark ...` depended on the first benchmark, and so on the order of the suite and on `--filter`: a suite starting with a one-command benchmark never printed it, and the same suite filtered to start with a benchmark of several commands always did. The note now appears when the benchmarks share at least one command name and one of them lacks another, in any order; a suite whose benchmarks share none, such as a regression suite with a start-up benchmark, gets no note.
- A command or hook whose working directory did not exist failed with `fork/exec /usr/bin/mkdir: no such file or directory`, which named a program that was there. The failure now names the missing directory and the `cwd` it came from, and for a hook says that it runs in the benchmark's `cwd` unless it sets its own, so a setup step that creates that directory needs `cwd: ${workdir}`.
- A benchmark whose `${workdir}` held a directory without write permission, as a Go module cache in `${workdir}` does, failed at cleanup with `permission denied` after measuring, and the temporary directory stayed on disk. himorime now gives the owner write permission inside its own temporary directories before removing them, without following symbolic links.

## [0.1.3] - 2026-09-18

### Changed

- The `gate-cpu-memory` recipe gives its command 150ms of work and 32MiB to hold, so that the metrics it gates on are measurable everywhere. Its CPU `min_difference` is 32ms, two Windows scheduler ticks. Measured on a GitHub-hosted `windows-latest` runner, the recipe's old command spent so little time on a CPU that its median was `0ns` and the comparison was inconclusive on every run.
- The noise gate `regression.max_cv` compares the spread that matches the statistic being judged: the interquartile range divided by 1.349 and by the median for the median and the other order statistics, the coefficient of variation for the mean. A few slow runs, which shared CI runners produce, no longer make a comparison inconclusive and no longer hide a real change; a distribution with two modes far apart still does. The key, its default of `0.5` and the reason string are unchanged, but a `max_cv` tuned for the old definition is worth reviewing, and `max_cv: 0` is no longer the workaround for outliers. JSON reports gain `robust_cv` next to `cv`, and CSV a `robust_cv` stat row.

### Added

- `bench/thirdparty/json` measures jq, gojq and jaq writing one field of every line of generated JSON Lines, at 5MiB and at 100MiB, as a worked example of measuring a program you did not write. The input comes from the generator in `bench/thirdparty/gen`, the three outputs are compared byte for byte before measuring, and every tool is installed at a pinned version. `.github/workflows/thirdparty.yml` runs it weekly on a pinned runner to keep the example correct, and publishes nothing: the numbers of a run are in its job summary and its artifact. The new Real-world suites page says what each suite shows. No suite carries a budget, so the speed of a program this repository does not maintain never fails its CI.
- `metrics.throughput.work` in JSON has `measured_min` and `measured_max`, the work each run was measured over, and CSV has `measured_work_min` and `measured_work_max` stat rows for `throughput`. A `file_size` is read before every run, so the work can differ between runs and between revisions. See Throughput on the Metrics page.

### Fixed

- Progress lines on standard error say `1 measurement unit`, `1 run` and `1 round` instead of `1 measurement units`, `1 runs` and `1 rounds`.
- A comparison of throughput over different amounts of work is inconclusive instead of a regression or an improvement, and its reason names both amounts. A file named by `metrics.throughput.work.file_size` whose size differs between the revisions moved throughput without the command running any faster or slower, and `compare` and `ci` called that a regression at full confidence and exited 1.
- A relative path whose `..` leaves the repository holding the suite, or the suite's own directory when it is not in a repository, is rejected when the suite is loaded, so `validate`, `run`, `compare` and `ci` exit 2. Such a path validated ok and then failed the run with exit 4, after the build, on the first run. A `..` that stays inside is unchanged.
- `unit: KB` with a byte-rate budget and tolerance reported three errors, two of which asked for the unit the first error had rejected; it now reports one. A rejected work unit is no longer used in the hints of the errors after it.
- On Linux the peak RSS floor has 1MiB of slack. Read exactly, it could come out a few pages below the peak folded into a command, and a coverage build on a GitHub runner reported one run of `true` in ten above its floor.

## [0.1.2] - 2026-09-17

### Added

- `--section NAME` for `run`, `compare` and `ci`, and `section` under `report.outputs`, update one section of an existing Markdown page, between `<!-- himorime:begin NAME -->` and `<!-- himorime:end NAME -->`, and keep the rest of the page byte for byte. Heading levels follow the page, the file is replaced atomically, and a missing file or marker fails the run with exit 4. See Publish results in documentation on the Reports page.
- `report.versions` records the versions of the compared tools, such as `jc: [jc, --version]`. Each command runs once before measuring; its first line is written to `environment.tools` in JSON and under the Markdown report. A version command that fails exits 4.
- `metrics.peak_rss` in JSON has `floor` and `samples_at_floor`, and CSV has `floor` and `samples_at_floor` stat rows for `peak_rss`. See Peak RSS on the Metrics page.

### Changed

- Markdown reports and job summaries of plain runs leave out rows of commands that do not measure a metric group, and the `Result` column when no command has a budget and nothing failed. Markdown no longer explains why a geometric mean is missing; the terminal still does. CSV and the terminal table are unchanged, and JSON only gains `environment.tools`.

### Fixed

- The peak RSS floor is read after a run instead of before it. Read before, it missed what starting the command added, and a stripped release binary on a CI runner reported `true` above its floor, as a precise value that was really the spawner's.
- On Linux, a command's peak RSS could never be reported below himorime's own peak, because the kernel counts the peak of the process that starts a command as part of the command's (macOS and the BSDs are treated the same way), and every memory spike of himorime raised it for all later runs. `/usr/bin/true`, which GNU time reports at 1.4MiB, was reported at 9.3 to 10.4MiB on Linux. Commands whose memory is measured now start from a small spawner process, which on Linux also resets its own peak before each start: `/usr/bin/true` is reported at 5.6 to 6.8MiB. The remaining floor is recorded, and a peak at or below it is shown as `≤ floor`. A comparison of such a peak reports only a regression or an improvement that holds whatever the real value below the floor is, and is inconclusive otherwise; a budget on it passes only when the floor itself is within the budget, and is skipped otherwise. Windows was not affected. Latency-only runs start commands as before; a run that measures memory takes about a millisecond longer to start the spawner and tens of microseconds more per run, outside the measured interval.

## [0.1.1] - 2026-09-17

### Changed

- `compare` and `ci` no longer fail with exit 4 on the pull request that adds a suite. A suite whose directory does not exist in the base revision is reported as new in this revision (`new_in_head` in JSON, counted in `summary.new_suites`, a `new_in_head` CSV row and a notice annotation); nothing is built or run for it and it does not change the exit status.

### Fixed

- A command that runs Git in its repository, such as a build embedding `git describe --dirty` or himorime itself, was measured slower in the base revision than in the working tree with identical code: the files of the fresh base worktree were written in the same second as its index, so Git compared their content again on every `git status`, and kept doing so when it ran without optional locks. The base worktree's index is now refreshed after that second has passed. On himorime's own suite this removed a false 15% improvement.
- `himorime run` ran `git worktree prune` in the suite's repository. Only a comparison, which creates a worktree, prunes stale worktree entries now.

## [0.1.0] - 2026-09-17

### Added

- `himorime init`, `validate`, `list`, `run`, `compare`, `ci`, `version` and `completion` (bash, zsh, fish, PowerShell).
- Versioned suite format (`version: "1"`) with a JSON Schema for editors; suites are validated against the same schema before semantic checks.
- Interleaved measurement with a seeded order, warmup, fixed and adaptive runs, timeouts that stop the whole process tree, stdin fixtures, and `setup`, `prepare_each` and `cleanup` hooks.
- Revision comparison in a temporary Git worktree, including uncommitted changes, with a bootstrap confidence test that classifies each command as pass, improved, regression or inconclusive.
- Metrics besides latency: throughput from declared work (`value` or `file_size`), user, system and total CPU time and CPU utilization of the process tree, and peak RSS normalized to bytes. CPU time and peak RSS are read from the operating system after each run (`wait4` on Unix, Job Object accounting on Windows) without polling.
- Absolute budgets on any metric and aggregation: min, max, mean, median and percentiles `p1` to `p99.9`, with typed durations, byte sizes, rates and percentages, and operators checked against the metric's direction.
- Regression checks for latency, throughput (higher is better), CPU time and peak RSS, each with its own `metric`, `max_percent`, `min_difference` and `gate` under `regression.latency`, `regression.throughput`, `regression.cpu` and `regression.memory`. `gate: false` keeps a metric compared and reported, marked `(NOT GATED)` and counted in `summary.not_gated`, without letting it fail the run.
- `${head_root}`, the suite directory in the working tree for every revision, for fixtures and helpers both revisions must share.
- `metrics.unsupported: fail | skip`; unsupported and failed metrics are reported as such and never as zero.
- Reports as terminal tables per metric group, JSON with raw samples of every metric (`schema_version` `"1"`), long-format CSV, per-run samples CSV, Markdown and a GitHub Actions job summary, plus workflow annotations in GitHub Actions.
- Stable exit codes 0 to 6; 6 means a requested metric could not be measured. A non-zero status ends the log with a line explaining it.
- Every metric in a report records its `source` and `process_aggregation`, such as `rusage` and `max_of_single_process_peaks` for peak RSS on Unix.
- `make calibration`: verdict rates of the regression classifier on seeded synthetic data, including shared drift, outliers and near-threshold changes.

### Changed

Changes made before the first release, while the formats are not yet published:

- Relative paths (`cwd` of commands and hooks, `stdin`, `file_size`) and the default working directory are relative to `${root}` of the revision being measured, not to the suite file in the working tree. A comparison without a build step used to run the working tree's scripts for both revisions and could not see a change; write `${head_root}/...` where both revisions must read the working tree's file.
- The latency tolerance moved from `regression.metric`, `max_percent` and `min_difference` to `regression.latency`, like every other metric.
- JSON report: `comparison` and a measurement's `mean_ns`, `median_ns`, `stddev_ns`, `min_ns`, `max_ns`, `cv` and `samples_ns` are removed; the same values are in `comparisons.latency` and `metrics.latency`. Comparisons gain `gate`, metrics `source` and `process_aggregation`, and the summary `not_gated` and `skipped`. The CSV report gains the `scope`, `source`, `process_aggregation` and `gate` columns.
- Adaptive runs of a comparison continue until `regression.min_samples` samples per revision instead of stopping at `min_runs` with an inconclusive result.
- On Windows a command is created suspended and resumed only after it joined its Job Object, so no child process can escape the tree. A command that cannot be assigned to a job is killed and reported as an error instead of being measured without the tree guarantees.
- A comparison fails early when the suite directory does not exist in the base revision, with or without a build step.
