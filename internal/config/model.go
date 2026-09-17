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

	"github.com/nao1215/yahiko/internal/metric"
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

// Metric names the statistic a regression check compares.
type Metric string

// Statistics a regression check can use.
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
	// FormatSamplesCSV is CSV with one row per measured value of every run.
	FormatSamplesCSV Format = "samples-csv"
)

// Formats lists every report format in documentation order.
func Formats() []Format {
	return []Format{FormatTable, FormatJSON, FormatCSV, FormatMarkdown, FormatGitHub, FormatSamplesCSV}
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
	Metrics     Metrics
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

// Unsupported policies decide what happens when a requested metric cannot be
// measured on this platform.
const (
	// UnsupportedFail stops before measuring: a budget or regression check
	// that silently vanished would read as a pass.
	UnsupportedFail = "fail"
	// UnsupportedSkip measures the rest and reports the metric as
	// unsupported, with its budgets and comparisons skipped.
	UnsupportedSkip = "skip"
)

// Metrics is what a benchmark measures. Latency is always measured.
type Metrics struct {
	CPU    bool
	Memory bool
	// Throughput is nil unless work is declared.
	Throughput *Work
	// Unsupported is UnsupportedFail or UnsupportedSkip.
	Unsupported string
}

// Collects reports whether the benchmark measures a group of metrics.
func (m Metrics) Collects(g metric.Group) bool {
	switch g {
	case metric.GroupLatency:
		return true
	case metric.GroupThroughput:
		return m.Throughput != nil
	case metric.GroupCPU:
		return m.CPU
	case metric.GroupMemory:
		return m.Memory
	}
	return false
}

// NeedsUsage reports whether runs must collect resource usage.
func (m Metrics) NeedsUsage() bool { return m.CPU || m.Memory }

// WorkUnit is the unit throughput is expressed in, or "".
func (m Metrics) WorkUnit() string {
	if m.Throughput == nil {
		return ""
	}
	return m.Throughput.Unit
}

// Work is the amount of work one run does, declared so that throughput can be
// computed as work divided by latency.
type Work struct {
	// Value is a fixed amount; 0 when FileSize is used.
	Value float64
	// FileSize is a path whose size in bytes is the work of each run. It is
	// read after prepare_each, outside the measured interval.
	FileSize string
	Unit     string
}

// Budget is one absolute budget of one command.
type Budget struct {
	Command     string
	Metric      metric.Name
	Aggregation metric.Aggregation
	Threshold   metric.Threshold
}

// Regression configures the comparison between a base revision and the
// working tree. Metric, MaxPercent and MinDifference apply to latency.
type Regression struct {
	Metric     Metric
	MaxPercent float64
	// MinDifference is the smallest latency difference, in nanoseconds, that
	// can be a regression; 0 disables it.
	MinDifference float64
	Confidence    float64
	MinSamples    int
	MaxCV         float64
	// Commands limits the comparison to these commands; all when empty.
	Commands   []string
	Throughput MetricRegression
	CPU        MetricRegression
	Memory     MetricRegression
}

// MetricRegression is the tolerance of one metric in a comparison.
type MetricRegression struct {
	Metric     Metric
	MaxPercent float64
	// MinDifference is in the metric's canonical unit; 0 disables it.
	MinDifference float64
	// unit and unitPath remember the work unit a throughput min_difference
	// was written in, so a benchmark can check it against its work.
	unit     string
	unitPath path
}

// For returns the tolerance of a comparable metric: latency, throughput,
// cpu_total or peak_rss.
func (r Regression) For(n metric.Name) (MetricRegression, bool) {
	switch n {
	case metric.Latency:
		return MetricRegression{Metric: r.Metric, MaxPercent: r.MaxPercent, MinDifference: r.MinDifference}, true
	case metric.Throughput:
		return r.Throughput, true
	case metric.CPUTotal:
		return r.CPU, true
	case metric.PeakRSS:
		return r.Memory, true
	case metric.CPUUser, metric.CPUSystem, metric.CPUUtilization:
	}
	return MetricRegression{}, false
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
