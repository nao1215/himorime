---
title: Understand CPU utilization above 100%
---

The CPU table says `246%` and you want to know whether that is a bug.

<!-- example: examples/cpu-utilization/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: understand CPU utilization above 100%.
# https://nao1215.github.io/himorime/cookbook/#understand-cpu-utilization-above-100
version: "1"

name: cpu utilization
description: A single-threaded and a multi-threaded word count of the same file.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 8

benchmarks:
  - name: count 500k lines
    setup:
      - command: ["${artifact}", -gen, "500000", -o, "${workdir}/input.txt"]
    metrics:
      cpu: true
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner, "${workdir}/input.txt"]
        budget:
          # Utilization is CPU time divided by wall-clock time. A
          # single-threaded program stays near or below 100%.
          cpu_utilization:
            median: "<= 150%"
      parallel:
        command: ["${artifact}", -impl, parallel, -workers, "4", "${workdir}/input.txt"]
```

```console
$ himorime run examples/cpu-utilization
```

`scanner` shows a utilization near 100%, `parallel` well above it on a machine with several CPUs, while its latency is lower.

- [CPU utilization](/metrics/#cpu-time-and-utilization) is total CPU time divided by wall-clock time, times 100. One CPU busy for the whole run is 100%; four CPUs busy for the whole run is 400%.
- A Go, Java or .NET program runs garbage collection and runtime threads next to your code, so even a single-threaded program can exceed 100% slightly.
- A value far below 100% means the program waited: for I/O, a lock, a child process or a sleep.
- Utilization has no better direction, so it can carry a budget in either direction but is never judged as a regression.

Example: [`examples/cpu-utilization`](https://github.com/nao1215/himorime/tree/main/examples/cpu-utilization)
