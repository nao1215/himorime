---
title: Platform support
description: Operating systems and architectures himorime supports, and the few behaviors that differ between them.
---

himorime runs on Linux, macOS and Windows, on amd64 and arm64. Release archives are published for those six combinations. CI runs the unit tests (with the race detector on Linux) and the end-to-end suite on Linux, macOS and Windows.

The same suite runs on all three when commands are written as argument lists. The differences are:

| Behavior | Linux, macOS | Windows |
|---|---|---|
| `shell: true` | `/bin/sh -c` | `cmd.exe /d /s /c` (`%ComSpec%`); values holding `"`, `%` or a line break cannot be substituted |
| Stopping a process tree on timeout, Ctrl+C, or when the command exits | the command joins its own process group before it runs, and the group receives SIGKILL | the command is created suspended, assigned to a Job Object and only then resumed; the job is terminated, and himorime waits until it is empty |
| `${artifact}` and `${exe}` | no suffix | `.exe` |
| Program lookup | `PATH` | `PATH` and `PATHEXT` |

Because a Windows command runs no code before it belongs to its Job Object, every process it starts is in the job too: nothing escapes being stopped or having its CPU time counted, however quickly it starts. The time between creating the suspended process and resuming it is not counted as latency. If the process cannot be assigned to a job, for example because a parent job forbids it, himorime kills the still suspended process and reports the run as an `internal` error instead of measuring it without the guarantees. On Unix a descendant that moves itself into another process group or session (a daemon does) leaves the group and is not stopped with it.

## Metrics per platform

| Metric | Linux | macOS | Windows | FreeBSD, OpenBSD, NetBSD |
|---|---|---|---|---|
| Latency, throughput | monotonic clock | monotonic clock | monotonic clock | monotonic clock |
| CPU time, utilization | `wait4` usage of the process and waited-for descendants (`rusage`, `sum_of_waited_descendants`) | same as Linux | Job Object accounting of every process in the job (`job_object`, `sum_of_job_processes`) | same as Linux |
| Peak RSS | `ru_maxrss`, kilobytes, largest peak of a single waited-for process (`rusage`, `max_of_single_process_peaks`) | `ru_maxrss`, bytes, same as Linux | peak working set of the started process; unsupported when it started other processes (`process_memory_counters`, `started_process_only`) | `ru_maxrss`, kilobytes, same as Linux |
| Peak RSS floor | the spawner's RSS, reset before each start (a few MiB) | the spawner's peak RSS (`getrusage`) | none, always 0 | same as macOS |

Every value is read after the process exits, without polling. `ru_maxrss` is normalized to bytes on every platform. The names in parentheses are the `source` and `process_aggregation` every report records for the metric. Other Unix systems (Solaris, illumos, AIX) build, but report CPU time and peak RSS as unsupported, because their `wait4` does not fill `ru_maxrss` the same way. See [Metrics](/metrics/) for what each value includes.

FreeBSD, OpenBSD and NetBSD build and pass the linters, but no release binaries are published for them and CI does not run the suite there.

## Measurement caveats per platform

- Process creation cost differs a lot between operating systems; Windows is typically the slowest. Compare results only within one platform.
- Peak RSS is not comparable across operating systems: page sizes, the loader and what counts as resident differ.
- On Linux a command's peak RSS includes the peak of the process that started it, and himorime assumes the same on macOS and the BSDs, so a command smaller than that floor is reported as `<=` the floor. Commands whose memory is measured start from a small spawner to keep the floor low; only Linux can also reset the floor before each run. See [The floor](/metrics/#the-floor).
- A suite that measures memory for a command starting child processes runs on Windows with `metrics.unsupported: skip`; without it, it exits 6 there.
- Virtual machines and laptops on battery scale CPU frequency. A comparison interleaves revisions to spread this out, but it cannot remove it.
