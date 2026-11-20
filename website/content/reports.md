---
title: Reports
description: Terminal, JSON, CSV, Markdown and GitHub Actions output from himorime.
toc: true
---

`--format` selects `table` (the default), `json`, `csv`, `samples-csv`, `markdown` or `github`. Use `--output FILE` to write the report to a file and `--summary FILE` to append a GitHub-flavored Markdown summary. Progress stays on standard error.

A suite can also write reports after each run:

```yaml
report:
  outputs:
    - {format: json, path: bench/result.json}
    - {format: markdown, path: bench/result.md}
```

An output error ends the run with exit status 4.

## Terminal tables

A plain run shows measured values and budget results, with failures and inconclusive results first:

```text
TARGET              METRIC                                    VALUE  RESULT  REASON
df large / jsonize  Latency (median)                         14.20ms  PASS    -
df large / jc       Latency (median)  41.08ms (2.89x vs baseline)       PASS    -

2 passed · 1 benchmark · seed 42 · exit 0
```

Ratios are against the configured baseline, or the fastest command. Rows show median latency, throughput, CPU time and peak RSS; budget rows name their own statistic.

A comparison shows base → head and the relative change:

```text
TARGET         METRIC                                     VALUE  RESULT      REASON
encode / tool  Latency (median)  8.10ms → 9.43ms (+16.4%)          REGRESSION  -
```

`INCONCLUSIVE` means the samples cannot separate the change from noise; see [Regression detection](/regression-detection/). With several benchmarks, the summary adds a geometric mean of the ratios as an overall direction.

## JSON

JSON is the lossless format: metadata, tool versions, raw samples, statistics, budgets, comparisons and the exit code, defined by the [JSON Schema](/schema/report.schema.json).

```console
$ himorime run --format json --output result.json
$ jq '.suites[].benchmarks[].commands[] | {name, median_ns: .head.metrics.latency.stats.median}' result.json
```

Durations are in nanoseconds and memory in bytes. The `samples` arrays hold the raw observations.

### Reuse a saved report

Render a saved report without rerunning commands:

```console
$ himorime report result.json
$ himorime report result.json --format markdown --output result.md
```

It keeps the saved verdicts and exit code and does not recompute statistics. Reports with an older or unknown schema version are rejected.

## CSV

`--format csv` writes one row per statistic, budget or comparison; empty cells do not apply to that row. `--format samples-csv` writes one row per raw observation, with `run` and `side` (base or head) columns.

```console
$ himorime compare --against main --format samples-csv --output samples.csv
```

## Markdown and GitHub Actions

`markdown` writes detailed tables for a checked-in page. `github` starts with a compact table and puts statistics, decision rules and the environment in expandable sections. In GitHub Actions it is also written to the log on standard error, and `himorime ci` appends it to `$GITHUB_STEP_SUMMARY`.

On same-repository pull requests, setup-himorime posts a comment from the saved JSON report; see [GitHub Actions](/github-actions/#a-pull-request-comment). It lists at most ten problems, errors first, then regressions, budget violations and inconclusive results, and links to the run. Passing results are left out.

## Update part of a page

Markdown output can replace a named section while preserving the surrounding file:

```markdown
# Benchmarks

<!-- himorime:begin speed -->
old report
<!-- himorime:end speed -->
```

```console
$ himorime run --format markdown --output docs/benchmarks.md --section speed
```

Section names use lowercase letters, digits and hyphens. Each marker is a line of its own, indented by at most three spaces; markers in code blocks are ignored. `report.outputs` accepts the same `section` field.
