package runner

import (
	"time"

	"github.com/nao1215/yahiko/internal/config"
	"github.com/nao1215/yahiko/internal/metric"
)

// Side names. A plain run measures only the head side.
const (
	SideHead = "head"
	SideBase = "base"
)

// FailureKind classifies why a measurement or benchmark did not complete.
// The strings are part of the JSON report contract.
type FailureKind string

// Failure kinds.
const (
	FailBuild       FailureKind = "build_failed"
	FailSetup       FailureKind = "setup_failed"
	FailPrepareEach FailureKind = "prepare_each_failed"
	FailCleanup     FailureKind = "cleanup_failed"
	FailExitCode    FailureKind = "exit_code"
	FailTimeout     FailureKind = "timeout"
	FailStart       FailureKind = "start_failed"
	FailInterrupted FailureKind = "interrupted"
	FailPath        FailureKind = "invalid_path"
	FailInternal    FailureKind = "internal"
	// FailMetricUnsupported: a requested metric cannot be measured on this
	// platform (or for this process tree) and the suite's unsupported policy
	// is fail.
	FailMetricUnsupported FailureKind = "metric_unsupported"
	// FailMetricCollection: the platform supports the metric but reading it
	// failed, or the declared work could not be determined.
	FailMetricCollection FailureKind = "metric_collection_failed"
)

// IsMetricFailure reports whether a failure is about collecting a metric
// rather than running the command.
func (k FailureKind) IsMetricFailure() bool {
	return k == FailMetricUnsupported || k == FailMetricCollection
}

// Failure describes one failure.
type Failure struct {
	Kind    FailureKind
	Message string
	// ExitCode is set for FailExitCode.
	ExitCode int
	// Stderr is the redacted tail of the process's standard error, when any.
	Stderr string
	// Metric is the metric group a metric failure is about; empty otherwise.
	Metric metric.Group
}

// Side is one tree being measured.
type Side struct {
	Name string
	// Root is the suite's directory inside this tree; ${root} expands to it,
	// and every relative path of the suite is relative to it, so each
	// revision runs its own scripts and reads its own files.
	Root string
	// HeadRoot is the suite's directory inside the working tree, the same for
	// every side; ${head_root} expands to it. A fixture both revisions must
	// share is written with it.
	HeadRoot string
	// ProjectRoot is the top of this tree (the Git worktree or the suite
	// directory); paths may not resolve outside it.
	ProjectRoot string
	// Artifact is where the build writes ${artifact}; empty without a build.
	Artifact string
}

// Measurement holds the samples of one command on one side. Every slice
// holds one value per measured run, in execution order, and all collected
// slices have the same length as Samples.
type Measurement struct {
	// Samples is the wall-clock latency of each run.
	Samples []time.Duration
	// CPUUser and CPUSystem are collected when the benchmark measures cpu.
	CPUUser   []time.Duration
	CPUSystem []time.Duration
	// PeakRSS, in bytes, is collected when the benchmark measures memory.
	PeakRSS []int64
	// Work is the declared work of each run when the benchmark measures
	// throughput.
	Work    []float64
	Warmups int
	Failure *Failure
	// Unsupported maps a requested metric group this side could not measure
	// under the skip policy to the reason. Such a group has no samples.
	Unsupported map[metric.Group]string
}

// Skipped reports why a group was not measured, if it was skipped.
func (m *Measurement) Skipped(g metric.Group) (string, bool) {
	if m == nil || m.Unsupported == nil {
		return "", false
	}
	reason, ok := m.Unsupported[g]
	return reason, ok
}

func (m *Measurement) skip(g metric.Group, reason string) {
	if m.Unsupported == nil {
		m.Unsupported = map[metric.Group]string{}
	}
	m.Unsupported[g] = reason
	switch g {
	case metric.GroupCPU:
		m.CPUUser, m.CPUSystem = nil, nil
	case metric.GroupMemory:
		m.PeakRSS = nil
	case metric.GroupLatency, metric.GroupThroughput:
	}
}

// CommandResult is one command of a benchmark.
type CommandResult struct {
	Command config.Command
	// Sides maps SideHead / SideBase to the measurement. A command excluded
	// from a revision comparison has no entry for SideBase.
	Sides map[string]*Measurement
}

// BenchmarkResult is one benchmark.
type BenchmarkResult struct {
	Benchmark config.Benchmark
	// Failure is a benchmark-level failure: a hook failed or the run was
	// interrupted. Command results may still hold partial samples.
	Failure  *Failure
	Rounds   int
	Commands []CommandResult
}
