package report

import (
	"slices"
	"strings"

	"github.com/nao1215/himorime/internal/metric"
	"github.com/nao1215/himorime/internal/runner"
)

// cellResult supports the Markdown table renderer.
type cellResult struct {
	label  string
	result Result
}

func benchmarkErrorCell(b Benchmark) cellResult {
	r := ResultError
	if b.Error != nil {
		r = errorResult(runner.FailureKind(b.Error.Kind))
	}
	return cellResult{verdictLabel(r), r}
}

// compactRow is the small, problem-focused view shared by the terminal and
// GitHub renderers. The report and JSON renderers remain lossless; this view
// deliberately chooses one representative statistic per metric group.
type compactRow struct {
	target  string
	metric  string
	value   string
	result  string
	verdict Result
	reason  string
	order   int
}

func compactRows(r *Report, s Suite) []compactRow {
	var rows []compactRow
	for _, b := range s.Benchmarks {
		target := b.Name
		if b.Error != nil {
			rows = append(rows, errorCompactRow(target, "benchmark", b.Error))
		}
		for _, c := range b.Commands {
			commandTarget := target + " / " + c.Name
			if c.Head != nil && c.Head.Error != nil {
				rows = append(rows, errorCompactRow(commandTarget+" (head)", "execution", c.Head.Error))
			}
			if c.Base != nil && c.Base.Error != nil {
				rows = append(rows, errorCompactRow(commandTarget+" (base)", "execution", c.Base.Error))
			}
			if c.Result == ResultError && (c.Head == nil || c.Head.Error == nil) && (c.Base == nil || c.Base.Error == nil) {
				rows = append(rows, compactRow{target: commandTarget, metric: "execution", value: "-", result: verdictLabel(c.Result), verdict: c.Result, order: compactOrder(c.Result)})
			}
			if suiteMode(r.Mode, s) == ModeCompare {
				rows = append(rows, compactComparisonRows(commandTarget, c)...)
			} else {
				rows = append(rows, compactRunRows(commandTarget, c)...)
			}
		}
	}
	if s.Error != nil {
		rows = append(rows, errorCompactRow(s.Name, "suite", s.Error))
	}
	slices.SortStableFunc(rows, func(a, b compactRow) int {
		if a.order != b.order {
			return b.order - a.order
		}
		return strings.Compare(a.target+"/"+a.metric, b.target+"/"+b.metric)
	})
	return rows
}

func errorCompactRow(target, metricName string, e *Error) compactRow {
	result := ResultError
	if e != nil {
		result = errorResult(runner.FailureKind(e.Kind))
	}
	reason := errorLine(e)
	if e != nil && e.Stderr != "" {
		reason += " (stderr: " + oneLine(lastLines(e.Stderr, 3)) + ")"
	}
	return compactRow{target: target, metric: metricName, value: "-", result: verdictLabel(result), verdict: result, reason: reason, order: compactOrder(result)}
}

func compactRunRows(target string, c Command) []compactRow {
	var rows []compactRow
	for _, g := range metric.Groups() {
		def, ms := representativeMetric(c.Head, g)
		if ms == nil || ms.Status == StatusNotRequested {
			continue
		}
		label, result := groupLabel(c, g)
		reason := ms.Reason
		if reason == "" {
			reason = budgetReason(c, def.Name)
		}
		value := statCell(c.Head, def.Name, medianOf)
		if def.Name == metric.Latency && c.Relative != nil {
			var relative *float64
			if c.Relative.VsBaseline != nil {
				relative = c.Relative.VsBaseline
			} else {
				relative = c.Relative.VsFastest
			}
			if relative != nil && *relative != 1 {
				reference := "fastest"
				if c.Relative.VsBaseline != nil {
					reference = "baseline"
				}
				value += " (" + FormatRatio(*relative) + " vs " + reference + ")"
			}
		}
		rows = append(rows, compactRow{target: target, metric: compactMetricTitle(def.Name, "median"), value: value, result: label, verdict: result, reason: reason, order: compactOrder(result)})
	}
	for _, bc := range c.Budgets {
		v := budgetView(c, bc)
		reason := bc.Reason
		if v.label == labelFail && reason == "" {
			reason = "budget not met"
		}
		value := v.actual + " (limit " + v.limit + ")"
		rows = append(rows, compactRow{target: target, metric: v.metric, value: value, result: v.label, verdict: v.result, reason: reason, order: compactOrder(v.result)})
	}
	return rows
}

func compactComparisonRows(target string, c Command) []compactRow {
	var rows []compactRow
	for _, def := range comparedMetricsForCommand(c) {
		mc := c.Comparisons[string(def.Name)]
		if mc == nil {
			continue
		}
		value := compactComparisonValue(c, mc, def)
		label, result := comparisonLabel(c, mc, def.Name)
		reason := mc.Reason
		statistic := mc.Statistic
		if statistic == "" {
			statistic = "median"
		}
		rows = append(rows, compactRow{target: target, metric: compactMetricTitle(def.Name, statistic), value: value, result: label, verdict: result, reason: reason, order: compactOrder(result)})
	}
	for _, bc := range c.Budgets {
		v := budgetView(c, bc)
		rows = append(rows, compactRow{target: target, metric: v.metric, value: v.actual + " (limit " + v.limit + ")", result: v.label, verdict: v.result, reason: bc.Reason, order: compactOrder(v.result)})
	}
	return rows
}

func comparedMetricsForCommand(c Command) []metric.Def {
	seen := map[string]bool{}
	for name := range c.Comparisons {
		seen[name] = true
	}
	var out []metric.Def
	for _, def := range metric.Defs() {
		if seen[string(def.Name)] {
			out = append(out, def)
		}
	}
	return out
}

func representativeMetric(m *Measurement, g metric.Group) (metric.Def, *MetricSummary) {
	defs := metric.InGroup(g)
	// CPU time is the one compact representative for the four CPU metrics.
	if g == metric.GroupCPU {
		defs = append([]metric.Def{metric.MustLookup(metric.CPUTotal)}, defs...)
	}
	for _, def := range defs {
		if ms := metricSummary(m, def.Name); ms != nil && ms.Status != StatusNotRequested {
			return def, ms
		}
	}
	return metric.Def{}, nil
}

func budgetReason(c Command, n metric.Name) string {
	for _, bc := range c.Budgets {
		if bc.Metric == string(n) && bc.Reason != "" {
			return bc.Reason
		}
	}
	return ""
}

func compactMetricTitle(n metric.Name, statistic string) string {
	if statistic == "" {
		return metricTitle(n)
	}
	return metricTitle(n) + " (" + statistic + ")"
}

func compactComparisonValue(c Command, mc *MetricComparison, def metric.Def) string {
	row := comparisonView(mc)
	bounded := false
	format := func(m *Measurement, value *float64, display string) string {
		if value == nil || display == "-" {
			return display
		}
		if ms := metricSummary(m, def.Name); ms != nil && ms.atFloor(*value) {
			bounded = true
			return floorCell(ms.Floor)
		}
		return display
	}
	value := format(c.Base, mc.Base, row.base) + " → " + format(c.Head, mc.Head, row.head)
	if !bounded && row.change != "-" {
		value += " (" + row.change + ")"
	}
	return value
}

func compactOrder(result Result) int {
	switch result {
	case ResultError, ResultMetricError, ResultRegression, ResultOverBudget:
		return 3
	case ResultInconclusive:
		return 2
	case ResultImproved:
		return 1
	case ResultPass:
		return 0
	default:
		return 0
	}
}
