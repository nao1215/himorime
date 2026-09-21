[![UnitTest](https://github.com/nao1215/himorime/actions/workflows/unit_test.yml/badge.svg)](https://github.com/nao1215/himorime/actions/workflows/unit_test.yml) [![E2E](https://github.com/nao1215/himorime/actions/workflows/e2e.yml/badge.svg)](https://github.com/nao1215/himorime/actions/workflows/e2e.yml) [![Lint](https://github.com/nao1215/himorime/actions/workflows/lint.yml/badge.svg)](https://github.com/nao1215/himorime/actions/workflows/lint.yml) [![Go Reference](https://pkg.go.dev/badge/github.com/nao1215/himorime.svg)](https://pkg.go.dev/github.com/nao1215/himorime)
![GitHub Downloads (all assets, all releases)](https://img.shields.io/github/downloads/nao1215/himorime/total)


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

## Install

```shell
go install github.com/nao1215/himorime@latest
```

Homebrew: `brew install --cask nao1215/tap/himorime`. The [release page](https://github.com/nao1215/himorime/releases) has archives and Linux packages, and [setup-himorime](https://github.com/nao1215/setup-himorime) installs a release in GitHub Actions.

## The name

himorime was inspired by atago and named after 火防女 (himorime), the maiden who tends the fire in Demon's Souls.

## License

[MIT](./LICENSE)
