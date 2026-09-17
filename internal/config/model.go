// Package config loads, validates and resolves yahiko suite files.
//
// A suite file is versioned YAML (see schema/yahiko.schema.json). Loading
// happens in three stages: strict decoding into the Raw types, which rejects
// unknown keys and wrong types; semantic validation, which collects every
// problem it finds with its position in the file; and resolution into the
// Suite model, where every inherited default has been applied. Nothing in
// this package runs a command.
package config

import (
	"time"
)

// SupportedVersion is the only suite format version this build understands.
const SupportedVersion = "1"

// DefaultFileName is the suite file looked up when no path is given, and in
// every directory given as a path.
const DefaultFileName = "yahiko.yaml"

// Defaults applied when a suite does not set a value. They are documented in
// the Configuration page, which TestDocsDefaultsInSync keeps in step.
const (
	DefaultWarmup        = 1
	DefaultMinRuns       = 10
	DefaultMaxRuns       = 100
	DefaultMinTime       = 2 * time.Second
	DefaultTimeout       = time.Minute
	DefaultHookTimeout   = 5 * time.Minute
	DefaultBuildTimeout  = 10 * time.Minute
	DefaultMetric        = MetricMedian
	DefaultMaxPercent    = 10.0
	DefaultConfidence    = 0.95
	DefaultMinSamples    = 10
	DefaultMaxCV         = 0.5
	OutputDiscard        = "discard"
	MaxRunsLimit         = 100000
	MaxWarmupLimit       = 10000
	MinConfidence        = 0.5
	MaxConfidence        = 0.999
	MaxPercentLimit      = 1000.0
	MaxCVLimit           = 10.0
	MinSamplesLowerBound = 2
)

// Metric names a statistic used by budgets and regression checks.
type Metric string

// Metrics a budget or a regression check can use.
const (
	MetricMean   Metric = "mean"
	MetricMedian Metric = "median"
	MetricMin    Metric = "min"
	MetricMax    Metric = "max"
)

// Format names a report format.
type Format string

// Report formats.
const (
	FormatTable    Format = "table"
	FormatJSON     Format = "json"
	FormatCSV      Format = "csv"
	FormatMarkdown Format = "markdown"
	FormatGitHub   Format = "github"
)

// Formats lists every report format in documentation order.
func Formats() []Format {
	return []Format{FormatTable, FormatJSON, FormatCSV, FormatMarkdown, FormatGitHub}
}

// Suite is a fully resolved suite file.
type Suite struct {
	// Path is the absolute path of the suite file.
	Path string
	// Dir is the absolute directory holding the suite file.
	Dir         string
	Name        string
	Description string
	Build       *Exec
	Benchmarks  []Benchmark
	Outputs     []Output
}

// Exec is a process yahiko starts: a build step, a hook, or a measured command.
type Exec struct {
	// Argv is the argument list for a command run without a shell.
	Argv []string
	// Script is the command line for `shell: true`.
	Script  string
	Shell   bool
	Cwd     string
	Env     []EnvVar
	Timeout time.Duration
}

// Display returns the unexpanded command as written in the suite. Reports and
// logs show this form, so a value substituted from ${env:NAME} never appears.
func (e Exec) Display() string {
	if e.Shell {
		return e.Script
	}
	return joinArgv(e.Argv)
}

// EnvVar is one environment variable, kept in a stable (sorted) order.
type EnvVar struct {
	Name  string
	Value string
}

// Benchmark is one resolved benchmark case.
type Benchmark struct {
	Name        string
	Description string
	Tags        []string
	Warmup      int
	// Runs is the fixed number of measured runs, or 0 for adaptive runs.
	Runs        int
	MinRuns     int
	MaxRuns     int
	MinTime     time.Duration
	Stdin       Stdin
	Setup       []Exec
	PrepareEach []Exec
	Cleanup     []Exec
	Baseline    string
	Commands    []Command
	Budgets     []Budget
	Regression  Regression
}

// Command returns the command with the given name.
func (b Benchmark) Command(name string) (Command, bool) {
	for _, c := range b.Commands {
		if c.Name == name {
			return c, true
		}
	}
	return Command{}, false
}

// HasTag reports whether the benchmark carries tag.
func (b Benchmark) HasTag(tag string) bool {
	for _, t := range b.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// Stdin is the standard input of every run of a benchmark.
type Stdin struct {
	// File is a fixture path, reopened for every run.
	File string
	// Content is inline text.
	Content string
	// Kind is "", "file" or "content".
	Kind string
}

// Stdin kinds.
const (
	StdinNone    = ""
	StdinFile    = "file"
	StdinContent = "content"
)

// Command is one named command of a benchmark.
type Command struct {
	Name string
	Exec
	Stdout    string
	Stderr    string
	ExitCodes []int
}

// Budget is one absolute budget of one command.
type Budget struct {
	Command string
	Metric  Metric
	Expr    BudgetExpr
}

// Regression configures the comparison between a base revision and the
// working tree.
type Regression struct {
	Metric     Metric
	MaxPercent float64
	Confidence float64
	MinSamples int
	MaxCV      float64
	// Commands limits the comparison to these commands; all when empty.
	Commands []string
}

// Compares reports whether a command takes part in revision comparisons.
func (r Regression) Compares(name string) bool {
	if len(r.Commands) == 0 {
		return true
	}
	for _, c := range r.Commands {
		if c == name {
			return true
		}
	}
	return false
}

// Output is one report file written after a run.
type Output struct {
	Format Format
	Path   string
}
