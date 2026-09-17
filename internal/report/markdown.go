package report

import (
	"fmt"
	"io"
	"strings"
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
			markdownCompare(sb, s)
		} else {
			markdownRun(sb, s)
		}
		markdownNotes(sb, r.Mode, s)
	}
	markdownEnvironment(sb, r)
}

func markdownRun(sb *strings.Builder, s Suite) {
	sb.WriteString("| Benchmark | Command | Median | Mean | Stddev | Min | Max | Runs | vs baseline | vs fastest | Result |\n")
	sb.WriteString("|---|---|--:|--:|--:|--:|--:|--:|--:|--:|---|\n")
	for _, b := range s.Benchmarks {
		if len(b.Commands) == 0 {
			fmt.Fprintf(sb, "| %s | - | - | - | - | - | - | - | - | - | %s |\n", EscapeMarkdown(b.Name), verdictLabel(ResultError))
			continue
		}
		for _, c := range b.Commands {
			cells := []string{"-", "-", "-", "-", "-", "-", "-", "-"}
			if c.Head != nil && c.Head.Count > 0 {
				cells = []string{
					FormatDuration(c.Head.MedianNS), FormatDuration(c.Head.MeanNS), FormatDuration(c.Head.StddevNS),
					FormatDuration(c.Head.MinNS), FormatDuration(c.Head.MaxNS), fmt.Sprint(c.Head.Count), "-", "-",
				}
			}
			if c.Relative != nil {
				if c.Relative.VsBaseline != nil {
					cells[6] = FormatRatio(*c.Relative.VsBaseline)
				}
				if c.Relative.VsFastest != nil {
					cells[7] = FormatRatio(*c.Relative.VsFastest)
				}
			}
			fmt.Fprintf(sb, "| %s | %s | %s | %s |\n", EscapeMarkdown(b.Name), EscapeMarkdown(c.Name), strings.Join(cells, " | "), verdictLabel(c.Result))
		}
	}
	sb.WriteString("\n")
}

func markdownCompare(sb *strings.Builder, s Suite) {
	sb.WriteString("| Benchmark | Command | Base | Head | Change | Interval | Confidence | Tolerance | Result |\n")
	sb.WriteString("|---|---|--:|--:|--:|--:|--:|--:|---|\n")
	for _, b := range s.Benchmarks {
		if len(b.Commands) == 0 {
			fmt.Fprintf(sb, "| %s | - | - | - | - | - | - | - | %s |\n", EscapeMarkdown(b.Name), verdictLabel(ResultError))
			continue
		}
		for _, c := range b.Commands {
			base, head, change, interval := "-", "-", "-", "-"
			metric := "median"
			if c.Comparison != nil {
				metric = c.Comparison.Metric
				change = FormatChange(c.Comparison.ChangePercent)
				interval = fmt.Sprintf("%s … %s", FormatChange(c.Comparison.CILowPercent), FormatChange(c.Comparison.CIHighPercent))
			}
			if c.Base != nil && c.Base.Count > 0 {
				base = FormatDuration(metricValue(c.Base, metric))
			}
			if c.Head != nil && c.Head.Count > 0 {
				head = FormatDuration(metricValue(c.Head, metric))
			}
			fmt.Fprintf(sb, "| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
				EscapeMarkdown(b.Name), EscapeMarkdown(c.Name), base, head, change, interval,
				FormatConfidence(c.Comparison), FormatTolerance(c.Comparison), verdictLabel(c.Result))
		}
	}
	sb.WriteString("\n")
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
