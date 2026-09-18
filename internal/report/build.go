package report

import (
	"fmt"
	"hash/fnv"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/nao1215/himorime/internal/config"
	"github.com/nao1215/himorime/internal/exitcode"
	"github.com/nao1215/himorime/internal/metric"
	"github.com/nao1215/himorime/internal/proc"
	"github.com/nao1215/himorime/internal/runner"
	"github.com/nao1215/himorime/internal/stats"
)

// SuiteInput is what the runner produced for one suite.
type SuiteInput struct {
	Suite *config.Suite
	// File is the suite path as shown to the user.
	File string
	// BuildFailure is set when a side's build failed; no benchmark ran.
	BuildFailure *runner.Failure
	// NewInHead is set in a comparison when the base revision has no suite
	// directory; only the head side was built and run.
	NewInHead  bool
	Benchmarks []runner.BenchmarkResult
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
	if r.Environment.Tools == nil {
		r.Environment.Tools = []Tool{}
	}
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
	if in.NewInHead {
		// A suite the base revision does not have was measured in the
		// working tree only, and is judged as a plain run: its budgets and
		// failures decide its result, and nothing is compared.
		s.NewInHead = true
		o.Mode = ModeRun
	}
	if in.BuildFailure != nil {
		s.Error = toError(in.BuildFailure)
		s.Result = errorResult(in.BuildFailure.Kind)
	}
	for _, br := range in.Benchmarks {
		b := judgeBenchmark(br, o)
		s.Result = worst(s.Result, b.Result)
		s.Benchmarks = append(s.Benchmarks, b)
	}
	switch {
	case s.NewInHead:
		s.GeometricMeanUnavailable = reasonNewInHead
	case o.Mode == ModeCompare:
		s.GeometricMean, s.GeometricMeanUnavailable = compareGeoMean(s)
	default:
		s.GeometricMean, s.GeometricMeanUnavailable = runGeoMean(s)
	}
	if s.Error != nil {
		s.GeometricMean = nil
		s.GeometricMeanUnavailable = "the build failed"
	}
	return s
}

// errorResult classifies a failure: metric failures are told apart from
// failures to run the command.
func errorResult(k runner.FailureKind) Result {
	if k.IsMetricFailure() {
		return ResultMetricError
	}
	return ResultError
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
		b.Result = errorResult(br.Failure.Kind)
	}

	for _, cr := range br.Commands {
		if o.Mode == ModeCompare && cr.Sides[runner.SideBase] == nil {
			continue
		}
		c := Command{Name: cr.Command.Name, Command: cr.Command.Display(), Result: ResultPass, Budgets: []BudgetCheck{}}
		percentiles := budgetPercentiles(cfg, c.Name)
		c.Head = measurement(cr.Sides[runner.SideHead], cfg, percentiles)
		if o.Mode == ModeCompare {
			c.Base = measurement(cr.Sides[runner.SideBase], cfg, percentiles)
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
		if r, failed := commandFailure(b, c, o.Mode); failed {
			c.Result = r
		}
		budgets(c, cfg)
		if o.Mode == ModeCompare && !c.Result.isError() {
			compareMetrics(c, br, cfg, o.Seed)
		}
		b.Result = worst(b.Result, c.Result)
	}
	return b
}

func (r Result) isError() bool { return r == ResultError || r == ResultMetricError }

// commandFailure returns the error result of a command whose benchmark or
// measurement failed.
func commandFailure(b Benchmark, c *Command, mode Mode) (Result, bool) {
	var errs []*Error
	if b.Error != nil {
		errs = append(errs, b.Error)
	}
	sides := []*Measurement{c.Head}
	if mode == ModeCompare {
		sides = append(sides, c.Base)
	}
	missing := false
	for _, m := range sides {
		switch {
		case m == nil:
			missing = true
		case m.Error != nil:
			errs = append(errs, m.Error)
		}
	}
	if len(errs) == 0 && !missing {
		return ResultPass, false
	}
	result := ResultMetricError
	if missing {
		result = ResultError
	}
	for _, e := range errs {
		result = worst(result, errorResult(runner.FailureKind(e.Kind)))
	}
	return result, true
}

func hasError(m *Measurement) bool { return m == nil || m.Error != nil }

func measurement(m *runner.Measurement, cfg config.Benchmark, percentiles []metric.Aggregation) *Measurement {
	if m == nil {
		return nil
	}
	out := &Measurement{
		Count:   len(m.Samples),
		Warmups: m.Warmups,
		Metrics: metricSummaries(m, cfg, percentiles),
	}
	if m.Failure != nil {
		out.Error = toError(m.Failure)
	}
	return out
}

