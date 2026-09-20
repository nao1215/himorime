---
title: Save JSON, CSV and Markdown reports locally
---

You want results you can archive, load into a spreadsheet, paste into a pull request, or analyze yourself.

<!-- example: examples/reports/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: save JSON, CSV and Markdown reports locally.
# https://nao1215.github.io/himorime/cookbook/#save-json-csv-and-markdown-reports-locally
#
#   himorime run examples/reports --format json --output result.json
#   himorime run examples/reports --format csv --output result.csv
#   himorime run examples/reports --format samples-csv --output samples.csv
#   himorime run examples/reports --format markdown --output result.md
#   himorime run examples/reports --summary summary.md
version: "1"

name: reports
description: Two ways to count the same file, for report examples.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 10

benchmarks:
  - name: count 20k lines
    setup:
      - command: ["${artifact}", -gen, "20000", -o, "${workdir}/input.txt"]
    metrics:
      throughput:
        work:
          value: 20000
          unit: lines
      cpu: true
      memory: true
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner, "${workdir}/input.txt"]
      readall:
        command: ["${artifact}", -impl, readall, "${workdir}/input.txt"]
```

```console
$ himorime run examples/reports --format json --output result.json
$ himorime run examples/reports --format csv --output result.csv
$ himorime run examples/reports --format samples-csv --output samples.csv
$ himorime run examples/reports --format markdown --output result.md
```

- The output contracts and `schema_version: "1"` are documented on [Reports](/reports/); JSON validates against `schema/report.schema.json`.
- Use JSON for complete samples, CSV for tabular analysis, samples CSV to join values by run, and Markdown for a readable summary.

To write several reports on every run, list them under `report.outputs` in the suite, or append a job summary with `--summary FILE`. Reports never include environment variables or host names.

Example: [`examples/reports`](https://github.com/nao1215/himorime/tree/main/examples/reports)
