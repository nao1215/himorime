---
title: Use a shell pipeline
---

You need a pipe or a redirection, which an argument list cannot express.

<!-- example: examples/shell/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipes: portable commands without a shell, and an explicit shell pipeline.
# https://nao1215.github.io/himorime/cookbook/#use-a-shell-pipeline
version: "1"

name: shell
description: The same count through argv and through a shell pipeline.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 5

benchmarks:
  - name: pipeline
    setup:
      - command: ["${artifact}", -gen, "10000", -o, "${workdir}/input.txt"]
    baseline: argv
    commands:
      argv:
        command: ["${artifact}", "${workdir}/input.txt"]
      pipeline:
        # shell: true runs the string through /bin/sh -c, or cmd.exe on
        # Windows. Variables are quoted for that shell before substitution.
        # The measured time includes starting the shell.
        command: "${artifact} < ${workdir}/input.txt | ${artifact}"
        shell: true
```

```console
$ himorime run examples/shell
```

Two rows: `argv` and `pipeline`. The pipeline is slower partly because its time includes starting the shell and a second process.

- `shell: true` uses `/bin/sh -c`, or `cmd.exe` on Windows; the pipeline in this example is valid in both.
- Substituted variables are quoted for the shell. himorime never subtracts shell start-up time.

Example: [`examples/shell`](https://github.com/nao1215/himorime/tree/main/examples/shell)
