---
title: Cookbook
description: Runnable recipes for himorime, found by what you want to do. Every recipe is an example suite in the repository, and CI runs each one.
toc: true
filter: true
---

Every recipe here is a suite under [`examples/`](https://github.com/nao1215/himorime/tree/main/examples) that you can run from a clone of the repository. The end-to-end suite runs each recipe (`test/e2e/atago/cookbook.atago.yaml`, one scenario per heading) on Linux, macOS and Windows, and a test fails if a recipe and its scenario drift apart. Recipes that reference other CLIs use `git`, which CI already has; nothing is downloaded.

The examples measure two small programs in `examples/tools`: `wordcount`, a word counter with a streaming, an in-memory and a parallel implementation, and `sleepy`, a stand-in whose run time, CPU use, child processes and exit status you control.

## Keep a CLI within a budget

### [Protect the startup latency of a CLI](protect-the-startup-latency-of-a-cli/)

### [Protect the throughput of large CSV and JSON processing](protect-the-throughput-of-large-csv-and-json-processing/)

## Catch regressions against a base revision

### [Detect a CPU time regression](detect-a-cpu-time-regression/)

### [Detect a peak RSS regression](detect-a-peak-rss-regression/)

### [Compare the Git base and head on every metric](compare-the-git-base-and-head-on-every-metric/)

### [Compare a script-based CLI without a build step](compare-a-script-based-cli-without-a-build-step/)

### [Gate on CPU time and memory, report latency only](gate-on-cpu-time-and-memory-report-latency-only/)

## Run it in CI

### [Fail a GitHub Actions job when a performance budget is violated](fail-a-github-actions-job-when-a-performance-budget-is-violated/)

### [Cope with noise on GitHub-hosted runners](cope-with-noise-on-github-hosted-runners/)

## Compare programs or inputs

### [Compare similar CLIs on the same input](compare-similar-clis-on-the-same-input/)

### [Compare parsers on a stdin fixture](compare-parsers-on-a-stdin-fixture/)

### [Compare small, medium and large inputs](compare-small-medium-and-large-inputs/)

### [Understand the geometric mean](understand-the-geometric-mean/)

## Understand the numbers

### [Understand CPU utilization above 100%](understand-cpu-utilization-above-100/)

### [Understand process tree measurement on each OS](understand-process-tree-measurement-on-each-os/)

### [Handle a metric this platform cannot measure](handle-a-metric-this-platform-cannot-measure/)

## Keep results

### [Save JSON, CSV and Markdown reports locally](save-json-csv-and-markdown-reports-locally/)

## Control state between runs

### [Reset state before every run](reset-state-before-every-run/)

### [Clean up after success, failure or interruption](clean-up-after-success-failure-or-interruption/)

### [Benchmark a warm cache](benchmark-a-warm-cache/)

## Choose what runs

### [Run only smoke benchmarks](run-only-smoke-benchmarks/)

## Handle failures

### [Stop a command that hangs](stop-a-command-that-hangs/)

### [Treat a non-zero exit as an error](treat-a-non-zero-exit-as-an-error/)

### [Use a shell pipeline](use-a-shell-pipeline/)
