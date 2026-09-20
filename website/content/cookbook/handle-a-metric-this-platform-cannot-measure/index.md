---
title: Handle a metric this platform cannot measure
---

The same suite runs on Linux and Windows, and one metric cannot be measured on one of them.

<!-- example: examples/unsupported-metrics/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: handle a metric this platform cannot measure.
# https://nao1215.github.io/himorime/cookbook/#handle-a-metric-this-platform-cannot-measure
version: "1"

name: unsupported metrics
description: Peak RSS of a command that starts a child process, which Windows cannot report.

build:
  command: [go, build, -o, "${artifact}", ../tools/sleepy]

defaults:
  warmup: 0
  runs: 5
  metrics:
    cpu: true
    memory: true
    # The default, fail, stops before measuring when a requested metric
    # cannot be measured here. skip measures everything else and reports the
    # metric as unsupported, with its budgets and comparisons skipped.
    unsupported: skip

benchmarks:
  - name: parent and child
    commands:
      sleepy:
        command: ["${artifact}", -ms, "20", -children, "1"]
        budget:
          peak_rss:
            max: "<= 256MiB"
```

```console
$ himorime run examples/unsupported-metrics
```

On Linux and macOS every metric is measured and the budget passes. On Windows the command starts a child process, so peak RSS is reported as `unsupported`, its budget as `SKIPPED` with the reason, CPU time is still measured, and himorime exits 0.

- `unsupported: skip` and the default `fail` behavior are defined in [Metrics](/metrics/#unsupported-failed-and-not-requested).
- An unsupported metric is never reported as zero: its `stats` are `null`, its status says `unsupported`, and the reason says why; a supported collection failure exits 6.

Example: [`examples/unsupported-metrics`](https://github.com/nao1215/himorime/tree/main/examples/unsupported-metrics)
