---
title: Metrics
description: How himorime measures latency, throughput, CPU time, utilization and peak RSS across platforms.
toc: true
aliases: ["/platforms/"]
---

Every benchmark measures latency. Throughput, CPU and memory are switched on per benchmark, or for all benchmarks under `defaults`:

```yaml
version: "1"

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
          latency: {p95: "<= 100ms"}
          throughput: {median: ">= 50MiB/s"}
          cpu_total: {median: "<= 70ms"}
          peak_rss: {max: "<= 64MiB"}
```

## Platform support

Release archives cover Linux, macOS and Windows on amd64 and arm64. FreeBSD, OpenBSD and NetBSD build but do not have release archives.

| Behavior | Linux, macOS | Windows |
|---|---|---|
| `shell: true` | `/bin/sh -c` | `cmd.exe /d /s /c` |
| `${artifact}` and `${exe}` | no suffix | `.exe` |
| `terminal: true` | supported | unsupported |

Compare results only within one operating system: process creation, memory accounting and CPU resolution differ.

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

Reports keep the raw per-run samples and compute count, min, max, mean, median, sample standard deviation, coefficient of variation, p90, p95, p99 and any percentile a budget uses. Percentiles interpolate linearly between the closest ranks (type 7 in NumPy and R).

## Latency

Monotonic wall-clock time from just before the process starts until it is reaped. It includes process creation, the dynamic loader and the shell of `shell: true`. It excludes hooks, the build, reading declared work, collecting other metrics and writing reports.

## Throughput

Throughput is the declared work of one run divided by its latency:

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

- `value` is a fixed amount greater than zero, in any unit you name: `operations` (the default), `records`, `lines`, `files`.
- `file_size` is the size of a file in bytes, read before every run after `prepare_each` and outside the measured time. A missing or empty file is a metric collection failure. `unit` must be `bytes` (the default).
- Byte throughput is shown as KiB/s, MiB/s and so on; other units with a k, M or G prefix, such as `12.35k records/s`.
- Budgets are floors in the work unit: `">= 50MiB/s"`, `">= 1000 records/s"`. A unit that does not match the declared work is a validation error.

`min` is the slowest run and `p95` the fast end, so use `min` or `p5` for a worst-case floor. Reports record the measured work as `work.measured_min` and `work.measured_max` in JSON and `measured_work_min` and `measured_work_max` rows in CSV.

A throughput comparison is work divided by the latency statistic, with latency's verdict and confidence. Budgets and plain runs use per-run rates instead. The comparison needs constant, equal work on both revisions; otherwise it is skipped and the reason names the measured ranges.

## CPU time and utilization

User and system CPU time are read from the operating system when a run exits, and `cpu_total` is their sum. Waiting for I/O, locks or sleeps does not count, so CPU time can be far below latency, or far above it on several cores.

`cpu_utilization` is `cpu_total / latency × 100` per run, not divided by the number of CPUs: a command keeping four cores busy shows 400%. It can carry a budget in either direction but is never judged as a regression.

Linux and macOS report microseconds from `rusage`. Windows reports whole 15.625ms scheduler ticks from the Job Object: samples on a GitHub-hosted runner were 0, 15.625ms, 31.25ms and 46.875ms, so a command using a few milliseconds of CPU has a median of `0ns`. Judge CPU time on workloads of tens of milliseconds, hundreds on Windows. A comparison whose base is `0ns` is inconclusive with the reason `the base measurement is zero`.

## Peak RSS

Peak RSS is the kernel's high-water mark of physical memory, in bytes, read once when the run exits, so a short spike is never missed. himorime reports the largest peak of any single process in the tree (`process_aggregation: max_of_single_process_peaks`): two concurrent children of 100MiB each report about 100MiB. It is not the heap size of a language runtime. It counts mapped shared libraries and files, and not memory that was reserved but never touched, or swapped out.

### The floor

On Linux a command's peak RSS starts at the peak of the process that started it, which for himorime is about 10MiB. macOS and the BSDs are treated the same way. So a benchmark that measures memory starts its commands from a spawner: a small copy of the himorime executable that only starts commands and stays at a few MiB. On Linux it resets its recorded peak before each start. If the spawner cannot start, himorime starts the command itself. The spawner costs about a millisecond once and tens of microseconds per run, outside the measured interval.

The floor is the starter's peak RSS read after the run, plus 1MiB on Linux because the kernel's per-CPU RSS counters are approximate. JSON records `metrics.peak_rss.floor`, the largest floor in bytes, and `samples_at_floor`, the runs at or below it, whose real peak is unknown. Statistics are still computed from the raw values.

