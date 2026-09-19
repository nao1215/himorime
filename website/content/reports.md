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

A plain run prints one table for each measured metric and a budget table when needed:

```text
latency
BENCHMARK  COMMAND   MEDIAN     MEAN    STDDEV  RELATIVE  RESULT
df large   jsonize  14.20ms  14.31ms  310.20µs     1.00x  PASS
df large   jc       41.08ms  41.77ms    1.02ms     2.89x  PASS

2 passed · 1 benchmark · seed 42 · exit 0
```

`RELATIVE` divides a command's median latency by the configured baseline, or by the fastest command when no baseline is set. CPU values and peak RSS are medians across runs; the memory table also shows the highest peak. [Metrics](/metrics/) explains the scope of each measurement.

A comparison adds base, head, change, confidence, tolerance and verdict columns:

```text
BENCHMARK  BASE     HEAD     DIFF  CHANGE  CONFIDENCE  TOLERANCE  RESULT
encode     8.10ms   9.43ms  1.33ms  +16.4%       98.1%       +10%  REGRESSION
```

`REGRESSION` means the bootstrap probability of exceeding the tolerance meets the configured confidence. `INCONCLUSIVE` means the samples cannot separate the change from noise. [Regression detection](/regression-detection/) describes the test and gating rules.

For suites with several benchmarks, the comparison summary includes a geometric mean of ratios. It is useful as an overall direction, while each benchmark still keeps its own verdict.

## JSON

JSON is the lossless report format. It includes metadata, tool versions, raw samples, statistics, budgets, comparisons and the final exit code. The published [JSON Schema](/schema/report.schema.json) defines the fields and units.

```console
$ himorime run --format json --output result.json
$ jq '.suites[].benchmarks[].commands[] | {name, median_ns: .head.metrics.latency.stats.median}' result.json
```

Values use canonical units: nanoseconds for durations, bytes for memory, and numeric rates and percentages. The `samples` arrays contain the observations used for statistics and comparison.

## CSV

`--format csv` writes one row per statistic, budget or comparison, with columns for command identity and metric details. Empty cells mean that the field does not apply to that row. This long layout can represent latency, throughput, CPU and memory in one file.

`--format samples-csv` writes one row per raw observation. Use `run` to join metrics from the same run and `side` to distinguish base and head samples.

```console
$ himorime compare --against main --format samples-csv --output samples.csv
```

## Markdown and GitHub Actions

Markdown contains the same result tables as the terminal report and is suitable for a checked-in page. The `github` format writes a compact GitHub Actions job summary. Performance failures and measurement failures use distinct titles and exit statuses; see [Reference](/reference/#exit-codes).

`himorime ci` writes Markdown to `$GITHUB_STEP_SUMMARY` automatically and emits annotations for exceeded budgets, regressions, inconclusive comparisons and execution failures. A metric with `gate: false` produces a notice rather than an error.

`himorime comment REPORT.json` posts a JSON report from a completed GitHub Actions `workflow_run` to its pull request. Use the two-workflow pattern in [GitHub Actions](/github-actions/#a-pull-request-comment), so the benchmark job stays read-only.

PR comments show command-level counts, failures requiring attention and measurement caveats before any tables. Expand individual metrics for base/head values and changes, decision evidence for thresholds and reasons, or run metadata for versions, commits and environment. Tables containing only passing comparisons omit the result column; unavailable or floor-limited measurements remain explicit. The workflow link leads to the complete JSON report and logs, including any text or commands omitted to fit the comment limit.

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

The section name uses lowercase letters, digits and hyphens. The same fields are available in `report.outputs` so a normal run can update the page.
