package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/nao1215/himorime/internal/metric"
)

// EscapeMarkdown makes s safe inside a Markdown table cell: pipes cannot split
// the cell, line breaks cannot end the row, and HTML cannot be injected into a
// rendered page.
func EscapeMarkdown(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		"|", `\|`,
		"`", "\\`",
		"*", `\*`,
		"_", `\_`,
		"[", `\[`,
		"]", `\]`,
		"<", "&lt;",
		">", "&gt;",
		"\r\n", " ",
		"\n", " ",
		"\r", " ",
	)
	return r.Replace(s)
}

// WriteMarkdown renders the report as GitHub-flavored Markdown suitable for a
// README, a blog post or release notes.
func WriteMarkdown(w io.Writer, r *Report) error {
	_, err := io.WriteString(w, renderMarkdown(r, 2))
	return err
}

// renderMarkdown renders the report with suite headings at level; the
// headings of its tables are one level deeper.
func renderMarkdown(r *Report, level int) string {
	var sb strings.Builder
	writeMarkdownBody(&sb, r, level)
	return sb.String()
}

// heading returns the marker of a heading at level, never deeper than the
// six levels Markdown has.
func heading(level int) string {
	return strings.Repeat("#", min(level, 6))
}

func writeMarkdownBody(sb *strings.Builder, r *Report, level int) {
	h := heading(level)
	for _, s := range r.Suites {
		fmt.Fprintf(sb, "%s %s\n\n", h, EscapeMarkdown(s.Name))
		if s.Description != "" {
			sb.WriteString(EscapeMarkdown(s.Description) + "\n\n")
		}
		if s.Error != nil {
			fmt.Fprintf(sb, "**Error:** %s\n\n", EscapeMarkdown(errorLine(s.Error)))
			continue
		}
		if s.NewInHead {
			line := newInHeadLine(s)
			fmt.Fprintf(sb, "%s.\n\n", EscapeMarkdown(strings.ToUpper(line[:1])+line[1:]))
			continue
		}
		if r.Mode == ModeCompare {
			markdownCompare(sb, s, level)
		} else {
			markdownRun(sb, s, level)
		}
		markdownNotes(sb, r.Mode, s)
	}
	markdownEnvironment(sb, r)
}

// markdownRun writes the tables of a plain run. A metric group table lists
// only the commands that measured the group, and a group no command measured
// has no table. The Result column is left out when the suite judged nothing:
// with no budget and no error, every cell would say PASS.
func markdownRun(sb *strings.Builder, s Suite, level int) {
	result := resultJudged(s)
	var tables []markdownTable
	for _, g := range groupsShown(s) {
		var t markdownTable
		switch g {
		case metric.GroupLatency:
			t = markdownLatency(s, result)
		case metric.GroupThroughput:
			t = markdownGroup(s, g, result, []string{"Median", "Mean", "Min", "P95"}, func(m *Measurement) []string {
				return []string{statCell(m, metric.Throughput, medianOf), statCell(m, metric.Throughput, meanOf), statCell(m, metric.Throughput, minOf), statCell(m, metric.Throughput, percentileOf("p95"))}
			})
			t.note = "Throughput is the declared work divided by the latency of each run."
		case metric.GroupCPU:
			t = markdownGroup(s, g, result, []string{"User", "System", "Total", "Total p95", "Utilization"}, func(m *Measurement) []string {
				return []string{statCell(m, metric.CPUUser, medianOf), statCell(m, metric.CPUSystem, medianOf), statCell(m, metric.CPUTotal, medianOf), statCell(m, metric.CPUTotal, percentileOf("p95")), statCell(m, metric.CPUUtilization, medianOf)}
			})
			t.note = withProcessNote(cpuFootnote, s, g)
		case metric.GroupMemory:
			t = markdownGroup(s, g, result, []string{"Peak RSS (median)", "Peak RSS (max)"}, func(m *Measurement) []string {
				return []string{statCell(m, metric.PeakRSS, medianOf), statCell(m, metric.PeakRSS, maxOf)}
			})
			t.note = withProcessNote("Peak RSS is a resident set size, not the heap size of a language runtime.", s, g)
		}
		if len(t.rows) > 0 {
			tables = append(tables, t)
		}
	}
	titled := len(tables) > 1 || hasBudgets(s)
	h := heading(level + 1)
	for _, t := range tables {
		if titled {
			fmt.Fprintf(sb, "%s %s\n\n", h, groupTitle(t.group))
		}
		t.write(sb)
	}
	if hasBudgets(s) {
		fmt.Fprintf(sb, "%s Budgets\n\n", h)
		markdownBudgets(sb, s)
	}
}

