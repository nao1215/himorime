---
title: Understand the geometric mean
---

You want one overall number across cases, and need to know when himorime refuses to give one.

<!-- example: examples/missing-case/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: why no overall score is printed when a case is missing.
# https://nao1215.github.io/himorime/cookbook/#understand-the-geometric-mean
version: "1"

name: missing case
description: The readall implementation has no measurement for the large case.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 5

benchmarks:
  - name: small
    setup:
      - command: ["${artifact}", -gen, "100", -o, "${workdir}/input.txt"]
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner, "${workdir}/input.txt"]
      readall:
        command: ["${artifact}", -impl, readall, "${workdir}/input.txt"]

  - name: large
    setup:
      - command: ["${artifact}", -gen, "100000", -o, "${workdir}/input.txt"]
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner, "${workdir}/input.txt"]
```

```console
$ himorime run examples/missing-case
```

Per-case rows, then `no geometric mean: command "readall" is missing from benchmark "large"`. With every command in every case, as in [Compare small, medium and large inputs](/cookbook/compare-small-medium-and-large-inputs/), the geometric mean is printed.

- The geometric mean is computed only when every command completed every case; a failure or timeout suppresses it rather than being left out.
- A suite whose benchmarks share no command name, such as a regression suite with a one-command start-up benchmark, is not a comparison: it gets neither a geometric mean nor a note, whatever order its benchmarks are in.
- himorime never averages ratios arithmetically.

Example: [`examples/missing-case`](https://github.com/nao1215/himorime/tree/main/examples/missing-case)
