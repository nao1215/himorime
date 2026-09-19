---
title: Comparison
description: Choose between himorime, hyperfine, benchstat, Bencher and CodSpeed for command-line performance work.
toc: true
---

Information and documentation were checked on 2026-09-19. The release basis is himorime v0.3.0, hyperfine v1.20.0, and benchstat v0.0.0-20260825160852-19be9d8e6c70; Bencher and CodSpeed are documented by their continuously updated service documentation. See the primary sources at the end of the page.

## himorime and hyperfine

| Task | himorime | hyperfine |
|---|---|---|
| Repeatable setup | A YAML suite can build a program and define setup, cleanup, work and budgets. | Commands, warmups, `--prepare`/`--conclude` hooks and parameter scans are supplied at invocation. |
| Compare changes | Checks a Git base worktree and the working tree in interleaved rounds, including uncommitted head changes. | Compares commands or parameter values; checking out and building revisions is an external workflow. |
| Measurements | Latency, declared-work throughput, CPU time and peak RSS. | Wall-clock timings with [user/system CPU fields](https://github.com/sharkdp/hyperfine/blob/v1.20.0/src/timer/unix_timer.rs); statistical analysis centers on timing. |
| Result handling | Absolute budgets and relative regression gates; JSON, CSV, Markdown and GitHub Actions output. | CSV, JSON, Markdown and AsciiDoc exporters; JSON can feed further analysis. |

Choose hyperfine for a quick timing experiment, a parameter scan, or a report to paste into a discussion. Choose himorime when the suite and its limits belong in the repository and a base/head decision should run locally or in CI. The [hyperfine README](https://github.com/sharkdp/hyperfine/tree/v1.20.0) documents its command options and exporters; its [v1.20.0 release](https://github.com/sharkdp/hyperfine/releases/tag/v1.20.0) is the version checked here.

You write a YAML suite, choose workloads and possibly add build/setup hooks, and account for OS-specific metric support. Peak RSS has a platform-dependent floor and reports the largest observed single-process peak, not a sum of process RSS. On Windows, a command that starts children has CPU accounting but no peak RSS result, and himorime's PTY mode is limited to Linux and macOS. It keeps no result history or server and does not profile inside a runtime. Relative gates compare only the median or mean; p95 and p99 are available for absolute latency budgets. See [metrics](/metrics/), [configuration](/configuration/) and [regression detection](/regression-detection/) for exact semantics and platform differences.

## Tools that complement or surround measurement

| Tool | Use it when | What to account for |
|---|---|---|
| [benchstat](https://pkg.go.dev/golang.org/x/perf@v0.0.0-20260825160852-19be9d8e6c70/cmd/benchstat) | You have Go benchmark result files and want statistical summaries and A/B comparisons. | It is a result-analysis command in the Go performance toolchain; its pseudo-version is not a standalone release. |
| [Bencher](https://bencher.dev/docs/) | You need stored history, testbeds, dashboards and configurable thresholds/alerts across local or CI runs. | It is a CLI plus a hosted or self-hosted continuous-benchmarking service, so account/API or server setup is part of the workflow. |
| [CodSpeed](https://codspeed.io/docs/integrations/ci) | You want supported benchmark integrations, CI checks, historical tracking and instruments such as simulation or walltime. | It is a service-oriented CI platform; its integrations and supported language/harness matrix determine setup. |

## Complementary correctness testing

[atago](https://github.com/nao1215/atago) tests what a CLI does: exit status, output, files, services and terminal interaction. himorime tests how the command performs. A project can use atago for behavioral end-to-end coverage and himorime for performance budgets; neither replaces the other.

## Sources

- [himorime v0.3.0](https://github.com/nao1215/himorime/releases/tag/v0.3.0), [metrics](/metrics/), [configuration](/configuration/), and [regression detection](/regression-detection/)
- [hyperfine v1.20.0 README](https://github.com/sharkdp/hyperfine/tree/v1.20.0) and [release](https://github.com/sharkdp/hyperfine/releases/tag/v1.20.0)
- [benchstat package documentation](https://pkg.go.dev/golang.org/x/perf@v0.0.0-20260825160852-19be9d8e6c70/cmd/benchstat)
- [Bencher continuous benchmarking](https://bencher.dev/docs/explanation/continuous-benchmarking/) and [thresholds](https://bencher.dev/docs/explanation/thresholds/)
- [CodSpeed CI](https://codspeed.io/docs/integrations/ci) and [walltime instrument](https://codspeed.io/docs/instruments/walltime)
