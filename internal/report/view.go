package report

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/nao1215/himorime/internal/metric"
	"github.com/nao1215/himorime/internal/proc"
	"github.com/nao1215/himorime/internal/stats"
)

// The view helpers turn judged results into the text the terminal table,
// Markdown and the job summary show. Judgement happens in build.go on the
// stored values; nothing here decides a result, it only formats one.

// Labels shown in a RESULT column besides the Result values.
const (
	labelUnsupported = "UNSUPPORTED"
	labelSkipped     = "SKIPPED"
	labelNoData      = "NO DATA"
	labelFail        = "FAIL"
	// labelNotGated follows the verdict of a comparison with gate: false.
	labelNotGated = "(NOT GATED)"
)

// groupsShown returns the metric groups any command of the suite measured,
// or tried to, in report order. Latency is always first.
func groupsShown(s Suite) []metric.Group {
	shown := map[metric.Group]bool{metric.GroupLatency: true}
	for _, b := range s.Benchmarks {
		for _, c := range b.Commands {
			for _, m := range []*Measurement{c.Head, c.Base} {
				if m == nil {
					continue
				}
				for _, ms := range m.Metrics {
					if ms.Status != StatusNotRequested {
						shown[metric.Group(ms.Group)] = true
					}
				}
			}
		}
	}
	var out []metric.Group
	for _, g := range metric.Groups() {
		if shown[g] {
			out = append(out, g)
		}
	}
	return out
}

// hasNotGated reports whether any comparison of the suite has gate: false.
func hasNotGated(s Suite) bool {
	for _, b := range s.Benchmarks {
		for _, c := range b.Commands {
			for _, mc := range c.Comparisons {
				if !mc.Gate && mc.DerivedFrom == "" {
					return true
				}
			}
		}
	}
	return false
}

// checksNote summarizes the comparisons that did not decide the result: not
// gated ones with a verdict worth attention, and skipped checks. It returns
// "" when there are none.
func checksNote(s Summary) string {
	var parts []string
	if n := s.NotGated; n.Regression > 0 || n.Inconclusive > 0 {
		var p []string
		if n.Regression > 0 {
			p = append(p, fmt.Sprintf("%d regressed", n.Regression))
		}
		if n.Inconclusive > 0 {
			p = append(p, fmt.Sprintf("%d inconclusive", n.Inconclusive))
		}
		parts = append(parts, "not gated: "+strings.Join(p, ", "))
	}
	if s.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d checks skipped", s.Skipped))
	}
	return strings.Join(parts, " · ")
}

// newInHeadLine explains a suite the base revision does not have. The
// directory is taken from the suite path as the user gave it.
// suiteMode is the mode a suite is shown in: a suite new in this revision
// was measured and judged as a plain run, even in a comparison.
func suiteMode(mode Mode, s Suite) Mode {
	if s.NewInHead {
		return ModeRun
	}
	return mode
}

func newInHeadLine(s Suite) string {
	return fmt.Sprintf("new in this revision: %s does not exist in the base revision, so only this revision is measured and its budgets are checked", filepath.Dir(s.File))
}

// newSuitesNote counts the suites the base revision does not have, or
// returns "" when there are none.
func newSuitesNote(s Summary) string {
	switch s.NewSuites {
	case 0:
		return ""
	case 1:
		return "1 suite new in this revision"
	}
	return fmt.Sprintf("%d suites new in this revision", s.NewSuites)
}

func hasBudgets(s Suite) bool {
	for _, b := range s.Benchmarks {
		for _, c := range b.Commands {
			if len(c.Budgets) > 0 {
				return true
			}
		}
	}
	return false
}

// metricSummary returns a metric of a measurement, or nil.
func metricSummary(m *Measurement, n metric.Name) *MetricSummary {
	if m == nil || m.Metrics == nil {
		return nil
	}
	return m.Metrics[string(n)]
}

// groupLabel is the RESULT cell of one command in the table of one metric
// group: an error first, then UNSUPPORTED, then the worst of the group's
// budgets and comparisons.
func groupLabel(c Command, g metric.Group) (string, Result) {
	if c.Result.isError() {
		return verdictLabel(c.Result), c.Result
	}
	result := ResultPass
	unsupported := false
	for _, def := range metric.InGroup(g) {
		for _, m := range []*Measurement{c.Head, c.Base} {
			if ms := metricSummary(m, def.Name); ms != nil && ms.Status == StatusUnsupported {
				unsupported = true
			}
		}
		for _, bc := range c.Budgets {
			if bc.Metric == string(def.Name) && bc.Status == BudgetFail {
				result = worst(result, ResultOverBudget)
			}
		}
		if mc := c.Comparisons[string(def.Name)]; mc != nil {
			result = worst(result, verdictResult(mc.Verdict))
		}
	}
	if unsupported && result == ResultPass {
		return labelUnsupported, ResultInconclusive
	}
	return verdictLabel(result), result
}