// budgetPercentiles lists the percentiles reported for a command: p90, p95
// and p99, and every percentile one of its budgets uses.
func budgetPercentiles(cfg config.Benchmark, command string) []metric.Aggregation {
	out := slices.Clone(metric.DefaultPercentiles)
	for _, bud := range cfg.Budgets {
		if _, ok := bud.Aggregation.Percentile(); ok && bud.Command == command && !slices.Contains(out, bud.Aggregation) {
			out = append(out, bud.Aggregation)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Less(out[j]) })
	return out
}

// series derives the samples of every measured metric from the raw values of
// each run. Throughput and CPU utilization are computed per run, so their
// statistics describe runs, not a ratio of statistics.
func series(m *runner.Measurement, cfg config.Benchmark) map[metric.Name][]float64 {
	n := len(m.Samples)
	out := map[metric.Name][]float64{metric.Latency: stats.Durations(m.Samples)}
	seconds := func(i int) float64 {
		// A run is never shorter than a nanosecond; the floor keeps a
		// division by a clock that did not advance finite.
		return math.Max(float64(m.Samples[i]), 1) / float64(time.Second)
	}
	if cfg.Metrics.Throughput != nil && len(m.Work) == n {
		tp := make([]float64, n)
		for i := range tp {
			tp[i] = m.Work[i] / seconds(i)
		}
		out[metric.Throughput] = tp
	}
	if cfg.Metrics.CPU && len(m.CPUUser) == n && len(m.CPUSystem) == n {
		user, system, total, util := make([]float64, n), make([]float64, n), make([]float64, n), make([]float64, n)
		for i := range n {
			user[i] = float64(m.CPUUser[i])
			system[i] = float64(m.CPUSystem[i])
			total[i] = user[i] + system[i]
			util[i] = total[i] / math.Max(float64(m.Samples[i]), 1) * 100
		}
		out[metric.CPUUser], out[metric.CPUSystem], out[metric.CPUTotal], out[metric.CPUUtilization] = user, system, total, util
	}
	if cfg.Metrics.Memory && len(m.PeakRSS) == n {
		rss := make([]float64, n)
		for i, v := range m.PeakRSS {
			rss[i] = float64(v)
		}
		out[metric.PeakRSS] = rss
	}
	return out
}

