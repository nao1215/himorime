---
title: Metrics
description: What yahiko measures — latency, throughput, CPU time and utilization, peak RSS — in which unit, over which processes, on which platform, and what it cannot tell you.
toc: true
---

Every benchmark measures latency. Throughput, CPU and memory are switched on
per benchmark, or for all benchmarks under `defaults`:

```yaml
version: "1"

suite:
  name: metrics

benchmarks:
  - name: convert a large file
    setup:
      - command: [mytool, generate, -o, "${workdir}/large.json"]
    metrics:
      throughput:
        work:
          file_size: "${workdir}/large.json"
      cpu: true
      memory: true
    commands:
      mytool:
        command: [mytool, convert, "${workdir}/large.json"]
    budget:
      mytool:
        latency: {p95: "<= 100ms"}
        throughput: {median: ">= 50MiB/s"}
        cpu: {total: {median: "<= 70ms"}}
        memory: {peak_rss: {max: "<= 64MiB"}}
```

## The metrics

| Metric | JSON name | Unit | Better | Covers | Enabled by |
|---|---|---|---|---|---|
| Latency | `latency` | ns | lower | wall-clock time of the run | always |
| Throughput | `throughput` | work unit per second | higher | wall-clock time of the run | `metrics.throughput` |
| User CPU time | `cpu_user` | ns | lower | process tree | `metrics.cpu` |
| System CPU time | `cpu_system` | ns | lower | process tree | `metrics.cpu` |
| Total CPU time | `cpu_total` | ns | lower | process tree | `metrics.cpu` |
| CPU utilization | `cpu_utilization` | percent | neutral | process tree | `metrics.cpu` |
| Peak RSS | `peak_rss` | bytes | lower | process tree | `metrics.memory` |

Every value is recorded per measured run. Reports keep the raw samples, and
every statistic is computed from them: count, min, max, mean, median, sample
standard deviation, coefficient of variation, and the 90th, 95th and 99th
percentiles plus any percentile a budget uses. Percentiles interpolate
linearly between the closest ranks (the method NumPy and R call type 7).

## Latency

The wall-clock time from just before the process is started until it has been
reaped, measured with the monotonic clock. It includes process creation, the
dynamic loader, a shell when `shell: true` is used, and everything the
program does. It excludes `setup`, `prepare_each`, `cleanup`, the build,
reading declared work, collecting other metrics and writing reports.

## Throughput

Throughput is the work one run does divided by that run's latency. yahiko
cannot know what "work" means for your program, so you declare it, and a
suite without a declaration has no throughput:

```yaml
metrics:
  throughput:
    work:
      value: 100000
      unit: records
```

```yaml
metrics:
  throughput:
    work:
      file_size: testdata/large.json
      unit: bytes
```

- `value` is a fixed amount greater than zero, in any unit you name:
  `operations` (the default), `records`, `lines`, `files`.
- `file_size` is the size of a file in bytes, read before every run after
  `prepare_each` and outside the measured time. A missing or empty file is a
  metric collection failure. `unit` must be `bytes` (the default).
- Byte throughput is shown as KiB/s, MiB/s and so on; other units with a k,
  M or G prefix, such as `12.35k records/s`.
- Budgets are floors in the work unit: `">= 50MiB/s"`, `">= 1000 records/s"`.
  A unit that does not match the declared work is a validation error.

Because throughput is computed per run, `min` is the slowest run and `p95`
is the fast end of the distribution. Use `min` or a low percentile such as
`p5` for a worst-case floor.

## CPU time and utilization

User and system CPU time are read from the operating system when a run exits.
`cpu_total` is their sum. They count time the processes spent running on a
CPU, not time spent waiting for I/O, locks, sleeps or other processes, so CPU
time can be much smaller than latency (a program waiting on disk) or much
larger (a program using several cores).

`cpu_utilization` is `cpu_total / latency × 100` for each run:

