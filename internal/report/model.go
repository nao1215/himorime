// Package report turns raw measurements into judged results and renders them
// as a terminal table, JSON, CSV, Markdown or a GitHub Actions job summary.
//
// The Report type is the JSON report contract, described by
// schema/report.schema.json. Once released, fields are only ever added within
// a schema version; renaming or removing one requires a new SchemaVersion.
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
	// ResultMetricError: a requested metric could not be collected, so the
	// command's performance could not be judged.
	ResultMetricError Result = "metric_error"
	ResultError       Result = "error"
)

// Results lists every result in severity order.
func Results() []Result {
	return []Result{ResultPass, ResultImproved, ResultInconclusive, ResultOverBudget, ResultRegression, ResultMetricError, ResultError}
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

// Report is the complete result of one himorime invocation.
type Report struct {
	SchemaVersion   string      `json:"schema_version"`
	Mode            Mode        `json:"mode"`
	HimorimeVersion string      `json:"himorime_version"`
	Seed            uint64      `json:"seed"`
	StartedAt       time.Time   `json:"started_at"`
	FinishedAt      time.Time   `json:"finished_at"`
	Environment     Environment `json:"environment"`
	Git             *Git        `json:"git"`
	Suites          []Suite     `json:"suites"`
	Summary         Summary     `json:"summary"`
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
	// Tools holds the versions of the tools named in report.versions, in the
	// order the suites list them; empty when none are named.
	Tools []Tool `json:"tools"`
}

// Tool is the version of one tool, as its version command printed it.
type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
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
	Name        string `json:"name"`
	Description string `json:"description"`
	File        string `json:"file"`
	Result      Result `json:"result"`
	Error       *Error `json:"error"`
	// NewInHead is true in a comparison when the suite directory does not
	// exist in the base revision. Only the head side was built and run, as
	// in a plain run: its commands have no base and no comparisons, and its
	// budgets and failures decide the result.
	NewInHead  bool        `json:"new_in_head"`
	Benchmarks []Benchmark `json:"benchmarks"`
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
	Command  string       `json:"command"`
	Result   Result       `json:"result"`
	Head     *Measurement `json:"head"`
	Base     *Measurement `json:"base"`
	Relative *Relative    `json:"relative"`
	// Comparisons holds every compared metric, latency included, keyed by
	// metric name. It is null outside a revision comparison.
	Comparisons map[string]*MetricComparison `json:"comparisons"`
	Budgets     []BudgetCheck                `json:"budgets"`
}

// Measurement is one command on one side. Every metric, latency included, is
// described once, in its canonical unit, under Metrics.
type Measurement struct {
	// Count is the number of measured runs.
	Count   int `json:"count"`
	Warmups int `json:"warmups"`
	// Metrics is keyed by metric name and holds every metric himorime knows,
	// with a status saying whether it was measured.
	Metrics map[string]*MetricSummary `json:"metrics"`
	Error   *Error                    `json:"error"`
}

// Metric statuses. The strings are part of the JSON report contract.
const (
	StatusMeasured     = "measured"
	StatusNotRequested = "not_requested"
	StatusUnsupported  = "unsupported"
	StatusFailed       = "failed"
)

// MetricSummary is one metric of one measurement.
type MetricSummary struct {
	Name   string `json:"name"`
	Group  string `json:"group"`
	Unit   string `json:"unit"`
	Better string `json:"better"`
	// Scope is wall_clock or process_tree. Source and ProcessAggregation say
	// how a process_tree value was obtained on the measuring platform and how
	// the processes of the tree were combined; see Metrics in the docs.
	Scope              string `json:"scope"`
	Source             string `json:"source"`
	ProcessAggregation string `json:"process_aggregation"`
	// Status is measured, not_requested, unsupported or failed. Only a
	// measured metric has Stats and Samples; the others never carry zeros
	// that could be mistaken for a value.
	Status string `json:"status"`
	Reason string `json:"reason"`
	// Stats is null unless the metric was measured with at least one sample.
	Stats *MetricStats `json:"stats"`
	// Samples holds one value per measured run, in execution order.
	Samples []float64 `json:"samples"`
	// Work is the declared work of throughput; null for other metrics.
	Work *Work `json:"work"`
	// Floor, in bytes, is the largest peak RSS the process starting the
	// command already had before one of the runs. On Unix the kernel counts
	// that process's peak as part of the command's, so a peak RSS at or below
	// the floor only says the command used at most that much. 0 for other
	// metrics and where the platform has no floor.
	Floor int64 `json:"floor"`
	// SamplesAtFloor counts the runs whose peak RSS is at or below that run's
	// floor.
	SamplesAtFloor int `json:"samples_at_floor"`
}

// ReasonAtFloor explains a comparison or budget whose peak RSS statistic is
// at or below the floor: the command's true value is unknown.
const ReasonAtFloor = "the peak RSS is at or below the measurement floor"

// atFloor reports whether a statistic of the metric is at or below its floor.
func (m *MetricSummary) atFloor(v float64) bool {
	return m != nil && m.Floor > 0 && v <= float64(m.Floor)
}