func verdictResult(v string) Result {
	switch v {
	case string(ResultRegression):
		return ResultRegression
	case string(ResultImproved):
		return ResultImproved
	case string(ResultInconclusive):
		return ResultInconclusive
	}
	return ResultPass
}

// statCell formats one statistic of a metric, or "-" when it has none.
func statCell(m *Measurement, n metric.Name, pick func(*MetricStats) float64) string {
	ms := metricSummary(m, n)
	if ms == nil {
		return "-"
	}
	switch ms.Status {
	case StatusUnsupported:
		return "unsupported"
	case StatusFailed:
		return "failed"
	case StatusNotRequested:
		return "-"
	}
	if ms.Stats == nil {
		return "-"
	}
	def := metric.MustLookup(n)
	v := pick(ms.Stats)
	if ms.atFloor(v) {
		return floorCell(ms.Floor)
	}
	return metric.Format(def.Kind, v, workUnitOf(ms))
}

// floorCell shows a peak RSS at or below the floor as the most it can be. It
// uses ≤ rather than <=, which a budget column already uses for a threshold.
func floorCell(floor int64) string {
	return "≤ " + metric.Format(metric.KindBytes, float64(floor), "")
}

// FloorNote explains floorCell under a table that shows one.
const FloorNote = "≤ marks a peak RSS at or below what the process starting the command already used; the command used at most that much."

// memoryShowsFloor reports whether the memory table of a plain run shows a
// peak RSS as <= its floor.
func memoryShowsFloor(s Suite) bool {
	for _, b := range s.Benchmarks {
		for _, c := range b.Commands {
			ms := metricSummary(c.Head, metric.PeakRSS)
			if ms != nil && ms.Stats != nil && (ms.atFloor(ms.Stats.Median) || ms.atFloor(ms.Stats.Max)) {
				return true
			}
		}
	}
	return false
}

// budgetsShowFloor reports whether the budget table shows a measured value as
// <= its floor.
func budgetsShowFloor(s Suite) bool {
	for _, b := range s.Benchmarks {
		for _, c := range b.Commands {
			for _, bc := range c.Budgets {
				if bc.Actual != nil && metricSummary(c.Head, metric.Name(bc.Metric)).atFloor(*bc.Actual) {
					return true
				}
			}
		}
	}
	return false
}

func workUnitOf(ms *MetricSummary) string {
	if ms == nil || ms.Work == nil {
		return ""
	}
	return ms.Work.Unit
}

func medianOf(st *MetricStats) float64 { return st.Median }
func meanOf(st *MetricStats) float64   { return st.Mean }
func stddevOf(st *MetricStats) float64 { return st.Stddev }
func maxOf(st *MetricStats) float64    { return st.Max }
func minOf(st *MetricStats) float64    { return st.Min }

func percentileOf(p string) func(*MetricStats) float64 {
	return func(st *MetricStats) float64 {
		if v, ok := st.Percentiles[p]; ok {
			return v
		}
		return math.NaN()
	}
}

// budgetRow is one budget as table cells.
type budgetRow struct {
	metric, limit, actual, label string
	result                       Result
}

func budgetView(c Command, bc BudgetCheck) budgetRow {
	def := metric.MustLookup(metric.Name(bc.Metric))
	unit := workUnitFromUnit(bc.Unit)
	row := budgetRow{
		metric: def.Label + " " + bc.Aggregation,
		limit:  bc.Operator + " " + metric.Format(def.Kind, bc.Limit, unit),
		actual: "-",
	}
	if bc.Actual != nil {
		row.actual = metric.Format(def.Kind, *bc.Actual, unit)
		if ms := metricSummary(c.Head, def.Name); ms.atFloor(*bc.Actual) {
			row.actual = floorCell(ms.Floor)
		}
	}
	switch bc.Status {
	case BudgetPass:
		row.label, row.result = verdictLabel(ResultPass), ResultPass
	case BudgetFail:
		row.label, row.result = labelFail, ResultOverBudget
	case BudgetSkipped:
		row.label, row.result = labelSkipped, ResultInconclusive
	default:
		row.label, row.result = labelNoData, ResultError
		if c.Result.isError() {
			row.label, row.result = verdictLabel(c.Result), c.Result
		}
	}
	return row
}

// workUnitFromUnit recovers the work unit from a rate unit such as
// "records/s".
func workUnitFromUnit(unit string) string {
	return strings.TrimSuffix(unit, "/s")
}

// comparisonRow is one metric comparison as table cells.
type comparisonRow struct {
	base, head, diff, change, interval, confidence, tolerance string
}