// workRange returns the smallest and largest work of the measured runs. It
// reports false when no run was measured, so that a report never carries a
// zero that could be read as an amount of work.
func workRange(work []float64) (float64, float64, bool) {
	if len(work) == 0 {
		return 0, 0, false
	}
	lo, hi := work[0], work[0]
	for _, v := range work[1:] {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	return lo, hi, true
}

func metricSummaries(m *runner.Measurement, cfg config.Benchmark, percentiles []metric.Aggregation) map[string]*MetricSummary {
	values := series(m, cfg)
	out := map[string]*MetricSummary{}
	for _, def := range metric.Defs() {
		c := collection(def.Group)
		ms := &MetricSummary{
			Name:               string(def.Name),
			Group:              string(def.Group),
			Unit:               def.Unit(cfg.Metrics.WorkUnit()),
			Better:             string(def.Better),
			Scope:              string(def.Scope),
			Source:             c.Source,
			ProcessAggregation: c.ProcessAggregation,
			Samples:            []float64{},
		}
		out[string(def.Name)] = ms
		if def.Name == metric.Throughput && cfg.Metrics.Throughput != nil {
			w := cfg.Metrics.Throughput
			ms.Work = &Work{FileSize: w.FileSize, Unit: w.Unit}
			if w.FileSize == "" {
				v := w.Value
				ms.Work.Value = &v
			}
			if lo, hi, ok := workRange(m.Work); ok {
				ms.Work.MeasuredMin, ms.Work.MeasuredMax = &lo, &hi
			}
		}
		if !cfg.Metrics.Collects(def.Group) {
			ms.Status = StatusNotRequested
			continue
		}
		if reason, skipped := m.Skipped(def.Group); skipped {
			ms.Status, ms.Reason = StatusUnsupported, reason
			continue
		}
		if m.Failure != nil && m.Failure.Kind.IsMetricFailure() && m.Failure.Metric == def.Group {
			ms.Status, ms.Reason = StatusFailed, m.Failure.Message
			continue
		}
		samples, ok := values[def.Name]
		if !ok {
			ms.Status, ms.Reason = StatusFailed, "the runs did not record this metric"
			continue
		}
		ms.Status = StatusMeasured
		ms.Samples = samples
		ms.Stats = metricStats(samples, percentiles)
		if def.Name == metric.PeakRSS {
			ms.Floor, ms.SamplesAtFloor = floorOf(m)
		}
	}
	return out
}

// floorOf returns the largest peak RSS floor of a measurement's runs and the
// number of runs whose peak RSS is at or below its own floor.
func floorOf(m *runner.Measurement) (int64, int) {
	if len(m.PeakRSSFloor) != len(m.PeakRSS) {
		return 0, 0
	}
	var floor int64
	at := 0
	for i, f := range m.PeakRSSFloor {
		floor = max(floor, f)
		if f > 0 && m.PeakRSS[i] <= f {
			at++
		}
	}
	return floor, at
}

// Collection sources of the metrics himorime derives itself.
const (
	// SourceWallClock is the monotonic clock around the process, from just
	// before it starts until it is reaped.
	SourceWallClock = "wall_clock"
	// SourceDeclaredWork is the suite's declared work divided by latency.
	SourceDeclaredWork = "declared_work"
)

// collection describes how the metrics of a group are obtained on the
// platform himorime runs on.
func collection(g metric.Group) proc.Collection {
	switch g {
	case metric.GroupCPU:
		return proc.CPUCollection()
	case metric.GroupMemory:
		return proc.MemoryCollection()
	case metric.GroupThroughput:
		return proc.Collection{Source: SourceDeclaredWork, ProcessAggregation: proc.AggregationNone}
	case metric.GroupLatency:
	}
	return proc.Collection{Source: SourceWallClock, ProcessAggregation: proc.AggregationNone}
}

func metricStats(samples []float64, percentiles []metric.Aggregation) *MetricStats {
	if len(samples) == 0 {
		return nil
	}
	sum := stats.Summarize(samples)
	st := &MetricStats{
		Count: sum.Count, Min: sum.Min, Max: sum.Max, Mean: sum.Mean, Median: sum.Median,
		Stddev: sum.Stddev, CV: sum.CV, RobustCV: sum.RobustCV, Percentiles: map[string]float64{},
	}
	for _, p := range percentiles {
		if v, ok := stats.Aggregate(samples, p); ok {
			st.Percentiles[string(p)] = v
		}
	}
	return st
}

func toError(f *runner.Failure) *Error {
	e := &Error{Kind: string(f.Kind), Message: f.Message, Stderr: f.Stderr, Metric: string(f.Metric)}
	if f.Kind == runner.FailExitCode || f.ExitCode != 0 {
		code := f.ExitCode
		e.ExitCode = &code
	}
	return e
}

// latencyStats returns the latency statistics of a measurement that
// completed with at least one run, or nil.
func latencyStats(m *Measurement) *MetricStats {
	if hasError(m) || m.Count == 0 {
		return nil
	}
	ms := metricSummary(m, metric.Latency)
	if ms == nil || ms.Status != StatusMeasured {
		return nil
	}
	return ms.Stats
}

func relative(b *Benchmark) {
	var fastest float64
	for _, c := range b.Commands {
		if st := latencyStats(c.Head); st != nil && (fastest == 0 || st.Median < fastest) {
			fastest = st.Median
		}
	}
	var baseline float64
	for _, c := range b.Commands {
		if st := latencyStats(c.Head); st != nil && c.Name == b.Baseline {
			baseline = st.Median
		}
	}
	for i := range b.Commands {
		c := &b.Commands[i]
		st := latencyStats(c.Head)
		if st == nil {
			continue
		}
		rel := &Relative{}
		if fastest > 0 {
			v := st.Median / fastest
			rel.VsFastest = &v
		}
		if baseline > 0 {
			v := st.Median / baseline
			rel.VsBaseline = &v
		}
		c.Relative = rel
	}
}

// budgets checks every absolute budget of the command against the head.
func budgets(c *Command, cfg config.Benchmark) {
	for _, bud := range cfg.Budgets {
		if bud.Command != c.Name {
			continue
		}
		def := metric.MustLookup(bud.Metric)
		check := BudgetCheck{
			Metric:      string(bud.Metric),
			Aggregation: string(bud.Aggregation),
			Operator:    string(bud.Threshold.Op),
			Limit:       bud.Threshold.Limit,
			Unit:        def.Unit(cfg.Metrics.WorkUnit()),
			Status:      BudgetNoData,
		}
		var ms *MetricSummary
		if c.Head != nil {
			ms = c.Head.Metrics[string(bud.Metric)]
		}
		switch {
		case ms != nil && ms.Status == StatusUnsupported:
			check.Status, check.Reason = BudgetSkipped, ms.Reason
		case ms == nil || ms.Status != StatusMeasured || len(ms.Samples) == 0:
			check.Reason = "no successful run measured this metric"
			if ms != nil && ms.Reason != "" {
				check.Reason = ms.Reason
			}
		default:
			actual, _ := stats.Aggregate(ms.Samples, bud.Aggregation)
			check.Actual = &actual
			switch {
			case ms.atFloor(actual):
				// The true value is at most the floor. An upper bound the
				// floor meets is met; anything else cannot be decided.
				check.Status, check.Reason = BudgetSkipped, ReasonAtFloor
				if bud.Threshold.Op.Upper() && bud.Threshold.Allows(float64(ms.Floor)) {
					check.Status, check.Pass = BudgetPass, true
				}
			default:
				check.Pass = bud.Threshold.Allows(actual)
				check.Status = BudgetFail
				if check.Pass {
					check.Status = BudgetPass
				}
			}
		}
		if check.Status == BudgetFail && !c.Result.isError() {
			c.Result = worst(c.Result, ResultOverBudget)
		}
		c.Budgets = append(c.Budgets, check)
	}
}

// compareMetrics compares every comparable metric the benchmark measures.
// Only the verdicts of gated metrics decide the command's result; a metric
// with gate: false is compared and reported, but never changes the result.
func compareMetrics(c *Command, br runner.BenchmarkResult, cfg config.Benchmark, seed uint64) {
	result := ResultPass
	if c.Result == ResultOverBudget {
		result = ResultOverBudget
	}
	c.Comparisons = map[string]*MetricComparison{}
	for _, def := range metric.Defs() {
		if !def.Comparable || !cfg.Metrics.Collects(def.Group) {
			continue
		}
		mc := compareMetric(c, def, cfg, comparisonSeed(br.Benchmark.Name, c.Name, def.Name, seed))
		c.Comparisons[string(def.Name)] = mc
		if !mc.Gate {
			continue
		}
		switch stats.Verdict(mc.Verdict) {
		case stats.VerdictRegression:
			result = worst(result, ResultRegression)
		case stats.VerdictImproved:
			result = worst(result, ResultImproved)
		case stats.VerdictInconclusive:
			result = worst(result, ResultInconclusive)
		case stats.VerdictPass:
		}
	}
	c.Result = result
}

func compareMetric(c *Command, def metric.Def, cfg config.Benchmark, seed uint64) *MetricComparison {
	reg, _ := cfg.Regression.For(def.Name)
	mc := &MetricComparison{
		Metric:             string(def.Name),
		Statistic:          string(reg.Metric),
		Unit:               def.Unit(cfg.Metrics.WorkUnit()),
		Better:             string(def.Better),
		MaxPercent:         reg.MaxPercent,
		MinDifference:      reg.MinDifference,
		RequiredConfidence: cfg.Regression.Confidence,
		MinSamples:         cfg.Regression.MinSamples,
		MaxCV:              cfg.Regression.MaxCV,
		Gate:               reg.Gate,
	}
	base, head := c.Base.Metrics[string(def.Name)], c.Head.Metrics[string(def.Name)]
	for _, side := range []*MetricSummary{base, head} {
		if side.Status != StatusMeasured {
			mc.Verdict = VerdictSkipped
			mc.Reason = side.Status
			if side.Reason != "" {
				mc.Reason = side.Status + ": " + side.Reason
			}
			return mc
		}
	}
	statistic := stats.Median
	if reg.Metric == config.MetricMean {
		statistic = stats.Mean
	}
	baseAtFloor, headAtFloor := base.atFloor(statOf(base, statistic)), head.atFloor(statOf(head, statistic))
	res := stats.Compare(raiseToFloor(base, baseAtFloor), raiseToFloor(head, headAtFloor), stats.CompareOptions{
		Metric:         statistic,
		HigherIsBetter: def.Better == metric.HigherIsBetter,
		MaxPercent:     reg.MaxPercent,
		MinDifference:  reg.MinDifference,
		Confidence:     cfg.Regression.Confidence,
		MinSamples:     cfg.Regression.MinSamples,
		MaxCV:          cfg.Regression.MaxCV,
		Seed:           seed,
	})
	if res.Base.Count > 0 && res.Head.Count > 0 {
		b, h, d := res.BaseValue, res.HeadValue, res.Difference
		mc.Base, mc.Head, mc.Difference = &b, &h, &d
	}
	mc.ChangePercent = finite(res.Change)
	mc.CILowPercent = finite(res.Low)
	mc.CIHighPercent = finite(res.High)
	mc.ProbRegression = res.ProbRegression
	mc.ProbImprovement = res.ProbImprovement
	mc.Verdict = string(res.Verdict)
	mc.Reason = res.Reason
	if res.Base.Count > 0 && res.Head.Count > 0 && (baseAtFloor || headAtFloor) {
		// A side at its floor is compared as if it used the whole floor,
		// the most it can have used. That can only understate a change away
		// from that side, so a regression from a base at its floor, or an
		// improvement to a head at its floor, still holds. Every other
		// verdict could be wrong, as the real value may be anywhere below.
		regression := res.Verdict == stats.VerdictRegression && !headAtFloor
		improvement := res.Verdict == stats.VerdictImproved && !baseAtFloor
		if !regression && !improvement {
			mc.Verdict, mc.Reason = string(stats.VerdictInconclusive), ReasonAtFloor
		}
	}
	if def.Name == metric.Throughput && !base.Work.sameWork(head.Work) {
		// Throughput is work divided by latency, so a revision that declares
		// a different amount of work changes it without the command running
		// any faster or slower. Comparing the two would report the change of
		// input as a change of the program.
		mc.Verdict = string(stats.VerdictInconclusive)
		mc.Reason = reasonWorkDiffers(base.Work, head.Work)
	}
	return mc
}

// reasonWorkDiffers explains a throughput comparison whose sides did
// different amounts of work.
func reasonWorkDiffers(base, head *Work) string {
	return "the work differs between the revisions: " + workAmount(base) + " in the base, " + workAmount(head) + " in the head"
}

// workAmount renders the work of one side, as a range when it varied between
// that side's runs.
func workAmount(w *Work) string {
	unit := " " + w.Unit
	if *w.MeasuredMin == *w.MeasuredMax {
		return trimFloat(*w.MeasuredMin) + unit
	}
	return trimFloat(*w.MeasuredMin) + " to " + trimFloat(*w.MeasuredMax) + unit
}

// statOf is the compared statistic of a measured metric.
func statOf(ms *MetricSummary, statistic stats.Metric) float64 {
	if ms.Stats == nil {
		return 0
	}
	if statistic == stats.Mean {
		return ms.Stats.Mean
	}
	return ms.Stats.Median
}

// raiseToFloor returns the samples of a side, with every value below the
// floor raised to it when the side's statistic is at its floor.
func raiseToFloor(ms *MetricSummary, atFloor bool) []float64 {
	if !atFloor {
		return ms.Samples
	}
	out := make([]float64, len(ms.Samples))
	for i, v := range ms.Samples {
		out[i] = math.Max(v, float64(ms.Floor))
	}
	return out
}

// comparisonSeed derives a per-comparison bootstrap seed, so a comparison's
// result does not depend on which other benchmarks or metrics were selected.
func comparisonSeed(benchmark, command string, name metric.Name, seed uint64) uint64 {
	h := fnv.New64a()
	for i, part := range []string{benchmark, command, string(name)} {
		if i > 0 {
			_, _ = h.Write([]byte{0})
		}
		_, _ = h.Write([]byte(part))
	}
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
	reasonTooFewCases     = "needs at least two cases"
	reasonTooFewCommands  = "needs at least two commands per benchmark"
	reasonNoSharedCommand = "the benchmarks share no command"
	reasonNewInHead       = "the suite does not exist in the base revision"
)

// GeometricMeanNote returns the explanation worth showing for a missing
// geometric mean, or "" when there is nothing to explain.
func GeometricMeanNote(s Suite) string {
	switch s.GeometricMeanUnavailable {
	case "", reasonTooFewCases, reasonTooFewCommands, reasonNoSharedCommand, reasonNewInHead:
		return ""
	}
	return s.GeometricMeanUnavailable
}

func runGeoMean(s Suite) (*GeometricMean, string) {
	if len(s.Benchmarks) < 2 {
		return nil, reasonTooFewCases
	}
	// A geometric mean summarizes implementations compared across cases, which
	// share their command names. A suite whose benchmarks share none, such as
	// a regression suite with a start-up benchmark of one command, is not
	// such a comparison, whatever order its benchmarks are in.
	names, shared := commandNames(s.Benchmarks)
	if len(names) < 2 {
		return nil, reasonTooFewCommands
	}
	if !shared {
		return nil, reasonNoSharedCommand
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
		for _, c := range b.Commands {
			if c.Result.isError() || c.Relative == nil {
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
			base, head := latencyStats(c.Base), latencyStats(c.Head)
			if c.Result.isError() || base == nil || head == nil {
				return nil, fmt.Sprintf("command %q did not complete benchmark %q on both revisions", c.Name, b.Name)
			}
			pick := medianOf
			if mc := c.Comparisons[string(metric.Latency)]; mc != nil && mc.Statistic == string(config.MetricMean) {
				pick = meanOf
			}
			if pick(base) <= 0 {
				return nil, fmt.Sprintf("command %q measured zero time in benchmark %q", c.Name, b.Name)
			}
			ratios = append(ratios, pick(head)/pick(base))
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

// commandNames returns every command name of the benchmarks that completed,
// sorted, and whether at least one of them is in each of those benchmarks. A
// benchmark that failed before its commands ran has none, and is reported by
// completeCases instead.
func commandNames(benchmarks []Benchmark) ([]string, bool) {
	count := map[string]int{}
	completed := 0
	for _, b := range benchmarks {
		if b.Error != nil {
			continue
		}
		completed++
		for _, n := range commandSet(b) {
			count[n]++
		}
	}
	names := make([]string, 0, len(count))
	shared := false
	for n, c := range count {
		names = append(names, n)
		if c == completed {
			shared = true
		}
	}
	sort.Strings(names)
	return names, shared
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
	countChecks(r)
	count := func(res Result) {
		switch res {
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
		case ResultMetricError:
			sum.MetricError++
		case ResultError:
			sum.Error++
		}
	}
	for _, s := range r.Suites {
		if s.NewInHead {
			sum.NewSuites++
		}
		if s.Error != nil {
			count(errorResult(runner.FailureKind(s.Error.Kind)))
		}
		for _, b := range s.Benchmarks {
			sum.Benchmarks++
			if b.Error != nil && len(b.Commands) == 0 {
				count(errorResult(runner.FailureKind(b.Error.Kind)))
			}
			for _, c := range b.Commands {
				sum.Commands++
				count(c.Result)
			}
			if b.Error != nil && len(b.Commands) > 0 && !anyError(b.Commands) {
				count(errorResult(runner.FailureKind(b.Error.Kind)))
			}
		}
	}
	switch {
	case sum.Error > 0:
		sum.ExitCode = exitcode.Execution
	case sum.MetricError > 0:
		sum.ExitCode = exitcode.Metric
	case sum.Regression > 0 || sum.OverBudget > 0:
		sum.ExitCode = exitcode.Failed
	case sum.Inconclusive > 0 && sum.FailOnInconclusive:
		sum.ExitCode = exitcode.Failed
	default:
		sum.ExitCode = exitcode.OK
	}
}

// countChecks counts the comparisons that are not gated, by verdict, and the
// budgets and comparisons skipped because their metric is unsupported or a
// peak RSS at the floor cannot decide them.
func countChecks(r *Report) {
	sum := &r.Summary
	for _, s := range r.Suites {
		for _, b := range s.Benchmarks {
			for _, c := range b.Commands {
				for _, bc := range c.Budgets {
					if bc.Status == BudgetSkipped {
						sum.Skipped++
					}
				}
				for _, mc := range c.Comparisons {
					switch {
					case mc.Verdict == VerdictSkipped:
						sum.Skipped++
					case mc.Gate:
					case mc.Verdict == string(stats.VerdictRegression):
						sum.NotGated.Regression++
					case mc.Verdict == string(stats.VerdictImproved):
						sum.NotGated.Improved++
					case mc.Verdict == string(stats.VerdictInconclusive):
						sum.NotGated.Inconclusive++
					default:
						sum.NotGated.Pass++
					}
				}
			}
		}
	}
}

func anyError(cs []Command) bool {
	for _, c := range cs {
		if c.Result.isError() {
			return true
		}
	}
	return false
}

// verdictLabel is the upper-case form used in tables.
func verdictLabel(r Result) string {
	return strings.ToUpper(strings.ReplaceAll(string(r), "_", " "))
}
