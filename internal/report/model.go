// Package report turns raw measurements into judged results and renders them
// as a terminal table, JSON, CSV, Markdown or a GitHub Actions job summary.
//
// The Report type is the JSON report contract, described by
// schema/report.schema.json. Fields are only ever added within a schema
// version; renaming or removing one requires a new SchemaVersion.
package report

import "time"

// SchemaVersion is the version of the JSON report format.
const SchemaVersion = "1"

// Mode is what produced a report.
type Mode string

// Modes.
const (
	ModeRun     Mode = "run"
	ModeCompare Mode = "compare"
)

// Result classifies a command or a benchmark.
type Result string

// Results, ordered from best to worst by severity().
const (
	ResultPass         Result = "pass"
	ResultImproved     Result = "improved"
	ResultInconclusive Result = "inconclusive"
	ResultOverBudget   Result = "over_budget"
	ResultRegression   Result = "regression"
	ResultError        Result = "error"
)

// Results lists every result in severity order.
func Results() []Result {
	return []Result{ResultPass, ResultImproved, ResultInconclusive, ResultOverBudget, ResultRegression, ResultError}
}

func (r Result) severity() int {
	for i, x := range Results() {
		if x == r {
			return i
		}
	}
	return len(Results())
}

func worst(a, b Result) Result {
	if b.severity() > a.severity() {
		return b
	}
	return a
}

// Report is the complete result of one yahiko invocation.
type Report struct {
	SchemaVersion string      `json:"schema_version"`
	Mode          Mode        `json:"mode"`
	YahikoVersion string      `json:"yahiko_version"`
	Seed          uint64      `json:"seed"`
	StartedAt     time.Time   `json:"started_at"`
	FinishedAt    time.Time   `json:"finished_at"`
	Environment   Environment `json:"environment"`
	Git           *Git        `json:"git"`
	Suites        []Suite     `json:"suites"`
	Summary       Summary     `json:"summary"`
}

// Environment describes the machine. It never holds a host name or
// environment variables.
type Environment struct {
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	CPUModel    string `json:"cpu_model"`
	LogicalCPUs int    `json:"logical_cpus"`
	GoVersion   string `json:"go_version"`
	CI          string `json:"ci"`
}

// Git describes the revisions measured.
type Git struct {
	HeadSHA    string `json:"head_sha"`
	Dirty      bool   `json:"dirty"`
	BaseRef    string `json:"base_ref"`
	BaseSHA    string `json:"base_sha"`
	BaseSource string `json:"base_source"`
}

// Suite is one suite file.
type Suite struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	File        string      `json:"file"`
	Result      Result      `json:"result"`
	Error       *Error      `json:"error"`
	Benchmarks  []Benchmark `json:"benchmarks"`
	// GeometricMean is present only when every command completed every case.
	GeometricMean *GeometricMean `json:"geometric_mean"`
	// GeometricMeanUnavailable explains why GeometricMean is absent.
	GeometricMeanUnavailable string `json:"geometric_mean_unavailable"`
}

// Benchmark is one benchmark case.
type Benchmark struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Tags        []string  `json:"tags"`
	Baseline    string    `json:"baseline"`
	Rounds      int       `json:"rounds"`
	Result      Result    `json:"result"`
	Error       *Error    `json:"error"`
	Commands    []Command `json:"commands"`
}

// Command is one command of a benchmark.
type Command struct {
	Name string `json:"name"`
	// Command is the command as written in the suite, before variables were
	// substituted, so no secret passed through ${env:NAME} appears.
	Command    string        `json:"command"`
	Result     Result        `json:"result"`
	Head       *Measurement  `json:"head"`
	Base       *Measurement  `json:"base"`
	Relative   *Relative     `json:"relative"`
	Comparison *Comparison   `json:"comparison"`
	Budgets    []BudgetCheck `json:"budgets"`
}

// Measurement is the statistics of one command on one side. Durations are
// integer nanoseconds; SamplesNS holds every measured run in execution order.
type Measurement struct {
	Count     int     `json:"count"`
	Warmups   int     `json:"warmups"`
	MeanNS    int64   `json:"mean_ns"`
	MedianNS  int64   `json:"median_ns"`
	StddevNS  int64   `json:"stddev_ns"`
	MinNS     int64   `json:"min_ns"`
	MaxNS     int64   `json:"max_ns"`
	CV        float64 `json:"cv"`
	SamplesNS []int64 `json:"samples_ns"`
	Error     *Error  `json:"error"`
}

// Relative compares medians within one benchmark of a plain run.
type Relative struct {
	// VsBaseline is median / baseline median; null without a usable baseline.
	VsBaseline *float64 `json:"vs_baseline"`
	// VsFastest is median / fastest median of the benchmark's successful commands.
	VsFastest *float64 `json:"vs_fastest"`
}

// Comparison is the base/head judgement of one command.
type Comparison struct {
	Metric             string  `json:"metric"`
	ChangePercent      float64 `json:"change_percent"`
	CILowPercent       float64 `json:"ci_low_percent"`
	CIHighPercent      float64 `json:"ci_high_percent"`
	ProbRegression     float64 `json:"probability_regression"`
	ProbImprovement    float64 `json:"probability_improvement"`
	RequiredConfidence float64 `json:"required_confidence"`
	MaxPercent         float64 `json:"max_percent"`
	MinSamples         int     `json:"min_samples"`
	MaxCV              float64 `json:"max_cv"`
	Verdict            string  `json:"verdict"`
	Reason             string  `json:"reason"`
}

// BudgetCheck is one absolute budget.
type BudgetCheck struct {
	Metric   string `json:"metric"`
	Operator string `json:"operator"`
	LimitNS  int64  `json:"limit_ns"`
	ActualNS *int64 `json:"actual_ns"`
	Pass     bool   `json:"pass"`
}

// Error describes a failure.
type Error struct {
	Kind     string `json:"kind"`
	Message  string `json:"message"`
	ExitCode *int   `json:"exit_code"`
	Stderr   string `json:"stderr"`
}

// Summary counts results over every command of every suite.
type Summary struct {
	Suites             int  `json:"suites"`
	Benchmarks         int  `json:"benchmarks"`
	Commands           int  `json:"commands"`
	Pass               int  `json:"pass"`
	Improved           int  `json:"improved"`
	Inconclusive       int  `json:"inconclusive"`
	OverBudget         int  `json:"over_budget"`
	Regression         int  `json:"regression"`
	Error              int  `json:"error"`
	FailOnInconclusive bool `json:"fail_on_inconclusive"`
	ExitCode           int  `json:"exit_code"`
}

// GeometricMean is the supplementary cross-case summary.
type GeometricMean struct {
	// Reference is "baseline" (ratios to the common baseline command),
	// "fastest" (ratios to each case's fastest command) or "base" (head/base
	// ratios of a revision comparison).
	Reference string          `json:"reference"`
	Cases     int             `json:"cases"`
	Values    []GeometricItem `json:"values"`
}

// GeometricItem is the geometric mean ratio of one command.
type GeometricItem struct {
	Command string  `json:"command"`
	Ratio   float64 `json:"ratio"`
}
