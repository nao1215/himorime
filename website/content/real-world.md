---
title: Real-world suites
description: Suites in this repository that measure programs nobody here maintains, what each one shows about writing a himorime.yaml, and what was made equal between the programs.
toc: true
---

A program you did not write is measured like your own: name it, give it an input, and read the numbers. The suites below do that with real third-party tools, and they are meant to be read and copied. Each one is a working `himorime.yaml` for a shape of program you are likely to have.

Run one yourself, with the tools it names on your PATH:

```console
$ himorime run bench/thirdparty/json
```

[thirdparty.yml](https://github.com/nao1215/himorime/blob/main/.github/workflows/thirdparty.yml) runs them weekly on a pinned runner so the examples stay correct as the tools and himorime change. The numbers of the latest run are in that workflow's job summary and in its artifact. They are not repeated here: a number measured on a shared runner last month says less than the suite you can run today on the machine you care about.

No suite here carries a budget. The speed of a program this repository does not maintain is not a reason to fail its CI, so the scheduled job fails only when a measurement fails: a tool that cannot be installed, a command that exits non-zero, or himorime reporting an error.

What every suite has in common, so the sections below only say what is particular to each one:

- Input files are written by [bench/thirdparty/gen](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/gen) from a fixed seed, so no data from anyone else's project is committed here and every run measures the same bytes; the shells read keys written in the suite instead. Each suite measures a small job, where starting the program dominates, and a large one, where the work dominates.
- Every command gets the same input and writes to the same kind of destination in the benchmark's working directory: a file, or a directory for the tree copy.
- A hook checks that the programs produced the same result, in `setup` before anything is measured (in `cleanup` for the shells, which need a terminal), so a program that stopped doing the job fails the benchmark instead of looking fast.
- `report.versions` records the version of every program. [thirdparty.yml](https://github.com/nao1215/himorime/blob/main/.github/workflows/thirdparty.yml) installs each one at a pinned version, checked against a SHA256 for a download and through the Go checksum database for `go install`, or takes it from the pinned runner image.
- The baseline is the oldest or most widely used implementation, never one by this repository's author, so `RELATIVE` reads as a multiple of it.
- CPU time is summed over the process tree. A program that collects garbage or compiles code on threads of its own, which the sections name, can use more CPU time than wall-clock time.

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

Same job: write `.user.id` of every line of generated JSON Lines, compact, one per line. Measured at 5MiB and 100MiB.

Made equal: compact output (`-c`) and the same filter on every tool.

Not equal: gojq runs a Go garbage collector, while jq and jaq stay on one thread. jaq reads the whole input into memory, so its peak RSS follows the input size; the peaks of jq and gojq stay near the cost of starting a process and are reported at or below that floor. gojq starts a Go runtime, which is most of the difference at 5MiB.

Baseline: jq. Suite: [bench/thirdparty/json](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/json)

## Compression tools

Same job: compress generated JSON Lines with gzip, bzip2 and xz, each in its own format, with one worker where the tool has the option.

Made equal: the input and the single worker. Setup decompresses each output and compares it with the input; the compressed bytes are not compared, because the formats differ.

Not equal: the formats and algorithms, so output size, CPU time and peak RSS describe each format, not which tool is better; xz and bzip2 have different memory profiles from gzip even at comparable level names.

Baseline: gzip. Suite: [bench/thirdparty/compression](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/compression)

## Text search

Same job: write every line that contains the fixed string `ERROR`, as `path:line:text`, from a generated tree of 64KiB log files. Measured on 80 files (5MiB) and 1600 files (100MiB).

Made equal: the tree is searched as the relative path `tree` from the working directory, with line numbers. ripgrep searches what `grep -r` searches (`--no-ignore --hidden`) and prints as grep does (`--no-heading --color never`). `LC_ALL=C.UTF-8`, so the reader's locale does not change how GNU grep reads bytes. Setup sorts each output before comparing, and checks that grep found a line.

Not equal: GNU grep uses one thread. ripgrep is measured as `rg-j1` (`--threads 1`) and as `rg`, whose default thread count depends on the machine; `rg` prints files in the order its threads finish them, which is why outputs are sorted. Peak RSS is not measured, because every command stayed at the cost of starting a process.

Throughput is a fixed `value` in files, because the input is a directory; setup counts the files so the number cannot drift from the tree.

Baseline: GNU grep. Suite: [bench/thirdparty/search](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/search)

## Tree copy

Same job: copy a generated tree of 64KiB log files into a new directory, without timestamps or ownership (`cp -R tree dest`, `rsync -r tree/ dest`). Measured on 80 and 1600 files.

Made equal: the same destination. A copy leaves `dest` behind, so the next `cp -R` would copy into `dest/tree` and rsync would compare every file; `prepare_each` removes `dest` before every run, outside the measured time. Setup compares each copy with the tree using `diff -r`.

Not equal: rsync forks processes of its own even on one machine, and all of them are counted. Neither tool syncs to disk, so the numbers describe the page cache, not the disk. Peak RSS is not measured, because every command stayed at the cost of starting a process.

Baseline: cp. Suite: [bench/thirdparty/copy](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/copy)

## CSV processors

Same job: read generated CSV on standard input and write its `id` and `note` columns as CSV. Measured at 5MiB and 100MiB.

Made equal: `stdin` reopens the file for every run, so no command names an input file. The same columns in the same order (`mlr --csv cut -o -f id,note`, `qsv select id,note`, `xan select id,note`).

Not equal: xan keeps the quotes of a field as the input wrote them, while Miller and qsv quote only when a field needs it. The generated input quotes only those fields, so the outputs match; an input that quotes every field would not. Miller runs a Go garbage collector, and its peak RSS rose with the input size while those of qsv and xan did not.

Baseline: Miller. Suite: [bench/thirdparty/csv](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/csv)

## SQL over CSV

Same job: run `SELECT city, count(*) AS n ... GROUP BY city ORDER BY city` over generated CSV and write the result as CSV with a header. Measured at 5MiB and 100MiB.

Made equal: each tool names a table after a file in its own way (`FROM 'input.csv'` for DuckDB, `FROM input.csv` for trdsql, `FROM input` for csvq and sqly), so the commands run in the working directory. One thread for DuckDB (`SET threads=1`) and one CPU for csvq (`--cpu 1`). `HOME` points into the working directory, because DuckDB reads `~/.duckdbrc`, trdsql reads a `config.json` that can switch its database driver, and sqly writes a history even with `--sql`; `TMPDIR` does the same for SQLite's temporary files.

Not equal: how much of the file each tool holds, which is what peak RSS shows: trdsql imports it into a temporary SQLite table on disk behind a small page cache, sqly into an in-memory SQLite database, csvq reads it into memory, and DuckDB scans it. trdsql, csvq and sqly run a Go garbage collector.

Baseline: DuckDB; sqly, by the author of himorime, is measured like the others. Suite: [bench/thirdparty/sql](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/sql)

## Diff tools

Same job: write the unified diff, with three lines of context, of two generated JSON Lines files; the second has `"ok":true` turned into `"ok":false` on every hundredth line where it appears. Measured at 5MiB and 100MiB.

Made equal: `git diff --no-index` compares two files as `diff -u` does. `GIT_CONFIG_GLOBAL=/dev/null` and `GIT_CONFIG_NOSYSTEM=1` keep git from reading a `diff.algorithm` or `diff.context` of the reader's, `GIT_DIR` pointing at a missing path keeps it out of a repository around the working directory, and `--no-ext-diff` keeps an external diff program out. Both tools exit with status 1 when the files differ, which is the result asked for, so the suite declares `exit_codes: [1]`; 0 or 2 fails the benchmark.

Not equal: the headers, so setup compares only the hunks. diff names the files with their modification times, and git with `a/` and `b/` and an `index` line, for which it hashes both files. The hunks match because every line is unique, the changes are a hundred lines apart and no line starts with a letter, which git would append to the `@@` line; on input with repeated lines the two tools can place a change differently. Both tools hold both files in memory.

Baseline: GNU diff; on macOS, `diff` is not GNU diff, and the report says which one ran. Suite: [bench/thirdparty/diff](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/diff)

## YAML to JSON

Same job: read one generated YAML document on standard input and write it as compact JSON. Measured at 1MiB and 10MiB, smaller than elsewhere because every tool holds the whole document in memory, many times its size.

Made equal: standard input for every tool, because yj reads nothing else; compact output without color (`yq -M -o=json -I=0`, `yj -yj`, `gojq --yaml-input -c`), with `-M` because yq colors its output even into `/dev/null`. The document double-quotes its timestamps, which a YAML 1.1 reader could resolve to timestamps, and writes no number with a leading zero, which yq reads as decimal and the others as octal.

Not equal: gojq sorts the keys of every object, while yq and yj keep the document's order. Key order does not change what a JSON object means, so setup passes each output through `jq -S -c` before comparing; jq is not measured. All three run a Go garbage collector.

Baseline: yq. Suite: [bench/thirdparty/yaml](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/yaml)

## awk interpreters

Same job: run [sum.awk](https://github.com/nao1215/himorime/blob/main/bench/thirdparty/awk/sum.awk) over generated CSV and write, for each city, its row count and the total of its amount column. Measured at 5MiB and 100MiB.

Made equal: the program sits next to the suite and every command names it as `-f sum.awk`, which works because commands run in the suite's directory unless `cwd` says otherwise; in a comparison each revision runs its own copy. `LC_ALL=C`, because GNU awk handles characters rather than bytes in a UTF-8 locale. Setup checks that GNU awk wrote six lines, one per city.

Not equal: `for (key in array)` walks the keys in an order each interpreter chooses, so setup sorts the outputs before comparing. GoAWK runs a Go garbage collector. Peak RSS is not measured, because the three read a line at a time and stayed near the cost of starting a process.

Baseline: GNU awk. Suite: [bench/thirdparty/awk](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/awk)

## sort

Same job: write generated CSV ordered by its amount column read as a number (`-t , -k 4,4n`), on one thread (`--parallel=1`). Measured at 5MiB and 100MiB.

Made equal: `LC_ALL=C`, so both compare bytes and read a dot as the decimal point, and `TMPDIR` in the working directory for the files a sort larger than its buffer writes. Without `-s`, both order lines with the same amount by their whole bytes.

Two packages install a program named `sort`: GNU coreutils, the `sort` of most Linux systems, and uutils coreutils, a Rust reimplementation that Ubuntu installs as `sort` from 25.10 on. The suite runs uutils through its multi-call binary as `coreutils sort`, a name that cannot be GNU sort, and setup checks that `sort --version` names GNU coreutils; on a system whose `sort` is uutils, the suite would otherwise compare uutils with itself and pass.

Not equal: both read the whole file before writing, into different structures, so peak RSS follows the input size for both.

Baseline: GNU sort. Suite: [bench/thirdparty/sort](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/sort)

## Interactive shells

Same job: start an interactive shell without startup files on an 80 by 24 pseudo-terminal, read a typed command, print the result and exit. Measured with `echo $((6*7))`, where starting dominates, and with a POSIX loop that counts to 100000, where evaluating dominates.

Made equal: the same keys, typed from `stdin` one at a time once the shell has read the one before, ending with `exit`. `bash --norc --noprofile -i` and `dash -i` with `ENV` empty. `TERM=dumb`, because with a `TERM` such as `xterm` bash 5.1 and later write bracketed-paste escape sequences around every command. `HOME` in the working directory, `INPUTRC=/dev/null` so readline reads neither `~/.inputrc` nor `/etc/inputrc`, and `HISTFILE` empty so no run starts from the history the last one wrote.

A command that needs a terminal cannot run in setup, so the check is in `cleanup`: it reads the terminal output of the last measured run, kept with `stdout`, and looks for the result, which the typed keys do not contain (the loop prints `count=100000` for that reason). The result is not always alone on its line, because the terminal echoes keys typed ahead before the shell switches echo off. A failing cleanup fails the benchmark.

Not equal: bash reads keys one at a time through readline, so the wait for each key counts toward its latency, which is above its CPU time; dash has no line editor and reads a line at once. The prompts differ and are not compared.

Baseline: bash. dash has no option that prints its version, so the report records its path and the workflow prints its package version. `terminal: true` is not available on Windows. Suite: [bench/thirdparty/shell](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/shell)

## C compilers

Same job: compile one generated C file with `-O2` and link it into an executable. Measured at 64KiB and 512KiB of source; the size is approximate, because C cannot be padded without changing the work, and throughput reads the file.

Made equal: `TMPDIR` in the working directory for the files the drivers write before linking. The executables differ in bytes, so setup builds and runs both and compares what they print; the file chains unsigned arithmetic, which wraps by definition, so a correct build prints the same number from either compiler.

A compile takes seconds, so the suite fixes `runs: 5` after one warmup instead of measuring adaptively, and raises `timeout` from one minute to five.

Not equal: `-O2` names different optimizations in each compiler, and the speed of the programs is not measured. gcc runs its compiler, assembler and linker as separate processes, while clang assembles in its own process; peak RSS is that of the largest process of the tree.

Baseline: GCC. Suite: [bench/thirdparty/cc](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/cc)

## JavaScript runtimes

Same job: run [count.mjs](https://github.com/nao1215/himorime/blob/main/bench/thirdparty/js/count.mjs) over generated JSON Lines and write, for each event in sorted order, how many records carry it and the sum of their `dur_ms`. Measured at 5MiB and 100MiB.

Made equal: the module uses only `node:fs` and `node:process`, so the three runtimes run it unchanged. Bun reads `~/.bunfig.toml`, Deno keeps compiled modules under `DENO_DIR` and checks online for a newer release, and `NODE_OPTIONS` can pass flags to Node.js, so `HOME` and `DENO_DIR` point into the working directory, `DENO_NO_UPDATE_CHECK` is set and `NODE_OPTIONS` is empty; each benchmark starts with an empty cache that the warmup runs fill. Deno reads a file only when allowed, so it gets `--allow-read`, the one permission the job needs, and `--no-config` so it does not read a `deno.json` of the directory.

Not equal: Node.js and Deno run V8 and Bun runs JavaScriptCore; all three compile and collect garbage on threads of their own. The module reads the whole file into one string, so peak RSS follows the input size.

Baseline: Node.js. Suite: [bench/thirdparty/js](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/js)
