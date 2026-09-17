---
title: Reports
description: himorime's terminal tables, JSON with raw samples of every metric, long-format CSV, samples CSV, Markdown, the GitHub Actions job summary and annotations, and what each column means.
toc: true
---

`--format` chooses what goes to standard output (or to `--output FILE`):
`table` (default), `json`, `csv`, `samples-csv`, `markdown` or `github`.
`--summary FILE` additionally appends a GitHub-flavored Markdown summary;
`himorime ci` does that to `$GITHUB_STEP_SUMMARY` on its own. A suite can also
write report files after every run:

```yaml
report:
  outputs:
    - format: json
      path: bench/result.json
    - format: csv
      path: bench/result.csv
    - format: markdown
      path: bench/result.md
```

Progress lines go to standard error and never into a report. A report that
cannot be written fails the run with exit status 4. When the exit status is
not 0, the last line on standard error says why in words.

## Terminal tables

A plain run shows one table per metric group the suite measures, then a
budgets table when there are budgets. A suite that measures only latency
shows just the first table:

```text
suite: jsonize benchmarks (himorime.yaml)
latency
BENCHMARK  COMMAND   MEDIAN     MEAN    STDDEV  RELATIVE  RESULT
df large   jsonize  14.20ms  14.31ms  310.20µs     1.00x  PASS
df large   jc       41.08ms  41.77ms    1.02ms     2.89x  PASS

throughput
BENCHMARK  COMMAND       MEDIAN       MEAN        MIN  RESULT
df large   jsonize  72.10MiB/s  71.55MiB/s  68.02MiB/s  PASS
df large   jc       24.93MiB/s  24.51MiB/s  23.60MiB/s  PASS

cpu
BENCHMARK  COMMAND      USER   SYSTEM     TOTAL  UTILIZATION  RESULT
df large   jsonize  12.80ms   1.90ms   14.70ms       103.5%  PASS
df large   jc       35.10ms   5.20ms   40.30ms        98.1%  PASS

memory
BENCHMARK  COMMAND  PEAK RSS       MAX  RESULT
df large   jsonize   9.82MiB  10.07MiB  PASS
df large   jc       31.40MiB  31.52MiB  OVER BUDGET
PEAK RSS is the median over runs; MAX is the highest run.
Peak RSS is the largest peak of any single process of the tree (rusage ru_maxrss), not the combined memory of processes running at the same time.

budgets
BENCHMARK  COMMAND  METRIC              BUDGET  MEASURED  RESULT
df large   jsonize  latency p95     <= 30.00ms   14.90ms  PASS
df large   jc       peak rss max  <= 16.00MiB  31.52MiB  FAIL
  df large / jc: budget peak rss max <= 16.00MiB not met (measured 31.52MiB)

1 passed, 1 over budget · 1 benchmark · seed 42 · exit 1
himorime: exit 1: performance check failed: a budget or regression threshold was violated (the measurement itself succeeded)
```

- `RELATIVE` is the command's median latency divided by the median of the
  benchmark's `baseline` command, or of its fastest command when no baseline
  is set.
