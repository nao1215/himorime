---
title: GitHub Actions
description: Run benchmarks on pull requests and let setup-himorime post a short problem-only notification.
toc: true
aliases: ["/security/"]
---

These examples require himorime v0.4.0 or later; earlier releases cannot read the simplified version 1 layout. This repository currently uses a frozen historical suite with its pinned v0.2.0 observer.

## A workflow

<!-- example: examples/github-actions/benchmark.yml -->
```yaml
# Copy this file to .github/workflows/benchmark.yml.
# Recipe: fail a GitHub Actions job when a performance budget is violated.
# https://nao1215.github.io/himorime/cookbook/#fail-a-github-actions-job-when-a-performance-budget-is-violated
name: Benchmark

on:
  pull_request:

# Fork PRs run with read-only permissions and receive no comment.
permissions:
  contents: read

jobs:
  benchmark:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          # The base commit must exist locally for the temporary worktree.
          fetch-depth: 0
          persist-credentials: false
      # Go builds the suite's program; drop it if your suite needs no Go.
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version: stable
      # Installs a prebuilt, checksum-verified himorime release.
      - uses: nao1215/setup-himorime@14aeeb3fe55ad42cf29ee0802d578820faac3897 # v0.1.2
      # Compares the pull request base and head, writes a job summary and
      # annotations, and saves JSON for setup-himorime's automatic comment.
      - run: himorime ci --format json --output "$RUNNER_TEMP/himorime.json"
      # Keep raw samples available from the run page.
      - uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1
        if: always()
        with:
          name: himorime-report
          path: ${{ runner.temp }}/himorime.json
          retention-days: 7
```

`himorime ci`:

1. Finds the base commit from `--against`, `$HIMORIME_BASE_REF` or the event payload: `pull_request.base.sha` for `pull_request` and reviews, `merge_group.base_sha` for merge queues, and `before` for `push`.
2. Checks the base out into a temporary worktree and compares it with the checked-out head, exactly like `himorime compare`.
3. Writes the table to the log without colors, and appends the Markdown summary to `$GITHUB_STEP_SUMMARY`.
4. Prints an annotation for every missed budget, regression, inconclusive comparison and failure, so they show on the pull request. A regression of a metric with `gate: false` is a notice, not an error.
5. Exits 1 on a confirmed regression of a gated metric or an exceeded budget, 0 otherwise. Add `--fail-on-inconclusive` to fail on inconclusive gated comparisons too. See [What fails the run](/regression-detection/#what-fails-the-run).

On the pull request that adds the suite, the base revision has no suite file yet. The log, the job summary and a notice annotation say the suite is new in this revision, the working tree is measured alone and its budgets are checked (see [What fails the run](/regression-detection/#what-fails-the-run)); comparisons start with the next pull request.

The same suite file and the same command work on a laptop: `himorime compare --against main` does steps 1 to 3 and 5 against a branch you name, and `himorime run` checks the budgets without a base revision.

## Require every budget to pass

To reject undecidable budgets as a workflow policy, add this step immediately after the measurement step above. It uses `jq` on the Ubuntu runner and requires at least one budget, all with status `pass`, in a successful report:

```yaml
      - name: Require every budget to pass
        shell: bash
        run: |
          jq -e '
            .schema_version == "1" and .summary.exit_code == 0 and
            ([.suites[].benchmarks[].commands[].budgets[]?] |
              length > 0 and all(.status == "pass"))
          ' "$RUNNER_TEMP/himorime.json"
```

This rejects `skipped`, `fail` and `no_data` budgets; their reasons remain in the full report. A memory budget proven by its measurement floor still passes. Skipped comparisons alone do not fail this policy: `summary.skipped` combines budgets and comparisons, so it is not a budget-policy check. `--fail-on-inconclusive` applies only to gated comparisons.

The check covers budgets in the selected benchmarks, not budgets removed from the suite or excluded by filters. Do not add `continue-on-error` to the measurement step: its existing failures must still fail the job. A missing file, invalid JSON or unsupported schema version also fails this step; no himorime verdict or exit code is changed.

## A pull request comment

The pinned setup-himorime action above posts a short notification at the end of the job from `$RUNNER_TEMP/himorime.json`, even if the measurement step fails after writing that report. It shows only the overall status and problems, with a link to the full tables in the run's logs. No second workflow, comment command or helper executable is needed.

Grant `pull-requests: write` only to the benchmark job. Automatic comments are supported on same-repository `pull_request` events; fork PRs and other events never post. A rerun replaces the previous bot-owned report. Missing or stale JSON produces no comment; malformed reports or API failures fail the post step. Use one reporting benchmark job per PR.

## Why did the job fail?

| Exit | Last log line | Annotation title | Meaning |
|---|---|---|---|
| `1` | `himorime: exit 1: performance check failed: ...` | `himorime: performance budget exceeded`, `himorime: performance regression` | The code got slower, less productive or hungrier. |
| `4` | `himorime: exit 4: the measurement did not complete: ...` | `himorime: benchmark could not run` | A command, hook, build or Git operation failed. |
| `6` | `himorime: exit 6: a requested metric could not be measured or a required budget could not be assessed` | `himorime: metric could not be measured` or `himorime: required budget could not be assessed` | The platform cannot measure a requested metric, did not report it, or a required budget has no assessable value. |
| `2`, `3` | the validation or usage error | none | The suite or the command line is wrong; nothing ran. |

To keep performance advisory while still failing when the measurement breaks, accept only status 1 in the step: `himorime ci || [ $? -eq 1 ]`. The job summary and annotations still report the regression.

## Security

- A suite runs its declared commands with the current user's privileges. Treat it like a script, not as a sandbox for untrusted code.
- Same-repository PRs run in a job with comment-write permission. Grant it only to contributors you trust; do not pass the publication token to measured commands or persist checkout credentials.
- Fork PRs use read-only permissions and receive no comment. Do not enable write tokens for fork workflows. himorime refuses `pull_request_target`.

Report vulnerabilities through the repository's [private security advisory form](https://github.com/nao1215/himorime/security/advisories/new). The [security policy](https://github.com/nao1215/himorime/blob/main/SECURITY.md) lists another contact method.

## Checkout

The base commit must exist in the clone. Use `fetch-depth: 0`, or fetch the base explicitly. With the default shallow clone, himorime stops with:

```text
himorime ci: revision "…" is not a commit in …; fetch it first (in GitHub Actions, check out with fetch-depth: 0)
```

For a `pull_request` event, `actions/checkout` checks out the merge commit of the pull request into its base branch, so the head measured is what would be merged.

## Keeping the report

The workflow saves `himorime-report` after measurement reaches the report-writing step. A regression (exit 1) and execution failures during measurement still produce a report. Setup, worktree, configuration, or metric setup failures (exit 2, 4, or 6 before report writing) can stop before an artifact exists. Download the JSON artifact from the run page for raw samples.

## Other CI systems

Nothing in himorime depends on GitHub except reading the event payload. On any CI, name the base yourself:

```console
$ himorime ci --against origin/main
$ HIMORIME_BASE_REF=origin/main himorime ci
```

## Runner noise

Hosted runners are shared virtual machines. Expect inconclusive results for small tolerances on fast commands, and read [Regression detection](/regression-detection/) before tightening `max_percent`. Budgets are best kept generous in CI, as a guard against order-of-magnitude slowdowns; set `min_difference` so tiny absolute changes never fail a pull request, and `gate: false` on a metric you want reported but not enforced, such as latency when CPU time is the gate. The recipe [Cope with noise on GitHub-hosted runners](/cookbook/#cope-with-noise-on-github-hosted-runners) has a runnable example.
