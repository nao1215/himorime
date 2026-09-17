package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/nao1215/yahiko/internal/metric"
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
	var sb strings.Builder
	writeMarkdownBody(&sb, r, 2)
	_, err := io.WriteString(w, sb.String())
	return err
}

func writeMarkdownBody(sb *strings.Builder, r *Report, level int) {
	h := strings.Repeat("#", level)
	for _, s := range r.Suites {
		fmt.Fprintf(sb, "%s %s\n\n", h, EscapeMarkdown(s.Name))
		if s.Description != "" {
			sb.WriteString(EscapeMarkdown(s.Description) + "\n\n")
		}
		if s.Error != nil {
			fmt.Fprintf(sb, "**Error:** %s\n\n", EscapeMarkdown(errorLine(s.Error)))
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

func markdownRun(sb *strings.Builder, s Suite, level int) {
	groups := groupsShown(s)
	titled := len(groups) > 1 || hasBudgets(s)
	h := strings.Repeat("#", level+1)
	for _, g := range groups {
		if titled {
			fmt.Fprintf(sb, "%s %s\n\n", h, groupTitle(g))
		}
		switch g {
		case metric.GroupLatency:
			markdownLatency(sb, s)
		case metric.GroupThroughput:
			markdownGroup(sb, s, g, []string{"Median", "Mean", "Min", "P95"}, func(m *Measurement) []string {
				return []string{statCell(m, metric.Throughput, medianOf), statCell(m, metric.Throughput, meanOf), statCell(m, metric.Throughput, minOf), statCell(m, metric.Throughput, percentileOf("p95"))}
			})
			sb.WriteString("Throughput is the declared work divided by the latency of each run.\n\n")
		case metric.GroupCPU:
			markdownGroup(sb, s, g, []string{"User", "System", "Total", "Total p95", "Utilization"}, func(m *Measurement) []string {
				return []string{statCell(m, metric.CPUUser, medianOf), statCell(m, metric.CPUSystem, medianOf), statCell(m, metric.CPUTotal, medianOf), statCell(m, metric.CPUTotal, percentileOf("p95")), statCell(m, metric.CPUUtilization, medianOf)}
			})
			sb.WriteString(cpuFootnote)
			if note := processNote(s, g); note != "" {
				sb.WriteString(" " + note)
			}
			sb.WriteString("\n\n")
		case metric.GroupMemory:
			markdownGroup(sb, s, g, []string{"Peak RSS (median)", "Peak RSS (max)"}, func(m *Measurement) []string {
				return []string{statCell(m, metric.PeakRSS, medianOf), statCell(m, metric.PeakRSS, maxOf)}
			})
			sb.WriteString("Peak RSS is a resident set size, not the heap size of a language runtime.")
			if note := processNote(s, g); note != "" {
				sb.WriteString(" " + note)
			}
			sb.WriteString("\n\n")
		}
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

func markdownLatency(sb *strings.Builder, s Suite) {
	sb.WriteString("| Benchmark | Command | Median | P95 | Mean | Stddev | Min | Max | Runs | Relative | Result |\n")
	sb.WriteString("|---|---|--:|--:|--:|--:|--:|--:|--:|--:|---|\n")
	for _, b := range s.Benchmarks {
		if len(b.Commands) == 0 {
			fmt.Fprintf(sb, "| %s | - | - | - | - | - | - | - | - | - | %s |\n", EscapeMarkdown(b.Name), benchmarkErrorCell(b).label)
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
			fmt.Fprintf(sb, "| %s | %s | %s | %s |\n", EscapeMarkdown(b.Name), EscapeMarkdown(c.Name), strings.Join(cells, " | "), label)
		}
	}
	sb.WriteString("\nRelative is the median divided by the baseline command's median, or by the fastest command's.\n\n")
}

func markdownGroup(sb *strings.Builder, s Suite, g metric.Group, headers []string, cells func(*Measurement) []string) {
	sb.WriteString("| Benchmark | Command | " + strings.Join(headers, " | ") + " | Result |\n")
	sb.WriteString("|---|---|" + strings.Repeat("--:|", len(headers)) + "---|\n")
	for _, b := range s.Benchmarks {
		for _, c := range b.Commands {
			label, _ := groupLabel(c, g)
			fmt.Fprintf(sb, "| %s | %s | %s | %s |\n", EscapeMarkdown(b.Name), EscapeMarkdown(c.Name), strings.Join(cells(c.Head), " | "), label)
		}
	}
	sb.WriteString("\n")
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
	h := strings.Repeat("#", level+1)
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
	details(&sub, mode, s)
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
	fmt.Fprintf(sb, "Measured with yahiko %s on %s/%s, %s (%d logical CPUs)",
		EscapeMarkdown(r.YahikoVersion), EscapeMarkdown(e.OS), EscapeMarkdown(e.Arch), EscapeMarkdown(cpu), e.LogicalCPUs)
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
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
