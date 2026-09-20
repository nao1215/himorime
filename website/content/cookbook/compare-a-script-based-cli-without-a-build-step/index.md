---
title: Compare a script-based CLI without a build step
---

Your CLI is a script run by an interpreter, so there is nothing to build, and you still want `main` and your working tree compared, each running its own script on the same input.

<!-- example: examples/script-compare/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipe: compare a script-based CLI without a build step.
# https://nao1215.github.io/himorime/cookbook/#compare-a-script-based-cli-without-a-build-step
#
#   himorime compare --against main examples/script-compare
version: "1"

name: script compare
description: A shell script measured as the base revision has it and as the working tree has it, with no build step.

defaults:
  warmup: 1
  runs: 15

benchmarks:
  - name: count records
    # Commands and hooks run in ${root}, this directory inside the revision
    # being measured, and relative paths are relative to it. The base
    # revision therefore runs the base's work.sh, and the head the working
    # tree's.
    #
    # ${head_root} is this directory inside the working tree for both
    # revisions: both read exactly the same records, even when the fixture
    # changed or is new in the working tree.
    stdin: "${head_root}/testdata/records.txt"
    commands:
      script:
        command: [sh, work.sh]
    regression:
      latency:
        max_percent: 20
        min_difference: 20ms
```

```console
$ himorime compare --against main examples/script-compare
```

One `count records` row. A slower `work.sh` in the working tree is a `REGRESSION`; an unchanged one passes.

- Commands and hooks run in `${root}` of each revision, and relative paths such as `work.sh` resolve there, so the base worktree runs the base's script. A command that ran from the working tree in both revisions would compare your changes with themselves.
- `${head_root}` is the suite directory in the working tree for both revisions. Use it for a fixture both must read, as `stdin` does here, or for a tool you do not want compared.
- A relative path the base revision does not have, such as a fixture added in this change, fails the base with a hint to use `${head_root}`.
- The script needs `sh`; on Windows, point `command` at an interpreter that exists there.

Example: [`examples/script-compare`](https://github.com/nao1215/himorime/tree/main/examples/script-compare)