- CPU values are medians over runs; `UTILIZATION` above 100% means more than
  one CPU was busy. `PEAK RSS` is the median of the runs' peaks and `MAX` the
  highest run. The sentence under the CPU and memory tables says which
  processes the values cover and how they were combined on this platform. A
  peak RSS at or below the floor, the memory the process starting the command
  already used, is shown as `<=` the floor, with a sentence saying so (see
  [The floor](/metrics/#the-floor)).
- Each table's `RESULT` is about that metric group: `PASS`, `OVER BUDGET`,
  `UNSUPPORTED` (skipped on this platform), `METRIC ERROR` or `ERROR`. The
  budgets table says `PASS`, `FAIL`, `SKIPPED` or `NO DATA` per budget.
- The lines under the tables say which budget was missed, which metric was not
  measured and why, why a command failed, and the tail of its standard error.

A comparison shows one table per compared metric: `latency`, and `throughput`,
`cpu total` and `peak rss` when the suite measures them.

```text
latency
BENCHMARK     BASE     HEAD      DIFF  CHANGE  CONFIDENCE  TOLERANCE  RESULT
df small    1.84ms   1.89ms  +50.00µs   +2.7%         low       +10%  PASS
df large   14.20ms  16.41ms   +2.21ms  +15.6%       98.1%       +10%  REGRESSION

throughput
BENCHMARK        BASE        HEAD         DIFF  CHANGE  CONFIDENCE  TOLERANCE  RESULT
df large   72.10MiB/s  62.40MiB/s  -9.70MiB/s  -13.5%       97.2%        -8%  REGRESSION
```

- `BASE` and `HEAD` are the compared statistic (median unless configured).
- `DIFF` is `HEAD - BASE` and `CHANGE` is `(HEAD - BASE) / BASE`. Their sign
  is the direction the value moved, not whether that is better.
- `TOLERANCE` is `max_percent` in the metric's worse direction: `+` for
  latency, CPU time and peak RSS, `-` for throughput.
- `CONFIDENCE` is the bootstrap probability that the change exceeds the
  tolerance in the direction it went, shown as `low` below 50%.
- `RESULT` is `PASS`, `IMPROVED`, `REGRESSION`, `INCONCLUSIVE`, `SKIPPED`,
  `OVER BUDGET` or an error. A metric with `gate: false` adds `(NOT GATED)`,
  such as `REGRESSION (NOT GATED)`: the verdict is shown but did not decide
  the result, and the summary line counts it as `not gated: 1 regressed`. See
  [What fails the run](/regression-detection/#what-fails-the-run).

Values are rounded for reading: durations in ns, µs, ms or s, sizes in B, KiB,
MiB or GiB, rates with a k, M or G prefix, all with two decimals. Colors are
used only on an interactive terminal and never by `himorime ci`; `--no-color`
and `NO_COLOR` turn them off.

## Geometric mean

When a suite has at least two benchmarks and every benchmark ran the same
commands to completion, the table ends with the geometric mean of each
command's latency ratio across the cases: to the common baseline when all
benchmarks name the same one, otherwise to each case's fastest command. In a
comparison it is the geometric mean of the head/base latency ratios.

It is supplementary. The per-case rows are the result. An arithmetic mean of
ratios would let one large case dominate, so it is never used. If a command
is missing from a case, or any command failed or timed out, no overall number
is printed, and the report says why instead of quietly leaving the failure
out.

## JSON

`--format json` is the complete, machine-readable report, described by
[report.schema.json](https://raw.githubusercontent.com/nao1215/himorime/main/schema/report.schema.json).

- `schema_version` is `"1"`. Until the first release the format may still
  change; afterwards fields are only added within a version.
- For every command and side, `metrics` holds all seven metrics by name. Each
  has `unit`, `better` (`lower`, `higher` or `neutral`), `scope`, `source`
  and `process_aggregation` (how the value was collected and how the
  processes of the tree were combined; see [Metrics](/metrics/#process-tree)),
  `status`
  (`measured`, `not_requested`, `unsupported` or `failed`), `reason`, `stats`
  (count, min, max, mean, median, stddev, cv and `percentiles` keyed by
  `p90`, `p95`, `p99` and any percentile a budget uses), every raw sample in
  execution order in `samples`, and for throughput the declared `work`.
  `floor` and `samples_at_floor` are 0 except for `peak_rss`, where `floor`
  is the largest peak RSS, in bytes, the process starting the command already
  had before a run, and `samples_at_floor` counts the runs at or below their
  floor (see [The floor](/metrics/#the-floor)).
  Values are unrounded numbers in the canonical unit: ns, bytes, work per
  second, percent. A metric that was not measured has `stats: null` and no
  samples, never zeros.
- Latency is described there once, like every other metric: its samples are
  `metrics.latency.samples` in nanoseconds, and `count` and `warmups` are the
  measured and warmup runs.
- `budgets` lists every budget with `metric`, `aggregation`, `operator`,
  `limit`, `actual`, `unit`, `status` (`pass`, `fail`, `skipped`, `no_data`)
  and `reason`. A budget on a peak RSS at the floor has the reason `the peak
  RSS is at or below the measurement floor`.
- `comparisons` holds each compared metric with `statistic`, `base`, `head`,
  `difference`, `change_percent`, the bootstrap interval, both tail
  probabilities, `max_percent`, `min_difference`, the verdict, its reason, and
  `gate`: `false` when the metric has `gate: false` and its verdict did not
  decide the result. `comparisons` is `null` outside a comparison.
- `environment` records the operating system, architecture, CPU model, logical
  CPU count, Go version and CI provider, and `tools` the versions of the tools
  named in `report.versions`, as `name` and `version` in the order the suites
  list them (`[]` when none are named). `himorime_version`, `seed` and `git`
  (head and base commits, and whether the working tree was dirty) complete what
  is needed to reproduce a run. Host names, user names and environment
  variables are never recorded.
- `summary` counts command results, `metric_error` among them, and
  `exit_code` is the exit status of the run. `not_gated` counts the
  comparisons with `gate: false` by verdict, and `skipped` the budgets and
  comparisons skipped because their metric is unsupported, and the budgets a
  peak RSS at the floor cannot decide; neither changes a result.
- In a comparison, a suite whose directory does not exist in the base
  revision has `new_in_head: true`, no benchmarks and result `pass`, and is
  counted in `summary.new_suites`. It never changes the exit status.

```console
$ himorime run --format json --output result.json
$ jq '.suites[].benchmarks[].commands[] | {name, p95: .head.metrics.latency.stats.percentiles.p95, rss: .head.metrics.peak_rss.stats.max}' result.json
```

## CSV

`--format csv` is long rather than wide: one row per statistic, budget or
comparison of one metric, so a spreadsheet can filter and pivot on it without
parsing a cell. The header is fixed:

```text
suite,benchmark,command,result,record,side,metric,unit,scope,source,process_aggregation,status,statistic,value,operator,limit,base,head,difference,change_percent,ci_low_percent,ci_high_percent,probability_regression,max_percent,verdict,gate,reason,error_kind,error_message
```

- `record` is `stat`, `budget`, `comparison`, `error` or `new_in_head`, the
  only row of a suite the base revision does not have, with the explanation in
  `reason`.
- A `stat` row has `side` (`base` or `head`), `metric`, `unit`, `scope`,
  `source`, `process_aggregation`, `status`,
  `statistic` (`count`, `min`, `median`, `mean`, `max`, `stddev`, `cv`,
  percentiles, `floor` and `samples_at_floor` for `peak_rss`, and
  `relative_to_baseline` or `relative_to_fastest` for latency) and `value`. A requested metric that was not measured has one row
  with its status and reason.
- A `budget` row has `statistic` (the aggregation), `value` (the measured
  value), `operator`, `limit` and `verdict`.
- A `comparison` row has `base`, `head`, `difference`, `change_percent`, the
  interval, `probability_regression`, `max_percent`, `verdict` and `gate`
  (`true` or `false`).
- Numbers are unrounded, in the canonical unit, without exponents. Fields
  holding commas, quotes or line breaks are quoted.

`--format samples-csv` writes every raw sample, one row per metric per run:

```text
suite,benchmark,command,side,run,metric,unit,value
jsonize benchmarks,df large,jsonize,head,1,latency,ns,14187342
jsonize benchmarks,df large,jsonize,head,1,peak_rss,bytes,10297344
```

`run` is the 1-based index of the measured run, so the metrics of one run can
be joined on it.

## Markdown

`--format markdown` writes, per suite, one table per metric group and a
budgets table when there are budgets (only the latency table for a
latency-only suite), followed by notes and one line describing the machine.
Names are escaped, so a `|` in a benchmark name cannot break a table. It is
meant to be pasted into a README, a pull request, a blog post or release
notes, so it leaves out what only matters while measuring:

- A metric group table lists only the commands that measure the group. A
  benchmark without declared work has no throughput row.
- In a plain run, the `Result` column appears only when a command has a
  budget or something failed. Otherwise every cell would say `PASS`.
- Why a geometric mean could not be computed is shown in the terminal, not in
  Markdown.

The tools named in `report.versions` follow the machine line, one per line,
such as `- jc: jc version 1.25.7`.

## Publish results in documentation

A page that shows benchmark results can be regenerated with one command
instead of copying numbers by hand. Mark the part himorime owns with two
comment lines:

```markdown
# Compare

## Speed

The numbers below are measured with himorime on the machine named under the
table.

<!-- himorime:begin speed -->
<!-- himorime:end speed -->

## Features
```

Then update it:

```console
$ himorime run --quiet --format markdown --output docs/compare.md --section speed bench
```

himorime replaces everything between the two lines with the report, with one
blank line inside each marker, and keeps every other byte of the page,
including its line endings. Running it again with the same results writes the
same file. The suite heading is one level below the nearest heading above the
begin line (`###` under `## Speed`), its tables one level further, and never
deeper than `######`; without a heading above, they are `##` and `###`.
Headings and marker lines inside fenced code blocks are ignored.

- `--section` needs `--output FILE` and `--format markdown`; anything else is
  a usage error (exit 3). A suite does the same with `section:` under
  `report.outputs`.
- The file must already exist and hold exactly one begin line and, after it,
  exactly one end line. A missing file, a missing, repeated or misordered
  marker fails the run with exit 4, and the message names the file, the
  section and what is wrong.
- The page is written to a temporary file next to it and renamed into place,
  so a failed run never leaves a truncated page.

List the compared tools under `report.versions` so the page says which
versions were measured:

```yaml
report:
  versions:
    jc: [jc, --version]
    jo: [jo, -v]
```

## GitHub Actions job summary and annotations

`--format github`, `--summary FILE` and `himorime ci` write the Markdown report
under a one-line verdict such as `✅ himorime benchmark comparison: no
regression` or `❌ himorime benchmarks: budget exceeded`, so the outcome is
visible at the top of the job page.

When `GITHUB_ACTIONS` is `true`, `run`, `compare` and `ci` also print one
workflow annotation per problem to standard error, which GitHub shows on the
run page and the pull request:

```text
::error file=himorime.yaml,title=himorime%3A performance budget exceeded::df large / jc: peak rss max budget <= 16.00MiB, measured 31.52MiB
::error file=himorime.yaml,title=himorime%3A performance regression::df large / jsonize: latency 14.20ms -> 16.41ms (+2.21ms, +15.6%25; tolerance +10%25)
::warning file=himorime.yaml,title=himorime%3A inconclusive comparison::...
::error file=himorime.yaml,title=himorime%3A metric could not be measured::...
::error file=himorime.yaml,title=himorime%3A benchmark could not run::...
```

The titles tell a performance failure (budget, regression) apart from a
failure to measure (metric, benchmark), and so does the exit status: 1 for
the former, 4 or 6 for the latter. See [Exit codes](/exit-codes/).
