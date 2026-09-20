package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/nao1215/himorime/internal/metric"
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
		if s.NewInHead {
			sb.WriteString(newInHeadLine(s) + "\n")
		}
		compactTable(&sb, compactRows(r, s), o.Color)
		if g := s.GeometricMean; g != nil && g.Cases > 1 {
			for _, v := range g.Values {
				fmt.Fprintf(&sb, "geometric mean over %d cases (%s): %s %s\n", g.Cases, geoLabel(g.Reference), v.Command, FormatRatio(v.Ratio))
			}
		} else if note := GeometricMeanNote(s); note != "" {
			fmt.Fprintf(&sb, "no geometric mean: %s\n", note)
		}
		if hasNotGated(s) {
			sb.WriteString("(NOT GATED) marks a metric with gate: false: it is compared and reported, but never fails the run.\n")
		}
		for _, g := range []metric.Group{metric.GroupCPU, metric.GroupMemory} {
			if note := processNote(s, g); note != "" {
				sb.WriteString(note + "\n")
			}
		}
	}
	sb.WriteString("\n")
	summaryLine(&sb, r)
	_, err := io.WriteString(w, sb.String())
	return err
}

// compactTable keeps the first terminal view focused on the target, the
// measured value or change, and its verdict. Rows needing attention sort
// first; their reason remains in the same row instead of below a wall of
// descriptive statistics.
func compactTable(sb *strings.Builder, rows []compactRow, color bool) {
	if len(rows) == 0 {
		return
	}
	t := &table{header: []string{"TARGET", "METRIC", "VALUE", "RESULT", "REASON"}, right: []bool{false, false, true, false, false}}
	results := make([]cellResult, 0, len(rows))
	for _, row := range rows {
		reason := row.reason
		if reason == "" {
			reason = "-"
		}
		t.add(row.target, row.metric, row.value, row.result, reason)
		results = append(results, cellResult{label: row.result, result: row.verdict})
	}
	lines := t.lines()
	sb.WriteString(lines[0] + "\n")
	resultStart := resultColumnOffset(t)
	for i, line := range lines[1:] {
		if color && i < len(results) {
			label := results[i].label
			line = colorResultCell(line, resultStart, label, results[i].result)
		}
		sb.WriteString(line + "\n")
	}
	for _, row := range rows {
		if strings.HasPrefix(row.metric, "Peak RSS") && strings.Contains(row.value, "≤") {
			sb.WriteString(FloorNote + "\n")
			break
		}
	}
}

func resultColumnOffset(t *table) int {
	widths := make([]int, len(t.header))
	for i, h := range t.header {
		widths[i] = len([]rune(h))
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if n := len([]rune(cell)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	return widths[0] + widths[1] + widths[2] + 6
}

func colorResultCell(line string, start int, label string, result Result) string {
	runes := []rune(line)
	if start+len([]rune(label)) > len(runes) {
		return line
	}
	byteStart := len(string(runes[:start]))
	byteEnd := len(string(runes[:start+len([]rune(label))]))
	return line[:byteStart] + colorLabel(label, result) + line[byteEnd:]
}

// details writes the notes under a suite's tables. geoNote adds why the
// geometric mean is missing, when it is.
func details(sb *strings.Builder, mode Mode, s Suite, geoNote bool) {
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
	} else if note := GeometricMeanNote(s); note != "" && geoNote {
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
	for _, note := range []string{newSuitesNote(s), checksNote(s)} {
		if note != "" {
			extra += " · " + note
		}
	}
	fmt.Fprintf(sb, "%s · %d %s%s · seed %d · exit %d\n", strings.Join(parts, ", "), s.Benchmarks, noun, extra, r.Seed, s.ExitCode)
}
