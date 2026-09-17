package report

import (
	"fmt"
	"io"
	"strings"
)

// WriteGitHubSummary renders the report for $GITHUB_STEP_SUMMARY: a one-line
// verdict first, so the result is visible without scrolling, then the tables.
func WriteGitHubSummary(w io.Writer, r *Report) error {
	var sb strings.Builder
	s := r.Summary
	icon := "✅"
	verdict := "no regression"
	switch {
	case s.Error > 0:
		icon, verdict = "❌", "errors"
	case s.MetricError > 0:
		icon, verdict = "❌", "metrics could not be measured"
	case s.Regression > 0 && s.OverBudget > 0:
		icon, verdict = "❌", "regression and budget violations"
	case s.Regression > 0:
		icon, verdict = "❌", "performance regression"
	case s.OverBudget > 0:
		icon, verdict = "❌", "budget exceeded"
	case s.Inconclusive > 0 && s.FailOnInconclusive:
		icon, verdict = "❌", "inconclusive results (--fail-on-inconclusive)"
	case s.Inconclusive > 0:
		icon, verdict = "⚠️", "some comparisons were inconclusive"
	case s.NotGated.Regression > 0:
		icon, verdict = "⚠️", "no regression in gated metrics; a metric that is not gated regressed"
	}
	title := "benchmarks"
	if r.Mode == ModeCompare {
		title = "benchmark comparison"
	}
	fmt.Fprintf(&sb, "## %s himorime %s: %s\n\n", icon, title, verdict)
	fmt.Fprintf(&sb, "%d passed · %d improved · %d inconclusive · %d over budget · %d regressed · %d metric errors · %d errored",
		s.Pass, s.Improved, s.Inconclusive, s.OverBudget, s.Regression, s.MetricError, s.Error)
	if note := checksNote(s); note != "" {
		sb.WriteString(" · " + note)
	}
	sb.WriteString("\n\n")
	writeMarkdownBody(&sb, r, 3)
	if r.Mode == ModeCompare {
		sb.WriteString("\n<sub>Shared CI runners are noisy. A regression is reported only when the bootstrap confidence reaches the configured level; see https://nao1215.github.io/himorime/regression-detection/.</sub>\n")
	}
	_, err := io.WriteString(w, sb.String())
	return err
}
