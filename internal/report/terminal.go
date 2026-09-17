package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/nao1215/himorime/internal/metric"
	"github.com/nao1215/himorime/internal/runner"
)

// TerminalOptions control the terminal table.
type TerminalOptions struct {
	Color bool
}

const (
	ansiReset  = "\x1b[0m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiBold   = "\x1b[1m"
)

func colorLabel(label string, r Result) string {
	switch r {
	case ResultPass:
		return ansiGreen + label + ansiReset
	case ResultImproved:
		return ansiGreen + ansiBold + label + ansiReset
	case ResultInconclusive:
		return ansiYellow + label + ansiReset
	case ResultOverBudget, ResultRegression, ResultMetricError, ResultError:
		return ansiRed + ansiBold + label + ansiReset
	}
	return label
}

// WriteTerminal renders the human-readable table report. Colors are applied
// after alignment so escape codes never skew the columns.
func WriteTerminal(w io.Writer, r *Report, o TerminalOptions) error {
	var sb strings.Builder
	for i, s := range r.Suites {
		if i > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "suite: %s (%s)\n", cellText(s.Name), cellText(s.File))
		if s.Error != nil {
			fmt.Fprintf(&sb, "error: %s\n", errorLine(s.Error))
			writeStderr(&sb, s.Error)
			continue
		}
		if r.Mode == ModeCompare {
			compareTable(&sb, s, o)
		} else {
			runTable(&sb, s, o)
		}
		details(&sb, r.Mode, s)
	}
	sb.WriteString("\n")
	summaryLine(&sb, r)
	_, err := io.WriteString(w, sb.String())
	return err
}

func runTable(sb *strings.Builder, s Suite, o TerminalOptions) {
	groups := groupsShown(s)
	titled := len(groups) > 1 || hasBudgets(s)
	for _, g := range groups {
		switch g {
		case metric.GroupLatency:
			if titled {
				sb.WriteString("latency\n")
			}
			latencyRunTable(sb, s, o)
		case metric.GroupThroughput:
			sb.WriteString("\nthroughput\n")
			groupRunTable(sb, s, o, g, []string{"MEDIAN", "MEAN", "MIN"}, func(m *Measurement) []string {
				return []string{statCell(m, metric.Throughput, medianOf), statCell(m, metric.Throughput, meanOf), statCell(m, metric.Throughput, minOf)}
			})
			sb.WriteString("THROUGHPUT is the declared work divided by the latency of each run; MIN is the slowest run.\n")
		case metric.GroupCPU:
			sb.WriteString("\ncpu\n")
			groupRunTable(sb, s, o, g, []string{"USER", "SYSTEM", "TOTAL", "UTILIZATION"}, func(m *Measurement) []string {
				return []string{statCell(m, metric.CPUUser, medianOf), statCell(m, metric.CPUSystem, medianOf), statCell(m, metric.CPUTotal, medianOf), statCell(m, metric.CPUUtilization, medianOf)}
			})
			sb.WriteString(cpuFootnote + "\n")
			if note := processNote(s, g); note != "" {
				sb.WriteString(note + "\n")
			}
		case metric.GroupMemory:
			sb.WriteString("\nmemory\n")
			groupRunTable(sb, s, o, g, []string{"PEAK RSS", "MAX"}, func(m *Measurement) []string {
				return []string{statCell(m, metric.PeakRSS, medianOf), statCell(m, metric.PeakRSS, maxOf)}
			})
			sb.WriteString("PEAK RSS is the median over runs; MAX is the highest run.\n")
			if note := processNote(s, g); note != "" {
				sb.WriteString(note + "\n")
			}
		}
	}
	if hasBudgets(s) {
		sb.WriteString("\nbudgets\n")
		budgetTable(sb, s, o)
	}
}

