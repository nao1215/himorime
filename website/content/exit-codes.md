---
title: Exit codes
description: yahiko's exit statuses are a stable contract. What each one means and which one wins when several apply.
---

yahiko's exit status is part of its public interface and does not change
within a major version.

<!-- BEGIN GENERATED: exit-codes -->
| Code | Name | Meaning |
|---|---|---|
| `0` | ok | Every selected benchmark completed, and every budget and regression check passed. Inconclusive comparisons also exit 0 unless --fail-on-inconclusive is given. |
| `1` | failed | A performance budget was exceeded, a regression was confirmed on any metric, or --fail-on-inconclusive was given and a comparison was inconclusive. The measurement itself succeeded. |
| `2` | config | A suite file is not valid YAML, does not match the schema, or fails semantic validation. Nothing was executed. |
| `3` | usage | The command line is invalid: an unknown flag or command, a missing argument, or no benchmark matched the selection. |
| `4` | execution | A measured command, hook or build failed or timed out, a Git operation failed, the base revision could not be resolved, or the run was interrupted. |
| `5` | internal | yahiko hit an unexpected internal error. Please report it. |
| `6` | metric | A requested metric (throughput, cpu or memory) could not be measured: this platform does not support it and metrics.unsupported is fail, or the operating system did not report it. The commands themselves ran. |
<!-- END GENERATED: exit-codes -->

## When several apply

Validation happens before anything runs, so exit 2 and exit 3 never combine
with the others. After measuring, the status is decided in this order:

1. `4` when any command, hook or build failed, a report could not be written,
   cleanup failed, or the run was interrupted. Results of a run that did not
   complete are not presented as a pass.
2. `1` when any budget was exceeded or any regression was confirmed.
3. `1` when `--fail-on-inconclusive` was given and any comparison was
   inconclusive.
4. `0` otherwise.

## Inconclusive results

An inconclusive comparison exits `0` by default: shared CI runners are noisy,
and failing a pull request on a result that cannot be told apart from noise
teaches people to ignore the check. The table, the JSON report and the job
summary still say `INCONCLUSIVE` and why.

Pass `--fail-on-inconclusive` to `compare` or `ci` to exit `1` instead, for
example on a dedicated benchmark machine where noise is low and an
inconclusive result deserves attention.

The JSON report carries the status it produced in `summary.exit_code`.
