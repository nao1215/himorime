package report

import (
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/nao1215/yahiko/internal/config"
	"github.com/nao1215/yahiko/internal/exitcode"
	"github.com/nao1215/yahiko/internal/runner"
	"github.com/nao1215/yahiko/internal/stats"
)

// SuiteInput is what the runner produced for one suite.
type SuiteInput struct {
	Suite *config.Suite
	// File is the suite path as shown to the user.
	File string
	// BuildFailure is set when a side's build failed; no benchmark ran.
	BuildFailure *runner.Failure
	Benchmarks   []runner.BenchmarkResult
}

// Options control judgement.
type Options struct {
	Mode               Mode
	Seed               uint64
	FailOnInconclusive bool
}

// Judge computes statistics, relative speeds, budgets and verdicts for every
// suite and fills r.Suites and r.Summary.
func Judge(r *Report, inputs []SuiteInput, o Options) {
	r.SchemaVersion = SchemaVersion
	r.Mode = o.Mode
	r.Seed = o.Seed
	r.Summary = Summary{FailOnInconclusive: o.FailOnInconclusive}
	r.Suites = []Suite{}
	for _, in := range inputs {
		s := judgeSuite(in, o)
		r.Suites = append(r.Suites, s)
	}
	summarize(r)
}

func judgeSuite(in SuiteInput, o Options) Suite {
	s := Suite{
		Name:        in.Suite.Name,
		Description: in.Suite.Description,
		File:        in.File,
		Result:      ResultPass,
		Benchmarks:  []Benchmark{},
	}
	if in.BuildFailure != nil {
		s.Error = toError(in.BuildFailure)
		s.Result = ResultError
	}
	for _, br := range in.Benchmarks {
		b := judgeBenchmark(br, o)
		s.Result = worst(s.Result, b.Result)
		s.Benchmarks = append(s.Benchmarks, b)
	}
	if o.Mode == ModeCompare {
		s.GeometricMean, s.GeometricMeanUnavailable = compareGeoMean(s)
	} else {
		s.GeometricMean, s.GeometricMeanUnavailable = runGeoMean(s)
	}
	if s.Error != nil {
		s.GeometricMean = nil
		s.GeometricMeanUnavailable = "the build failed"
	}
	return s
}

func judgeBenchmark(br runner.BenchmarkResult, o Options) Benchmark {
	cfg := br.Benchmark
	b := Benchmark{
		Name:        cfg.Name,
		Description: cfg.Description,
		Tags:        nonNil(cfg.Tags),
		Baseline:    cfg.Baseline,
		Rounds:      br.Rounds,
		Result:      ResultPass,
		Commands:    []Command{},
	}
	if br.Failure != nil {
		b.Error = toError(br.Failure)
		b.Result = ResultError
	}

	for _, cr := range br.Commands {
		if o.Mode == ModeCompare && cr.Sides[runner.SideBase] == nil {
			continue
		}
		c := Command{Name: cr.Command.Name, Command: cr.Command.Display(), Result: ResultPass, Budgets: []BudgetCheck{}}
		head := cr.Sides[runner.SideHead]
		c.Head = measurement(head)
		if o.Mode == ModeCompare {
			c.Base = measurement(cr.Sides[runner.SideBase])
		}
		b.Commands = append(b.Commands, c)
	}

	if o.Mode == ModeRun {
		relative(&b)
	}
	for i := range b.Commands {
		c := &b.Commands[i]
		// A benchmark that did not complete (a hook failed, the run was
		// interrupted) judges none of its commands: partial samples are kept
		// in the report but never presented as a pass.
		if b.Error != nil || hasError(c.Head) || (o.Mode == ModeCompare && hasError(c.Base)) {
			c.Result = ResultError
		}
		budgets(c, cfg)
		if o.Mode == ModeCompare && c.Result != ResultError {
			compare(c, br, cfg, o.Seed)
		}
		b.Result = worst(b.Result, c.Result)
	}
	return b
}

