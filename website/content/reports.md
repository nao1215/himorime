---
title: Reports
description: yahiko's terminal table, JSON with raw samples, CSV, Markdown and GitHub Actions job summary, and what each column means.
toc: true
---

`--format` chooses what goes to standard output (or to `--output FILE`):
`table` (default), `json`, `csv`, `markdown` or `github`. `--summary FILE`
additionally appends a GitHub-flavored Markdown summary; `yahiko ci` does that
to `$GITHUB_STEP_SUMMARY` on its own. A suite can also write report files
after every run:

```yaml
report:
  outputs:
    - format: json
      path: bench/result.json
    - format: markdown
      path: bench/result.md
```

Progress lines go to standard error and never into a report. A report that
cannot be written fails the run with exit status 4.

## Terminal table

A plain run shows one row per command:

```text
BENCHMARK       COMMAND    MEDIAN    MEAN      STDDEV    RELATIVE    RESULT
df small        jsonize     1.82ms    1.86ms    0.09ms    1.00x       PASS
df small        jc         28.41ms   29.02ms    1.31ms   15.61x       PASS
```

- `RELATIVE` is the command's median divided by the median of the benchmark's
  `baseline` command, or of its fastest command when no baseline is set.
- `RESULT` is `PASS`, `OVER BUDGET` or `ERROR`. The lines under the table say
  which budget was missed, why a command failed, and the tail of its standard
  error.

A comparison shows one row per compared command:

```text
BENCHMARK       BASE       HEAD       CHANGE     CONFIDENCE    BUDGET    RESULT
df small         1.84ms     1.89ms      +2.7%       low         +10%     PASS
df large        14.20ms    16.41ms     +15.6%       98.1%       +10%     REGRESSION
```

- `BASE` and `HEAD` are the regression metric (median unless configured).
- `CHANGE` is `(HEAD - BASE) / BASE`.
- `CONFIDENCE` is the bootstrap probability that the change exceeds `BUDGET`
  in the direction it went, shown as `low` below 50%.
- `BUDGET` is the tolerated slowdown, `regression.max_percent`.
- `RESULT` is `PASS`, `IMPROVED`, `REGRESSION`, `INCONCLUSIVE`, `OVER BUDGET`
  or `ERROR`. See [Regression detection](/regression-detection/).

Durations are rounded for reading: nanoseconds, microseconds, milliseconds or
seconds with two decimals. Colors are used only on an interactive terminal
and never by `yahiko ci`; `--no-color` and `NO_COLOR` turn them off.

## Geometric mean

When a suite has at least two benchmarks and every benchmark ran the same
commands to completion, the table ends with the geometric mean of each
command's ratio across the cases: to the common baseline when all benchmarks
name the same one, otherwise to each case's fastest command. In a comparison
it is the geometric mean of the head/base ratios.

It is supplementary. The per-case rows are the result. An arithmetic mean of
ratios would let one large case dominate, so it is never used. If a command
is missing from a case, or any command failed or timed out, no overall number
is printed, and the report says why instead of quietly leaving the failure
out.

## JSON

`--format json` is the complete, machine-readable report, described by
[report.schema.json](https://raw.githubusercontent.com/nao1215/yahiko/main/schema/report.schema.json).

- `schema_version` is `"1"`. Fields are only added within a version.
- Every duration is an integer number of nanoseconds, before rounding.
  `samples_ns` holds every measured run in execution order.
- `environment` records the operating system, architecture, CPU model, logical
  CPU count, Go version and CI provider. `yahiko_version`, `seed` and `git`
  (head and base commits, and whether the working tree was dirty) complete what
  is needed to reproduce a run. Host names, user names and environment
  variables are never recorded.
- `relative.vs_baseline` and `relative.vs_fastest` are kept apart.
- `comparison` holds the change, the bootstrap interval, both tail
  probabilities, the configured thresholds, the verdict and the reason for an
  inconclusive one.
- `summary.exit_code` is the exit status of the run.

```console
$ yahiko run --format json --output result.json
$ jq '.suites[].benchmarks[].commands[] | {name, median: .head.median_ns}' result.json
```

## CSV

`--format csv` writes one row per command, and per revision in a comparison,
with a fixed header:

```text
suite,benchmark,command,side,result,count,mean_ns,median_ns,stddev_ns,min_ns,max_ns,cv,vs_baseline,vs_fastest,change_percent,ci_low_percent,ci_high_percent,probability_regression,error_kind,error_message
```

Fields holding commas, quotes or line breaks are quoted.

## Markdown

`--format markdown` writes a table per suite with median, mean, standard
deviation, min, max, run count, both ratios and the result, followed by notes
and one line describing the machine. Names are escaped, so a `|` in a
benchmark name cannot break the table. It is meant to be pasted into a README,
a pull request, a blog post or release notes.

## GitHub Actions job summary

`--format github`, `--summary FILE` and `yahiko ci` write the Markdown report
under a one-line verdict such as `✅ yahiko benchmark comparison: no
regression`, so the outcome is visible at the top of the job page.
