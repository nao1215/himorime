---
title: GitHub Actions
description: Run yahiko ci on pull requests with read-only permissions, safe for forks, with a job summary and a failing check on confirmed regressions.
toc: true
---

## A workflow

<!-- example: examples/github-actions/benchmark.yml -->
```yaml
# Copy this file to .github/workflows/benchmark.yml.
# Recipe: fail a GitHub Actions job when a performance budget is violated.
# https://nao1215.github.io/yahiko/cookbook/#fail-a-github-actions-job-when-a-performance-budget-is-violated
name: Benchmark

on:
  pull_request:

# Read-only: yahiko needs no secret and no write token, so pull requests from
# forks run it safely. Do not use pull_request_target, which would run the
# pull request's code with the base repository's secrets.
permissions:
  contents: read

jobs:
  benchmark:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          # The base commit must exist locally for the temporary worktree.
          fetch-depth: 0
          persist-credentials: false
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version: stable
      - run: go install github.com/nao1215/yahiko@latest
      # Finds the pull request's base commit from the event, compares it with
      # the checked-out head on every metric the suite measures, writes a
      # summary to the job page and annotations to the pull request, and
      # exits 1 on a missed budget or a confirmed regression. Exit 4 or 6
      # means the measurement itself failed, not the performance.
      - run: yahiko ci
```

`yahiko ci`:

1. Finds the base commit: `--against`, else `$YAHIKO_BASE_REF`, else the event
   payload — `pull_request.base.sha` for `pull_request` (and pull request
   reviews), `merge_group.base_sha` for merge queues, `before` for `push`.
2. Checks the base out into a temporary worktree and compares it with the
   checked-out head, exactly like `yahiko compare`.
3. Writes the table to the log without colors, and appends the Markdown
   summary to `$GITHUB_STEP_SUMMARY`.
4. Prints an annotation for every missed budget, regression, inconclusive
   comparison and failure, so they show on the pull request. A regression of a
   metric with `gate: false` is a notice, not an error.
5. Exits 1 on a confirmed regression of a gated metric or an exceeded budget,
   0 otherwise. Add `--fail-on-inconclusive` to fail on inconclusive gated
   comparisons too. See
   [What fails the run](/regression-detection/#what-fails-the-run).

The same suite file and the same command work on a laptop: `yahiko compare
--against main` does steps 1 to 3 and 5 against a branch you name, and
`yahiko run` checks the budgets without a base revision.

## Why did the job fail?

| Exit | Last log line | Annotation title | Meaning |
|---|---|---|---|
| `1` | `yahiko: exit 1: performance check failed: ...` | `yahiko: performance budget exceeded`, `yahiko: performance regression` | The code got slower, less productive or hungrier. |
| `4` | `yahiko: exit 4: the measurement did not complete: ...` | `yahiko: benchmark could not run` | A command, hook, build or Git operation failed. |
| `6` | `yahiko: exit 6: a requested metric could not be measured; ...` | `yahiko: metric could not be measured` | The platform cannot measure a requested metric, or did not report it. |
| `2`, `3` | the validation or usage error | none | The suite or the command line is wrong; nothing ran. |

To keep performance advisory while still failing when the measurement breaks,
accept only status 1 in the step: `yahiko ci || [ $? -eq 1 ]`. The job summary
and annotations still report the regression.

## Security

- The workflow needs only `contents: read`. yahiko never calls the GitHub API,
  never comments on the pull request, and needs no secret.
- It runs on `pull_request`, so a pull request from a fork runs with a
  read-only token and no secrets. yahiko refuses to run for
  `pull_request_target`, which would execute the pull request's commands with
  the base repository's secrets and a write token.
- The suite executes commands. On `pull_request`, those commands come from the
  pull request itself, like its tests do; that is why the job must not have
  secrets or write access.

## Checkout

The base commit must exist in the clone. Use `fetch-depth: 0`, or fetch the
base explicitly. With the default shallow clone, yahiko stops with:

```text
yahiko ci: revision "…" is not a commit in …; fetch it first (in GitHub Actions, check out with fetch-depth: 0)
```

For a `pull_request` event, `actions/checkout` checks out the merge commit of
the pull request into its base branch, so the head measured is what would be
merged.

## Keeping the report

```yaml
      - run: yahiko ci --format json --output benchmark.json
      - uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1
        if: always()
        with:
          name: benchmark
          path: benchmark.json
```

## Other CI systems

Nothing in yahiko depends on GitHub except reading the event payload. On any
CI, name the base yourself:

```console
$ yahiko ci --against origin/main
$ YAHIKO_BASE_REF=origin/main yahiko ci
```

## Runner noise

Hosted runners are shared virtual machines. Expect inconclusive results for
small tolerances on fast commands, and read
[Regression detection](/regression-detection/) before tightening
`max_percent`. Budgets are best kept generous in CI, as a guard against
order-of-magnitude slowdowns; set `min_difference` so tiny absolute changes
never fail a pull request, and `gate: false` on a metric you want reported but
not enforced, such as latency when CPU time is the gate. The recipe
[Cope with noise on GitHub-hosted runners](/cookbook/#cope-with-noise-on-github-hosted-runners)
has a runnable example.
