---
title: Platform support
description: Operating systems and architectures yahiko supports, and the few behaviors that differ between them.
---

yahiko runs on Linux, macOS and Windows, on amd64 and arm64. Release archives
are published for those six combinations. CI runs the unit tests (with the race
detector on Linux) and the end-to-end suite on Linux, macOS and Windows.

The same suite runs on all three when commands are written as argument lists.
The differences are:

| Behavior | Linux, macOS | Windows |
|---|---|---|
| `shell: true` | `/bin/sh -c` | `cmd.exe /d /s /c` (`%ComSpec%`); values holding `"`, `%` or a line break cannot be substituted |
| Stopping a process tree on timeout, Ctrl+C, or when the command exits | the command runs in its own process group, which receives SIGKILL | the command is assigned to a Job Object, which is terminated; yahiko waits until the job is empty |
| `${artifact}` and `${exe}` | no suffix | `.exe` |
| Program lookup | `PATH` | `PATH` and `PATHEXT` |

On Windows a process can start a child in the instant between its creation and
its assignment to the Job Object; such a child is not stopped with the tree
and its CPU time is not counted. The window is microseconds wide.

## Metrics per platform

| Metric | Linux | macOS | Windows | FreeBSD, OpenBSD, NetBSD |
|---|---|---|---|---|
| Latency, throughput | monotonic clock | monotonic clock | monotonic clock | monotonic clock |
| CPU time, utilization | `wait4` usage of the process and waited-for descendants | same as Linux | Job Object accounting of every process in the job | same as Linux |
| Peak RSS | `ru_maxrss`, kilobytes, largest waited-for process | `ru_maxrss`, bytes, largest waited-for process | peak working set of the started process; unsupported when it started other processes | `ru_maxrss`, kilobytes |

Every value is read after the process exits, without polling. `ru_maxrss` is
normalized to bytes on every platform. Other Unix systems (Solaris, illumos,
AIX) build, but report CPU time and peak RSS as unsupported, because their
`wait4` does not fill `ru_maxrss` the same way. See [Metrics](/metrics/) for
what each value includes.

FreeBSD, OpenBSD and NetBSD build and pass the linters, but no release
binaries are published for them and CI does not run the suite there.

## Measurement caveats per platform

- Process creation cost differs a lot between operating systems; Windows is
  typically the slowest. Compare results only within one platform.
- Peak RSS is not comparable across operating systems: page sizes, the
  loader and what counts as resident differ.
- A suite that measures memory for a command starting child processes runs on
  Windows with `metrics.unsupported: skip`; without it, it exits 6 there.
- Virtual machines and laptops on battery scale CPU frequency. A comparison
  interleaves revisions to spread this out, but it cannot remove it.
