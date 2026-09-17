package config

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// durationPattern is the only duration spelling a suite accepts. It is
// deliberately narrower than time.ParseDuration: no sign, no bare number, and
// every component carries a unit, so "10" (ten what?) and "-1s" are refused.
// schema/yahiko.schema.json repeats this pattern; TestSchemaPatternsMatchGo
// keeps the two identical.
const durationPattern = `^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$`

// budgetPattern is a performance budget: a comparison operator followed by a
// duration, such as "< 20ms" or "<=1.5s".
const budgetPattern = `^\s*(<=|<)\s*(([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+)\s*$`

// percentPattern is a percentage written as a string, such as "10%".
const percentPattern = `^([0-9]+(\.[0-9]+)?)%$`

var (
	durationRE = regexp.MustCompile(durationPattern)
	budgetRE   = regexp.MustCompile(budgetPattern)
	percentRE  = regexp.MustCompile(percentPattern)
)

// maxDuration bounds every duration a suite may declare. A day is far beyond
// any sensible benchmark and keeps arithmetic on sums of samples clear of
// overflow.
const maxDuration = 24 * time.Hour

// ParseDuration parses a suite duration such as "250ms" or "1m30s".
func ParseDuration(s string) (time.Duration, error) {
	if !durationRE.MatchString(s) {
		return 0, fmt.Errorf("invalid duration %q: write a number with a unit, such as 500ms, 2s or 1m30s", s)
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	if d > maxDuration {
		return 0, fmt.Errorf("duration %q exceeds the maximum of 24h", s)
	}
	return d, nil
}

// nodeError is a decoding error that knows where in the file it happened.
type nodeError struct {
	msg    string
	line   int
	column int
}

func (e *nodeError) Error() string { return e.msg }

func errAt(node ast.Node, format string, args ...any) error {
	e := &nodeError{msg: fmt.Sprintf(format, args...)}
	if node != nil {
		if tk := node.GetToken(); tk != nil && tk.Position != nil {
			e.line, e.column = tk.Position.Line, tk.Position.Column
		}
	}
	return e
}

// scalarString returns the text of a string scalar. Numbers, booleans, null,
// lists and mappings are not strings.
func scalarString(node ast.Node) (string, bool) {
	switch n := unwrap(node).(type) {
	case *ast.StringNode:
		return n.Value, true
	case *ast.LiteralNode:
		return n.Value.Value, true
	default:
		return "", false
	}
}

func kindOf(node ast.Node) string {
	switch unwrap(node).(type) {
	case *ast.StringNode, *ast.LiteralNode:
		return "a string"
	case *ast.IntegerNode:
		return "an integer"
	case *ast.FloatNode:
		return "a number"
	case *ast.BoolNode:
		return "a boolean"
	case *ast.NullNode:
		return "null"
	case *ast.SequenceNode:
		return "a list"
	case *ast.MappingNode, *ast.MappingValueNode:
		return "a mapping"
	default:
		return "a value"
	}
}

// Duration is a YAML duration string.
type Duration struct {
	D   time.Duration
	Raw string
}

// UnmarshalYAML accepts only a duration string.
func (d *Duration) UnmarshalYAML(_ context.Context, node ast.Node) error {
	s, ok := scalarString(node)
	if !ok {
		return errAt(node, "expected a duration string such as 500ms or 2s, got %s", kindOf(node))
	}
	parsed, err := ParseDuration(s)
	if err != nil {
		return errAt(node, "%v", err)
	}
	d.D = parsed
	d.Raw = s
	return nil
}

// Percent is a percentage: a number (10) or a string with a percent sign ("10%").
type Percent struct {
	Value float64
	Set   bool
}

// ParsePercent parses a percentage written as "10%" or "10".
func ParsePercent(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if m := percentRE.FindStringSubmatch(s); m != nil {
		s = m[1]
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("invalid percentage %q: write a number such as 10 or \"10%%\"", s)
	}
	return v, nil
}

// UnmarshalYAML accepts a number or a "N%" string.
func (p *Percent) UnmarshalYAML(_ context.Context, node ast.Node) error {
	switch n := unwrap(node).(type) {
	case *ast.IntegerNode, *ast.FloatNode:
		v, err := ParsePercent(n.String())
		if err != nil {
			return errAt(node, "%v", err)
		}
		p.Value, p.Set = v, true
		return nil
	}
	s, ok := scalarString(node)
	if !ok || !percentRE.MatchString(strings.TrimSpace(s)) {
		return errAt(node, "expected a percentage such as 10 or \"10%%\", got %s", kindOf(node))
	}
	v, err := ParsePercent(s)
	if err != nil {
		return errAt(node, "%v", err)
	}
	p.Value, p.Set = v, true
	return nil
}

// BudgetExpr is an absolute performance budget such as "< 20ms".
type BudgetExpr struct {
	Inclusive bool
	Limit     time.Duration
	Raw       string
}

// ParseBudget parses a budget expression such as "< 20ms" or "<= 1s".
func ParseBudget(s string) (BudgetExpr, error) {
	m := budgetRE.FindStringSubmatch(s)
	if m == nil {
		return BudgetExpr{}, fmt.Errorf("invalid budget %q: write an operator and a duration, such as \"< 20ms\" or \"<= 1s\"", s)
	}
	d, err := ParseDuration(m[2])
	if err != nil {
		return BudgetExpr{}, err
	}
	if d <= 0 {
		return BudgetExpr{}, fmt.Errorf("invalid budget %q: the limit must be greater than zero", s)
	}
	return BudgetExpr{Inclusive: m[1] == "<=", Limit: d, Raw: strings.TrimSpace(s)}, nil
}

// Allows reports whether a measured value satisfies the budget.
func (b BudgetExpr) Allows(v time.Duration) bool {
	if b.Inclusive {
		return v <= b.Limit
	}
	return v < b.Limit
}

// Operator returns "<" or "<=".
func (b BudgetExpr) Operator() string {
	if b.Inclusive {
		return "<="
	}
	return "<"
}

// UnmarshalYAML accepts a budget string.
func (b *BudgetExpr) UnmarshalYAML(_ context.Context, node ast.Node) error {
	s, ok := scalarString(node)
	if !ok {
		return errAt(node, "expected a budget string such as \"< 20ms\", got %s", kindOf(node))
	}
	parsed, err := ParseBudget(s)
	if err != nil {
		return errAt(node, "%v", err)
	}
	*b = parsed
	return nil
}

// Argv is a command: either a list of arguments executed without a shell, or a
// single string, which is only valid together with `shell: true`.
type Argv struct {
	List   []string
	Script string
	IsList bool
	Set    bool
}

// UnmarshalYAML accepts a list of strings or a single string.
func (a *Argv) UnmarshalYAML(_ context.Context, node ast.Node) error {
	if seq, ok := unwrap(node).(*ast.SequenceNode); ok {
		list := make([]string, 0, len(seq.Values))
		for _, item := range seq.Values {
			s, ok := scalarString(item)
			if !ok {
				return errAt(item, "every argument must be a string, got %s; quote numbers and booleans, such as \"1\"", kindOf(item))
			}
			list = append(list, s)
		}
		a.List, a.IsList, a.Set = list, true, true
		return nil
	}
	s, ok := scalarString(node)
	if !ok {
		return errAt(node, "expected a list of arguments, or a string together with shell: true, got %s", kindOf(node))
	}
	a.Script, a.Set = s, true
	return nil
}

// StdinSpec is the standard input of a benchmark: a fixture file path (the
// scalar form or `file:`) or inline `content:`.
type StdinSpec struct {
	File    string  `yaml:"file"`
	Content *string `yaml:"content"`
	scalar  bool
}

// UnmarshalYAML accepts a path string or a {file|content} mapping.
func (s *StdinSpec) UnmarshalYAML(_ context.Context, node ast.Node) error {
	if path, ok := scalarString(node); ok {
		s.File, s.scalar = path, true
		return nil
	}
	if _, ok := unwrap(node).(*ast.MappingNode); !ok {
		return errAt(node, "expected a fixture path or a mapping with file or content, got %s", kindOf(node))
	}
	type plain StdinSpec
	var p plain
	if err := yaml.NodeToValue(node, &p, yaml.DisallowUnknownField()); err != nil {
		return err
	}
	*s = StdinSpec(p)
	return nil
}

// Commands is the ordered mapping of command names to commands.
type Commands struct {
	Names []string
	ByKey map[string]RawCommand
}

// UnmarshalYAML decodes the mapping while keeping the declared order, which is
// the order results are reported in.
func (c *Commands) UnmarshalYAML(_ context.Context, node ast.Node) error {
	m, ok := unwrap(node).(*ast.MappingNode)
	if !ok {
		return errAt(node, "expected a mapping of command names to commands, got %s", kindOf(node))
	}
	c.ByKey = map[string]RawCommand{}
	for _, mv := range m.Values {
		name, ok := scalarString(mv.Key)
		if !ok {
			return errAt(mv.Key, "command names must be strings")
		}
		if _, dup := c.ByKey[name]; dup {
			return errAt(mv.Key, "command %q is declared twice", name)
		}
		if _, ok := unwrap(mv.Value).(*ast.MappingNode); !ok {
			return errAt(mv.Value, "command %q must be a mapping with a command key, got %s", name, kindOf(mv.Value))
		}
		var rc RawCommand
		if err := yaml.NodeToValue(mv.Value, &rc, yaml.DisallowUnknownField()); err != nil {
			return err
		}
		c.Names = append(c.Names, name)
		c.ByKey[name] = rc
	}
	return nil
}
