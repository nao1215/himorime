package metric

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Op is a comparison operator of a budget.
type Op string

// Operators.
const (
	OpLess         Op = "<"
	OpLessEqual    Op = "<="
	OpGreater      Op = ">"
	OpGreaterEqual Op = ">="
)

// Holds reports whether actual satisfies "actual Op limit". The comparison
// is on the stored float64 values; it never compares formatted strings.
func (o Op) Holds(actual, limit float64) bool {
	switch o {
	case OpLess:
		return actual < limit
	case OpLessEqual:
		return actual <= limit
	case OpGreater:
		return actual > limit
	case OpGreaterEqual:
		return actual >= limit
	}
	return false
}

// Upper reports whether the operator bounds a value from above.
func (o Op) Upper() bool { return o == OpLess || o == OpLessEqual }

// Threshold is an absolute budget such as "<= 64MiB": an operator and a limit
// in the canonical unit of its kind.
type Threshold struct {
	Op    Op
	Limit float64
	Kind  Kind
	// Unit is the work unit of a rate ("bytes" for byte rates); empty for
	// other kinds.
	Unit string
	// Raw is the expression as written in the suite.
	Raw string
}

// Allows reports whether a measured value satisfies the threshold.
func (t Threshold) Allows(actual float64) bool {
	if math.IsNaN(actual) {
		return false
	}
	return t.Op.Holds(actual, t.Limit)
}

// String renders the threshold for humans, such as "<= 64.00MiB".
func (t Threshold) String() string {
	return string(t.Op) + " " + Format(t.Kind, t.Limit, t.Unit)
}

// budgetHint is the example budget of a kind, used in messages.
func budgetHint(k Kind) string {
	switch k {
	case KindDuration:
		return `"<= 80ms"`
	case KindBytes:
		return `"<= 64MiB"`
	case KindRate:
		return `">= 50MiB/s" or ">= 1000 records/s"`
	case KindPercent:
		return `"<= 150%"`
	}
	return ""
}

// ParseThreshold parses a budget of the given kind. The accepted operators
// follow the metric's direction: a lower-is-better metric takes an upper
// bound (< or <=), a higher-is-better metric a lower bound (> or >=), and a
// neutral metric either.
func ParseThreshold(k Kind, better Direction, s string) (Threshold, error) {
	t := Threshold{Kind: k, Raw: strings.TrimSpace(s)}
	var m []string
	switch k {
	case KindDuration:
		m = durationBudgetRE.FindStringSubmatch(s)
		if m == nil {
			return t, budgetError(k, better, s)
		}
		d, err := ParseDuration(m[2])
		if err != nil {
			return t, err
		}
		t.Op, t.Limit = Op(m[1]), float64(d)
	case KindBytes:
		m = bytesBudgetRE.FindStringSubmatch(s)
		if m == nil {
			return t, budgetError(k, better, s)
		}
		v, err := parseBytes(m[2] + m[4])
		if err != nil {
			return t, err
		}
		t.Op, t.Limit = Op(m[1]), v
	case KindRate:
		m = rateBudgetRE.FindStringSubmatch(s)
		if m == nil {
			return t, budgetError(k, better, s)
		}
		v, unit, err := rateValue(s, m[2], m[4])
		if err != nil {
			return t, err
		}
		t.Op, t.Limit, t.Unit = Op(m[1]), v, unit
	case KindPercent:
		m = percentBudgetRE.FindStringSubmatch(s)
		if m == nil {
			return t, budgetError(k, better, s)
		}
		v, err := finiteFloat(m[2])
		if err != nil {
			return t, err
		}
		t.Op, t.Limit = Op(m[1]), v
	default:
		return t, fmt.Errorf("unknown kind %v", k)
	}
	if t.Limit <= 0 {
		return t, fmt.Errorf("invalid budget %q: the limit must be greater than zero", t.Raw)
	}
	if err := checkDirection(t.Op, better, t.Raw); err != nil {
		return t, err
	}
	return t, nil
}

func budgetError(k Kind, better Direction, s string) error {
	ops := `"<" or "<="`
	switch better {
	case HigherIsBetter:
		ops = `">" or ">="`
	case Neutral:
		ops = `"<", "<=", ">" or ">="`
	case LowerIsBetter:
	}
	return fmt.Errorf("invalid budget %q: write %s and a %s greater than zero, such as %s", strings.TrimSpace(s), ops, k, budgetHint(k))
}

func checkDirection(op Op, better Direction, raw string) error {
	switch better {
	case LowerIsBetter:
		if !op.Upper() {
			return fmt.Errorf("invalid budget %q: this metric is better when lower, so a budget is an upper bound written with < or <=", raw)
		}
	case HigherIsBetter:
		if op.Upper() {
			return fmt.Errorf("invalid budget %q: this metric is better when higher, so a budget is a lower bound written with > or >=", raw)
		}
	case Neutral:
	}
	return nil
}

// Aggregation reduces a metric's samples to one value: min, max, mean,
// median, or a percentile written pNN such as p95 or p99.9.
type Aggregation string

// Aggregations with fixed names.
const (
	AggMin    Aggregation = "min"
	AggMax    Aggregation = "max"
	AggMean   Aggregation = "mean"
	AggMedian Aggregation = "median"
)

// PercentilePattern is a percentile aggregation: p1 to p99, with optional
// decimals such as p99.9. p0 and p100 are min and max.
const PercentilePattern = `^p([1-9][0-9]?)(\.[0-9]+)?$`

// DefaultPercentiles are reported for every measured metric.
var DefaultPercentiles = []Aggregation{"p90", "p95", "p99"}

// ParseAggregation validates an aggregation name.
func ParseAggregation(s string) (Aggregation, error) {
	switch Aggregation(s) {
	case AggMin, AggMax, AggMean, AggMedian:
		return Aggregation(s), nil
	}
	if percentileKeyRE.MatchString(s) {
		return Aggregation(s), nil
	}
	return "", fmt.Errorf("unknown aggregation %q: use min, max, mean, median or a percentile such as p95 (p1 to p99.9)", s)
}

// Percentile returns the percentile of a pNN aggregation as a fraction
// between 0 and 1.
func (a Aggregation) Percentile() (float64, bool) {
	if !percentileKeyRE.MatchString(string(a)) {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimPrefix(string(a), "p"), 64)
	if err != nil || v <= 0 || v >= 100 {
		return 0, false
	}
	return v / 100, true
}

// order ranks aggregations for stable output: fixed names first, then
// percentiles in ascending order.
func (a Aggregation) order() float64 {
	switch a {
	case AggMin:
		return -4
	case AggMedian:
		return -3
	case AggMean:
		return -2
	case AggMax:
		return -1
	}
	if p, ok := a.Percentile(); ok {
		return p
	}
	return 2
}

// Less orders aggregations: min, median, mean, max, then percentiles.
func (a Aggregation) Less(b Aggregation) bool {
	if a.order() != b.order() {
		return a.order() < b.order()
	}
	return a < b
}
