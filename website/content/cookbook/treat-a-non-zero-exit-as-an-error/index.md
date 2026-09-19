---
title: Treat a non-zero exit as an error
---

A benchmark whose command fails is measuring an error path; that must be reported, not averaged in.

<!-- example: examples/failing-command/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: treat a failing command as an error, or allow expected exit codes.
# The first benchmark fails on purpose (exit 4).
# https://nao1215.github.io/himorime/cookbook/#treat-a-non-zero-exit-as-an-error
version: "1"

name: failing command
description: A command that exits 3, and one whose exit 1 is expected.

build:
  command: [go, build, -o, "${artifact}", ../tools/sleepy]

defaults:
  warmup: 0
  runs: 3

benchmarks:
  - name: unexpected failure
    commands:
      sleepy:
        command: ["${artifact}", -ms, "1", -exit, "3"]

  - name: expected exit code
    commands:
      sleepy:
        command: ["${artifact}", -ms, "1", -exit, "1"]
        # Like grep without a match: exit 1 is part of normal operation.
        exit_codes: [0, 1]
```

```console
$ himorime run examples/failing-command
```

`unexpected failure` shows `ERROR` with `exited with status 3` and the last lines of its standard error; `expected exit code` passes because `exit_codes: [0, 1]` allows exit 1. himorime exits 4.

- Only successful runs become samples, and a failed command is never left out of the table.
- Values of secret-looking environment variables are masked in the reported standard error.

Example: [`examples/failing-command`](https://github.com/nao1215/himorime/tree/main/examples/failing-command)
