---
title: Benchmark a warm cache
---

Your program has a cache, and you want to measure both the warm path and a cold start in one suite.

<!-- example: examples/cache/himorime.yaml -->
```yaml
# yaml-language-server: $schema=../../schema/himorime.schema.json
#
# Recipes: benchmark with a warm cache, and reproduce a cold cache with
# prepare_each.
# https://nao1215.github.io/himorime/cookbook/#benchmark-a-warm-cache
version: "1"

name: cache
description: The same count with a primed cache and with a cache wiped before every run.

build:
  command: [go, build, -o, "${artifact}", ../tools/wordcount]

defaults:
  warmup: 1
  runs: 10

benchmarks:
  - name: warm cache
    setup:
      - command: ["${artifact}", -gen, "100000", -o, "${workdir}/input.txt"]
      # Prime the cache once; every measured run then hits it.
      - command: ["${artifact}", -cache, "${workdir}/cache", "${workdir}/input.txt"]
    commands:
      wordcount:
        command: ["${artifact}", -cache, "${workdir}/cache", "${workdir}/input.txt"]

  - name: cold cache
    setup:
      - command: ["${artifact}", -gen, "100000", -o, "${workdir}/input.txt"]
    # prepare_each runs before every warmup and measured run, outside the
    # measured time. Removing the cache makes every run a cold one.
    prepare_each:
      - command: ["${artifact}", -cache, "${workdir}/cache", -purge]
    commands:
      wordcount:
        command: ["${artifact}", -cache, "${workdir}/cache", "${workdir}/input.txt"]
```

```console
$ himorime run examples/cache
```

`warm cache` is much faster than `cold cache`: `setup` primes the cache once for the warm case, while `prepare_each` purges it before every run of the cold case.

- A cold start reproduced this way is "cold" for the program's own cache only; the operating system's file cache is still warm.
- The warmup run of `warm cache` is also a cache hit, because `setup` ran before it.

Example: [`examples/cache`](https://github.com/nao1215/himorime/tree/main/examples/cache)