func latencyRunTable(sb *strings.Builder, s Suite, o TerminalOptions) {
	t := &table{
		header: []string{"BENCHMARK", "COMMAND", "MEDIAN", "MEAN", "STDDEV", "RELATIVE", "RESULT"},
		right:  []bool{false, false, true, true, true, true, false},
	}
	var results []cellResult
	for _, b := range s.Benchmarks {
		if len(b.Commands) == 0 {
			t.add(b.Name, "-", "-", "-", "-", "-", "")
			results = append(results, benchmarkErrorCell(b))
			continue
		}
		for _, c := range b.Commands {
			median, mean, stddev, rel := "-", "-", "-", "-"
			if c.Head != nil && c.Head.Count > 0 {
				median = statCell(c.Head, metric.Latency, medianOf)
				mean = statCell(c.Head, metric.Latency, meanOf)
				stddev = statCell(c.Head, metric.Latency, stddevOf)
			}
			if c.Relative != nil {
				switch {
				case c.Relative.VsBaseline != nil:
					rel = FormatRatio(*c.Relative.VsBaseline)
				case c.Relative.VsFastest != nil:
					rel = FormatRatio(*c.Relative.VsFastest)
				}
			}
			t.add(b.Name, c.Name, median, mean, stddev, rel, "")
			label, r := groupLabel(c, metric.GroupLatency)
			results = append(results, cellResult{label, r})
		}
	}
	renderColored(sb, t, results, o.Color)
	sb.WriteString("RELATIVE is the median divided by the baseline command's median, or by the fastest command's when no baseline is set.\n")
}

// groupRunTable renders one metric group of a plain run: benchmark and
// command, the given value columns, and the group's result.
func groupRunTable(sb *strings.Builder, s Suite, o TerminalOptions, g metric.Group, headers []string, cells func(*Measurement) []string) {
	t := &table{header: append(append([]string{"BENCHMARK", "COMMAND"}, headers...), "RESULT")}
	t.right = make([]bool, len(t.header))
	for i := range headers {
		t.right[i+2] = true
	}
	var results []cellResult
	for _, b := range s.Benchmarks {
		for _, c := range b.Commands {
			row := append(append([]string{b.Name, c.Name}, cells(c.Head)...), "")
			t.add(row...)
			label, r := groupLabel(c, g)
			results = append(results, cellResult{label, r})
		}
	}
	if len(t.rows) == 0 {
		return
	}
	renderColored(sb, t, results, o.Color)
}

func budgetTable(sb *strings.Builder, s Suite, o TerminalOptions) {
	t := &table{
		header: []string{"BENCHMARK", "COMMAND", "METRIC", "BUDGET", "MEASURED", "RESULT"},
		right:  []bool{false, false, false, true, true, false},
	}
	var results []cellResult
	for _, b := range s.Benchmarks {
		for _, c := range b.Commands {
			for _, bc := range c.Budgets {
				row := budgetView(c, bc)
				t.add(b.Name, c.Name, row.metric, row.limit, row.actual, "")
				results = append(results, cellResult{row.label, row.result})
			}
		}
	}
	renderColored(sb, t, results, o.Color)
}

func compareTable(sb *strings.Builder, s Suite, o TerminalOptions) {
	defs := comparedMetrics(s)
	for i, def := range defs {
		if len(defs) > 1 {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(def.Label + "\n")
		}
		compareMetricTable(sb, s, o, def)
	}
	sb.WriteString("BASE and HEAD show the compared statistic; CONFIDENCE is the bootstrap probability that the change exceeds TOLERANCE in its direction.\n")
	if hasNotGated(s) {
		sb.WriteString("(NOT GATED) marks a metric with gate: false: it is compared and reported, but never fails the run.\n")
	}
	for _, g := range []metric.Group{metric.GroupCPU, metric.GroupMemory} {
		if note := processNote(s, g); note != "" {
			sb.WriteString(note + "\n")
		}
	}
	if hasBudgets(s) {
		sb.WriteString("\nbudgets (head)\n")
		budgetTable(sb, s, o)
	}
}

