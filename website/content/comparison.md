---
title: Comparison
description: Where yahiko sits next to hyperfine, Bencher, CodSpeed and github-action-benchmark, and when to use which.
---

Benchmarking tools answer different questions at different layers. Several of
them work well together.

| Tool | What it is | Use it when |
|---|---|---|
| [hyperfine](https://github.com/sharkdp/hyperfine) | An excellent command-line tool for measuring commands you type, with rich statistics, parameter scans and exports. | You want to measure a few commands right now, interactively. |
| yahiko | Performance budgets for a CLI kept in the repository as YAML — latency, throughput, CPU time and peak RSS — run the same way locally and in CI, comparing a Git base revision with the working tree and failing on confirmed regressions or broken budgets. | You want every pull request to answer "does this CLI stay within its performance budget?", without an external service. |
| [atago](https://github.com/nao1215/atago) | An end-to-end test tool for command-line programs: exit codes, output, files, TUI screens. | You want to check that the CLI behaves as expected. Use it next to yahiko, which checks that it performs as expected. |
| [Bencher](https://bencher.dev/) | A continuous benchmarking service that stores results over time, tracks thresholds and comments on pull requests. It ingests output from many harnesses. | You want history, dashboards and alerts across many runs. |
| [CodSpeed](https://codspeed.io/) | A hosted service that measures benchmarks with instrumentation to reduce CI noise, integrated with language benchmark frameworks. | You benchmark functions in code and want low-noise results on hosted CI. |
| [github-action-benchmark](https://github.com/benchmark-action/github-action-benchmark) | A GitHub Action that collects output of benchmark frameworks, stores it in a branch and charts it on GitHub Pages. | You want a simple history chart of existing benchmark output inside GitHub. |

## yahiko and hyperfine

Both measure end-to-end process time. hyperfine is built for the terminal: you
name commands on the command line and get a detailed measurement. yahiko is
built for the repository: the commands, inputs, hooks, budgets and tolerances
live in a reviewed file; a comparison builds two revisions and interleaves
them; the result is a verdict with an exit status designed for CI.

If you only need a one-off measurement, hyperfine is the better tool.

## yahiko and hosted services

yahiko keeps no history and runs no server. Each run compares two revisions
measured side by side in the same job, which works without storing results
and without trusting that yesterday's runner was the same machine as today's.
If you also want trends over months, export `--format json` and feed a service
that stores history.

## Layers

- In-process benchmarks of functions (`go test -bench`, criterion) measure
  code paths without process start-up. They are the right tool below the
  command line.
- yahiko measures the command line as users run it: start-up, I/O, the whole
  process tree.
- History and dashboards belong to a service.

## What yahiko does not do

These are deliberate non-goals, so the tool stays small and its results stay
easy to trust:

- HTTP load testing, distributed or concurrent load generation.
- Monitoring long-running daemons or services, system-wide CPU or memory, or
  network service SLOs.
- Continuous profiling, heap allocation or garbage collection statistics,
  hardware counters and energy measurement.
- A hosted service, an API, a web UI, a results database or history
  dashboards.
- CPU profiling or flame graphs.
- Orchestrating Docker or Kubernetes environments.
- Asserting on a command's output; use a test tool for that.
- Commenting on pull requests; the job summary and exit status carry the
  result.
