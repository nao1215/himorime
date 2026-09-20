---
title: Stop a command that hangs
---

A command can hang, and a benchmark must not block CI forever or leave processes behind.

<!-- example: examples/timeout/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: stop a command that hangs. This example fails on purpose (exit 4).
# https://nao1215.github.io/himorime/cookbook/#stop-a-command-that-hangs
version: "1"

name: timeout
description: A command that would take 30 seconds is stopped after one.

build:
  command: [go, build, -o, "${artifact}", ../tools/sleepy]

defaults:
  warmup: 0
  runs: 3

benchmarks:
  - name: hangs
    commands:
      sleepy:
        command: ["${artifact}", -ms, "30000"]
        # The run and every process it started are stopped when the limit
        # passes: a process group on Unix, a Job Object on Windows.
        timeout: 1s
```

```console
$ himorime run examples/timeout
```

`ERROR` with `timed out after 1s and was stopped`, and exit status 4. This example fails on purpose.

- The whole process tree is stopped: a process group on Unix, a Job Object on Windows.
- A timeout is an execution error, not a slow sample: the command did not complete.

Example: [`examples/timeout`](https://github.com/nao1215/himorime/tree/main/examples/timeout)
