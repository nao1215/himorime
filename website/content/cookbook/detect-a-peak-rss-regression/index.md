---
title: Detect a peak RSS regression
---

A change made the program hold much more memory at its peak, and you want the pull request to fail.

<!-- example: examples/memory-regression/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: detect a peak RSS regression.
# https://nao1215.github.io/himorime/cookbook/#detect-a-peak-rss-regression
#
#   himorime compare --against main examples/memory-regression
version: "1"

name: memory regression
description: Peak resident set size of the word counter, compared between two revisions.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 12

benchmarks:
  - name: count 200k lines
    setup:
      - command: ["${artifact}", -gen, "200000", -o, "${workdir}/input.txt"]
    metrics:
      memory: true
    commands:
      wordcount:
        command: ["${artifact}", "${workdir}/input.txt"]
        budget:
          peak_rss:
            max: "<= 512MiB"
    regression:
      latency:
        max_percent: 50
      peak_rss:
        max_percent: 20
        min_difference: 4MiB
```

```console
$ himorime compare --against main examples/memory-regression
```

A `peak rss` comparison table in MiB. When the head holds clearly more memory the row says `REGRESSION` and himorime exits 1. The end-to-end suite proves it by switching the word counter's default implementation to one that reads the whole input into memory.

- [Peak RSS](/metrics/#peak-rss) is the largest resident set size of one process in the tree.
- On Unix peak RSS has a floor of a few MiB. A streaming base at the floor still shows a regression when the head clearly exceeds it; see [The floor](/metrics/#the-floor).

Example: [`examples/memory-regression`](https://github.com/nao1215/himorime/tree/main/examples/memory-regression)