func compareMetricTable(sb *strings.Builder, s Suite, o TerminalOptions, def metric.Def) {
	t := &table{
		header: []string{"BENCHMARK", "BASE", "HEAD", "DIFF", "CHANGE", "CONFIDENCE", "TOLERANCE", "RESULT"},
		right:  []bool{false, true, true, true, true, true, true, false},
	}
	var results []cellResult
	for _, b := range s.Benchmarks {
		if len(b.Commands) == 0 {
			if def.Name == metric.Latency {
				t.add(b.Name, "-", "-", "-", "-", "-", "-", "")
				results = append(results, benchmarkErrorCell(b))
			}
			continue
		}
		for _, c := range b.Commands {
			mc := c.Comparisons[string(def.Name)]
			if mc == nil && def.Name != metric.Latency {
				continue
			}
			name := b.Name
			if len(b.Commands) > 1 {
				name = fmt.Sprintf("%s (%s)", b.Name, c.Name)
			}
			row := comparisonView(mc)
			t.add(name, row.base, row.head, row.diff, row.change, row.confidence, row.tolerance, "")
			label, r := comparisonLabel(c, mc, def.Name)
			results = append(results, cellResult{label, r})
		}
	}
	renderColored(sb, t, results, o.Color)
}

// cellResult is the text of a RESULT cell and the result that colors it.
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

// renderColored fills the RESULT column after alignment, so escape codes
// never skew the column widths.
func renderColored(sb *strings.Builder, t *table, results []cellResult, color bool) {
	for i := range t.rows {
		t.rows[i][len(t.rows[i])-1] = results[i].label
	}
	lines := t.lines()
	sb.WriteString(lines[0] + "\n")
	for i, line := range lines[1:] {
		if color && i < len(results) {
			label := results[i].label
			if strings.HasSuffix(line, label) {
				line = strings.TrimSuffix(line, label) + colorLabel(label, results[i].result)
			}
		}
		sb.WriteString(line + "\n")
	}
}

func details(sb *strings.Builder, mode Mode, s Suite) {
	var notes []string
	for _, b := range s.Benchmarks {
		if b.Error != nil {
			notes = append(notes, fmt.Sprintf("%s: %s", b.Name, errorLine(b.Error)))
			if b.Error.Stderr != "" {
				notes = append(notes, "  stderr: "+oneLine(lastLines(b.Error.Stderr, 3)))
			}
		}
		for _, c := range b.Commands {
			notes = append(notes, commandNotes(mode, b, c)...)
		}
	}
	for _, n := range notes {
		sb.WriteString("  " + cellText(n) + "\n")
	}
	if s.GeometricMean != nil {
		for _, v := range s.GeometricMean.Values {
			fmt.Fprintf(sb, "geometric mean over %d cases (%s): %s %s\n", s.GeometricMean.Cases, geoLabel(s.GeometricMean.Reference), v.Command, FormatRatio(v.Ratio))
		}
	} else if note := GeometricMeanNote(s); note != "" {
		fmt.Fprintf(sb, "no geometric mean: %s\n", note)
	}
}

func geoLabel(ref string) string {
	switch ref {
	case "baseline":
		return "relative to the baseline"
	case "fastest":
		return "relative to the fastest"
	default:
		return "head relative to base"
	}
}

func errorLine(e *Error) string {
	return fmt.Sprintf("%s: %s", strings.ReplaceAll(e.Kind, "_", " "), oneLine(e.Message))
}

func writeStderr(sb *strings.Builder, e *Error) {
	if e.Stderr != "" {
		sb.WriteString("  stderr: " + oneLine(lastLines(e.Stderr, 3)) + "\n")
	}
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

func summaryLine(sb *strings.Builder, r *Report) {
	s := r.Summary
	parts := []string{fmt.Sprintf("%d passed", s.Pass)}
	for _, x := range []struct {
		n     int
		label string
	}{{s.Improved, "improved"}, {s.Inconclusive, "inconclusive"}, {s.OverBudget, "over budget"}, {s.Regression, "regressed"}, {s.MetricError, "metric errors"}, {s.Error, "errored"}} {
		if x.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", x.n, x.label))
		}
	}
	noun := "benchmarks"
	if s.Benchmarks == 1 {
		noun = "benchmark"
	}
	extra := ""
	if note := checksNote(s); note != "" {
		extra = " · " + note
	}
	fmt.Fprintf(sb, "%s · %d %s%s · seed %d · exit %d\n", strings.Join(parts, ", "), s.Benchmarks, noun, extra, r.Seed, s.ExitCode)
}