func hasError(m *Measurement) bool { return m == nil || m.Error != nil }

func measurement(m *runner.Measurement) *Measurement {
	if m == nil {
		return nil
	}
	sum := stats.Summarize(m.Samples)
	out := &Measurement{
		Count:     sum.Count,
		Warmups:   m.Warmups,
		MeanNS:    int64(sum.Mean),
		MedianNS:  int64(sum.Median),
		StddevNS:  int64(sum.Stddev),
		MinNS:     int64(sum.Min),
		MaxNS:     int64(sum.Max),
		CV:        sum.CV,
		SamplesNS: make([]int64, len(m.Samples)),
	}
	for i, s := range m.Samples {
		out.SamplesNS[i] = int64(s)
	}
	if m.Failure != nil {
		out.Error = toError(m.Failure)
	}
	return out
}

func toError(f *runner.Failure) *Error {
	e := &Error{Kind: string(f.Kind), Message: f.Message, Stderr: f.Stderr}
	if f.Kind == runner.FailExitCode || f.ExitCode != 0 {
		code := f.ExitCode
		e.ExitCode = &code
	}
	return e
}

func relative(b *Benchmark) {
	var fastest int64
	for _, c := range b.Commands {
		if hasError(c.Head) || c.Head.Count == 0 {
			continue
		}
		if fastest == 0 || c.Head.MedianNS < fastest {
			fastest = c.Head.MedianNS
		}
	}
	var baseline int64
	for _, c := range b.Commands {
		if c.Name == b.Baseline && !hasError(c.Head) && c.Head.Count > 0 {
			baseline = c.Head.MedianNS
		}
	}
	for i := range b.Commands {
		c := &b.Commands[i]
		if hasError(c.Head) || c.Head.Count == 0 {
			continue
		}
		rel := &Relative{}
		if fastest > 0 {
			v := float64(c.Head.MedianNS) / float64(fastest)
			rel.VsFastest = &v
		}
		if baseline > 0 {
			v := float64(c.Head.MedianNS) / float64(baseline)
			rel.VsBaseline = &v
		}
		c.Relative = rel
	}
}

func budgets(c *Command, cfg config.Benchmark) {
	for _, bud := range cfg.Budgets {
		if bud.Command != c.Name {
			continue
		}
		check := BudgetCheck{Metric: string(bud.Metric), Operator: bud.Expr.Operator(), LimitNS: int64(bud.Expr.Limit)}
		if c.Head != nil && c.Head.Count > 0 {
			actual := metricNS(c.Head, bud.Metric)
			check.ActualNS = &actual
			check.Pass = bud.Expr.Allows(time.Duration(actual))
		}
		if !check.Pass && c.Result != ResultError {
			c.Result = ResultOverBudget
		}
		c.Budgets = append(c.Budgets, check)
	}
}

func metricNS(m *Measurement, metric config.Metric) int64 {
	switch metric {
	case config.MetricMean:
		return m.MeanNS
	case config.MetricMin:
		return m.MinNS
	case config.MetricMax:
		return m.MaxNS
	case config.MetricMedian:
		return m.MedianNS
	}
	return m.MedianNS
}

