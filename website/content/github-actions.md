---
title: GitHub Actions
description: Run read-only benchmarks on pull requests and post their report safely from a separate workflow.
toc: true
aliases: ["/security/"]
---

## A workflow

<!-- example: examples/github-actions/benchmark.yml -->
```yaml
# Copy this file to .github/workflows/benchmark.yml.
# Recipe: fail a GitHub Actions job when a performance budget is violated.
# https://nao1215.github.io/himorime/cookbook/#fail-a-github-actions-job-when-a-performance-budget-is-violated
name: Benchmark

on:
  pull_request:

# Read-only: himorime needs no secret and no write token, so pull requests from
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
      # Go builds the suite's program; drop it if your suite needs no Go.
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version: stable
      # Installs a prebuilt, checksum-verified himorime release.
      - uses: nao1215/setup-himorime@b2896dee554ef31ea979f0388d9be22779ce73a2 # v0.1.1
      # Compares the pull request base and head, writes a job summary and
      # annotations, and saves JSON for the separate comment workflow.
      - run: himorime ci --format json --output "$RUNNER_TEMP/himorime.json"
      # The separate workflow_run workflow downloads this fixed artifact to
      # post a comment without giving this pull request write access.
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

On the pull request that adds the suite, the base revision has no suite directory yet. The log, the job summary and a notice annotation say the suite is new in this revision, the working tree is measured alone and its budgets are checked (see [What fails the run](/regression-detection/#what-fails-the-run)); comparisons start with the next pull request.

The same suite file and the same command work on a laptop: `himorime compare --against main` does steps 1 to 3 and 5 against a branch you name, and `himorime run` checks the budgets without a base revision.

## A pull request comment

The first workflow keeps the pull request job read-only, saves its JSON report as `himorime-report`, and can run for forks. Add this second workflow to put that report in one updated pull request comment:

<!-- example: examples/github-actions/comment.yml -->
```yaml
# Copy this file to .github/workflows/comment-benchmark.yml.
# Posts the artifact from the Benchmark workflow without checking out or
# executing pull request code.
name: Comment benchmark result

on:
  workflow_run:
    workflows: [Benchmark]
    types: [completed]

permissions:
  actions: read
  contents: read
  pull-requests: write

jobs:
  comment:
    if: github.event.workflow_run.event == 'pull_request'
    runs-on: ubuntu-latest
    steps:
      - uses: nao1215/setup-himorime@b2896dee554ef31ea979f0388d9be22779ce73a2 # v0.1.1
      - uses: actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c # v8.0.1
        with:
          name: himorime-report
          run-id: ${{ github.event.workflow_run.id }}
          github-token: ${{ github.token }}
          path: ${{ runner.temp }}
      - run: himorime comment "$RUNNER_TEMP/himorime.json"
        env:
          GITHUB_TOKEN: ${{ github.token }}
```

It runs after `Benchmark` completes, downloads only that run's artifact, and does not check out the pull request. `himorime comment` accepts only the trusted `workflow_run` payload, so the JSON report cannot select a different repository or pull request. The comment step exits 0 when the report found a regression; the Benchmark workflow keeps the performance result. Its default footer is `<sub>Generated by [himorime](https://github.com/nao1215/himorime)</sub>`; a trusted reporter workflow can omit it with `himorime comment --hide-footer "$RUNNER_TEMP/himorime.json"`.

## Why did the job fail?

| Exit | Last log line | Annotation title | Meaning |
|---|---|---|---|
| `1` | `himorime: exit 1: performance check failed: ...` | `himorime: performance budget exceeded`, `himorime: performance regression` | The code got slower, less productive or hungrier. |
| `4` | `himorime: exit 4: the measurement did not complete: ...` | `himorime: benchmark could not run` | A command, hook, build or Git operation failed. |
| `6` | `himorime: exit 6: a requested metric could not be measured; ...` | `himorime: metric could not be measured` | The platform cannot measure a requested metric, or did not report it. |
| `2`, `3` | the validation or usage error | none | The suite or the command line is wrong; nothing ran. |

To keep performance advisory while still failing when the measurement breaks, accept only status 1 in the step: `himorime ci || [ $? -eq 1 ]`. The job summary and annotations still report the regression.

## Security

- A suite runs its declared commands with the current user's privileges. Treat it like a script, not as a sandbox for untrusted code.
- The Benchmark workflow needs only `contents: read`; it has no secrets or write token. himorime refuses `pull_request_target`, which would execute pull request commands with base-repository credentials.
- The comment workflow runs from the base branch after the source workflow completes. It has `pull-requests: write`, but it downloads one artifact and never checks out or executes pull request code. This also lets fork pull requests receive the same comment.
- Keep the token only in the comment workflow. A suite executes commands from the pull request, so it must not receive the token that writes comments.

Report vulnerabilities through the repository's [private security advisory form](https://github.com/nao1215/himorime/security/advisories/new). The [security policy](https://github.com/nao1215/himorime/blob/main/SECURITY.md) lists another contact method.

## Checkout

The base commit must exist in the clone. Use `fetch-depth: 0`, or fetch the base explicitly. With the default shallow clone, himorime stops with:

```text
himorime ci: revision "…" is not a commit in …; fetch it first (in GitHub Actions, check out with fetch-depth: 0)
```

For a `pull_request` event, `actions/checkout` checks out the merge commit of the pull request into its base branch, so the head measured is what would be merged.

## Keeping the report

The workflow above saves `himorime-report` even when a benchmark exits 1, 4 or 6. Download it from the run page, or use the comment workflow to publish its compact summary.

## Other CI systems

Nothing in himorime depends on GitHub except reading the event payload. On any CI, name the base yourself:

```console
$ himorime ci --against origin/main
$ HIMORIME_BASE_REF=origin/main himorime ci
```

## Runner noise

Hosted runners are shared virtual machines. Expect inconclusive results for small tolerances on fast commands, and read [Regression detection](/regression-detection/) before tightening `max_percent`. Budgets are best kept generous in CI, as a guard against order-of-magnitude slowdowns; set `min_difference` so tiny absolute changes never fail a pull request, and `gate: false` on a metric you want reported but not enforced, such as latency when CPU time is the gate. The recipe [Cope with noise on GitHub-hosted runners](/cookbook/#cope-with-noise-on-github-hosted-runners) has a runnable example.
