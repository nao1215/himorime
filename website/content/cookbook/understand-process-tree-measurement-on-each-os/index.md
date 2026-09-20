---
title: Understand process tree measurement on each OS
---

Your command starts other processes, a shell, `make`, a compiler, and you need to know what the CPU and memory numbers include.

<!-- example: examples/process-tree/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: understand process tree measurement on each OS.
# https://nao1215.github.io/himorime/cookbook/#understand-process-tree-measurement-on-each-os
version: "1"

name: process tree
description: CPU time of a program that does its work itself, and of one that delegates it to two child processes.

build:
  command: [go, build, -o, "${artifact}", ../tools/sleepy]

defaults:
  warmup: 1
  runs: 6

benchmarks:
  - name: spin 100ms
    metrics:
      # The started process and its descendants. Only process_tree exists.
      cpu: true
    commands:
      alone:
        command: ["${artifact}", -busy, -ms, "100"]
      two-children:
        # The parent only waits; each child spins for 100ms.
        command: ["${artifact}", -busy, -ms, "100", -children, "2"]
```

```console
$ himorime run examples/process-tree
```

`alone` shows about 100ms of CPU time, `two-children` about 200ms: the CPU time of the two children counts, although the parent itself only waits.

| | Linux, macOS, BSD | Windows |
|---|---|---|
| CPU time | the process and every descendant its parents waited for (`wait4` usage) | every process that was part of the command's Job Object |
| Peak RSS | the largest peak of the process or any descendant its parents waited for | the peak working set of the started process, reported only when it started no child processes |
| Not included | a descendant still running when the command exits, or orphaned before it exits | a child started in the microseconds before the process joined the job |

- Neither platform adds up processes that ran at the same time: peak RSS is the largest single process.
- Collection happens after the measured interval and uses the operating system's high-water marks; see [Metrics](/metrics/#overhead).
- `shell: true` adds the shell process to the tree: its start-up time, CPU time and memory count too.

Example: [`examples/process-tree`](https://github.com/nao1215/himorime/tree/main/examples/process-tree)
