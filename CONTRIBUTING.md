# Contributing to himorime

Thank you for helping. Bug reports, fixes, tests, documentation and reviews are all welcome.

## Before you start

- Bug: open an issue with the himorime version, your OS, the suite, the command line, and what you expected and got.
- Feature: open an issue first, so the direction can be agreed before you write code. himorime deliberately stays small; see the non-goals in the documentation's [Comparison](https://nao1215.github.io/himorime/comparison/).
- Security issue: follow [SECURITY.md](./SECURITY.md), not a public issue.

## Development

Go 1.26 or later, `git`, and for the end-to-end suite [atago](https://github.com/nao1215/atago) are required.

```shell
make tools     # golangci-lint, atago, goreleaser, actionlint, govulncheck
make build     # ./himorime
make test      # unit tests with coverage
make test-race # unit tests with the race detector
make lint      # golangci-lint for every target OS
make e2e       # builds himorime and runs test/e2e/atago
make docs      # regenerate generated documentation sections
make check     # fmt, vet, lint, test, test-race, e2e
```

## Rules of thumb

- Every behavior change comes with a test. Unit tests live next to the code; anything a user observes as a process (exit codes, output, files, Git worktrees, interrupts) gets an E2E scenario in `test/e2e/atago`.
- E2E scenarios never assert on millisecond differences. Use the helper in `test/e2e/helper`, with differences of tens of milliseconds, and assert on classifications, table structure, report schemas and exit codes.
- The suite schema (`schema/himorime.schema.json`) and the Go loader must agree. When you add a key, add it to both, and extend `internal/config/testdata/parity`.
- The JSON report is a contract: once released, add fields, never rename or remove them within `schema_version` `"1"`, and update `schema/report.schema.json`. Until the first release the format may still be cleaned up; record such a change under `### Changed` in `CHANGELOG.md`.
- Changes to the regression classifier come with `make calibration` numbers before and after (`internal/stats/calibration_test.go`).
- Exit codes (`internal/exitcode`) are a contract too.
- Documentation sections marked `BEGIN GENERATED` and example blocks are generated. Edit the source (command table, schema, example suites) and run `make docs`.
- In Markdown, write each paragraph and each list item on one line, without bold text. Code comments wrap as usual.
- Every Cookbook recipe is an example under `examples/` and a scenario in `test/e2e/atago/cookbook.atago.yaml` with the same name as the heading.
- Keep dependencies minimal and GitHub Actions pinned to commit SHAs. Workflows other than `release.yml` get read-only permissions.

## Pull requests

- Keep changes focused, and describe the problem and the solution.
- Update `CHANGELOG.md` under `## [Unreleased]` for user-visible changes.
- Make sure `make check` passes and consider Linux, macOS and Windows.
