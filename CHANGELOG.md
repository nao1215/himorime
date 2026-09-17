# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- `yahiko init`, `validate`, `list`, `run`, `compare`, `ci`, `version` and
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
  peak RSS, each with its own `max_percent` and `min_difference`.
- `metrics.unsupported: fail | skip`; unsupported and failed metrics are
  reported as such and never as zero.
- Reports as terminal tables per metric group, JSON with raw samples of every
  metric (`schema_version` `"1"`), long-format CSV, per-run samples CSV,
  Markdown and a GitHub Actions job summary, plus workflow annotations in
  GitHub Actions.
- Stable exit codes 0 to 6; 6 means a requested metric could not be
  measured. A non-zero status ends the log with a line explaining it.
