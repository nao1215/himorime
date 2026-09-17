//go:build !windows

package proc

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

// usageProbe has nothing to hold on Unix: wait4 returns the usage together
// with the exit status, and os/exec keeps it in ProcessState.
type usageProbe struct{}

func newUsageProbe(*exec.Cmd, *tree) *usageProbe { return &usageProbe{} }

func (*usageProbe) close() {}

// rusageSupported lists the systems whose wait4 fills ru_utime, ru_stime and
// ru_maxrss. Solaris and illumos leave ru_maxrss at zero, AIX reports it in
// another unit, and himorime does not guess.
func rusageSupported() bool {
	switch runtime.GOOS {
	case "linux", "android", "darwin", "ios", "freebsd", "openbsd", "netbsd", "dragonfly":
		return true
	}
	return false
}

func platformCapabilities() (cpu, memory error) {
	if !rusageSupported() {
		reason := fmt.Sprintf("himorime does not read resource usage on %s", runtime.GOOS)
		return &UnsupportedError{What: "cpu time", Reason: reason}, &UnsupportedError{What: "peak rss", Reason: reason}
	}
	return nil, nil
}

// collect reads the rusage wait4 stored for the reaped process. The kernel
// folds into it the usage of every descendant the process itself waited for,
// so it covers the process tree as long as parents reap their children: a
// descendant still running when the command exits, or orphaned and reaped by
// init, is not included. ru_maxrss is the peak of the largest single process
// in that tree, not the sum of processes running at the same time.
func (*usageProbe) collect(cmd *exec.Cmd, _ *tree) Usage {
	cpuErr, memErr := platformCapabilities()
	if cpuErr != nil {
		return Usage{CPUErr: cpuErr, MemoryErr: memErr}
	}
	if cmd.ProcessState == nil {
		err := errors.New("the process has no exit status")
		return Usage{CPUErr: err, MemoryErr: err}
	}
	ru, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage)
	if !ok || ru == nil {
		err := errors.New("the operating system returned no resource usage for the process")
		return Usage{CPUErr: err, MemoryErr: err}
	}
	return usageFromRusage(ru)
}

func usageFromRusage(ru *syscall.Rusage) Usage {
	u := Usage{
		UserCPU:   time.Duration(ru.Utime.Nano()),
		SystemCPU: time.Duration(ru.Stime.Nano()),
	}
	if u.UserCPU < 0 || u.SystemCPU < 0 {
		u.CPUErr = fmt.Errorf("the operating system reported negative CPU time (user %v, system %v)", u.UserCPU, u.SystemCPU)
	}
	u.PeakRSS, u.MemoryErr = normalizeMaxRSS(int64(ru.Maxrss)) //nolint:unconvert // Maxrss is int32 on some systems
	return u
}

// normalizeMaxRSS converts ru_maxrss to bytes. Its unit differs between
// systems: kilobytes (KiB) on Linux and the BSDs, bytes on macOS. A zero or
// negative peak cannot come from a process that ran, so it is a collection
// failure rather than a small value.
func normalizeMaxRSS(raw int64) (int64, error) {
	if raw <= 0 {
		return 0, fmt.Errorf("the operating system reported a peak resident set size of %d", raw)
	}
	const maxKiB = 1 << 53 // far beyond any real machine; guards the multiplication
	if maxRSSUnit != 1 && raw > maxKiB {
		return 0, fmt.Errorf("the operating system reported an implausible peak resident set size of %d", raw)
	}
	return raw * maxRSSUnit, nil
}

func platformCollection(memory bool) Collection {
	switch {
	case !rusageSupported():
		return Collection{Source: SourceUnavailable, ProcessAggregation: AggregationNone}
	case memory:
		return Collection{Source: SourceRusage, ProcessAggregation: AggregationMaxProcess}
	default:
		return Collection{Source: SourceRusage, ProcessAggregation: AggregationSumWaited}
	}
}
