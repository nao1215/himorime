---
title: Compare parsers on a stdin fixture
---

Your tools read standard input, and you want every run to see the same committed fixture from its first byte.

<!-- example: examples/stdin-fixture/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: compare parsers on a stdin fixture.
# https://nao1215.github.io/himorime/cookbook/#compare-parsers-on-a-stdin-fixture
version: "1"

name: stdin fixture
description: Both implementations read the same access log on standard input.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 10

benchmarks:
  - name: access log on stdin
    # The fixture is reopened for every run, so each run reads it from the
    # first byte. Relative paths are relative to this file's directory, ${root}.
    stdin: testdata/access.log
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner]
      readall:
        command: ["${artifact}", -impl, readall]

  - name: inline stdin
    stdin:
      content: "a short inline input\nwith two lines\n"
    baseline: scanner
    commands:
      scanner:
        command: ["${artifact}", -impl, scanner]
      readall:
        command: ["${artifact}", -impl, readall]
```

```console
$ himorime run examples/stdin-fixture
```

Two benchmarks, each with a `scanner` and a `readall` row, and a geometric mean line across both cases.

- A relative `stdin` path is relative to the suite file. The file is reopened for every run.
- `stdin: {content: ...}` passes inline text instead of a file.

Example: [`examples/stdin-fixture`](https://github.com/nao1215/himorime/tree/main/examples/stdin-fixture)

