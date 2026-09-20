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

Compare results only within one operating system. Process creation, memory accounting and CPU resolution differ across platforms. The process-tree section below lists the collection method for each metric.

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

Every value is recorded per measured run. Reports keep the raw samples, and every statistic is computed from them: count, min, max, mean, median, sample standard deviation, coefficient of variation, and the 90th, 95th and 99th percentiles plus any percentile a budget uses. Percentiles interpolate linearly between the closest ranks (the method NumPy and R call type 7).

## Latency

The wall-clock time from just before the process is started until it has been reaped, measured with the monotonic clock. It includes process creation, the dynamic loader, a shell when `shell: true` is used, and everything the program does. It excludes `setup`, `prepare_each`, `cleanup`, the build, reading declared work, collecting other metrics and writing reports.

## Throughput

Throughput is the work one run does divided by that run's latency. himorime cannot know what "work" means for your program, so you declare it, and a suite without a declaration has no throughput:

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

Because throughput is computed per run, `min` is the slowest run and `p95` is the fast end of the distribution. Use `min` or a low percentile such as `p5` for a worst-case floor.

Reports record the measured work, not only the declared amount: `work.measured_min` and `work.measured_max` in JSON, `measured_work_min` and `measured_work_max` rows in CSV. The work can differ between runs and between revisions.

A throughput comparison is derived from the latency comparison: its value is work divided by the selected latency statistic, and its verdict and confidence come from latency. This differs from the statistics of per-run rates shown for a plain run, which are also used by throughput budgets. Derivation requires constant, equal work on both revisions. If work differs between revisions or varies between runs, the throughput comparison is skipped and the reason names the measured ranges. A `file_size` fixture that grew or shrank changes throughput without the command running any faster or slower. The declared `value` is the same for both revisions, because a comparison reads one suite file, from the working tree.

## CPU time and utilization

User and system CPU time are read from the operating system when a run exits. `cpu_total` is their sum. They count time the processes spent running on a CPU, not time spent waiting for I/O, locks, sleeps or other processes, so CPU time can be much smaller than latency (a program waiting on disk) or much larger (a program using several cores).

`cpu_utilization` is `cpu_total / latency × 100` for each run:

- 100% means one CPU was busy for the whole run.
- Above 100% means more than one CPU was busy at the same time. A command using four cores fully shows 400%. It is not divided by the number of CPUs.
- Far below 100% means the command mostly waited.

Utilization has no better direction: more can mean better parallelism or wasted work. It can carry a budget in either direction, but it is never judged as a regression.

The resolution of that accounting differs by platform. Linux and macOS report microseconds from `rusage`: samples measured on Linux are multiples of 1µs. Windows reports whole scheduler ticks of 15.625ms from the Job Object: samples measured on a GitHub-hosted runner were 0, 15.625ms, 31.25ms and 46.875ms, and a command that spends a few milliseconds on a CPU has a median of `0ns` there.

Judge CPU time on a workload that uses more than a few multiples of that resolution, which means tens of milliseconds at least, and hundreds where Windows matters. A comparison whose base is `0ns` is inconclusive, with the reason `the base measurement is zero`, and a `min_difference` below one tick cannot tell two Windows measurements apart.

## Peak RSS

Peak resident set size is the largest amount of physical memory a process had mapped at one time, as recorded by the kernel, in bytes. himorime reports the largest peak of any single process in the tree. It is:

- not the heap size, the allocation count or the garbage collector's view of a language runtime: a Go, Java or Node.js program reserves and frees memory on its own schedule;
- not the sum of processes running at the same time: a command that runs two children of 100MiB each at the same time reports about 100MiB, not 200MiB. A report records this as `process_aggregation: max_of_single_process_peaks`; `scope: process_tree` only says which processes are candidates;
- counting shared libraries and files mapped into memory, and not counting memory that was reserved but never touched, or swapped out.

The value is the kernel's own high-water mark, read once when the run exits. himorime does not poll, so it does not miss a short spike the way sampling would, and collecting it adds no work while the command runs. `ru_maxrss` is reported in kilobytes on Linux and the BSDs and in bytes on macOS; himorime converts both to bytes.

### The floor

On Linux a command's peak RSS cannot be lower than the peak RSS of the process that started it: when the command starts, the kernel counts that process's peak as part of the command's. A command started by himorime directly would never be reported below himorime's own peak, about 10MiB and growing with every allocation himorime makes. himorime treats macOS and the BSDs the same way, since `ru_maxrss` there may carry the starting process's peak too.

So when a benchmark measures memory, himorime does not start the command itself. It starts it from a spawner: a copy of the himorime executable that runs next to himorime for as long as himorime runs, does nothing but start commands, and stays at a few MiB. The spawner measures latency around the start and the exit exactly as himorime does, and stops the process tree on a timeout, an interrupt, or when himorime itself goes away. On Linux it also resets its own recorded peak right before each start, so the floor is its current RSS rather than the largest it ever was. If the spawner cannot be started, himorime starts the command itself, and the floor is its own peak. Benchmarks that do not measure memory start commands directly: the spawner costs about a millisecond once and tens of microseconds per run, never inside the measured interval.

