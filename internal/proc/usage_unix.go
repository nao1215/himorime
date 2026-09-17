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
	return usageFromRaw(rawUsageOf(cmd))
}

// usageFromRaw converts the rusage of a reaped command, or says why there is
// none.
func usageFromRaw(raw rawUsage) Usage {
	cpuErr, memErr := platformCapabilities()
	if cpuErr != nil {
		return Usage{CPUErr: cpuErr, MemoryErr: memErr}
	}
	if raw.Missing != "" {
		err := errors.New(raw.Missing)
		return Usage{CPUErr: err, MemoryErr: err}
	}
	return usageFromValues(raw)
}

// rawUsage is the part of wait4's rusage himorime reads, as the process that
// reaped the command saw it. A spawner sends it to himorime as it is.
type rawUsage struct {
	// Missing says why there is no rusage; empty when there is.
	Missing   string `json:"missing,omitempty"`
	UserCPU   int64  `json:"user_cpu_ns"`
	SystemCPU int64  `json:"system_cpu_ns"`
	MaxRSS    int64  `json:"maxrss"`
}

func rawUsageOf(cmd *exec.Cmd) rawUsage {
	if cmd.ProcessState == nil {
		return rawUsage{Missing: "the process has no exit status"}
	}
	ru, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage)
	if !ok || ru == nil {
		return rawUsage{Missing: "the operating system returned no resource usage for the process"}
	}
	return rawUsage{UserCPU: ru.Utime.Nano(), SystemCPU: ru.Stime.Nano(), MaxRSS: int64(ru.Maxrss)} //nolint:unconvert // Maxrss is int32 on some systems
}

func usageFromValues(raw rawUsage) Usage {
	u := Usage{
		UserCPU:   time.Duration(raw.UserCPU),
		SystemCPU: time.Duration(raw.SystemCPU),
	}
	if u.UserCPU < 0 || u.SystemCPU < 0 {
		u.CPUErr = fmt.Errorf("the operating system reported negative CPU time (user %v, system %v)", u.UserCPU, u.SystemCPU)
	}
	u.PeakRSS, u.MemoryErr = normalizeMaxRSS(raw.MaxRSS)
	return u
}

func usageFromRusage(ru *syscall.Rusage) Usage {
	return usageFromValues(rawUsage{UserCPU: ru.Utime.Nano(), SystemCPU: ru.Stime.Nano(), MaxRSS: int64(ru.Maxrss)}) //nolint:unconvert // Maxrss is int32 on some systems
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

// startFloor is called by the process that starts a command, right before it
// starts it and outside the measured interval. It lowers this process's
// recorded peak RSS where the platform allows it (Linux) and returns the peak
// RSS the command inherits as a floor: the kernel folds the starting
// process's peak into the command's ru_maxrss at exec. It is the peak of the
// address space on Linux and getrusage's ru_maxrss elsewhere; 0 when neither
// can be read.
func startFloor() int64 {
	if !rusageSupported() {
		return 0
	}
	if peak, ok := resetMemoryPeak(); ok {
		return peak
	}
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	floor, err := normalizeMaxRSS(int64(ru.Maxrss)) //nolint:unconvert // Maxrss is int32 on some systems
	if err != nil {
		return 0
	}
	return floor
}
