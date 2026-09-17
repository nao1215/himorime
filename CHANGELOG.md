# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.2] - 2026-09-17

### Added

- `--section NAME` for `run`, `compare` and `ci`, and `section` under
  `report.outputs`, update one section of an existing Markdown page, between
  `<!-- himorime:begin NAME -->` and `<!-- himorime:end NAME -->`, and keep
  the rest of the page byte for byte. Heading levels follow the page, the
  file is replaced atomically, and a missing file or marker fails the run with
  exit 4. See Publish results in documentation on the Reports page.
- `report.versions` records the versions of the compared tools, such as
  `jc: [jc, --version]`. Each command runs once before measuring; its first
  line is written to `environment.tools` in JSON and under the Markdown
  report. A version command that fails exits 4.
- `metrics.peak_rss` in JSON has `floor` and `samples_at_floor`, and CSV has
  `floor` and `samples_at_floor` stat rows for `peak_rss`. See Peak RSS on the
  Metrics page.

### Changed

- Markdown reports and job summaries of plain runs leave out rows of commands
  that do not measure a metric group, and the `Result` column when no command
  has a budget and nothing failed. Markdown no longer explains why a
  geometric mean is missing; the terminal still does. CSV and the terminal
  table are unchanged, and JSON only gains `environment.tools`.

### Fixed

- On Linux, a command's peak RSS could never be reported below himorime's own
  peak, because the kernel counts the peak of the process that starts a
  command as part of the command's (macOS and the BSDs are treated the same
  way), and every memory spike of himorime raised
  it for all later runs. `/usr/bin/true`, which GNU time reports at 1.4MiB,
  was reported at 9.3 to 10.4MiB on Linux. Commands whose memory is measured
  now start from a small spawner process, which on Linux also resets its own
  peak before each start: `/usr/bin/true` is reported at 5.6 to 6.8MiB. The
  remaining floor is recorded, and a peak at or below it is shown as
  `≤ floor`. A comparison of such a peak reports only a regression or an
  improvement that holds whatever the real value below the floor is, and is
  inconclusive otherwise; a budget on it passes only when the floor itself is
  within the budget, and is skipped otherwise. Windows was not affected.
  Latency-only runs start commands as before; a run that measures memory
  takes about a millisecond longer to start the spawner and tens of
  microseconds more per run, outside the measured interval.

## [0.1.1] - 2026-09-17

### Changed

- `compare` and `ci` no longer fail with exit 4 on the pull request that adds
  a suite. A suite whose directory does not exist in the base revision is
  reported as new in this revision (`new_in_head` in JSON, counted in
  `summary.new_suites`, a `new_in_head` CSV row and a notice annotation);
  nothing is built or run for it and it does not change the exit status.

### Fixed

- A command that runs Git in its repository, such as a build embedding
  `git describe --dirty` or himorime itself, was measured slower in the base
  revision than in the working tree with identical code: the files of the
  fresh base worktree were written in the same second as its index, so Git
  compared their content again on every `git status`, and kept doing so when
  it ran without optional locks. The base worktree's index is now refreshed
  after that second has passed. On himorime's own suite this removed a false
  15% improvement.
- `himorime run` ran `git worktree prune` in the suite's repository. Only a
  comparison, which creates a worktree, prunes stale worktree entries now.

## [0.1.0] - 2026-09-17

### Added

- `himorime init`, `validate`, `list`, `run`, `compare`, `ci`, `version` and
  `completion` (bash, zsh, fish, PowerShell).
- Versioned suite format (`version: "1"`) with a JSON Schema for editors;
  suites are validated against the same schema before semantic checks.
- Interleaved measurement with a seeded order, warmup, fixed and adaptive runs,
  timeouts that stop the whole process tree, stdin fixtures, and `setup`,
  `prepare_each` and `cleanup` hooks.
- Revision comparison in a temporary Git worktree, including uncommitted
  changes, with a bootstrap confidence test that classifies each command as
  pass, improved, regression or inconclusive.
- Metrics besides latency: throughput from declared work (`value` or
  `file_size`), user, system and total CPU time and CPU utilization of the
  process tree, and peak RSS normalized to bytes. CPU time and peak RSS are
  read from the operating system after each run (`wait4` on Unix, Job Object
  accounting on Windows) without polling.
- Absolute budgets on any metric and aggregation: min, max, mean, median and
  percentiles `p1` to `p99.9`, with typed durations, byte sizes, rates and
  percentages, and operators checked against the metric's direction.
- Regression checks for latency, throughput (higher is better), CPU time and
  peak RSS, each with its own `metric`, `max_percent`, `min_difference` and
  `gate` under `regression.latency`, `regression.throughput`,
  `regression.cpu` and `regression.memory`. `gate: false` keeps a metric
  compared and reported, marked `(NOT GATED)` and counted in
  `summary.not_gated`, without letting it fail the run.
- `${head_root}`, the suite directory in the working tree for every revision,
  for fixtures and helpers both revisions must share.
- `metrics.unsupported: fail | skip`; unsupported and failed metrics are
  reported as such and never as zero.
- Reports as terminal tables per metric group, JSON with raw samples of every
  metric (`schema_version` `"1"`), long-format CSV, per-run samples CSV,
  Markdown and a GitHub Actions job summary, plus workflow annotations in
  GitHub Actions.
- Stable exit codes 0 to 6; 6 means a requested metric could not be
  measured. A non-zero status ends the log with a line explaining it.
- Every metric in a report records its `source` and `process_aggregation`,
  such as `rusage` and `max_of_single_process_peaks` for peak RSS on Unix.
- `make calibration`: verdict rates of the regression classifier on seeded
  synthetic data, including shared drift, outliers and near-threshold changes.

### Changed

Changes made before the first release, while the formats are not yet
published:

- Relative paths (`cwd` of commands and hooks, `stdin`, `file_size`) and the
  default working directory are relative to `${root}` of the revision being
  measured, not to the suite file in the working tree. A comparison without a
  build step used to run the working tree's scripts for both revisions and
  could not see a change; write `${head_root}/...` where both revisions must
  read the working tree's file.
- The latency tolerance moved from `regression.metric`, `max_percent` and
  `min_difference` to `regression.latency`, like every other metric.
- JSON report: `comparison` and a measurement's `mean_ns`, `median_ns`,
  `stddev_ns`, `min_ns`, `max_ns`, `cv` and `samples_ns` are removed; the same
  values are in `comparisons.latency` and `metrics.latency`. Comparisons gain
  `gate`, metrics `source` and `process_aggregation`, and the summary
  `not_gated` and `skipped`. The CSV report gains the `scope`, `source`,
  `process_aggregation` and `gate` columns.
- Adaptive runs of a comparison continue until `regression.min_samples`
  samples per revision instead of stopping at `min_runs` with an inconclusive
  result.
- On Windows a command is created suspended and resumed only after it joined
  its Job Object, so no child process can escape the tree. A command that
  cannot be assigned to a job is killed and reported as an error instead of
  being measured without the tree guarantees.
- A comparison fails early when the suite directory does not exist in the
  base revision, with or without a build step.
