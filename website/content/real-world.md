---
title: Real-world suites
description: Suites in this repository that measure programs nobody here maintains, what each one shows about writing a himorime.yaml, and what was made equal between the programs.
toc: true
---

himorime measures a command, so a program you did not write is measured like your own: name it, give it an input, and read the numbers. The suites below do that with real third-party tools, and they are meant to be read and copied. Each one is a working `himorime.yaml` for a shape of program you are likely to have.

Run one yourself, with the tools it names on your PATH:

```console
$ himorime run bench/thirdparty/json
```

[thirdparty.yml](https://github.com/nao1215/himorime/blob/main/.github/workflows/thirdparty.yml) runs them weekly on a pinned runner so the examples stay correct as the tools and himorime change. The numbers of the latest run are in that workflow's job summary and in its artifact. They are not repeated here: a number measured on a shared runner last month says less than the suite you can run today on the machine you care about.

No suite here carries a budget. The speed of a program this repository does not maintain is not a reason to fail its CI, so the scheduled job fails only when a measurement fails: a tool that cannot be installed, a command that exits non-zero, or himorime reporting an error.

| Suite | Programs | What it shows |
|---|---|---|
| [json](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/json) | jq, gojq, jaq | Several implementations of one job in a single benchmark, input generated from a fixed seed, throughput declared from that input file, and a setup hook that proves the outputs match before anything is measured |
| [compression](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/compression) | gzip, bzip2, xz | Binary output commands writing to different formats, with decompression and byte comparison in setup before latency, throughput, CPU time and peak RSS are measured |
| [search](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/search) | GNU grep, ripgrep | A directory tree as input, a pinned locale, outputs compared after sorting, throughput declared as a number of files, and one tool measured at two thread counts |
| [copy](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/copy) | cp, rsync | A command that changes the state the next run starts from, reset by `prepare_each` outside the measured time, and CPU time taken over a tool that forks processes of its own |
| [csv](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/csv) | Miller, qsv, xan | A program that reads standard input, given a generated file with `stdin` so every run reads it from the first byte, and outputs that match only because the input quotes as little as it can |

## JSON processors

Same job: write `.user.id` of every line of generated JSON Lines, compact, one per line, to a file. Measured at 5MiB, where starting the process dominates, and at 100MiB, where parsing dominates.

Made equal: compact output on every tool (`-c`), the same input file, the same filter, and the same destination, a file in the benchmark's working directory. Each tool also runs once in setup and the three outputs are compared byte for byte, so a tool that stopped doing the job would fail the benchmark instead of looking fast. A comparison of tools doing different work is a comparison of nothing.

Not equal: gojq runs a Go garbage collector on threads of its own, so its CPU time is above its wall-clock time while jq and jaq stay on one thread. jaq reads the whole input into memory, while jq and gojq read it as they go, so peak RSS follows the input size for jaq only; the peaks of jq and gojq are close to what starting a process already costs, and are reported as at or below that floor. jq and jaq are native binaries and gojq starts a Go runtime, which is most of the difference at 5MiB.

The baseline is jq, the oldest and most widely used of the three, so `RELATIVE` reads as a multiple of it.

The input comes from [bench/thirdparty/gen](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/gen) from a fixed seed, so no sample data from anyone else's project is committed here and every run measures the same bytes. Every tool is installed at a pinned version whose digest is checked.

Suite: [bench/thirdparty/json](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/json)

## Compression tools

Same job: compress the same generated JSON Lines file with gzip, bzip2 and xz, using each format's native output and one worker where the tool supports it. The suite measures a small input where startup matters and a larger input where sustained compression matters.

Made equal: the input bytes, compression level intent, single-worker setting, and source file. Setup decompresses each output and compares the result with the source. The compressed bytes are not compared because the formats are different.

Not equal: gzip, bzip2 and xz use different compression formats and algorithms, so output sizes and CPU behavior are properties of each format rather than a claim that one format is universally better. xz and bzip2 have different memory profiles from gzip even at comparable level names.

The baseline is gzip, a widely deployed reference implementation. Versions are recorded in the report so a local run can be interpreted with the exact tool versions.

Suite: [bench/thirdparty/compression](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/compression)

## Text search

Same job: write every line that contains the fixed string `ERROR`, as `path:line:text`, from a generated tree of 64KiB log files to a file. Measured on 80 files (5MiB), where starting the process dominates, and on 1600 files (100MiB), where reading and scanning dominate.

Made equal: the same tree, searched as the relative path `tree` from the working directory, the same fixed string, line numbers on, and the same kind of destination, a file in the working directory. ripgrep is told to search what `grep -r` searches, with `--no-ignore --hidden`, and to print as grep does, with `--no-heading --color never`. `LC_ALL` is set to `C.UTF-8` so the locale of whoever runs the suite does not change how GNU grep reads bytes. Setup sorts each output and compares it with grep's byte for byte, and checks that grep found at least one line.

Not equal: GNU grep searches on one thread. ripgrep is measured twice, as `rg-j1` with `--threads 1` and as `rg` with its default thread count, which depends on the number of CPUs of the machine; the CPU time of `rg` can exceed its wall-clock time, while that of grep and `rg-j1` cannot. `rg` prints files in the order its threads finish them, so its output order varies between runs; this is why setup compares sorted outputs. Peak RSS is not measured, because every command stayed at what starting a process costs when the suite was written.

The baseline is GNU grep, the reference implementation that `grep -r` scripts assume, so `RELATIVE` reads as a multiple of it.

Throughput is declared as a fixed `value` in files, because the input is a directory rather than one file whose size could be read; setup counts the files so the number cannot drift from the tree. The tree comes from [bench/thirdparty/gen](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/gen) from a fixed seed. GNU grep comes from the pinned runner image and ripgrep is installed at a pinned version whose digest is checked.

Suite: [bench/thirdparty/search](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/search)

## Tree copy

Same job: copy a generated tree of 64KiB log files into a directory that does not exist yet, without keeping timestamps or ownership; both tools give each file the source's permission bits masked by the umask. Measured on 80 files (5MiB) and on 1600 files (100MiB).

Made equal: the same tree, the same destination path in the working directory, and the same kind of copy, `cp -R tree dest` and `rsync -r tree/ dest`. A copy leaves `dest` behind: the next `cp -R tree dest` would copy into `dest/tree`, and rsync would compare every file with the one already there. `prepare_each` removes `dest` before every warmup and measured run, outside the measured time. Setup copies once with each tool and compares the copy with the tree using `diff -r`.

Not equal: cp is one process. rsync forks processes of its own even when both ends are on one machine; CPU time is taken over the process tree, so all of them are counted. Neither command syncs to disk, so the copies land in the page cache and the numbers do not describe the disk. Peak RSS is not measured, because every command stayed at what starting a process costs when the suite was written.

The baseline is cp, the copy POSIX specifies, so `RELATIVE` reads as a multiple of it. Throughput is declared in files, as in the text search suite; the tree comes from [bench/thirdparty/gen](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/gen) from a fixed seed, and setup counts its files. Both tools come from the pinned runner image and their versions are recorded in the report.

Suite: [bench/thirdparty/copy](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/copy)

## CSV processors

Same job: read generated CSV on standard input and write its `id` and `note` columns, as CSV, to a file. Measured at 5MiB and at 100MiB.

Made equal: the same input, given to every command through `stdin`, which reopens the file for every run, so no command names an input file. The same two columns in the same order (`mlr --csv cut -o -f id,note`, `qsv select id,note`, `xan select id,note`), and the same kind of destination, a file in the working directory. A third of the notes hold a comma or a double quote and are quoted, so the outputs have to quote them the same way; setup compares the three outputs byte for byte.

Not equal: xan keeps the quotes of a field as the input wrote them, while Miller and qsv write a field quoted only when it needs quoting. The generated input quotes only the fields that need it, so the outputs match; an input that quotes every field would not. Miller reads, processes and writes on separate goroutines and runs a Go garbage collector, so its CPU time can exceed its wall-clock time, and its peak RSS rose with the input size when the suite was written while those of qsv and xan did not.

The baseline is Miller, the oldest of the three projects, so `RELATIVE` reads as a multiple of it.

The input comes from [bench/thirdparty/gen](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/gen) from a fixed seed. Miller is installed with `go install` at a pinned version, and qsv and xan from release archives at pinned versions whose digests are checked.

Suite: [bench/thirdparty/csv](https://github.com/nao1215/himorime/tree/main/bench/thirdparty/csv)
