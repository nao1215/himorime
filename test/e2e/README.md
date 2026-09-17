# End-to-end tests

The suite under `test/e2e/atago` runs a built `yahiko` as a real process, the
way users and CI run it, with [atago](https://github.com/nao1215/atago). It
pins the command-line contract: exit codes, where output goes, table
structure, JSON and CSV report shapes, Git worktree handling, the GitHub
Actions adapter, and every Cookbook recipe.

```shell
go install github.com/nao1215/atago@v0.22.0
go run ./test/e2e/run                                      # every spec
go run ./test/e2e/run test/e2e/atago/compare.atago.yaml    # one spec
YAHIKO_BINARY=dist/yahiko_linux_amd64_v1/yahiko go run ./test/e2e/run   # a prebuilt binary
```

`test/e2e/run` is a Go program so the same entry point works on Linux, macOS
and Windows. It builds `yahiko` and the helper into a temporary directory put
first on `PATH`, sets `YAHIKO_REPO` to the repository root, clears GitHub
Actions variables so a CI job's real event cannot leak in, runs atago, and
fails if the repository's `git status` changed.

## Specs

| Spec | What it pins |
|---|---|
| `cli` | help, version, usage errors (exit 3), `init`, `validate` messages with file:line:column and exit 2, `list` text and JSON, completion scripts |
| `run` | tables, JSON with raw samples, CSV/Markdown/summary files, budgets (exit 1), failing commands and masked secrets (exit 4), timeouts that stop the process tree, tags and filters, stdin fixtures, `prepare_each`, cleanup after success and failure, interrupts (POSIX), shell pipelines, warm and cold caches, adaptive runs, geometric mean rules |
| `compare` | scratch Git repositories: pass, regression in uncommitted changes, improvement, inconclusive and `--fail-on-inconclusive`, unknown revisions, a base build failure, interrupts (POSIX), `regression.commands`; the working tree, branches and worktree list are unchanged afterwards |
| `ci` | simulated GitHub Actions events and job summary: pull_request, push, merge_group, `pull_request_target` refusal, missing base guidance, shallow clones, `YAHIKO_BASE_REF` |
| `cookbook` | one scenario per Cookbook recipe, named exactly like its heading, running the suite under `examples/` |

## The helper

`test/e2e/helper` is a small Go program standing in for `sleep`, `cp`, `rm`
and friends, so no scenario depends on a POSIX userland. Its delays are tens
to hundreds of milliseconds, and scenarios assert on classifications and
structure, never on millisecond differences. Subcommands: `sleep`,
`sleep-from` (a build copies a delay file to `${artifact}`, so each revision
has its own speed), `alternate` (bimodal noise), `copy`, `exit`, `gen`,
`count`, `spawn` (a grandchild that proves the process tree was stopped),
`counter`, `cache`, `write`, `remove`, `require`, `replace`, `consume`,
`event` (a GitHub event payload for a repository's HEAD) and `interrupt`.

Scenarios that deliver POSIX signals are skipped on Windows; process-tree
termination there is covered by the timeout scenarios and the unit tests of
`internal/proc`, which use a Job Object.

## Writing a scenario

- Escape yahiko's variables inside atago fixture content as `$${artifact}`,
  because atago expands `${...}` itself.
- A Cookbook recipe needs a heading on the Cookbook page, an example under
  `examples/`, and a scenario here with the same name; `internal/docgen`
  checks all three.
