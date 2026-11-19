[![CI](https://github.com/nao1215/himorime/actions/workflows/unit_test.yml/badge.svg)](https://github.com/nao1215/himorime/actions/workflows/unit_test.yml)
[![E2E](https://github.com/nao1215/himorime/actions/workflows/e2e.yml/badge.svg)](https://github.com/nao1215/himorime/actions/workflows/e2e.yml)
[![Lint](https://github.com/nao1215/himorime/actions/workflows/lint.yml/badge.svg)](https://github.com/nao1215/himorime/actions/workflows/lint.yml)
[![Gitleaks](https://github.com/nao1215/himorime/actions/workflows/gitleaks.yml/badge.svg)](https://github.com/nao1215/himorime/actions/workflows/gitleaks.yml)
[![Trivy](https://github.com/nao1215/himorime/actions/workflows/trivy.yml/badge.svg)](https://github.com/nao1215/himorime/actions/workflows/trivy.yml)
[![govulncheck](https://github.com/nao1215/himorime/actions/workflows/security.yml/badge.svg)](https://github.com/nao1215/himorime/actions/workflows/security.yml)
[![Gosec](https://github.com/nao1215/himorime/actions/workflows/gosec.yml/badge.svg)](https://github.com/nao1215/himorime/actions/workflows/gosec.yml)
[![tested with atago](https://img.shields.io/badge/tested%20with-atago-7c3aed?logo=data:image/svg%2Bxml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCI%2BPHBhdGggZmlsbD0iI2ZmZiIgZD0iTTMuNiA0LjIgMTEuOSAxMmwtOC4zIDcuOC0xLjktMi4yTDcuOSAxMiAxLjcgNi40eiIvPjxyZWN0IGZpbGw9IiNmZmYiIHg9IjEyLjYiIHk9IjE3LjIiIHdpZHRoPSI5LjciIGhlaWdodD0iMi44IiByeD0iMS40Ii8%2BPC9zdmc%2B&logoColor=white)](https://github.com/nao1215/atago)
[![measured with himorime](https://img.shields.io/badge/measured%20with-himorime-d9480f?logo=data:image/svg%2Bxml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCI%2BPHBhdGggZmlsbD0ibm9uZSIgc3Ryb2tlPSIjZmZmIiBzdHJva2Utd2lkdGg9IjIuNCIgc3Ryb2tlLWxpbmVjYXA9InJvdW5kIiBkPSJNNC4yIDE4LjVBOSA5IDAgMSAxIDE5LjggMTguNSIvPjxwYXRoIGZpbGw9Im5vbmUiIHN0cm9rZT0iI2ZmIiBzdHJva2Utd2lkdGg9IjIuNCIgc3Ryb2tlLWxpbmVjYXA9InJvdW5kIiBkPSJNMTIgMTQuNSAxNi41IDkiLz48Y2lyY2xlIGZpbGw9IiNmZmYiIGN4PSIxMiIgY3k9IjE0LjUiIHI9IjIuMiIvPjwvc3ZnPg==&logoColor=white)](https://github.com/nao1215/himorime)
[![Go Reference](https://pkg.go.dev/badge/github.com/nao1215/himorime.svg)](https://pkg.go.dev/github.com/nao1215/himorime)
[![GitHub Downloads (all assets, all releases)](https://img.shields.io/github/downloads/nao1215/himorime/total)](https://github.com/nao1215/himorime/releases)
[![License](https://img.shields.io/github/license/nao1215/himorime)](./LICENSE)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/nao1215/himorime/badge)](https://scorecard.dev/viewer/?uri=github.com/nao1215/himorime)


<p align="center">
  <img src="./doc/images/himorime-logo.jpeg" alt="himorime logo" width="600" />
</p>

himorime measures a command-line program from a YAML suite, checks latency and optional throughput, CPU and peak RSS budgets, and compares revisions for regressions.

## First run

```console
$ go run github.com/nao1215/himorime@latest init
$ go run github.com/nao1215/himorime@latest run
```

`init` writes one runnable `git --version` case with a latency budget and the [JSON Schema](https://raw.githubusercontent.com/nao1215/himorime/main/schema/himorime.schema.json) URL for editor completion. Replace the command with your tool, then keep the suite in your repository.

When developing himorime from a checkout, use `go run .`; a released `@latest` binary and the `main` schema can differ before a release. See [Configuration migration](https://nao1215.github.io/himorime/configuration/#migrating-from-the-previous-layout).

```text
1 passed · 1 benchmark · exit 0
```

Use [Getting started](https://nao1215.github.io/himorime/getting-started/) for the first suite, [Configuration](https://nao1215.github.io/himorime/configuration/) for the file format, [Metrics](https://nao1215.github.io/himorime/metrics/) for measured values, and [Reports](https://nao1215.github.io/himorime/reports/) for output formats.

For tool selection, see [Comparison](https://nao1215.github.io/himorime/comparison/); [atago](https://github.com/nao1215/atago) checks CLI behavior, while himorime checks performance.

## GitHub Actions

Use [setup-himorime](https://github.com/nao1215/setup-himorime) and `himorime ci` on pull requests; the [GitHub Actions guide](https://nao1215.github.io/himorime/github-actions/) contains the workflow.

## Measuring programs you did not write

himorime measures a command, so a program you did not write is measured like your own. The suites in [bench/thirdparty](./bench/thirdparty) measure real third-party tools and are written to be copied; [Real-world suites](https://nao1215.github.io/himorime/real-world/) says what each one shows.

| Suite | Programs |
|---|---|
| [json](./bench/thirdparty/json) | jq, gojq, jaq |
| [compression](./bench/thirdparty/compression) | gzip, bzip2, xz |
| [search](./bench/thirdparty/search) | GNU grep, ripgrep |
| [copy](./bench/thirdparty/copy) | cp, rsync |
| [csv](./bench/thirdparty/csv) | Miller, qsv, xan |
| [sql](./bench/thirdparty/sql) | DuckDB, trdsql, csvq, sqly |
| [diff](./bench/thirdparty/diff) | GNU diff, git diff |
| [yaml](./bench/thirdparty/yaml) | yq, yj, gojq |
| [awk](./bench/thirdparty/awk) | GNU awk, mawk, GoAWK |
| [sort](./bench/thirdparty/sort) | GNU sort, uutils sort |
| [shell](./bench/thirdparty/shell) | bash, dash |
| [cc](./bench/thirdparty/cc) | GCC, Clang |
| [js](./bench/thirdparty/js) | Node.js, Bun, Deno |

## Install

```shell
go install github.com/nao1215/himorime@latest
```

Homebrew: `brew install --cask nao1215/tap/himorime`. Arch Linux: `yay -S himorime-bin` from the [AUR](https://aur.archlinux.org/packages/himorime-bin). The [release page](https://github.com/nao1215/himorime/releases) has archives and Linux packages, and [setup-himorime](https://github.com/nao1215/setup-himorime) installs a release in GitHub Actions.

## Testing

| Technique | Bug class caught here |
|---|---|
| [Unit tests](./internal) | Wrong parsing, statistics, report rendering and exit codes |
| [CLI E2E with atago](./test/e2e/atago) | Wrong behavior of the built binary: exit codes, output, reports, Git worktrees |
| [Statistical calibration](./internal/stats/calibration_test.go) | False regressions and missed regressions at a known noise level |
| [Metamorphic relations and properties](./internal/stats/properties_test.go) | Verdicts that change with the unit, the better direction or the sample order |
| [Differential testing against /usr/bin/time](./test/differential/rusage_test.go) | Peak RSS and CPU time that look plausible but are wrong: a unit error, user and system time swapped, the starter's memory counted as the command's |
| [Fuzzing](./internal/config/parity_test.go) | Crashes and schema/loader disagreements on arbitrary suites, durations and reports |
| [Mutation testing](./.github/workflows/mutation.yml) | Statistics and threshold code that no test would notice going wrong |

Commands and CI jobs are listed in [CONTRIBUTING](./CONTRIBUTING.md#testing).

## The name

himorime was inspired by atago and named after 火防女 (himorime), the maiden who tends the fire in Demon's Souls.

## Contributors

- [Naohiro CHIKAMATSU](https://github.com/nao1215): code and documentation
- [Rafael Baboni Dominiquini](https://github.com/Dominiquini): the [`himorime-bin`](https://aur.archlinux.org/packages/himorime-bin) AUR package

## License

[MIT](./LICENSE)
