package report

import (
	"github.com/nao1215/himorime/internal/exitcode"
	"github.com/nao1215/himorime/internal/runner"
	"github.com/nao1215/himorime/internal/stats"
)

func summarize(r *Report) {
	sum := &r.Summary
	sum.Suites = len(r.Suites)
	countChecks(r)
	count := func(res Result) {
		switch res {
		case ResultPass:
			sum.Pass++
		case ResultImproved:
			sum.Improved++
		case ResultInconclusive:
			sum.Inconclusive++
		case ResultOverBudget:
			sum.OverBudget++
		case ResultRegression:
			sum.Regression++
		case ResultMetricError:
			sum.MetricError++
		case ResultError:
			sum.Error++
		}
	}
	for _, s := range r.Suites {
		if s.NewInHead {
			sum.NewSuites++
		}
		if s.Error != nil {
			count(errorResult(runner.FailureKind(s.Error.Kind)))
		}
		for _, b := range s.Benchmarks {
			sum.Benchmarks++
			if b.Error != nil && len(b.Commands) == 0 {
				count(errorResult(runner.FailureKind(b.Error.Kind)))
			}
			for _, c := range b.Commands {
				sum.Commands++
				count(c.Result)
			}
			if b.Error != nil && len(b.Commands) > 0 && !anyError(b.Commands) {
				count(errorResult(runner.FailureKind(b.Error.Kind)))
			}
		}
	}
	switch {
	case sum.Error > 0:
		sum.ExitCode = exitcode.Execution
	case sum.MetricError > 0:
		sum.ExitCode = exitcode.Metric
	case sum.Regression > 0 || sum.OverBudget > 0:
		sum.ExitCode = exitcode.Failed
	case sum.Inconclusive > 0 && sum.FailOnInconclusive:
		sum.ExitCode = exitcode.Failed
	default:
		sum.ExitCode = exitcode.OK
	}
}

// countChecks counts the comparisons that are not gated, by verdict, and the
// budgets and comparisons skipped because their metric is unsupported or
// peak RSS could not be observed beyond the measurement floor.
func countChecks(r *Report) {
	sum := &r.Summary
	for _, s := range r.Suites {
		for _, b := range s.Benchmarks {
			for _, c := range b.Commands {
				for _, bc := range c.Budgets {
					if bc.Status == BudgetSkipped {
						sum.Skipped++
					}
				}
				for _, mc := range c.Comparisons {
					switch {
					case mc.Verdict == VerdictSkipped:
						sum.Skipped++
					case mc.DerivedFrom != "":
					case mc.Gate:
					case mc.Verdict == string(stats.VerdictRegression):
						sum.NotGated.Regression++
					case mc.Verdict == string(stats.VerdictImproved):
						sum.NotGated.Improved++
					case mc.Verdict == string(stats.VerdictInconclusive):
						sum.NotGated.Inconclusive++
					default:
						sum.NotGated.Pass++
					}
				}
			}
		}
	}
}

func anyError(cs []Command) bool {
	for _, c := range cs {
		if c.Result.isError() {
			return true
		}
	}
	return false
}