// MetricStats summarizes the samples of one metric.
type MetricStats struct {
	Count  int     `json:"count"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Mean   float64 `json:"mean"`
	Median float64 `json:"median"`
	Stddev float64 `json:"stddev"`
	CV     float64 `json:"cv"`
	// RobustCV is the interquartile range divided by 1.349 and by the median.
	// It is the spread the noise gate compares with max_cv for the median and
	// the other order statistics; cv is used for the mean.
	RobustCV float64 `json:"robust_cv"`
	// Percentiles is keyed by pNN: p90, p95 and p99, plus any percentile a
	// budget of the command uses.
	Percentiles map[string]float64 `json:"percentiles"`
}

// Work is the work of one run that throughput divides by latency.
type Work struct {
	// Value is the declared amount; null when the work is a file's size.
	Value *float64 `json:"value"`
	// FileSize is the path whose size was used, as written in the suite.
	FileSize string `json:"file_size"`
	Unit     string `json:"unit"`
	// MeasuredMin and MeasuredMax, in Unit, are the smallest and largest work
	// of a measured run. A file_size read before every run can differ between
	// runs and between revisions, so throughput means nothing without them.
	// Both are null when no run was measured.
	MeasuredMin *float64 `json:"measured_min"`
	MeasuredMax *float64 `json:"measured_max"`
}

// sameWork reports whether two sides did the same work, so that their
// throughput describes the same job.
func (w *Work) sameWork(other *Work) bool {
	if w == nil || other == nil || w.MeasuredMin == nil || other.MeasuredMin == nil {
		return false
	}
	return w.MeasuredMax != nil && other.MeasuredMax != nil &&
		*w.MeasuredMin > 0 && *w.MeasuredMin == *w.MeasuredMax &&
		*w.MeasuredMin == *other.MeasuredMin && *other.MeasuredMin == *other.MeasuredMax
}

// Relative compares medians within one benchmark of a plain run.
type Relative struct {
	// VsBaseline is median / baseline median; null without a usable baseline.
	VsBaseline *float64 `json:"vs_baseline"`
	// VsFastest is median / fastest median of the benchmark's successful commands.
	VsFastest *float64 `json:"vs_fastest"`
}

// MetricComparison is the base/head judgement of one metric of one command.
type MetricComparison struct {
	Metric string `json:"metric"`
	// DerivedFrom names the independent comparison supplying this verdict.
	// Derived comparisons are informational and are not counted again.
	DerivedFrom string `json:"derived_from,omitempty"`
	// Statistic is the compared statistic: median or mean.
	Statistic string `json:"statistic"`
	Unit      string `json:"unit"`
	Better    string `json:"better"`
	// Base, Head and Difference (head - base) are in Unit; null when the
	// metric was not measured on both sides.
	Base       *float64 `json:"base"`
	Head       *float64 `json:"head"`
	Difference *float64 `json:"difference"`
	// ChangePercent is (head - base) / base * 100: its sign is the direction
	// the value moved, not whether that is better.
	ChangePercent   float64 `json:"change_percent"`
	CILowPercent    float64 `json:"ci_low_percent"`
	CIHighPercent   float64 `json:"ci_high_percent"`
	ProbRegression  float64 `json:"probability_regression"`
	ProbImprovement float64 `json:"probability_improvement"`
	// MaxPercent is the tolerated degradation: an increase for a
	// lower-is-better metric, a decrease for a higher-is-better one.
	MaxPercent         float64 `json:"max_percent"`
	MinDifference      float64 `json:"min_difference"`
	RequiredConfidence float64 `json:"required_confidence"`
	MinSamples         int     `json:"min_samples"`
	MaxCV              float64 `json:"max_cv"`
	// Verdict is pass, improved, regression, inconclusive or skipped.
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
	// Gate is true when Verdict decides the command's result and the exit
	// status. A comparison with Gate false is reported for information only:
	// its regression or inconclusive verdict never fails the run.
	Gate bool `json:"gate"`
}

// VerdictSkipped marks a comparison of a metric that was not measured.
const VerdictSkipped = "skipped"

// Budget statuses. The strings are part of the JSON report contract.
const (
	BudgetPass    = "pass"
	BudgetFail    = "fail"
	BudgetSkipped = "skipped"
	BudgetNoData  = "no_data"
)

// BudgetCheck is one absolute budget.
type BudgetCheck struct {
	Metric      string `json:"metric"`
	Aggregation string `json:"aggregation"`
	Operator    string `json:"operator"`
	// Limit and Actual are in Unit. Actual is null without a value.
	Limit  float64  `json:"limit"`
	Actual *float64 `json:"actual"`
	Unit   string   `json:"unit"`
	// Status is pass, fail, skipped (the metric was unsupported, or a peak
	// RSS at the floor could not be judged against the budget) or no_data
	// (the command produced no successful run).
	Status string `json:"status"`
	Pass   bool   `json:"pass"`
	Reason string `json:"reason"`
}

// Error describes a failure.
type Error struct {
	Kind     string `json:"kind"`
	Message  string `json:"message"`
	ExitCode *int   `json:"exit_code"`
	Stderr   string `json:"stderr"`
	// Metric is the metric group a metric_unsupported or
	// metric_collection_failed error is about; empty for other kinds.
	Metric string `json:"metric"`
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
	MetricError        int  `json:"metric_error"`
	Error              int  `json:"error"`
	FailOnInconclusive bool `json:"fail_on_inconclusive"`
	ExitCode           int  `json:"exit_code"`
	// NotGated counts independent metric comparisons with gate: false by
	// verdict. Derived throughput never counts the same inference again.
	NotGated VerdictCounts `json:"not_gated"`
	// Skipped counts the budgets and comparisons skipped because their
	// metric is unsupported on this platform (metrics.unsupported: skip), or
	// peak RSS could not be observed beyond the measurement floor.
	Skipped int `json:"skipped"`
	// NewSuites counts the suites with new_in_head: suites the base revision
	// does not have, so nothing of them was compared.
	NewSuites int `json:"new_suites"`
}

// VerdictCounts counts metric comparisons by verdict.
type VerdictCounts struct {
	Pass         int `json:"pass"`
	Improved     int `json:"improved"`
	Inconclusive int `json:"inconclusive"`
	Regression   int `json:"regression"`
}

// Total is the number of comparisons counted.
func (v VerdictCounts) Total() int { return v.Pass + v.Improved + v.Inconclusive + v.Regression }

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
