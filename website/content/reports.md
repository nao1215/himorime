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

Latency ratios use the configured baseline, or the fastest command when no baseline is set. The compact view shows median latency, throughput, total CPU time and peak RSS; budget rows name their own statistic. [Metrics](/metrics/) explains their scope and measurement bounds.

A comparison shows base → head and the relative change:

```text
TARGET         METRIC                                     VALUE  RESULT      REASON
encode / tool  Latency (median)  8.10ms → 9.43ms (+16.4%)          REGRESSION  -
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

### Reuse a saved report

Render a saved report without rerunning commands:

```console
$ himorime report result.json
$ himorime report result.json --format markdown --output result.md
```

The command validates the current report schema and renders the stored samples, units, bounds, verdicts, reasons and summary. It preserves the saved verdict and displayed measurement exit code; it does not recompute statistics or apply current suite defaults. Reports from an older or unknown schema version are rejected explicitly.

## CSV

`--format csv` writes one row per statistic, budget or comparison, with columns for command identity and metric details. Empty cells mean that the field does not apply to that row. This long layout can represent latency, throughput, CPU and memory in one file.

`--format samples-csv` writes one row per raw observation. Use `run` to join metrics from the same run and `side` to distinguish base and head samples.

```console
$ himorime compare --against main --format samples-csv --output samples.csv
```

## Markdown and GitHub Actions

Markdown includes detailed result tables suitable for a checked-in page. The `github` format starts with a compact result table; statistics, decision rules, collection methods and environment are available in expandable sections.

In GitHub Actions, this summary is also written to the log on standard error, including when JSON is written to standard output or a file. `himorime ci` appends it to `$GITHUB_STEP_SUMMARY` when available. Annotations and [exit statuses](/reference/#exit-codes) are unchanged.

setup-himorime automatically posts a notification from the saved JSON report on same-repository pull requests. See [GitHub Actions](/github-actions/#a-pull-request-comment) for the required path and job permissions; no local comment command or separate workflow is needed.

PR comments contain only the overall status, problem rows and a full-report link; passing and improved results are omitted. Errors appear before regressions, budget violations and inconclusive measurements. Comments show at most ten problems; the link leads to the benchmark run. The action's post step logs all saved results as tables, including with older himorime releases. The measurement step keeps its existing Job Summary, and raw samples remain in the JSON artifact.

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
