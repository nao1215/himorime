---
title: GitHub Actions
description: Run benchmarks on pull requests and let setup-himorime post a short problem-only notification.
toc: true
aliases: ["/security/"]
---

These examples need himorime v0.4.0 or later. This repository's own workflow still uses a frozen suite with its pinned v0.2.0 observer.

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
2. Compares it with the checked-out head in a temporary worktree, like `himorime compare`.
3. Writes the table to the log and the Markdown summary to `$GITHUB_STEP_SUMMARY`.
4. Annotates every missed budget, regression, inconclusive comparison and failure. A regression of a metric with `gate: false` is a notice.
5. Exits 1 on a gated regression or an exceeded budget, 0 otherwise. `--fail-on-inconclusive` also fails on inconclusive gated comparisons. See [What fails the run](/regression-detection/#what-fails-the-run).

On the pull request that adds the suite, the working tree is measured alone and its budgets are checked; comparisons start with the next pull request. Locally, `himorime compare --against main` does the same comparison, and `himorime run` checks budgets without a base.

## Require every budget to pass

To fail on budgets that could not be decided, add this step after the measurement step. It requires at least one budget, all with status `pass`, in a successful report:

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

It rejects `skipped`, `fail` and `no_data` budgets; a memory budget met by its measurement floor still passes. Skipped comparisons do not fail it. Do not set `continue-on-error` on the measurement step. A missing file, invalid JSON or an unsupported schema version also fails this step.

## A pull request comment

At the end of the job, setup-himorime posts a short comment from `$RUNNER_TEMP/himorime.json`, even if the measurement step failed after writing it. The comment shows the overall status and the problems, with a link to the full tables in the logs.

Grant `pull-requests: write` only to the benchmark job. Only same-repository `pull_request` events post; a rerun replaces the previous comment. Missing or stale JSON posts nothing, and a malformed report or API failure fails the post step. Use one reporting job per PR.

## Why did the job fail?

| Exit | Last log line | Annotation title | Meaning |
|---|---|---|---|
| `1` | `himorime: exit 1: performance check failed: ...` | `himorime: performance budget exceeded`, `himorime: performance regression` | The code got slower, less productive or hungrier. |
| `4` | `himorime: exit 4: the measurement did not complete: ...` | `himorime: benchmark could not run` | A command, hook, build or Git operation failed. |
| `6` | `himorime: exit 6: a requested metric could not be measured or a required budget could not be assessed` | `himorime: metric could not be measured` or `himorime: required budget could not be assessed` | The platform cannot measure a requested metric, did not report it, or a required budget has no assessable value. |
| `2`, `3` | the validation or usage error | none | The suite or the command line is wrong; nothing ran. |

To keep performance advisory but still fail when measurement breaks, run `himorime ci || [ $? -eq 1 ]`.

## Security

A suite runs its commands with the current user's privileges, like a script. Same-repository PRs run with comment-write permission, so do not pass the token to measured commands or persist checkout credentials. Fork PRs get read-only permissions and no comment, and himorime refuses `pull_request_target`.

Report vulnerabilities through the [private security advisory form](https://github.com/nao1215/himorime/security/advisories/new); the [security policy](https://github.com/nao1215/himorime/blob/main/SECURITY.md) lists another contact.

## Checkout

The base commit must exist in the clone, so use `fetch-depth: 0` or fetch it. With a shallow clone, himorime stops with:

```text
himorime ci: revision "…" is not a commit in …; fetch it first (in GitHub Actions, check out with fetch-depth: 0)
```

On `pull_request`, `actions/checkout` checks out the merge commit, so the head measured is what would be merged.

## Keeping the report

The `himorime-report` artifact holds the raw samples. Regressions and failures during measurement still write it; failures before measurement (exit 2, 4 or 6 during setup) can stop before it exists.

## Other CI systems

Only the event payload is GitHub-specific. Elsewhere, name the base:

```console
$ himorime ci --against origin/main
$ HIMORIME_BASE_REF=origin/main himorime ci
```

## Runner noise

Expect inconclusive results for small tolerances on fast commands, and read [Regression detection](/regression-detection/) before tightening `max_percent`. Keep CI budgets generous, set `min_difference`, and use `gate: false` for a metric you only want reported. See [Cope with noise on GitHub-hosted runners](/cookbook/#cope-with-noise-on-github-hosted-runners).