- 100% means one CPU was busy for the whole run.
- Above 100% means more than one CPU was busy at the same time. A command
  using four cores fully shows 400%. It is not divided by the number of CPUs.
- Far below 100% means the command mostly waited.

Utilization has no better direction: more can mean better parallelism or
wasted work. It can carry a budget in either direction, but it is never
judged as a regression.

The operating system accounts CPU time in scheduler ticks or microseconds,
so a command that runs for a millisecond or two can report `0ns` of user
time. Judge CPU time on workloads that use tens of milliseconds or more.

## Peak RSS

Peak resident set size is the largest amount of physical memory a process
had mapped at one time, as recorded by the kernel, in bytes. yahiko reports
the largest peak of any single process in the tree. It is:

- not the heap size, the allocation count or the garbage collector's view of
  a language runtime: a Go, Java or Node.js program reserves and frees
  memory on its own schedule;
- not the sum of processes running at the same time;
- counting shared libraries and files mapped into memory, and not counting
  memory that was reserved but never touched, or swapped out.

The value is the kernel's own high-water mark, read once when the run exits.
yahiko does not poll, so it does not miss a short spike the way sampling
would, and collecting it adds no work while the command runs. `ru_maxrss` is
reported in kilobytes on Linux and the BSDs and in bytes on macOS; yahiko
converts both to bytes.

## Process tree

CPU time and peak RSS cover the process yahiko starts and its descendants.
What "descendants" means depends on the operating system:

| | Linux, macOS, FreeBSD, OpenBSD, NetBSD | Windows |
|---|---|---|
| Source | `wait4` resource usage of the started process | Job Object accounting, and the process's own memory counters |
| CPU time includes | the process and every descendant whose parent waited for it | every process that was part of the job |
| Peak RSS includes | the largest peak among the process and descendants whose parent waited for them | the peak working set of the started process, only when it started no other process |
| Not included | a descendant still running when the command exits, or orphaned before it exits | a child started in the microseconds before the process joined the job |

On Windows, a command that starts child processes has CPU time but no peak
RSS: Windows keeps no peak working set for a job, and reporting only the
parent would under-report. yahiko reports it as unsupported for that run
instead. A process a command leaves running in the background is stopped when
the command exits, on every platform.

## Unsupported, failed and not requested

A metric in a report always has a status:

| Status | Meaning | Statistics |
|---|---|---|
| `measured` | Every successful run recorded it. | present |
| `not_requested` | The benchmark does not enable it. | `null` |
| `unsupported` | The platform cannot measure it, and `metrics.unsupported` is `skip`. | `null`, with a reason |
| `failed` | The platform supports it but did not report it, or the declared work could not be read. | `null`, with a reason |

A missing value is never reported as zero. With the default
`metrics.unsupported: fail`, yahiko stops before building or running anything
when a platform cannot measure a requested metric at all, and exits 6 when it
learns so during a run. With `skip`, the rest is measured, the metric is
`unsupported`, and its budgets and comparisons are `skipped`, never passed. A
`failed` metric exits 6 under either policy.

## Overhead

Latency is measured around the process only. CPU time and peak RSS come from
statistics the operating system already keeps for an exited process, read
after the measured interval ends; nothing samples the command while it runs.
On Linux, the `BenchmarkRunUsage` Go benchmark in `internal/proc` shows no
difference between starting and reaping a process with and without
collection beyond run-to-run noise (about 0.8ms per process either way on the
development machine), and the `collector overhead` benchmark in `bench/`
measures the same end to end on every pull request. On Windows collection is
two system calls per run, also after the measured interval.

## What yahiko does not measure

- Heap allocations, garbage collection pauses or other runtime-specific
  statistics.
- Hardware counters such as cycles, instructions or cache misses.
- CPU or memory of the whole machine, or of processes the command did not
  start.
- Energy use.
- Anything of a service running in the background: yahiko measures commands
  that start and finish.