func comparisonView(mc *MetricComparison) comparisonRow {
	row := comparisonRow{base: "-", head: "-", diff: "-", change: "-", interval: "-", confidence: "-", tolerance: "-"}
	if mc == nil {
		return row
	}
	def := metric.MustLookup(metric.Name(mc.Metric))
	unit := workUnitFromUnit(mc.Unit)
	row.tolerance = FormatMetricTolerance(mc)
	if mc.Verdict == VerdictSkipped {
		return row
	}
	if mc.Base != nil && mc.Head != nil && mc.Difference != nil {
		row.base = metric.Format(def.Kind, *mc.Base, unit)
		row.head = metric.Format(def.Kind, *mc.Head, unit)
		sign := "+"
		if *mc.Difference < 0 {
			sign = ""
		}
		row.diff = sign + metric.Format(def.Kind, *mc.Difference, unit)
		row.change = FormatChange(mc.ChangePercent)
		row.interval = fmt.Sprintf("%s … %s", FormatChange(mc.CILowPercent), FormatChange(mc.CIHighPercent))
	}
	row.confidence = formatMetricConfidence(mc)
	return row
}

// FormatMetricTolerance renders the tolerated degradation in the direction
// the metric degrades: +10% for latency, -8% for throughput.
func FormatMetricTolerance(mc *MetricComparison) string {
	if mc == nil {
		return "-"
	}
	sign := "+"
	if mc.Better == string(metric.HigherIsBetter) {
		sign = "-"
	}
	return sign + trimFloat(mc.MaxPercent) + "%"
}

// formatMetricConfidence renders the bootstrap probability that decided the
// verdict: that the change stays within the tolerance for a pass, that it
// exceeds it for a regression or an improvement, and the highest of these,
// below the required one, for a change too close to call. A verdict the
// probabilities did not decide, such as one below min_difference, too noisy
// or skipped, shows "-".
func formatMetricConfidence(mc *MetricComparison) string {
	var p float64
	switch {
	case mc.Verdict == string(stats.VerdictRegression):
		p = mc.ProbRegression
	case mc.Verdict == string(stats.VerdictImproved):
		p = mc.ProbImprovement
	case mc.Verdict == string(stats.VerdictPass) && mc.Reason == "":
		p = 1 - mc.ProbRegression
	case mc.Verdict == string(stats.VerdictInconclusive) && mc.Reason == stats.ReasonTooClose:
		p = max(mc.ProbRegression, mc.ProbImprovement, 1-mc.ProbRegression)
	default:
		return "-"
	}
	return fmt.Sprintf("%.1f%%", p*100)
}

// comparedMetrics lists the metrics any command of the suite compared, in
// report order.
func comparedMetrics(s Suite) []metric.Def {
	seen := map[string]bool{}
	for _, b := range s.Benchmarks {
		for _, c := range b.Commands {
			for name := range c.Comparisons {
				seen[name] = true
			}
		}
	}
	var out []metric.Def
	for _, def := range metric.Defs() {
		if seen[string(def.Name)] || def.Name == metric.Latency {
			out = append(out, def)
		}
	}
	return out
}

// comparisonLabel is the RESULT cell of one metric comparison. A comparison
// that is not gated says so next to its verdict, so a regression that did
// not fail the run is never shown as a plain PASS or a plain REGRESSION.
func comparisonLabel(c Command, mc *MetricComparison, name metric.Name) (string, Result) {
	if c.Result.isError() {
		return verdictLabel(c.Result), c.Result
	}
	if mc == nil {
		return "-", ResultPass
	}
	if mc.Verdict == VerdictSkipped {
		return labelSkipped, ResultInconclusive
	}
	result := verdictResult(mc.Verdict)
	overBudget := false
	for _, bc := range c.Budgets {
		if bc.Metric == string(name) && bc.Status == BudgetFail {
			overBudget = true
		}
	}
	if !mc.Gate {
		label := verdictLabel(result) + " " + labelNotGated
		if mc.DerivedFrom != "" {
			label = verdictLabel(result) + " (FROM LATENCY)"
		}
		if overBudget {
			return verdictLabel(ResultOverBudget) + ", " + label, ResultOverBudget
		}
		// A change that did not fail the run is colored as a warning at most.
		if result == ResultRegression {
			result = ResultInconclusive
		}
		return label, result
	}
	if overBudget {
		result = worst(result, ResultOverBudget)
	}
	return verdictLabel(result), result
}

