package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/nao1215/himorime/internal/metric"
)

// WriteGitHubSummary writes complete, table-only Markdown for Actions logs and
// Job Summaries. JSON remains the lossless source of raw observations.
func WriteGitHubSummary(w io.Writer, r *Report) error {
	t := githubTables{rules: map[string]string{}, collections: map[string]string{}}
	for _, s := range r.Suites {
		t.addError(s.Name, "suite", s.Error)
		for _, b := range s.Benchmarks {
			label := b.Name
			if len(r.Suites) > 1 {
				label = s.Name + " / " + label
			}
			t.addError(label, "benchmark", b.Error)
			for _, c := range b.Commands {
				t.addCommand(label+" / "+c.Name, c)
			}
		}
	}
	var out strings.Builder
	githubTable(&out, []string{"Benchmark", "Stage", "Error", "Exit code", "Reason", "Stderr"}, t.errors)
	githubTable(&out, []string{"Benchmark", "Metric", "Base", "Head", "Difference", "Change", "Interval", "Confidence", "Tolerance", "Result", "Reason", "Rule", "P(regression)", "P(improvement)"}, t.comparisons)
	githubTable(&out, []string{"Benchmark", "Budget", "Measured", "Limit", "Result", "Reason"}, t.budgets)
	githubTable(&out, []string{"Benchmark", "Side", "Metric", "Median", "Mean", "Stddev", "Min", "Max", "P90", "P95", "P99", "CV", "Robust CV", "Samples", "Status", "Reason", "Collection"}, t.measurements)
	githubTable(&out, []string{"Rule", "Statistic", "Minimum difference", "Required confidence", "Minimum samples", "Max CV"}, t.ruleRows)
	githubTable(&out, []string{"Collection", "Metric", "Source", "Scope", "Process aggregation"}, t.collectionRows)
	githubTable(&out, []string{"Field", "Value"}, githubMetadata(r))
	_, err := io.WriteString(w, strings.TrimRight(out.String(), "\n")+"\n")
	return err
}

type githubTables struct {
	errors, comparisons, budgets, measurements, ruleRows, collectionRows [][]string
	rules, collections                                                   map[string]string
}

func (t *githubTables) addCommand(label string, c Command) {
	for _, d := range metric.Defs() {
		mc := c.Comparisons[string(d.Name)]
		if mc == nil {
			continue
		}
		v := comparisonView(mc)
		result, _ := comparisonLabel(c, mc, d.Name)
		base, head := v.base, v.head
		if mc.Base != nil && metricSummary(c.Base, d.Name).atFloor(*mc.Base) {
			base = floorCell(metricSummary(c.Base, d.Name).Floor)
		}
		if mc.Head != nil && metricSummary(c.Head, d.Name).atFloor(*mc.Head) {
			head = floorCell(metricSummary(c.Head, d.Name).Floor)
		}
		pRegression, pImprovement := "-", "-"
		if mc.ProbRegression != 0 || mc.ProbImprovement != 0 {
			pRegression, pImprovement = fmt.Sprintf("%.1f%%", mc.ProbRegression*100), fmt.Sprintf("%.1f%%", mc.ProbImprovement*100)
		}
		t.comparisons = append(t.comparisons, []string{label, metricTitle(d.Name), base, head, v.diff, v.change, v.interval, v.confidence, v.tolerance, result, mc.Reason, t.rule(mc), pRegression, pImprovement})
	}
	for _, bc := range c.Budgets {
		v := budgetView(c, bc)
		t.budgets = append(t.budgets, []string{label, v.metric, v.actual, v.limit, v.label, bc.Reason})
	}
	for _, side := range []struct {
		name string
		m    *Measurement
	}{{"base", c.Base}, {"head", c.Head}} {
		if side.m == nil {
			continue
		}
		t.addError(label, side.name, side.m.Error)
		for _, d := range metric.Defs() {
			ms := metricSummary(side.m, d.Name)
			if ms == nil || ms.Status == StatusNotRequested {
				continue
			}
			n, cv, robustCV, stddev := "-", "-", "-", "-"
			if ms.Stats != nil {
				n = fmt.Sprint(ms.Stats.Count)
				cv, robustCV = fmt.Sprintf("%.3f", ms.Stats.CV), fmt.Sprintf("%.3f", ms.Stats.RobustCV)
				stddev = metric.Format(d.Kind, ms.Stats.Stddev, workUnitFromUnit(ms.Unit))
			}
			t.measurements = append(t.measurements, []string{label, side.name, metricTitle(d.Name),
				statCell(side.m, d.Name, medianOf), statCell(side.m, d.Name, meanOf), stddev, statCell(side.m, d.Name, minOf), statCell(side.m, d.Name, maxOf), statCell(side.m, d.Name, percentileOf("p90")), statCell(side.m, d.Name, percentileOf("p95")), statCell(side.m, d.Name, percentileOf("p99")), cv, robustCV,
				n, ms.Status, ms.Reason, t.collection(d, ms)})
		}
	}
}

