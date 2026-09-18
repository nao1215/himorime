package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/nao1215/himorime/internal/metric"
)

// Annotation titles. A title says at a glance whether the job failed because
// performance got worse or because himorime could not measure.
const (
	TitleBudget       = "himorime: performance budget exceeded"
	TitleRegression   = "himorime: performance regression"
	TitleInconclusive = "himorime: inconclusive comparison"
	TitleNotGated     = "himorime: performance regression (not gated)"
	TitleMetricError  = "himorime: metric could not be measured"
	TitleError        = "himorime: benchmark could not run"
	TitleNewInHead    = "himorime: suite new in this revision"
)

// WriteAnnotations writes GitHub Actions workflow commands (::error,
// ::warning and ::notice) for every problem in the report, one line each, so failures
// appear on the run page and the pull request without opening the log.
func WriteAnnotations(w io.Writer, r *Report) error {
	var sb strings.Builder
	for _, s := range r.Suites {
		file := s.File
		if s.Error != nil {
			annotate(&sb, "error", errorTitle(s.Error), file, fmt.Sprintf("suite %s: %s", s.Name, errorLine(s.Error)))
		}
		if s.NewInHead {
			// Nothing failed; the notice explains why the suite has no results.
			annotate(&sb, "notice", TitleNewInHead, file, fmt.Sprintf("suite %s: %s", s.Name, newInHeadLine(s)))
		}
		for _, b := range s.Benchmarks {
			if b.Error != nil {
				annotate(&sb, "error", errorTitle(b.Error), file, fmt.Sprintf("%s: %s", b.Name, errorLine(b.Error)))
			}
			for _, c := range b.Commands {
				benchmarkAnnotations(&sb, file, b, c)
			}
		}
	}
	_, err := io.WriteString(w, sb.String())
	return err
}

func benchmarkAnnotations(sb *strings.Builder, file string, b Benchmark, c Command) {
	label := fmt.Sprintf("%s / %s", b.Name, c.Name)
	for _, m := range []*Measurement{c.Base, c.Head} {
		if m != nil && m.Error != nil {
			annotate(sb, "error", errorTitle(m.Error), file, label+": "+errorLine(m.Error))
		}
	}
	for _, bc := range c.Budgets {
		if bc.Status != BudgetFail {
			continue
		}
		row := budgetView(c, bc)
		annotate(sb, "error", TitleBudget, file, fmt.Sprintf("%s: %s budget %s, measured %s", label, row.metric, row.limit, row.actual))
	}
	for _, def := range metric.Defs() {
		mc := c.Comparisons[string(def.Name)]
		if mc == nil || mc.DerivedFrom != "" {
			continue
		}
		row := comparisonView(mc)
		detail := fmt.Sprintf("%s: %s %s -> %s (%s, %s; tolerance %s)", label, def.Label, row.base, row.head, row.diff, row.change, row.tolerance)
		switch {
		case mc.Verdict == string(ResultRegression) && mc.Gate:
			annotate(sb, "error", TitleRegression, file, detail)
		case mc.Verdict == string(ResultRegression):
			// Reported so the change is seen, without failing anything.
			annotate(sb, "notice", TitleNotGated, file, detail)
		case mc.Verdict == string(ResultInconclusive) && mc.Gate:
			annotate(sb, "warning", TitleInconclusive, file, detail+": "+mc.Reason)
		}
	}
}

func errorTitle(e *Error) string {
	if e.Metric != "" || strings.HasPrefix(e.Kind, "metric_") {
		return TitleMetricError
	}
	return TitleError
}

// annotate writes one workflow command. Data and property values are escaped
// as the runner requires, so a message cannot end the command early or inject
// another one.
func annotate(sb *strings.Builder, level, title, file, message string) {
	fmt.Fprintf(sb, "::%s file=%s,title=%s::%s\n", level, escapeProperty(file), escapeProperty(title), escapeData(message))
}

func escapeData(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

func escapeProperty(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(s)
}