// commandNotes lists the failures, missed or skipped budgets, unmeasured
// metrics and inconclusive reasons of one command, one line each.
func commandNotes(mode Mode, b Benchmark, c Command) []string {
	var notes []string
	label := fmt.Sprintf("%s / %s", b.Name, c.Name)
	for _, side := range []sideMeasurement{{"base", c.Base}, {"head", c.Head}} {
		if side.m == nil {
			continue
		}
		l := label
		if mode == ModeCompare {
			l += " (" + side.name + ")"
		}
		if side.m.Error != nil {
			notes = append(notes, l+": "+errorLine(side.m.Error))
			if side.m.Error.Stderr != "" {
				notes = append(notes, "  stderr: "+oneLine(lastLines(side.m.Error.Stderr, 3)))
			}
		}
		for _, g := range metric.Groups() {
			if reason := unsupportedReason(side.m, g); reason != "" {
				notes = append(notes, fmt.Sprintf("%s: %s not measured: %s", l, g, reason))
			}
		}
	}
	notes = append(notes, budgetNotes(label, c)...)
	for _, def := range metric.Defs() {
		mc := c.Comparisons[string(def.Name)]
		if mc == nil {
			continue
		}
		// Latency, the metric every benchmark compares, is not named.
		name := def.Label + " "
		if def.Name == metric.Latency {
			name = ""
		}
		if !mc.Gate {
			name = def.Label + " (not gated) "
		}
		switch {
		case mc.Verdict == VerdictSkipped && mc.Reason != "":
			notes = append(notes, fmt.Sprintf("%s: %sskipped: %s", label, name, mc.Reason))
		case mc.DerivedFrom != "":
		case mc.Verdict == string(ResultInconclusive) && mc.Reason != "":
			notes = append(notes, fmt.Sprintf("%s: %sinconclusive: %s", label, name, mc.Reason))
		case mc.Verdict == string(ResultRegression) && !mc.Gate:
			notes = append(notes, fmt.Sprintf("%s: %s regressed beyond its tolerance, but gate: false keeps it from failing the run", label, def.Label))
		}
	}
	return notes
}

// budgetNotes explains the budgets of a command that failed or were skipped.
func budgetNotes(label string, c Command) []string {
	var notes []string
	for _, bc := range c.Budgets {
		switch bc.Status {
		case BudgetFail:
			row := budgetView(c, bc)
			notes = append(notes, fmt.Sprintf("%s: budget %s %s not met (measured %s)", label, budgetName(bc), row.limit, row.actual))
		case BudgetSkipped:
			reason := "the metric is unsupported here"
			if bc.Reason == ReasonAtFloor {
				reason = bc.Reason
			}
			notes = append(notes, fmt.Sprintf("%s: budget %s skipped: %s", label, budgetName(bc), reason))
		}
	}
	return notes
}

// budgetName names a budget in a note: "median" for the latency budgets
// suites have always had, "peak rss max" for the others.
func budgetName(bc BudgetCheck) string {
	if bc.Metric == string(metric.Latency) {
		return bc.Aggregation
	}
	return metric.MustLookup(metric.Name(bc.Metric)).Label + " " + bc.Aggregation
}

// unsupportedReason returns the reason a group was not measured on a side,
// once per group.
func unsupportedReason(m *Measurement, g metric.Group) string {
	for _, def := range metric.InGroup(g) {
		if ms := metricSummary(m, def.Name); ms != nil && ms.Status == StatusUnsupported {
			return ms.Reason
		}
	}
	return ""
}

// cpuFootnote explains utilization above 100%.
const cpuFootnote = "CPU values are medians over runs of the process tree. Utilization is CPU time divided by wall-clock time; above 100% means more than one CPU was busy."

// processNote describes which processes a CPU or memory value of the suite
// covers and how they were combined, from the collection the report records.
// It returns "" when no command measured the group.
func processNote(s Suite, g metric.Group) string {
	var ms *MetricSummary
	for _, b := range s.Benchmarks {
		for _, c := range b.Commands {
			for _, m := range []*Measurement{c.Head, c.Base} {
				for _, def := range metric.InGroup(g) {
					if x := metricSummary(m, def.Name); x != nil && x.Status == StatusMeasured && ms == nil {
						ms = x
					}
				}
			}
		}
	}
	if ms == nil {
		return ""
	}
	return aggregationText(ms.ProcessAggregation)
}

// aggregationText explains a process aggregation in one sentence.
func aggregationText(aggregation string) string {
	switch aggregation {
	case proc.AggregationSumWaited:
		return "Process tree: the command plus every descendant its parent waited for (rusage); a descendant left running or reaped by init is not counted."
	case proc.AggregationSumJob:
		return "Process tree: every process of the command's Job Object, whether or not it was waited for."
	case proc.AggregationMaxProcess:
		return "Peak RSS is the largest peak of any single process of the tree (rusage ru_maxrss), not the combined memory of processes running at the same time."
	case proc.AggregationStartedProcess:
		return "Peak RSS is the peak working set of the started process; a run that started other processes reports it as unsupported."
	}
	return ""
}
