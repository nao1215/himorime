---
title: Protect the throughput of large CSV and JSON processing
---

Your tool processes large files, and what matters is how much it gets through per second, not how long one file takes.

<!-- example: examples/throughput/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: protect the throughput of large CSV and JSON processing.
# https://nao1215.github.io/himorime/cookbook/#protect-the-throughput-of-large-csv-and-json-processing
version: "1"

name: throughput
description: Bytes and records per second of the word counter on large CSV and JSON Lines inputs.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 10

benchmarks:
  - name: csv bytes
    setup:
      - command: ["${artifact}", -gen, "200000", -gen-format, csv, -o, "${workdir}/input.csv"]
    metrics:
      throughput:
        # Throughput is work divided by latency, and himorime never guesses the
        # work: here it is the size of the input file, read before every run.
        work:
          file_size: "${workdir}/input.csv"
          unit: bytes
    commands:
      wordcount:
        command: ["${artifact}", "${workdir}/input.csv"]
        budget:
          # Higher is better, so a throughput budget is a floor.
          throughput:
            median: ">= 5MiB/s"
  - name: jsonl records
    setup:
      - command: ["${artifact}", -gen, "200000", -gen-format, jsonl, -o, "${workdir}/input.jsonl"]
    metrics:
      throughput:
        # A fixed amount of work in your own unit: records, lines, files.
        work:
          value: 200000
          unit: records
    commands:
      wordcount:
        command: ["${artifact}", "${workdir}/input.jsonl"]
        budget:
          throughput:
            median: ">= 50000 records/s"
            min: ">= 20000 records/s"
```

```console
$ himorime run examples/throughput
```

A throughput table in `MiB/s` for the CSV case and `records/s` for the JSON Lines case, and the budgets table. Throughput is computed per run, as the declared work divided by that run's latency.

- Declare `value` or `file_size`; himorime does not infer work. See [Throughput](/metrics/#throughput) for work units and statistic meanings.
- Throughput budgets are floors (`>=` or `>`), and their unit must match the work: `MiB/s` uses `unit: bytes`, while `records/s` uses `unit: records`.

Example: [`examples/throughput`](https://github.com/nao1215/himorime/tree/main/examples/throughput)
