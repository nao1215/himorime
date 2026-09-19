---
title: Clean up after success, failure or interruption
---

Your benchmark creates something that must not be left behind, whether the benchmark passes, a command fails, or you press Ctrl+C.

<!-- example: examples/cleanup/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: always clean up, even when a run fails or is interrupted.
# https://nao1215.github.io/himorime/cookbook/#clean-up-after-success-failure-or-interruption
version: "1"

name: cleanup
description: setup creates a cache, cleanup purges it however the benchmark ends.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 5

benchmarks:
  - name: with cleanup
    setup:
      - command: ["${artifact}", -gen, "1000", -o, "${workdir}/input.txt"]
      - command: ["${artifact}", -cache, "${workdir}/cache", "${workdir}/input.txt"]
    # cleanup runs after the benchmark whether it passed, failed or was
    # interrupted with Ctrl+C. ${workdir} itself is removed afterwards.
    cleanup:
      - command: ["${artifact}", -cache, "${workdir}/cache", -purge]
    commands:
      wordcount:
        command: ["${artifact}", -cache, "${workdir}/cache", "${workdir}/input.txt"]
```

```console
$ himorime run examples/cleanup
```

One `PASS` row. `cleanup` also runs when a command fails, when `setup` fails, and after an interrupt, and `${workdir}` is removed afterwards.

- A failing `cleanup` fails the benchmark with exit status 4.
- After Ctrl+C, cleanup gets up to 30 seconds before himorime gives up on it.

Example: [`examples/cleanup`](https://github.com/nao1215/himorime/tree/main/examples/cleanup)

