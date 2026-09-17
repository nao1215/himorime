package runner

import (
	"time"

	"github.com/nao1215/yahiko/internal/config"
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
)

// Failure describes one failure.
type Failure struct {
	Kind    FailureKind
	Message string
	// ExitCode is set for FailExitCode.
	ExitCode int
	// Stderr is the redacted tail of the process's standard error, when any.
	Stderr string
}

// Side is one tree being measured.
type Side struct {
	Name string
	// Root is the suite's directory inside this tree; ${root} expands to it.
	Root string
	// ProjectRoot is the top of this tree (the Git worktree or the suite
	// directory); paths may not resolve outside it.
	ProjectRoot string
	// Artifact is where the build writes ${artifact}; empty without a build.
	Artifact string
}

// Measurement holds the samples of one command on one side.
type Measurement struct {
	Samples []time.Duration
	Warmups int
	Failure *Failure
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
