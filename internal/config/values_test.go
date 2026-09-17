package config

import (
	"strings"
	"testing"
	"time"

	"github.com/nao1215/himorime/internal/metric"
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
