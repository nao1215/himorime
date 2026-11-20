---
title: Comparison
description: Choose between himorime, hyperfine, benchstat, Bencher and CodSpeed for command-line performance work.
toc: true
---

Checked on 2026-09-19 against himorime v0.3.0, hyperfine v1.20.0, benchstat v0.0.0-20260825160852-19be9d8e6c70, and the Bencher and CodSpeed documentation of that date.

## himorime and hyperfine

| Task | himorime | hyperfine |
|---|---|---|
| Repeatable setup | A YAML suite can build a program and define setup, cleanup, work and budgets. | Commands, warmups, `--prepare`/`--conclude` hooks and parameter scans are supplied at invocation. |
| Compare changes | Checks a Git base worktree and the working tree in interleaved rounds, including uncommitted head changes. | Compares commands or parameter values; checking out and building revisions is an external workflow. |
| Measurements | Latency, declared-work throughput, CPU time and peak RSS. | Wall-clock timings with [user/system CPU fields](https://github.com/sharkdp/hyperfine/blob/v1.20.0/src/timer/unix_timer.rs); statistical analysis centers on timing. |
| Result handling | Absolute budgets and relative regression gates; JSON, CSV, Markdown and GitHub Actions output. | CSV, JSON, Markdown and AsciiDoc exporters; JSON can feed further analysis. |

Use hyperfine for a quick timing, a parameter scan, or a table to paste into a discussion. Use himorime when the suite and its limits belong in the repository and a base/head decision runs locally or in CI.

himorime's limits: peak RSS has a platform-dependent [floor](/metrics/#the-floor) and is the largest single-process peak. On Windows a command that starts children has no peak RSS, and `terminal: true` works only on Linux and macOS. It keeps no result history, does not profile inside a runtime, and compares only the median or mean between revisions (p95 and p99 are for absolute budgets).

## Tools that complement or surround measurement

| Tool | Use it when | What to account for |
|---|---|---|
| [benchstat](https://pkg.go.dev/golang.org/x/perf@v0.0.0-20260825160852-19be9d8e6c70/cmd/benchstat) | You have Go benchmark result files and want statistical summaries and A/B comparisons. | It is a result-analysis command in the Go performance toolchain; its pseudo-version is not a standalone release. |
| [Bencher](https://bencher.dev/docs/) | You need stored history, testbeds, dashboards and configurable thresholds/alerts across local or CI runs. | It is a CLI plus a hosted or self-hosted continuous-benchmarking service, so account/API or server setup is part of the workflow. |
| [CodSpeed](https://codspeed.io/docs/integrations/ci) | You want supported benchmark integrations, CI checks, historical tracking and instruments such as simulation or walltime. | It is a service-oriented CI platform; its integrations and supported language/harness matrix determine setup. |

## Complementary correctness testing

[atago](https://github.com/nao1215/atago) tests what a CLI does (exit status, output, files, services, terminal interaction); himorime tests how fast it does it.

## Sources

- [himorime v0.3.0](https://github.com/nao1215/himorime/releases/tag/v0.3.0), [metrics](/metrics/), [configuration](/configuration/), and [regression detection](/regression-detection/)
- [hyperfine v1.20.0 README](https://github.com/sharkdp/hyperfine/tree/v1.20.0) and [release](https://github.com/sharkdp/hyperfine/releases/tag/v1.20.0)
- [benchstat package documentation](https://pkg.go.dev/golang.org/x/perf@v0.0.0-20260825160852-19be9d8e6c70/cmd/benchstat)
- [Bencher continuous benchmarking](https://bencher.dev/docs/explanation/continuous-benchmarking/) and [thresholds](https://bencher.dev/docs/explanation/thresholds/)
- [CodSpeed CI](https://codspeed.io/docs/integrations/ci) and [walltime instrument](https://codspeed.io/docs/instruments/walltime)
