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
its assignment to the Job Object; such a child is not stopped with the tree.
The window is microseconds wide.

FreeBSD, OpenBSD and NetBSD build and pass the linters, but no release
binaries are published for them and CI does not run the suite there.

## Measurement caveats per platform

- Process creation cost differs a lot between operating systems; Windows is
  typically the slowest. Compare results only within one platform.
- Virtual machines and laptops on battery scale CPU frequency. A comparison
  interleaves revisions to spread this out, but it cannot remove it.
