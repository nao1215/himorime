package proc

import (
	"errors"
	"fmt"
	"time"
)

// Usage is the resource usage the operating system reported for a finished
// process tree. yahiko reads it from the statistics the OS keeps for an
// exited process (wait4's rusage on Unix, Job Object accounting on Windows);
// nothing polls the process while it runs, so collecting usage adds no work
// to the measured interval.
//
// A value is only meaningful when its error is nil. A missing value is never
// reported as zero: CPUErr and MemoryErr say why it is missing, and an
// *UnsupportedError tells an unsupported platform apart from a failed
// collection.
type Usage struct {
	// UserCPU and SystemCPU are the CPU time the tree spent in user and
	// kernel mode.
	UserCPU   time.Duration
	SystemCPU time.Duration
	CPUErr    error
	// PeakRSS is the largest resident set size, in bytes, of any single
	// process of the tree.
	PeakRSS   int64
	MemoryErr error
	// Processes is the number of processes the tree ran, when the platform
	// counts them (Windows); 0 when unknown.
	Processes int
}

// UnsupportedError reports that a metric cannot be measured on this platform
// or for this process tree. It is not a measurement failure: the value was
// never available, rather than lost.
type UnsupportedError struct {
	What   string
	Reason string
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("%s is not supported: %s", e.What, e.Reason)
}

// IsUnsupported reports whether err is an *UnsupportedError.
func IsUnsupported(err error) bool {
	var u *UnsupportedError
	return errors.As(err, &u)
}

// errNotCollected marks usage that was not requested for a run.
var errNotCollected = errors.New("resource usage was not requested for this run")

func notCollected() Usage {
	return Usage{CPUErr: errNotCollected, MemoryErr: errNotCollected}
}

// Capabilities reports, without running anything, whether this platform can
// measure CPU time and peak RSS of a process tree. A nil error means it can;
// on Windows peak RSS is further limited at run time to trees of a single
// process, which Run reports per run.
func Capabilities() (cpu, memory error) {
	return platformCapabilities()
}