func (t *githubTables) rule(mc *MetricComparison) string {
	if mc.DerivedFrom != "" {
		return "from " + mc.DerivedFrom
	}
	d := metric.MustLookup(metric.Name(mc.Metric))
	row := []string{mc.Statistic, metric.Format(d.Kind, mc.MinDifference, workUnitFromUnit(mc.Unit)), fmt.Sprintf("%.1f%%", mc.RequiredConfidence*100), fmt.Sprint(mc.MinSamples), fmt.Sprint(mc.MaxCV)}
	return registerRow(t.rules, &t.ruleRows, "R", row)
}

func (t *githubTables) collection(d metric.Def, ms *MetricSummary) string {
	if ms.Source == "" && ms.ProcessAggregation == "" {
		return "-"
	}
	return registerRow(t.collections, &t.collectionRows, "C", []string{metricTitle(d.Name), ms.Source, ms.Scope, ms.ProcessAggregation})
}

func registerRow(ids map[string]string, rows *[][]string, prefix string, row []string) string {
	key := fmt.Sprintf("%q", row)
	if id, ok := ids[key]; ok {
		return id
	}
	id := fmt.Sprintf("%s%d", prefix, len(*rows)+1)
	ids[key] = id
	*rows = append(*rows, append([]string{id}, row...))
	return id
}

func (t *githubTables) addError(label, stage string, e *Error) {
	if e == nil {
		return
	}
	code := "-"
	if e.ExitCode != nil {
		code = fmt.Sprint(*e.ExitCode)
	}
	t.errors = append(t.errors, []string{label, stage, e.Kind, code, e.Message, e.Stderr})
}

func githubTable(out *strings.Builder, header []string, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(out, "| %s |\n|%s\n", strings.Join(header, " | "), strings.Repeat("---|", len(header)))
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, value := range row {
			if value == "" {
				value = "-"
			}
			cells[i] = EscapeMarkdown(value)
		}
		fmt.Fprintf(out, "| %s |\n", strings.Join(cells, " | "))
	}
	out.WriteString("\n")
}

func githubMetadata(r *Report) [][]string {
	e, s := r.Environment, r.Summary
	rows := [][]string{{"himorime", r.HimorimeVersion}, {"Mode", string(r.Mode)}, {"OS/architecture", e.OS + "/" + e.Arch}, {"CPU", fmt.Sprintf("%s (%d logical CPUs)", e.CPUModel, e.LogicalCPUs)}, {"Go", e.GoVersion}, {"CI", e.CI},
		{"Seed", fmt.Sprint(r.Seed)}, {"Exit code", fmt.Sprint(s.ExitCode)},
		{"Command results", fmt.Sprintf("%d passed; %d improved; %d inconclusive; %d over budget; %d regressed; %d metric errors", s.Pass, s.Improved, s.Inconclusive, s.OverBudget, s.Regression, s.MetricError)},
		{"Execution errors (commands, benchmarks and suites)", fmt.Sprint(s.Error)},
		{"Fail on inconclusive", fmt.Sprint(s.FailOnInconclusive)},
		{"Skipped comparisons and budgets", fmt.Sprint(s.Skipped)},
		{"Non-gating comparisons", fmt.Sprintf("%d passed; %d improved; %d inconclusive; %d regressed", s.NotGated.Pass, s.NotGated.Improved, s.NotGated.Inconclusive, s.NotGated.Regression)},
		{"New suites", fmt.Sprint(s.NewSuites)},
		{"Values", "Base/head use the rule's statistic; measurement columns use their named statistic."},
		{"Change and interval", "Recorded head-relative-to-base percentages; not a good/bad score."},
		{"Measurement floor", "≤ is an upper bound; floor-limited differences and intervals are not resolved changes."}}
	if r.Git != nil {
		g := r.Git
		rows = append(rows, []string{"Base", g.BaseSHA}, []string{"Head", g.HeadSHA}, []string{"Base ref", g.BaseRef}, []string{"Base source", g.BaseSource}, []string{"Working tree dirty", fmt.Sprint(g.Dirty)})
	}
	for _, tool := range e.Tools {
		rows = append(rows, []string{"Tool " + tool.Name, tool.Version})
	}
	for _, suite := range r.Suites {
		rows = append(rows, []string{"Suite", suite.Name})
		if suite.NewInHead {
			rows = append(rows, []string{"New suite", suite.Name})
		}
		if g := suite.GeometricMean; g != nil {
			for _, v := range g.Values {
				rows = append(rows, []string{"Geometric mean: " + suite.Name + " / " + v.Command, fmt.Sprintf("%s (%d cases; reference %s)", FormatRatio(v.Ratio), g.Cases, g.Reference)})
			}
		}
	}
	return rows
}