What remains is the floor: the peak RSS of the process that started a run, read after the run. Read before the start, it would miss what starting the command adds, since the command runs in its starter's memory until it replaces itself with the program. On Linux 1MiB is added to it, because the kernel sums RSS from per-CPU counters approximately and the value read can differ from the one folded into the command by a few pages. Every report records it. `metrics.peak_rss` in JSON has `floor`, the largest floor of the runs in bytes, and `samples_at_floor`, the runs whose peak RSS was at or below their floor. For those runs the command's real peak is unknown, only that it is at most the floor. Statistics are still computed from the raw values.

- Tables show a statistic at or below the floor as `≤ 6.30MiB`, the floor, with a sentence under the table.
- In a comparison, a side whose compared statistic is at or below its floor is compared as if it used the whole floor. A regression from a base at its floor, or an improvement to a head at its floor, is still reported, because the real change can only be larger. When one side alone is at its floor, any other verdict is inconclusive: "the peak RSS is at or below the measurement floor". When both sides are at their floors, memory was not observed beyond the floor on either side, so that metric is `skipped` and excluded from the comparison result and inconclusive count.
- A budget on a peak RSS at or below the floor passes when it is an upper bound (`<` or `<=`) that the floor itself meets, since the real value is lower still. Otherwise its budget status is `skipped` with the same reason, and the command is `metric_error` with exit 6 because the required budget could not be assessed.

To measure a command smaller than the floor, measure a larger input, or use a tool that reads the command's memory from inside it. Windows reads the peak working set of the started process itself, which the process that started it does not raise: its floor is always 0.

## Process tree

CPU time and peak RSS cover the process himorime starts and its descendants. What "descendants" means depends on the operating system:

| | Linux, macOS, FreeBSD, OpenBSD, NetBSD | Windows |
|---|---|---|
| Source | `wait4` resource usage of the started process | Job Object accounting, and the process's own memory counters |
| CPU time includes | the process and every descendant whose parent waited for it | every process that was part of the job |
| Peak RSS includes | the largest peak among the process and descendants whose parent waited for them | the peak working set of the started process, only when it started no other process |
| Not included | a descendant still running when the command exits, or orphaned before it exits | nothing: the command is suspended until it belongs to the job |

On Windows, a command that starts child processes has CPU time but no peak RSS: Windows keeps no peak working set for a job, and reporting only the parent would under-report. himorime reports it as unsupported for that run instead. A process a command leaves running in the background is stopped when the command exits, on every platform.

Every metric of every report says how it was collected, so a value can be read without knowing which platform produced it:

| Field | Values |
|---|---|
| `scope` | `wall_clock` (latency, throughput) or `process_tree` (CPU, memory) |
| `source` | `wall_clock`, `declared_work`, `rusage`, `job_object`, `process_memory_counters`, or `unavailable` on a platform that does not read the metric |
| `process_aggregation` | `none` (not a per-process value), `sum_of_waited_descendants`, `sum_of_job_processes`, `max_of_single_process_peaks`, `started_process_only` |

Tables and Markdown repeat the aggregation in a sentence under the CPU and memory tables. himorime does not measure the combined memory of a process tree or a container; a cgroup's memory peak is outside what it reads.

For a Linux-only combined-memory requirement, measure separately in a dedicated, delegated cgroup v2 and read `memory.peak`. This accounts for cgroup memory, including file cache and kernel charges, not summed process RSS. Shared pages and pages charged before entering the group need care. See the [kernel's memory controller documentation](https://docs.kernel.org/admin-guide/cgroup-v2.html#memory).

Use a fresh group per measurement, wait for all descendants, and keep wrapper startup outside any latency comparison. A wrapper needs writable delegation and an available memory controller; it is not a portable himorime collector. Do not sum individual process peaks: sequential allocations would be counted as concurrent, and shared pages could be counted twice. Keep `peak_rss` budgets for the documented single-process peak semantics.

## Unsupported, failed and not requested

A metric in a report always has a status:

| Status | Meaning | Statistics |
|---|---|---|
| `measured` | Every successful run recorded it. | present |
| `not_requested` | The benchmark does not enable it. | `null` |
| `unsupported` | The platform cannot measure it, and `metrics.unsupported` is `skip`. | `null`, with a reason |
| `failed` | The platform supports it but did not report it, or the declared work could not be read. | `null`, with a reason |

A missing value is never reported as zero. With the default `metrics.unsupported: fail`, himorime stops before building or running anything when a platform cannot measure a requested metric at all, and exits 6 when it learns so during a run. With `skip`, the rest is measured, the metric is `unsupported`, and its budgets and comparisons are `skipped` as an explicit waiver; a passing command is shown as `PASS WITH SKIPS`. A `failed` metric exits 6 under either policy. A required budget with no assessable value is not a waiver: an observed RSS floor keeps its budget `skipped` with a reason and makes the command `metric_error`; `no_data` caused by an execution failure remains an execution error.

## Overhead

Latency is measured around the process only. CPU time and peak RSS come from statistics the operating system already keeps for an exited process, read after the measured interval ends; nothing samples the command while it runs. On Windows collection is two system calls per run, also after the measured interval. On other platforms, a benchmark that measures memory starts its commands from the spawner described under [The floor](#the-floor): about a millisecond to start it once, and two messages per run, before and after the measured interval.

The `collector overhead` benchmark in `bench/` measures what collection costs end to end: the same small suite run by himorime with latency only and with every metric. It runs on every pull request, and the table below is produced by `make bench-docs` on the machine named under it.

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
