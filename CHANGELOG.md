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
- Absolute budgets on mean, median, min and max.
- Reports as a terminal table, JSON with raw samples (`schema_version` `"1"`),
  CSV, Markdown and a GitHub Actions job summary.
- Stable exit codes 0 to 5.
