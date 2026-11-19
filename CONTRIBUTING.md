# Contributing to himorime

Thank you for helping. Bug reports, fixes, tests, documentation and reviews are all welcome.

## Before you start

- Bug: open an issue with the himorime version, your OS, the suite, the command line, and what you expected and got.
- Feature: open an issue first, so the direction can be agreed before you write code. himorime deliberately stays small; see [what himorime does not measure](https://nao1215.github.io/himorime/metrics/#what-himorime-does-not-measure).
- Security issue: follow [SECURITY.md](./SECURITY.md), not a public issue.

## Development

Go 1.26 or later, `git`, and for the end-to-end suite [atago](https://github.com/nao1215/atago) are required.

```shell
make tools     # golangci-lint, atago, goreleaser, actionlint, govulncheck
make build     # ./himorime
make test      # unit tests with coverage
make coverage  # combined unit and CLI E2E coverage (needs atago)
make test-race # unit tests with the race detector
make lint      # golangci-lint for every target OS
make e2e       # builds himorime and runs test/e2e/atago
make docs      # regenerate generated documentation sections
make check     # fmt, vet, lint, test, test-race, e2e
```

## Testing

- Unit tests run with `make test` on Linux, macOS and Windows, at the go.mod minimum and the newest Go (`unit_test.yml`).
- The CLI E2E suite in `test/e2e/atago` runs with `make e2e` on all three systems (`e2e.yml`), and again against the snapshot release binaries (`release-smoke.yml`).
- `make calibration` measures the false regression and missed regression rates of the classifier (`internal/stats/calibration_test.go`).
- `internal/stats/properties_test.go` checks relations between comparisons with [rapid](https://pkg.go.dev/pgregory.net/rapid): the verdict does not change with the unit (both sides scaled by a power of two), flipping the better direction swaps regression and improvement, identical samples are never a change, and a slower head never lowers the regression probability. They run in `make test` with rapid's default 100 cases; pass `-rapid.checks=10000` to search longer. A failure prints the seed to rerun it with.
- `test/differential/rusage_test.go` (Linux and macOS) builds himorime and measures a fixed amount of memory and CPU work with both himorime and `/usr/bin/time` (GNU time on Linux, `time -l` on macOS), and fails when peak RSS differs by more than 2 MiB or 1%, or user or system time by more than 50 ms or 20%. For a peak at or below himorime's measurement floor, where himorime only claims the command used at most the floor, time must not have measured more than the floor. It runs in `go test ./...` and skips with `-short` or without `/usr/bin/time`.
- Fuzz targets (`func Fuzz*`) run for a short time on every pull request and longer every night (`fuzz.yml`); `make fuzz FUZZTIME=20s` runs them locally.
- Mutation testing with [gremlins](https://github.com/go-gremlins/gremlins) v0.6.0 (`mutation.yml`): pull requests mutate only the lines they change, and a weekly run mutates `internal/stats` and `internal/metric` and fails below a test efficacy of 90% and 92%. gremlins does not mutate switch case conditions, so `scripts/verdict_mutants.py` mutates the verdict switch of `internal/stats/compare.go` on every run and fails on a surviving mutant that it does not list as equivalent. Locally: `gremlins unleash --workers 4 --timeout-coefficient 10 ./internal/stats` and `python3 scripts/verdict_mutants.py`.
- Mutation baseline, 2026-11-19 (test efficacy = killed / (killed + lived)): `internal/stats` 94.53% (121 killed, 7 lived, 26 not covered), `internal/metric` 95.77% (68 killed, 3 lived, 14 not covered), verdict switch 18 killed, 6 equivalent. The remaining survivors are equivalent: `>` against `>=` where the two sides are equal floats (a dispersion exactly at max_cv, a change exactly at the tolerance plus epsilon), the end points of the percentile function, a percentile of 0 that the pattern already rejects, and ties in the aggregation order. Before the property tests and the tests added with this baseline, `internal/stats` was at 86.72% and `internal/metric` at 91.55%.

## Rules of thumb

- Add or update tests with every new feature, bug fix and behavior change; a bug fix comes with a test that fails without it. Unit tests live next to the code; anything a user observes as a process (exit codes, output, files, Git worktrees, interrupts) gets an E2E scenario in `test/e2e/atago`.
- E2E scenarios never assert on millisecond differences. Use the helper in `test/e2e/helper`, with differences of tens of milliseconds, and assert on classifications, table structure, report schemas and exit codes.
- The suite schema (`schema/himorime.schema.json`) and the Go loader must agree. When you add a key, add it to both, and extend `internal/config/testdata/parity`.
- The JSON report is a contract: once released, add fields, never rename or remove them within `schema_version` `"1"`, and update `schema/report.schema.json`.
- Changes to the regression classifier come with `make calibration` numbers before and after (`internal/stats/calibration_test.go`).
- Exit codes (`internal/exitcode`) are a contract too.
- Documentation sections marked `BEGIN GENERATED` and example blocks are generated. Edit the source (command table, schema, example suites) and run `make docs`.
- In Markdown, write each paragraph and each list item on one line, without bold text. Code comments wrap as usual.
- Every Cookbook recipe is an example under `examples/` and a scenario in `test/e2e/atago/cookbook.atago.yaml` with the same name as the heading.
- Keep dependencies minimal and GitHub Actions pinned to commit SHAs. Workflows get read-only permissions except `release.yml` and the benchmark job's `pull-requests: write` permission for automatic comments.

## Pull requests

- Keep changes focused, and describe the problem and the solution.
- Update `CHANGELOG.md` under `## [Unreleased]` for user-visible changes.
- Make sure `make check` passes and consider Linux, macOS and Windows.