func groupTitle(g metric.Group) string {
	switch g {
	case metric.GroupLatency:
		return "Latency"
	case metric.GroupThroughput:
		return "Throughput"
	case metric.GroupCPU:
		return "CPU"
	case metric.GroupMemory:
		return "Memory"
	}
	return string(g)
}

// markdownTable is one metric group table of a plain run. Every row ends with
// the Result cell, which write leaves out unless result is set.
type markdownTable struct {
	group   metric.Group
	headers []string
	result  bool
	rows    [][]string
	note    string
}

func (t markdownTable) write(sb *strings.Builder) {
	headers := append([]string{"Benchmark", "Command"}, t.headers...)
	align := "|---|---|" + strings.Repeat("--:|", len(t.headers))
	if t.result {
		headers = append(headers, "Result")
		align += "---|"
	}
	sb.WriteString("| " + strings.Join(headers, " | ") + " |\n")
	sb.WriteString(align + "\n")
	for _, row := range t.rows {
		if !t.result {
			row = row[:len(row)-1]
		}
		sb.WriteString("| " + strings.Join(row, " | ") + " |\n")
	}
	sb.WriteString("\n" + t.note + "\n\n")
}

// resultJudged reports whether a Result column says anything for a suite of
// a plain run: some command has a budget, or something failed.
func resultJudged(s Suite) bool {
	if hasBudgets(s) || s.Error != nil {
		return true
	}
	for _, b := range s.Benchmarks {
		if b.Error != nil || b.Result.isError() {
			return true
		}
		for _, c := range b.Commands {
			if c.Result.isError() {
				return true
			}
		}
	}
	return false
}

func withProcessNote(text string, s Suite, g metric.Group) string {
	if note := processNote(s, g); note != "" {
		return text + " " + note
	}
	return text
}

func markdownLatency(s Suite, result bool) markdownTable {
	t := markdownTable{
		group:   metric.GroupLatency,
		headers: []string{"Median", "P95", "Mean", "Stddev", "Min", "Max", "Runs", "Relative"},
		result:  result,
		note:    "Relative is the median divided by the baseline command's median, or by the fastest command's.",
	}
	for _, b := range s.Benchmarks {
		if len(b.Commands) == 0 {
			t.rows = append(t.rows, []string{EscapeMarkdown(b.Name), "-", "-", "-", "-", "-", "-", "-", "-", "-", benchmarkErrorCell(b).label})
			continue
		}
		for _, c := range b.Commands {
			cells := []string{"-", "-", "-", "-", "-", "-", "-", "-"}
			if c.Head != nil && c.Head.Count > 0 {
				cells = []string{
					statCell(c.Head, metric.Latency, medianOf), statCell(c.Head, metric.Latency, percentileOf("p95")), statCell(c.Head, metric.Latency, meanOf),
					statCell(c.Head, metric.Latency, stddevOf), statCell(c.Head, metric.Latency, minOf), statCell(c.Head, metric.Latency, maxOf), fmt.Sprint(c.Head.Count), "-",
				}
			}
			if c.Relative != nil {
				switch {
				case c.Relative.VsBaseline != nil:
					cells[7] = FormatRatio(*c.Relative.VsBaseline)
				case c.Relative.VsFastest != nil:
					cells[7] = FormatRatio(*c.Relative.VsFastest)
				}
			}
			label, _ := groupLabel(c, metric.GroupLatency)
			row := append([]string{EscapeMarkdown(b.Name), EscapeMarkdown(c.Name)}, cells...)
			t.rows = append(t.rows, append(row, label))
		}
	}
	return t
}

// markdownGroup builds the table of a metric group other than latency, with
// a row for every command that measured the group or tried to.
func markdownGroup(s Suite, g metric.Group, result bool, headers []string, cells func(*Measurement) []string) markdownTable {
	t := markdownTable{group: g, headers: headers, result: result}
	for _, b := range s.Benchmarks {
		for _, c := range b.Commands {
			if !requested(c.Head, g) {
				continue
			}
			label, _ := groupLabel(c, g)
			row := append([]string{EscapeMarkdown(b.Name), EscapeMarkdown(c.Name)}, cells(c.Head)...)
			t.rows = append(t.rows, append(row, label))
		}
	}
	return t
}

// requested reports whether a measurement has a metric of the group that was
// requested, whether or not it could be measured.
func requested(m *Measurement, g metric.Group) bool {
	for _, def := range metric.InGroup(g) {
		if ms := metricSummary(m, def.Name); ms != nil && ms.Status != StatusNotRequested {
			return true
		}
	}
	return false
}

