package report

import (
	"fmt"
	"io"
	"strings"
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

func colorResult(r Result, on bool) string {
	label := verdictLabel(r)
	if !on {
		return label
	}
	switch r {
	case ResultPass:
		return ansiGreen + label + ansiReset
	case ResultImproved:
		return ansiGreen + ansiBold + label + ansiReset
	case ResultInconclusive:
		return ansiYellow + label + ansiReset
	case ResultOverBudget, ResultRegression, ResultError:
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
	t := &table{
		header: []string{"BENCHMARK", "COMMAND", "MEDIAN", "MEAN", "STDDEV", "RELATIVE", "RESULT"},
		right:  []bool{false, false, true, true, true, true, false},
	}
	var results []Result
	for _, b := range s.Benchmarks {
		if len(b.Commands) == 0 {
			t.add(b.Name, "-", "-", "-", "-", "-", "")
			results = append(results, ResultError)
			continue
		}
		for _, c := range b.Commands {
			median, mean, stddev, rel := "-", "-", "-", "-"
			if c.Head != nil && c.Head.Count > 0 {
				median = FormatDuration(c.Head.MedianNS)
				mean = FormatDuration(c.Head.MeanNS)
				stddev = FormatDuration(c.Head.StddevNS)
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
			results = append(results, c.Result)
		}
	}
	renderColored(sb, t, results, o.Color)
	sb.WriteString("RELATIVE is the median divided by the baseline command's median, or by the fastest command's when no baseline is set.\n")
}

func compareTable(sb *strings.Builder, s Suite, o TerminalOptions) {
	t := &table{
		header: []string{"BENCHMARK", "BASE", "HEAD", "CHANGE", "CONFIDENCE", "BUDGET", "RESULT"},
		right:  []bool{false, true, true, true, true, true, false},
	}
	var results []Result
	for _, b := range s.Benchmarks {
		if len(b.Commands) == 0 {
			t.add(b.Name, "-", "-", "-", "-", "-", "")
			results = append(results, ResultError)
			continue
		}
		for _, c := range b.Commands {
			name := b.Name
			if len(b.Commands) > 1 {
				name = fmt.Sprintf("%s (%s)", b.Name, c.Name)
			}
			base, head, change := "-", "-", "-"
			metric := "median"
			if c.Comparison != nil {
				metric = c.Comparison.Metric
				change = FormatChange(c.Comparison.ChangePercent)
			}
			if c.Base != nil && c.Base.Count > 0 {
				base = FormatDuration(metricValue(c.Base, metric))
			}
			if c.Head != nil && c.Head.Count > 0 {
				head = FormatDuration(metricValue(c.Head, metric))
			}
			t.add(name, base, head, change, FormatConfidence(c.Comparison), FormatTolerance(c.Comparison), "")
			results = append(results, c.Result)
		}
	}
	renderColored(sb, t, results, o.Color)
	sb.WriteString("BASE and HEAD show the regression metric; CONFIDENCE is the bootstrap probability that the change exceeds BUDGET in its direction.\n")
}

func metricValue(m *Measurement, metric string) int64 {
	if metric == "mean" {
		return m.MeanNS
	}
	return m.MedianNS
}

// renderColored fills the RESULT column after alignment, so escape codes
// never skew the column widths.
func renderColored(sb *strings.Builder, t *table, results []Result, color bool) {
	for i := range t.rows {
		t.rows[i][len(t.rows[i])-1] = verdictLabel(results[i])
	}
	lines := t.lines()
	sb.WriteString(lines[0] + "\n")
	for i, line := range lines[1:] {
		if color && i < len(results) {
			label := verdictLabel(results[i])
			if strings.HasSuffix(line, label) {
				line = strings.TrimSuffix(line, label) + colorResult(results[i], true)
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

// commandNotes lists the failures, missed budgets and inconclusive reasons of
// one command, one line each.
func commandNotes(mode Mode, b Benchmark, c Command) []string {
	var notes []string
	label := fmt.Sprintf("%s / %s", b.Name, c.Name)
	for _, side := range []sideMeasurement{{"base", c.Base}, {"head", c.Head}} {
		if side.m == nil || side.m.Error == nil {
			continue
		}
		l := label
		if mode == ModeCompare {
			l += " (" + side.name + ")"
		}
		notes = append(notes, l+": "+errorLine(side.m.Error))
		if side.m.Error.Stderr != "" {
			notes = append(notes, "  stderr: "+oneLine(lastLines(side.m.Error.Stderr, 3)))
		}
	}
	for _, bc := range c.Budgets {
		if bc.Pass {
			continue
		}
		actual := "no successful run"
		if bc.ActualNS != nil {
			actual = FormatDuration(*bc.ActualNS)
		}
		notes = append(notes, fmt.Sprintf("%s: budget %s %s %s not met (measured %s)", label, bc.Metric, bc.Operator, FormatDuration(bc.LimitNS), actual))
	}
	if c.Comparison != nil && c.Comparison.Reason != "" {
		notes = append(notes, fmt.Sprintf("%s: inconclusive: %s", label, c.Comparison.Reason))
	}
	return notes
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
	}{{s.Improved, "improved"}, {s.Inconclusive, "inconclusive"}, {s.OverBudget, "over budget"}, {s.Regression, "regressed"}, {s.Error, "errored"}} {
		if x.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", x.n, x.label))
		}
	}
	noun := "benchmarks"
	if s.Benchmarks == 1 {
		noun = "benchmark"
	}
	fmt.Fprintf(sb, "%s · %d %s · seed %d · exit %d\n", strings.Join(parts, ", "), s.Benchmarks, noun, r.Seed, s.ExitCode)
}
