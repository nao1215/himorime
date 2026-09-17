package config

import (
	"strings"
	"testing"
	"time"
)

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

func TestParseBudget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in        string
		inclusive bool
		limit     time.Duration
	}{
		{"< 20ms", false, 20 * time.Millisecond},
		{"<20ms", false, 20 * time.Millisecond},
		{"<= 1s", true, time.Second},
		{"  <=1.5s  ", true, 1500 * time.Millisecond},
	}
	for _, tt := range tests {
		b, err := ParseBudget(tt.in)
		if err != nil {
			t.Fatalf("ParseBudget(%q): %v", tt.in, err)
		}
		if b.Inclusive != tt.inclusive || b.Limit != tt.limit {
			t.Errorf("ParseBudget(%q) = %+v", tt.in, b)
		}
	}
	for _, in := range []string{"20ms", "> 20ms", "< 0s", "<", "< 20", "== 1s", "<< 1s", "< -1s"} {
		if _, err := ParseBudget(in); err == nil {
			t.Errorf("ParseBudget(%q) accepted an invalid budget", in)
		}
	}
}

func TestBudgetAllowsBoundary(t *testing.T) {
	t.Parallel()
	lt, _ := ParseBudget("< 20ms")
	le, _ := ParseBudget("<= 20ms")
	at := 20 * time.Millisecond
	if lt.Allows(at) {
		t.Error("< 20ms allowed exactly 20ms")
	}
	if !le.Allows(at) {
		t.Error("<= 20ms rejected exactly 20ms")
	}
	if !lt.Allows(at-1) || le.Allows(at+1) {
		t.Error("budget boundary off by one")
	}
	if lt.Operator() != "<" || le.Operator() != "<=" {
		t.Error("Operator() mismatch")
	}
}

func TestSchemaPatternsMatchGo(t *testing.T) {
	t.Parallel()
	src := string(mustSchema(t))
	for name, pattern := range map[string]string{"duration": durationPattern, "budget": budgetPattern, "percent": percentPattern} {
		quoted := strings.ReplaceAll(pattern, `\`, `\\`)
		if !strings.Contains(src, `"pattern": "`+quoted+`"`) {
			t.Errorf("schema/yahiko.schema.json does not carry the Go %s pattern %s", name, pattern)
		}
	}
}