- Tables show a statistic at or below the floor as `≤ 6.30MiB`, the floor, with a sentence under the table.
- In a comparison, a side at its floor counts as using the whole floor. A regression from a base at its floor, or an improvement to a head at its floor, is still reported. Any other verdict with one side at its floor is inconclusive ("the peak RSS is at or below the measurement floor"), and with both sides at their floors the metric is `skipped`.
- An upper-bound budget (`<` or `<=`) that the floor meets passes. Otherwise the budget is `skipped` and the command is `metric_error` with exit 6.

To measure below the floor, use a larger input. On Windows the floor is always 0.

## Process tree

CPU time and peak RSS cover the process himorime starts and its descendants. What "descendants" means depends on the operating system:

| | Linux, macOS, FreeBSD, OpenBSD, NetBSD | Windows |
|---|---|---|
| Source | `wait4` resource usage of the started process | Job Object accounting, and the process's own memory counters |
| CPU time includes | the process and every descendant whose parent waited for it | every process that was part of the job |
| Peak RSS includes | the largest peak among the process and descendants whose parent waited for them | the peak working set of the started process, only when it started no other process |
| Not included | a descendant still running when the command exits, or orphaned before it exits | nothing: the command is suspended until it belongs to the job |

On Windows, a command that starts child processes has CPU time but its peak RSS is unsupported, because Windows keeps no peak working set for a job. A background process left behind is stopped when the command exits, on every platform.

Every metric in a report says how it was collected:

| Field | Values |
|---|---|
| `scope` | `wall_clock` (latency, throughput) or `process_tree` (CPU, memory) |
| `source` | `wall_clock`, `declared_work`, `rusage`, `job_object`, `process_memory_counters`, or `unavailable` on a platform that does not read the metric |
| `process_aggregation` | `none` (not a per-process value), `sum_of_waited_descendants`, `sum_of_job_processes`, `max_of_single_process_peaks`, `started_process_only` |

himorime does not measure the combined memory of a process tree or container. On Linux, measure that separately in a fresh, delegated cgroup v2 and read `memory.peak`, which includes file cache and kernel charges; see the [kernel's memory controller documentation](https://docs.kernel.org/admin-guide/cgroup-v2.html#memory). Do not sum process peaks, which counts sequential allocations as concurrent and shared pages twice.

## Unsupported, failed and not requested

A metric in a report always has a status:

| Status | Meaning | Statistics |
|---|---|---|
| `measured` | Every successful run recorded it. | present |
| `not_requested` | The benchmark does not enable it. | `null` |
| `unsupported` | The platform cannot measure it, and `metrics.unsupported` is `skip`. | `null`, with a reason |
| `failed` | The platform supports it but did not report it, or the declared work could not be read. | `null`, with a reason |

A missing value is never reported as zero. With the default `metrics.unsupported: fail`, himorime stops before building anything when the platform cannot measure a requested metric, and exits 6 if it finds out during a run. With `skip`, the metric is `unsupported`, its budgets and comparisons are `skipped`, and a passing command shows `PASS WITH SKIPS`. A `failed` metric exits 6 under either policy.

## Overhead

CPU time and peak RSS come from statistics the operating system keeps for an exited process, read after the measured interval; nothing samples the command while it runs. On Windows that is two system calls per run. Elsewhere, memory benchmarks add the [spawner](#the-floor): about a millisecond once and two messages per run, outside the measured interval.

The `collector overhead` benchmark in `bench/` runs the same small suite with latency only and with every metric, on every pull request. `make bench-docs` produced this table:

<!-- himorime:begin overhead -->

### himorime dogfood

Start-up, validation and a small measured run of himorime itself.

| Benchmark | Command | Median | P95 | Mean | Stddev | Min | Max | Runs | Relative |
|---|---|--:|--:|--:|--:|--:|--:|--:|--:|
| collector overhead | latency-only | 22.25ms | 23.44ms | 22.02ms | 1.11ms | 19.56ms | 23.78ms | 15 | 1.01x |
| collector overhead | all-metrics | 22.00ms | 23.12ms | 22.04ms | 752.02µs | 20.68ms | 23.68ms | 15 | 1.00x |

Relative is the median divided by the baseline command's median, or by the fastest command's.

Measured with himorime v0.1.1-9-g1ac04ac on linux/amd64, AMD RYZEN AI MAX+ 395 w/ Radeon 8060S (32 logical CPUs), head 1ac04ac93cbf, seed 514079471157815.

<!-- himorime:end overhead -->

## What himorime does not measure

- Heap allocations, garbage collection pauses or other runtime-specific statistics.
- Hardware counters such as cycles, instructions or cache misses.
- CPU or memory of the whole machine, or of processes the command did not start.
- Energy use.
- Anything of a service running in the background: himorime measures commands that start and finish.
