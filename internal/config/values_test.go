package config

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"

	"github.com/nao1215/himorime/internal/metric"
)

func valueNode(t *testing.T, value string) ast.Node {
	t.Helper()
	return valueNodeWithOptions(t, value)
}

func valueNodeWithOptions(t *testing.T, value string, options ...parser.Option) ast.Node {
	t.Helper()
	f, err := parser.ParseBytes([]byte("value: "+value+"\n"), 0, options...)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := child(f.Docs[0].Body, "value")
	if n == nil {
		t.Fatalf("value node is missing for %q", value)
	}
	return n
}

func rawValueNode(t *testing.T, value string) ast.Node {
	t.Helper()
	f, err := parser.ParseBytes([]byte("value: "+value+"\n"), 0)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := f.Docs[0].Body.(*ast.MappingNode)
	if !ok || len(m.Values) != 1 || m.Values[0].Value == nil {
		t.Fatalf("raw value node is missing for %q", value)
	}
	return m.Values[0].Value
}

func TestParseDuration(t *testing.T) {
	t.Parallel()
	valid := map[string]time.Duration{
		"0s":    0,
		"1ns":   time.Nanosecond,
		"250us": 250 * time.Microsecond,
		"250µs": 250 * time.Microsecond,
		"20ms":  20 * time.Millisecond,
		"1.5s":  1500 * time.Millisecond,
		"1m30s": 90 * time.Second,
		"2h":    2 * time.Hour,
		"24h":   24 * time.Hour,
		"10m0s": 10 * time.Minute,
	}
	for in, want := range valid {
		got, err := ParseDuration(in)
		if err != nil || got != want {
			t.Errorf("ParseDuration(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "10", "-1s", "+1s", "1 s", "1d", "s", "1.s", ".5s", "24h1ns", "1e3ms", " 1s", "1s ", "1S"} {
		if _, err := ParseDuration(in); err == nil {
			t.Errorf("ParseDuration(%q) accepted an invalid duration", in)
		}
	}
}

func TestParsePercent(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]float64{"10": 10, "10%": 10, "2.5%": 2.5, "\t7\t": 7, "0.1": 0.1} {
		got, err := ParsePercent(in)
		if err != nil || got != want {
			t.Errorf("ParsePercent(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "%", "ten", "10%%", "NaN", "Inf"} {
		if _, err := ParsePercent(in); err == nil {
			t.Errorf("ParsePercent(%q) accepted an invalid percentage", in)
		}
	}
}

func TestSchemaPatternsMatchGo(t *testing.T) {
	t.Parallel()
	src := string(mustSchema(t))
	for name, pattern := range map[string]string{
		"duration":        metric.DurationPattern,
		"duration budget": metric.DurationBudgetPattern,
		"bytes budget":    metric.BytesBudgetPattern,
		"rate budget":     metric.RateBudgetPattern,
		"percent budget":  metric.PercentBudgetPattern,
		"bytes":           metric.BytesPattern,
		"rate":            metric.RatePattern,
		"work unit":       metric.WorkUnitPattern,
		"percent":         percentPattern,
	} {
		quoted := strings.ReplaceAll(pattern, `\`, `\\`)
		if !strings.Contains(src, `"pattern": "`+quoted+`"`) {
			t.Errorf("schema/himorime.schema.json does not carry the Go %s pattern %s", name, pattern)
		}
	}
	agg := strings.TrimSuffix(strings.TrimPrefix(metric.PercentilePattern, "^p"), "$")
	if !strings.Contains(src, `"pattern": "^(min|max|mean|median|p`+strings.ReplaceAll(agg, `\`, `\\`)+`)$"`) {
		t.Errorf("schema/himorime.schema.json does not carry the aggregation pattern built from %s", metric.PercentilePattern)
	}
}

func TestValueDecodersRejectWrongNodeKinds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tt := range []struct {
		name  string
		call  func(ast.Node) error
		value string
		want  string
	}{
		{"duration list", func(n ast.Node) error { var d Duration; return d.UnmarshalYAML(ctx, n) }, "[1]", "got a list"},
		{"percent mapping", func(n ast.Node) error { var p Percent; return p.UnmarshalYAML(ctx, n) }, "{value: 1}", "got a mapping"},
		{"argv mapping", func(n ast.Node) error { var a Argv; return a.UnmarshalYAML(ctx, n) }, "{value: 1}", "got a mapping"},
		{"argv list item", func(n ast.Node) error { var a Argv; return a.UnmarshalYAML(ctx, n) }, "[true]", "every argument must be a string"},
		{"stdin integer", func(n ast.Node) error { var s StdinSpec; return s.UnmarshalYAML(ctx, n) }, "1", "got an integer"},
		{"versions list", func(n ast.Node) error { var v Versions; return v.UnmarshalYAML(ctx, n) }, "[one]", "got a list"},
		{"commands list", func(n ast.Node) error { var c Commands; return c.UnmarshalYAML(ctx, n) }, "[one]", "got a list"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.call(valueNode(t, tt.value)); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValueDecodersAcceptFormsAndRejectDuplicateNames(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	versions := &Versions{}
	if err := versions.UnmarshalYAML(ctx, valueNode(t, "{go: [go, version], rust: [rust, --version]}")); err != nil {
		t.Fatalf("versions: %v", err)
	}
	if len(versions.Names) != 2 || versions.Names[0] != "go" || len(versions.ByKey["rust"]) != 2 {
		t.Fatalf("versions = %+v", versions)
	}
	if err := (&Versions{}).UnmarshalYAML(ctx, valueNode(t, "{go: go}")); err == nil || !strings.Contains(err.Error(), "must be a list") {
		t.Fatalf("scalar version command error = %v", err)
	}
	if err := (&Versions{}).UnmarshalYAML(ctx, valueNode(t, "{1: [go]}")); err == nil || !strings.Contains(err.Error(), "tool names must be strings") {
		t.Fatalf("non-string version name error = %v", err)
	}
	if err := (&Versions{}).UnmarshalYAML(ctx, valueNodeWithOptions(t, "{go: [go], go: [other]}", parser.AllowDuplicateMapKey())); err == nil || !strings.Contains(err.Error(), "declared twice") {
		t.Fatalf("duplicate version error = %v", err)
	}

	commands := &Commands{}
	if err := commands.UnmarshalYAML(ctx, valueNode(t, "{build: {command: [go]}, test: {command: [go, test]}}")); err != nil {
		t.Fatalf("commands: %v", err)
	}
	if len(commands.Names) != 2 || commands.ByKey["test"].Command.List[1] != "test" {
		t.Fatalf("commands = %+v", commands)
	}
	if err := (&Commands{}).UnmarshalYAML(ctx, valueNode(t, "{build: [go]}")); err == nil || !strings.Contains(err.Error(), "must be a mapping") {
		t.Fatalf("scalar command error = %v", err)
	}
	if err := (&Commands{}).UnmarshalYAML(ctx, valueNode(t, "{1: {command: [go]}}")); err == nil || !strings.Contains(err.Error(), "command names must be strings") {
		t.Fatalf("non-string command name error = %v", err)
	}
	if err := (&Commands{}).UnmarshalYAML(ctx, valueNode(t, "{build: {command: [go], unknown: true}}")); err == nil {
		t.Fatal("unknown command field was accepted")
	}
	if err := (&Commands{}).UnmarshalYAML(ctx, valueNodeWithOptions(t, "{build: {command: [go]}, build: {command: [other]}}", parser.AllowDuplicateMapKey())); err == nil || !strings.Contains(err.Error(), "declared twice") {
		t.Fatalf("duplicate command error = %v", err)
	}
}

func TestKindOfAndScalarStringCoverYAMLNodes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		value, kind string
	}{
		{"hello", "a string"}, {"1", "an integer"}, {"1.5", "a number"}, {"true", "a boolean"}, {"null", "a string"}, {"[one]", "a list"}, {"{one: 1}", "a mapping"},
	} {
		n := valueNode(t, tt.value)
		if got := kindOf(n); got != tt.kind {
			t.Errorf("kindOf(%s) = %q, want %q", tt.value, got, tt.kind)
		}
	}
	if got, ok := scalarString(valueNode(t, "hello")); !ok || got != "hello" {
		t.Errorf("scalarString(string) = %q, %t", got, ok)
	}
	if got, ok := scalarString(valueNode(t, "1")); ok || got != "" {
		t.Errorf("scalarString(integer) = %q, %t", got, ok)
	}
	if got, ok := scalarString(valueNode(t, "[one]")); ok || got != "" {
		t.Errorf("scalarString(list) = %q, %t", got, ok)
	}
	if got := kindOf(rawValueNode(t, "null")); got != "null" {
		t.Errorf("kindOf(raw null) = %q", got)
	}
	if got, ok := scalarString(rawValueNode(t, "|\n  block")); !ok || !strings.Contains(got, "block") {
		t.Errorf("scalarString(block) = %q, %t", got, ok)
	}
	var stdin StdinSpec
	if err := stdin.UnmarshalYAML(context.Background(), valueNode(t, "{unknown: true}")); err == nil {
		t.Fatal("unknown stdin field was accepted")
	}
}
