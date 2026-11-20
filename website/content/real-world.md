---
title: Real-world suites
description: Suites in this repository that measure programs nobody here maintains, what each one shows about writing a himorime.yaml, and what was made equal between the programs.
toc: true
---

These suites measure third-party tools and are meant to be copied. Run one with the tools it names on your PATH:

```console
$ himorime run bench/thirdparty/json
```

[thirdparty.yml](https://github.com/nao1215/himorime/blob/main/.github/workflows/thirdparty.yml) runs them weekly on a pinned runner and puts the numbers in its job summary and artifact. No suite carries a budget, so the job fails only when a tool cannot be installed, a command exits non-zero, or himorime reports an error.

Common to every suite:

- [bench/thirdparty/gen](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/gen) writes the inputs from a fixed seed, in a small size where start-up dominates and a large one where the work dominates.
- Every command gets the same input and writes the same kind of output in the working directory.
- A `setup` hook (`cleanup` for the shells) checks that the programs produced the same result, so a tool that stopped doing the job fails instead of looking fast.
- `report.versions` records every version. The workflow installs each tool at a pinned version, checked by SHA256 or the Go checksum database, or takes it from the runner image.
- The baseline is the oldest or most widely used implementation, never one by this repository's author.
- CPU time is summed over the process tree, so a tool with its own GC or JIT threads can use more CPU time than wall-clock time.

| Suite | Programs | What it shows |
|---|---|---|
| [json](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/json) | jq, gojq, jaq | Several implementations of one job in a single benchmark, input generated from a fixed seed, throughput declared from that input file, and a setup hook that proves the outputs match before anything is measured |
| [compression](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/compression) | gzip, bzip2, xz | Binary output commands writing to different formats, with decompression and byte comparison in setup before latency, throughput, CPU time and peak RSS are measured |
| [search](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/search) | GNU grep, ripgrep | A directory tree as input, a pinned locale, outputs compared after sorting, throughput declared as a number of files, and one tool measured at two thread counts |
| [copy](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/copy) | cp, rsync | A command that changes the state the next run starts from, reset by `prepare_each` outside the measured time, and CPU time taken over a tool that forks processes of its own |
| [csv](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/csv) | Miller, qsv, xan | A program that reads standard input, given a generated file with `stdin` so every run reads it from the first byte, and outputs that match only because the input quotes as little as it can |
| [sql](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/sql) | DuckDB, trdsql, csvq, sqly | Peak RSS as the metric that separates the programs, a tool from this repository's author measured without being the baseline, and `HOME` and `TMPDIR` pointed into the working directory so no tool reads or writes the reader's files |
| [diff](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/diff) | GNU diff, git diff | Commands whose success is exit status 1, declared with `exit_codes`, outputs compared after dropping headers that differ by design, and git kept from reading the configuration files of whoever runs it |
| [yaml](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/yaml) | yq, yj, gojq | Outputs that differ only in key order, which the job allows, normalized with jq, which is not measured, before they are compared |
| [awk](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/awk) | GNU awk, mawk, GoAWK | Interpreters running one program kept as a file next to the suite, found by a path relative to the suite directory, which is where commands run unless `cwd` says otherwise |
| [sort](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/sort) | GNU sort, uutils sort | One program name installed by two packages, one of them reached through a multi-call binary and a subcommand, and a setup step that checks which implementation the name resolves to before anything is measured |
| [shell](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/shell) | bash, dash | Interactive programs run on a pseudo-terminal with `terminal: true` and keys typed from `stdin`, checked in `cleanup`, which reads what the last measured run printed, because a command that needs a terminal cannot run in setup |
| [cc](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/cc) | GCC, Clang | Commands that take seconds, with a fixed number of `runs` and a raised `timeout`, and outputs that are programs, checked by running them rather than by comparing their bytes |
| [js](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/js) | Node.js, Bun, Deno | Runtimes that keep settings and caches in the home directory, pointed into the working directory so the warmup runs fill a cache that starts empty, and a sandboxed runtime given the one permission the job needs |

## JSON processors

Write `.user.id` of every line of JSON Lines, compact (`-c`), with the same filter on each tool, at 5MiB and 100MiB. gojq starts a Go runtime, which is most of the difference at 5MiB. jaq reads the whole input, so its peak RSS follows the input size, while jq and gojq stay at the floor.

Baseline: jq. Suite: [bench/thirdparty/json](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/json)

## Compression tools

Compress JSON Lines with gzip, bzip2 and xz, one worker where the tool has the option. Setup decompresses each output and compares it with the input. The formats differ, so size, CPU time and peak RSS describe the format, not which tool is better.

Baseline: gzip. Suite: [bench/thirdparty/compression](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/compression)

## Text search

Print every line containing `ERROR` as `path:line:text` from a tree of 64KiB log files: 80 files (5MiB) and 1600 files (100MiB). ripgrep gets `--no-ignore --hidden --no-heading --color never` to match `grep -r`, and `LC_ALL=C.UTF-8` pins the locale. ripgrep runs as `rg-j1` (`--threads 1`) and as `rg` with its default threads, which print in a varying order, so setup sorts the outputs. Throughput is a file count that setup checks against the tree. Peak RSS is not measured.

Baseline: GNU grep. Suite: [bench/thirdparty/search](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/search)

## Tree copy

`cp -R tree dest` and `rsync -r tree/ dest` on 80 and 1600 files. `prepare_each` removes `dest` before every run, outside the measured time, and setup checks each copy with `diff -r`. rsync's own forked processes are counted. Neither tool syncs, so the numbers describe the page cache. Peak RSS is not measured.

Baseline: cp. Suite: [bench/thirdparty/copy](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/copy)

## CSV processors

Read CSV on `stdin` and write its `id` and `note` columns (`mlr --csv cut -o -f id,note`, `qsv select id,note`, `xan select id,note`), at 5MiB and 100MiB. xan keeps the input's quoting and the others quote only when needed, so the outputs match only because the input quotes as little as it can. Miller's peak RSS rose with the input size; qsv's and xan's did not.

Baseline: Miller. Suite: [bench/thirdparty/csv](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/csv)

## SQL over CSV

Run `SELECT city, count(*) AS n ... GROUP BY city ORDER BY city` over CSV and write CSV with a header, at 5MiB and 100MiB. DuckDB gets `SET threads=1` and csvq `--cpu 1`. `HOME` and `TMPDIR` point into the working directory, because DuckDB reads `~/.duckdbrc`, trdsql reads a `config.json`, and sqly writes a history even with `--sql`. Peak RSS separates them: trdsql imports into an on-disk SQLite table, sqly into an in-memory one, csvq reads the file into memory, and DuckDB scans it.

Baseline: DuckDB; sqly, by himorime's author, is measured like the others. Suite: [bench/thirdparty/sql](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/sql)

## Diff tools

Write the unified diff of two JSON Lines files that differ on every hundredth line, at 5MiB and 100MiB, with `diff -u` and `git diff --no-index --no-ext-diff`. `GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_NOSYSTEM=1` and a missing `GIT_DIR` keep the reader's git settings out. Both exit 1 when the files differ, so the suite declares `exit_codes: [1]`. The headers differ by design, so setup compares only the hunks.

Baseline: GNU diff; on macOS `diff` is not GNU diff, and the report says which one ran. Suite: [bench/thirdparty/diff](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/diff)

## YAML to JSON

Convert one YAML document on `stdin` to compact JSON (`yq -M -o=json -I=0`, `yj -yj`, `gojq --yaml-input -c`), at 1MiB and 10MiB, because every tool holds the document in memory many times over. The input quotes its timestamps and has no leading-zero numbers, which the tools read differently. gojq sorts keys, so setup normalizes each output with `jq -S -c`, which is not measured.

Baseline: yq. Suite: [bench/thirdparty/yaml](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/yaml)

## awk interpreters

Run [sum.awk](https://github.com/nao1215/himorime/blob/main/bench/thirdparty/awk/sum.awk) over CSV to total an amount column per city, at 5MiB and 100MiB. Every command names `-f sum.awk`, which resolves in the suite's directory, so each revision of a comparison runs its own copy. `LC_ALL=C` makes GNU awk read bytes. Setup sorts the outputs, since `for (key in array)` order varies. Peak RSS is not measured.

Baseline: GNU awk. Suite: [bench/thirdparty/awk](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/awk)

## sort

Sort CSV by its amount column (`-t , -k 4,4n`, `--parallel=1`, `LC_ALL=C`), at 5MiB and 100MiB. Ubuntu installs uutils coreutils, a Rust reimplementation, as `sort` from 25.10 on, so the suite runs uutils as `coreutils sort` and setup checks that `sort --version` names GNU coreutils. Otherwise it could compare uutils with itself and pass.

Baseline: GNU sort. Suite: [bench/thirdparty/sort](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/sort)

## Interactive shells

Start `bash --norc --noprofile -i` and `dash -i` on an 80 by 24 pseudo-terminal, type `echo $((6*7))` or a loop counting to 100000, then `exit`. `TERM=dumb` stops bash 5.1+ from writing bracketed-paste sequences, and `HOME`, `INPUTRC=/dev/null` and an empty `HISTFILE` keep every run identical. A terminal command cannot run in setup, so `cleanup` checks the last run's output for the result. bash reads keys one at a time through readline, so its latency is above its CPU time.

Baseline: bash. dash cannot print its version, so the report records its path. `terminal: true` is not available on Windows. Suite: [bench/thirdparty/shell](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/shell)

## C compilers

Compile and link one generated C file with `-O2`, at about 64KiB and 512KiB of source. A compile takes seconds, so the suite uses `runs: 5` after one warmup and a five-minute `timeout`. Setup builds and runs both executables and compares what they print. gcc runs its assembler and linker as separate processes, and peak RSS is that of the largest one.

Baseline: GCC. Suite: [bench/thirdparty/cc](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/cc)

## JavaScript runtimes

Run [count.mjs](https://github.com/nao1215/himorime/blob/main/bench/thirdparty/js/count.mjs) over JSON Lines with Node.js, Bun and Deno, at 5MiB and 100MiB. `HOME` and `DENO_DIR` point into the working directory, `DENO_NO_UPDATE_CHECK` is set and `NODE_OPTIONS` is empty, so each benchmark starts with an empty cache that warmup fills. Deno gets `--allow-read` and `--no-config`. The module reads the whole file into one string, so peak RSS follows the input size.

Baseline: Node.js. Suite: [bench/thirdparty/js](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/js)
