package report

import (
	"github.com/nao1215/himorime/internal/config"
	"github.com/nao1215/himorime/internal/metric"
	"github.com/nao1215/himorime/internal/stats"
)

// budgets checks every absolute budget of the command against the head.
// Every configured budget is required. A skipped budget caused by an
// unsupported metric is an explicit metrics.unsupported: skip waiver; a
// skipped budget caused by the RSS floor is an unassessed required check and
// therefore makes the command a metric error.
func budgets(c *Command, cfg config.Benchmark) {
	for _, bud := range cfg.Budgets {
		if bud.Command != c.Name {
			continue
		}
		check := evaluateBudget(c, cfg, bud)
		if check.Status == BudgetFail {
			c.Result = worst(c.Result, ResultOverBudget)
		}
		c.Budgets = append(c.Budgets, check)
	}
}

func evaluateBudget(c *Command, cfg config.Benchmark, bud config.Budget) BudgetCheck {
	def := metric.MustLookup(bud.Metric)
	check := BudgetCheck{
		Metric:      string(bud.Metric),
		Aggregation: string(bud.Aggregation),
		Operator:    string(bud.Threshold.Op),
		Limit:       bud.Threshold.Limit,
		Unit:        def.Unit(cfg.Metrics.WorkUnit()),
		Status:      BudgetNoData,
	}
	var ms *MetricSummary
	if c.Head != nil {
		ms = c.Head.Metrics[string(bud.Metric)]
	}
	switch {
	case ms != nil && ms.Status == StatusUnsupported:
		check.Status, check.Reason = BudgetSkipped, ms.Reason
	case ms == nil || ms.Status != StatusMeasured || len(ms.Samples) == 0:
		check.Reason = "no successful run measured this metric"
		if ms != nil && ms.Reason != "" {
			check.Reason = ms.Reason
		}
		// A configured budget is required. A missing value cannot count as
		// a successful check; worst preserves an execution error if present.
		c.Result = worst(c.Result, ResultMetricError)
	default:
		check = measuredBudget(c, check, ms, bud)
	}
	return check
}

func measuredBudget(c *Command, check BudgetCheck, ms *MetricSummary, bud config.Budget) BudgetCheck {
	actual, _ := stats.Aggregate(ms.Samples, bud.Aggregation)
	check.Actual = &actual
	if !ms.atFloor(actual) {
		check.Pass = bud.Threshold.Allows(actual)
		check.Status = BudgetFail
		if check.Pass {
			check.Status = BudgetPass
		}
		return check
	}
	// The true value is at most the floor. An upper bound the floor meets is
	// met; anything else cannot be decided and is a required metric error.
	check.Status, check.Reason = BudgetSkipped, ReasonAtFloor
	if bud.Threshold.Op.Upper() && bud.Threshold.Allows(float64(ms.Floor)) {
		check.Status, check.Pass = BudgetPass, true
		return check
	}
	c.Result = worst(c.Result, ResultMetricError)
	return check
}