func markdownBudgets(sb *strings.Builder, s Suite) {
	sb.WriteString("| Benchmark | Command | Metric | Budget | Measured | Result |\n")
	sb.WriteString("|---|---|---|--:|--:|---|\n")
	for _, b := range s.Benchmarks {
		for _, c := range b.Commands {
			for _, bc := range c.Budgets {
				row := budgetView(c, bc)
				fmt.Fprintf(sb, "| %s | %s | %s | %s | %s | %s |\n", EscapeMarkdown(b.Name), EscapeMarkdown(c.Name), row.metric, EscapeMarkdown(row.limit), EscapeMarkdown(row.actual), row.label)
			}
		}
	}
	sb.WriteString("\n")
}

func markdownCompare(sb *strings.Builder, s Suite, level int) {
	defs := comparedMetrics(s)
	h := heading(level + 1)
	for _, def := range defs {
		if len(defs) > 1 {
			fmt.Fprintf(sb, "%s %s\n\n", h, metricTitle(def.Name))
		}
		sb.WriteString("| Benchmark | Command | Base | Head | Difference | Change | Interval | Confidence | Tolerance | Result |\n")
		sb.WriteString("|---|---|--:|--:|--:|--:|--:|--:|--:|---|\n")
		for _, b := range s.Benchmarks {
			if len(b.Commands) == 0 {
				if def.Name == metric.Latency {
					fmt.Fprintf(sb, "| %s | - | - | - | - | - | - | - | - | %s |\n", EscapeMarkdown(b.Name), benchmarkErrorCell(b).label)
				}
				continue
			}
			for _, c := range b.Commands {
				mc := c.Comparisons[string(def.Name)]
				if mc == nil && def.Name != metric.Latency {
					continue
				}
				row := comparisonView(mc)
				label, _ := comparisonLabel(c, mc, def.Name)
				fmt.Fprintf(sb, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
					EscapeMarkdown(b.Name), EscapeMarkdown(c.Name), row.base, row.head, row.diff, row.change, row.interval,
					row.confidence, row.tolerance, label)
			}
		}
		sb.WriteString("\n")
	}
	if hasNotGated(s) {
		sb.WriteString("(NOT GATED) marks a metric with gate: false: it is compared and reported, but never fails the run.\n\n")
	}
	if hasBudgets(s) {
		fmt.Fprintf(sb, "%s Budgets (head)\n\n", h)
		markdownBudgets(sb, s)
	}
}

// metricTitle is the heading of a compared metric's table.
func metricTitle(n metric.Name) string {
	switch n {
	case metric.Latency:
		return "Latency"
	case metric.Throughput:
		return "Throughput"
	case metric.CPUTotal:
		return "CPU time"
	case metric.PeakRSS:
		return "Peak RSS"
	case metric.CPUUser, metric.CPUSystem, metric.CPUUtilization:
	}
	return metric.MustLookup(n).Label
}

func markdownNotes(sb *strings.Builder, mode Mode, s Suite) {
	var sub strings.Builder
	// A page publishing results has no use for why an overall number is
	// missing; the terminal still says so.
	details(&sub, mode, s, false)
	text := strings.TrimSpace(sub.String())
	if text == "" {
		return
	}
	for _, line := range strings.Split(text, "\n") {
		sb.WriteString("- " + EscapeMarkdown(strings.TrimSpace(line)) + "\n")
	}
	sb.WriteString("\n")
}

func markdownEnvironment(sb *strings.Builder, r *Report) {
	e := r.Environment
	cpu := e.CPUModel
	if cpu == "" {
		cpu = "unknown CPU"
	}
	fmt.Fprintf(sb, "Measured with himorime %s on %s/%s, %s (%d logical CPUs)",
		EscapeMarkdown(r.HimorimeVersion), EscapeMarkdown(e.OS), EscapeMarkdown(e.Arch), EscapeMarkdown(cpu), e.LogicalCPUs)
	if r.Git != nil {
		if r.Git.BaseSHA != "" {
			fmt.Fprintf(sb, ", base %s", shortSHA(r.Git.BaseSHA))
		}
		if r.Git.HeadSHA != "" {
			fmt.Fprintf(sb, ", head %s", shortSHA(r.Git.HeadSHA))
			if r.Git.Dirty {
				sb.WriteString(" with uncommitted changes")
			}
		}
	}
	fmt.Fprintf(sb, ", seed %d.\n", r.Seed)
	if len(e.Tools) > 0 {
		sb.WriteString("\n")
		for _, t := range e.Tools {
			fmt.Fprintf(sb, "- %s: %s\n", EscapeMarkdown(t.Name), EscapeMarkdown(t.Version))
		}
	}
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
