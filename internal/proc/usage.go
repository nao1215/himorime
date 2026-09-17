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

// Collection names how this platform obtains a group of usage metrics, so a
// report can say which processes a value covers and how their values were
// combined. The strings are part of the JSON report contract.
type Collection struct {
	// Source is where the values come from.
	Source string
	// ProcessAggregation is how the values of the processes of a tree are
	// combined into one value per run.
	ProcessAggregation string
}

// Collection sources.
const (
	// SourceRusage is the rusage wait4 returns for the reaped command.
	SourceRusage = "rusage"
	// SourceJobObject is the accounting of the Job Object the command and
	// its descendants run in.
	SourceJobObject = "job_object"
	// SourceProcessMemoryCounters is GetProcessMemoryInfo of the started
	// process.
	SourceProcessMemoryCounters = "process_memory_counters"
	// SourceUnavailable: this platform does not read the metric.
	SourceUnavailable = "unavailable"
)

// Process aggregations.
const (
	// AggregationSumWaited adds up the started process and every descendant
	// that was waited for by its parent. A descendant still running when the
	// command exits, or reparented and reaped by init, is not included.
	AggregationSumWaited = "sum_of_waited_descendants"
	// AggregationSumJob adds up every process that was ever part of the job,
	// whether or not anyone waited for it.
	AggregationSumJob = "sum_of_job_processes"
	// AggregationMaxProcess is the largest peak of any single process of the
	// tree. It is not the peak of the processes' combined usage: two
	// processes of 100MiB running at the same time report 100MiB.
	AggregationMaxProcess = "max_of_single_process_peaks"
	// AggregationStartedProcess is the started process alone; a run with
	// descendants is reported as unsupported rather than under-counted.
	AggregationStartedProcess = "started_process_only"
	// AggregationNone: the platform does not read the metric.
	AggregationNone = "none"
)

// CPUCollection describes how CPU time is collected on this platform.
func CPUCollection() Collection { return platformCollection(false) }

// MemoryCollection describes how peak RSS is collected on this platform.
func MemoryCollection() Collection { return platformCollection(true) }