func compare(c *Command, br runner.BenchmarkResult, cfg config.Benchmark, seed uint64) {
	var cr runner.CommandResult
	for _, x := range br.Commands {
		if x.Command.Name == c.Name {
			cr = x
		}
	}
	reg := cfg.Regression
	metric := stats.Median
	if reg.Metric == config.MetricMean {
		metric = stats.Mean
	}
	res := stats.Compare(cr.Sides[runner.SideBase].Samples, cr.Sides[runner.SideHead].Samples, stats.CompareOptions{
		Metric:     metric,
		MaxPercent: reg.MaxPercent,
		Confidence: reg.Confidence,
		MinSamples: reg.MinSamples,
		MaxCV:      reg.MaxCV,
		Seed:       comparisonSeed(br.Benchmark.Name, c.Name, seed),
	})
	c.Comparison = &Comparison{
		Metric:             string(reg.Metric),
		ChangePercent:      finite(res.Change),
		CILowPercent:       finite(res.Low),
		CIHighPercent:      finite(res.High),
		ProbRegression:     res.ProbRegression,
		ProbImprovement:    res.ProbImprovement,
		RequiredConfidence: reg.Confidence,
		MaxPercent:         reg.MaxPercent,
		MinSamples:         reg.MinSamples,
		MaxCV:              reg.MaxCV,
		Verdict:            string(res.Verdict),
		Reason:             res.Reason,
	}
	var verdict Result
	switch res.Verdict {
	case stats.VerdictRegression:
		verdict = ResultRegression
	case stats.VerdictImproved:
		verdict = ResultImproved
	case stats.VerdictInconclusive:
		verdict = ResultInconclusive
	case stats.VerdictPass:
		verdict = ResultPass
	}
	if c.Result == ResultOverBudget {
		c.Result = worst(ResultOverBudget, verdict)
		return
	}
	c.Result = verdict
}

// comparisonSeed derives a per-comparison bootstrap seed, so a comparison's
// result does not depend on which other benchmarks were selected.
func comparisonSeed(benchmark, command string, seed uint64) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(benchmark))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(command))
	return seed ^ h.Sum64()
}

