---
title: GitHub Actions
description: Run yahiko ci on pull requests with read-only permissions, safe for forks, with a job summary and a failing check on confirmed regressions.
toc: true
---

## A workflow

<!-- example: examples/github-actions/benchmark.yml -->
```yaml
# Copy this file to .github/workflows/benchmark.yml.
# Recipe: compare a pull request with its base in GitHub Actions.
# https://nao1215.github.io/yahiko/cookbook/#compare-a-pull-request-with-its-base-in-github-actions
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
      # the checked-out head, writes a summary to the job page, and exits 1 on
      # a confirmed regression.
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
4. Exits 1 on a confirmed regression or exceeded budget, 0 otherwise.
   Add `--fail-on-inconclusive` to fail on inconclusive results too.

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
order-of-magnitude slowdowns.
