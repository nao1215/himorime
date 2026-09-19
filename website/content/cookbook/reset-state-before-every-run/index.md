---
title: Reset state before every run
---

The command changes something it depends on (a file, a cache, a database), so every run has to start from the same state.

<!-- example: examples/prepare-each/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: reset state before every run with prepare_each.
# https://nao1215.github.io/himorime/cookbook/#reset-state-before-every-run
version: "1"

name: prepare each
description: Every run starts from a freshly generated output file.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 10

benchmarks:
  - name: regenerate then count
    # prepare_each is not measured. It runs before every warmup and every
    # measured run, in the same working directory as the command.
    prepare_each:
      - command: ["${artifact}", -gen, "5000", -o, "${workdir}/input.txt"]
    commands:
      wordcount:
        command: ["${artifact}", "${workdir}/input.txt"]
        # Keep the latest run's output for inspection instead of discarding it.
        stdout: last-output.txt
```

```console
$ himorime run examples/prepare-each
```

One `PASS` row. `prepare_each` ran 11 times (1 warmup + 10 runs) and none of that time is in the result.

- `prepare_each` runs before warmup runs too.
- `stdout: last-output.txt` keeps the latest run's output inside `${workdir}` instead of discarding it.

Example: [`examples/prepare-each`](https://github.com/nao1215/himorime/tree/main/examples/prepare-each)