func finite(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return f
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Reasons a geometric mean is absent because it would say nothing, as opposed
// to absent because data is missing. Only the latter is worth a note.
const (
	reasonTooFewCases    = "needs at least two cases"
	reasonTooFewCommands = "needs at least two commands per benchmark"
)

// GeometricMeanNote returns the explanation worth showing for a missing
// geometric mean, or "" when there is nothing to explain.
func GeometricMeanNote(s Suite) string {
	switch s.GeometricMeanUnavailable {
	case "", reasonTooFewCases, reasonTooFewCommands:
		return ""
	}
	return s.GeometricMeanUnavailable
}

func runGeoMean(s Suite) (*GeometricMean, string) {
	if len(s.Benchmarks) < 2 {
		return nil, reasonTooFewCases
	}
	names := commandSet(s.Benchmarks[0])
	if len(names) < 2 {
		return nil, reasonTooFewCommands
	}
	if reason := completeCases(s.Benchmarks, names); reason != "" {
		return nil, reason
	}
	reference := geoReference(s.Benchmarks)
	g := &GeometricMean{Reference: reference, Cases: len(s.Benchmarks)}
	for _, c := range s.Benchmarks[0].Commands {
		n := c.Name
		ratios, reason := commandRatios(s.Benchmarks, n, reference)
		if reason != "" {
			return nil, reason
		}
		gm, ok := stats.GeometricMean(ratios)
		if !ok {
			return nil, fmt.Sprintf("command %q has a ratio that is not positive", n)
		}
		g.Values = append(g.Values, GeometricItem{Command: n, Ratio: gm})
	}
	return g, ""
}

// completeCases explains why the benchmarks cannot be summarized together, or
// returns "" when every benchmark ran exactly the same commands to completion.
func completeCases(benchmarks []Benchmark, names []string) string {
	for _, b := range benchmarks {
		if b.Error != nil {
			return fmt.Sprintf("benchmark %q did not complete", b.Name)
		}
		set := commandSet(b)
		for _, n := range names {
			if !contains(set, n) {
				return fmt.Sprintf("command %q is missing from benchmark %q", n, b.Name)
			}
		}
		for _, n := range set {
			if !contains(names, n) {
				return fmt.Sprintf("command %q is missing from benchmark %q", n, benchmarks[0].Name)
			}
		}
		for _, c := range b.Commands {
			if c.Result == ResultError || c.Relative == nil {
				return fmt.Sprintf("command %q did not complete benchmark %q", c.Name, b.Name)
			}
		}
	}
	return ""
}

// geoReference is "baseline" when every benchmark names the same baseline,
// and "fastest" otherwise.
func geoReference(benchmarks []Benchmark) string {
	baseline := benchmarks[0].Baseline
	if baseline == "" {
		return "fastest"
	}
	for _, b := range benchmarks {
		if b.Baseline != baseline {
			return "fastest"
		}
	}
	return "baseline"
}

func commandRatios(benchmarks []Benchmark, name, reference string) ([]float64, string) {
	var ratios []float64
	for _, b := range benchmarks {
		for _, c := range b.Commands {
			if c.Name != name {
				continue
			}
			r := c.Relative.VsFastest
			if reference == "baseline" {
				r = c.Relative.VsBaseline
			}
			if r == nil {
				return nil, fmt.Sprintf("command %q has no ratio in benchmark %q", name, b.Name)
			}
			ratios = append(ratios, *r)
		}
	}
	return ratios, ""
}

func compareGeoMean(s Suite) (*GeometricMean, string) {
	var ratios []float64
	for _, b := range s.Benchmarks {
		if b.Error != nil {
			return nil, fmt.Sprintf("benchmark %q did not complete", b.Name)
		}
		for _, c := range b.Commands {
			if c.Result == ResultError || c.Head == nil || c.Base == nil || c.Head.Count == 0 || c.Base.Count == 0 {
				return nil, fmt.Sprintf("command %q did not complete benchmark %q on both revisions", c.Name, b.Name)
			}
			metric := config.MetricMedian
			if c.Comparison != nil && c.Comparison.Metric == string(config.MetricMean) {
				metric = config.MetricMean
			}
			base := metricNS(c.Base, metric)
			if base <= 0 {
				return nil, fmt.Sprintf("command %q measured zero time in benchmark %q", c.Name, b.Name)
			}
			ratios = append(ratios, float64(metricNS(c.Head, metric))/float64(base))
		}
	}
	if len(ratios) < 2 {
		return nil, reasonTooFewCases
	}
	gm, ok := stats.GeometricMean(ratios)
	if !ok {
		return nil, "a ratio is not positive"
	}
	return &GeometricMean{Reference: "base", Cases: len(ratios), Values: []GeometricItem{{Command: "head/base", Ratio: gm}}}, ""
}

func commandSet(b Benchmark) []string {
	out := make([]string, 0, len(b.Commands))
	for _, c := range b.Commands {
		out = append(out, c.Name)
	}
	sort.Strings(out)
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func summarize(r *Report) {
	sum := &r.Summary
	sum.Suites = len(r.Suites)
	for _, s := range r.Suites {
		if s.Error != nil {
			sum.Error++
		}
		for _, b := range s.Benchmarks {
			sum.Benchmarks++
			if b.Error != nil && len(b.Commands) == 0 {
				sum.Error++
			}
			for _, c := range b.Commands {
				sum.Commands++
				switch c.Result {
				case ResultPass:
					sum.Pass++
				case ResultImproved:
					sum.Improved++
				case ResultInconclusive:
					sum.Inconclusive++
				case ResultOverBudget:
					sum.OverBudget++
				case ResultRegression:
					sum.Regression++
				case ResultError:
					sum.Error++
				}
			}
			if b.Error != nil && len(b.Commands) > 0 && !anyError(b.Commands) {
				sum.Error++
			}
		}
	}
	switch {
	case sum.Error > 0:
		sum.ExitCode = exitcode.Execution
	case sum.Regression > 0 || sum.OverBudget > 0:
		sum.ExitCode = exitcode.Failed
	case sum.Inconclusive > 0 && sum.FailOnInconclusive:
		sum.ExitCode = exitcode.Failed
	default:
		sum.ExitCode = exitcode.OK
	}
}

func anyError(cs []Command) bool {
	for _, c := range cs {
		if c.Result == ResultError {
			return true
		}
	}
	return false
}

// verdictLabel is the upper-case form used in tables.
func verdictLabel(r Result) string {
	return strings.ToUpper(strings.ReplaceAll(string(r), "_", " "))
}
