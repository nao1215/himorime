---
title: Run only smoke benchmarks
---

The full suite is slow, and a pull request or a pre-commit check should run only a quick subset.

<!-- example: examples/tags/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: run only the smoke benchmarks with tags and filters.
# https://nao1215.github.io/himorime/cookbook/#run-only-smoke-benchmarks
#
#   himorime run examples/tags --tag smoke
#   himorime run examples/tags --skip-tag slow
#   himorime run examples/tags --filter '^startup'
version: "1"

name: tags
description: A quick smoke benchmark next to a slower one.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 5

benchmarks:
  - name: startup
    tags: [smoke]
    commands:
      wordcount:
        command: ["${artifact}", -version]

  - name: large input
    tags: [slow]
    setup:
      - command: ["${artifact}", -gen, "500000", -o, "${workdir}/input.txt"]
    commands:
      wordcount:
        command: ["${artifact}", "${workdir}/input.txt"]
```

```console
$ himorime run examples/tags --tag smoke
```

Only the `startup` benchmark runs. `--skip-tag slow` gives the same here, and `--filter` selects by a regular expression on the name.

- A selection that matches nothing exits 3, so a renamed tag cannot silently skip every benchmark.
- `himorime list --tag smoke` shows what would run.

Example: [`examples/tags`](https://github.com/nao1215/himorime/tree/main/examples/tags)
