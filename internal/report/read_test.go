package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nao1215/himorime/internal/config"
	"github.com/nao1215/himorime/internal/metric"
	"github.com/nao1215/himorime/internal/runner"
)

func TestReadValidatesCurrentSavedReport(t *testing.T) {
	r := judge(ModeRun, false, runResult("saved", "", map[string][]time.Duration{
		"tool": samples(2*time.Millisecond, 10, 0),
	}, "tool"))
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Read(data)
	if err != nil {
		t.Fatalf("Read valid report: %v", err)
	}
	if got.Summary.ExitCode != r.Summary.ExitCode || got.Suites[0].Benchmarks[0].Commands[0].Head.Metrics["latency"].Status != StatusMeasured {
		t.Fatalf("decoded report lost stored result: %+v", got.Summary)
	}
}

func TestReadRejectsMalformedTruncatedAndUnsupportedReports(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{"malformed", "{not json", "invalid JSON"},
		{"truncated", `{"schema_version":"1"`, "invalid JSON"},
		{"unsupported", `{"schema_version":"0"}`, "unsupported report schema version"},
		{"schema violation", `{"schema_version":"1"}`, "does not match schema"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Read([]byte(tt.data))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Read(%s) error = %v, want %q", tt.data, err, tt.want)
			}
		})
	}
}

func TestReadRejectsIntegerOverflowAfterSchemaValidation(t *testing.T) {
	r := judge(ModeRun, false, runResult("saved", "", map[string][]time.Duration{
		"tool": samples(2*time.Millisecond, 10, 0),
	}, "tool"))
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"suites":1,`), []byte(`"suites":9223372036854775808,`), 1)
	if _, err := Read(data); err == nil || !strings.Contains(err.Error(), "decode report") {
		t.Fatalf("Read integer overflow error = %v", err)
	}
}

func TestReadPreservesUnassessedRSSFloorDetails(t *testing.T) {
	b := floorResult("details", 3<<20, repeat(5<<20, 10)...)
	b.Benchmark.Budgets = []config.Budget{budget(t, "tool", metric.PeakRSS, metric.AggMedian, "< 5MiB")}
	r := judge(ModeRun, false, b)
	c := &r.Suites[0].Benchmarks[0].Commands[0]
	if c.Result != ResultMetricError || r.Summary.ExitCode != 6 || c.Budgets[0].Status != BudgetSkipped || c.Budgets[0].Reason != ReasonAtFloor {
		t.Fatalf("floor budget outcome = result %s, summary %+v, budget %+v", c.Result, r.Summary, c.Budgets[0])
	}
	rss := c.Head.Metrics["peak_rss"]
	if rss.Floor != 5<<20 || rss.SamplesAtFloor != 10 {
		t.Fatalf("floor details = %+v", rss)
	}
	var beforeTable, beforeMarkdown bytes.Buffer
	if err := Write(&beforeTable, config.FormatTable, r, false); err != nil {
		t.Fatal(err)
	}
	if err := Write(&beforeMarkdown, config.FormatMarkdown, r, false); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Read(data)
	if err != nil {
		t.Fatalf("Read report with stored statuses: %v", err)
	}
	gc := &got.Suites[0].Benchmarks[0].Commands[0]
	if got.Summary.ExitCode != 6 || gc.Budgets[0].Status != BudgetSkipped {
		t.Fatalf("stored status changed: summary=%+v budget=%+v", got.Summary, gc.Budgets[0])
	}
	if gc.Budgets[0].Reason != ReasonAtFloor || gc.Head.Metrics["peak_rss"].Floor != 5<<20 || gc.Head.Metrics["peak_rss"].SamplesAtFloor != 10 {
		t.Fatalf("stored RSS floor changed: %+v", gc.Head.Metrics["peak_rss"])
	}
	var afterTable, afterMarkdown bytes.Buffer
	if err := Write(&afterTable, config.FormatTable, got, false); err != nil {
		t.Fatal(err)
	}
	if err := Write(&afterMarkdown, config.FormatMarkdown, got, false); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeTable.Bytes(), afterTable.Bytes()) || !bytes.Equal(beforeMarkdown.Bytes(), afterMarkdown.Bytes()) {
		t.Fatal("rendering changed after a validated save/load round trip")
	}
}

func TestReadPreservesWaivedRequiredBudgetAndPassingExit(t *testing.T) {
	b := runResult("waived", "", map[string][]time.Duration{
		"tool": samples(2*time.Millisecond, 10, 0),
	}, "tool")
	b.Benchmark.Metrics = config.Metrics{Memory: true, Unsupported: config.UnsupportedSkip}
	b.Commands[0].Sides[runner.SideHead].Unsupported = map[metric.Group]string{metric.GroupMemory: "memory collection waived"}
	b.Benchmark.Budgets = []config.Budget{budget(t, "tool", metric.PeakRSS, metric.AggMedian, "<= 1MiB")}
	r := judge(ModeRun, false, b)
	if r.Summary.ExitCode != 0 || r.Suites[0].Benchmarks[0].Commands[0].Budgets[0].Status != BudgetSkipped {
		t.Fatalf("waived budget changed the saved outcome: summary=%+v budget=%+v", r.Summary, r.Suites[0].Benchmarks[0].Commands[0].Budgets[0])
	}
	var beforeTable, beforeMarkdown bytes.Buffer
	if err := Write(&beforeTable, config.FormatTable, r, false); err != nil {
		t.Fatal(err)
	}
	if err := Write(&beforeMarkdown, config.FormatMarkdown, r, false); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Read(data)
	if err != nil {
		t.Fatalf("Read waived report: %v", err)
	}
	c := got.Suites[0].Benchmarks[0].Commands[0]
	if got.Summary.ExitCode != 0 || c.Budgets[0].Status != BudgetSkipped || c.Budgets[0].Reason != "memory collection waived" {
		t.Fatalf("waived budget was not preserved: summary=%+v budget=%+v", got.Summary, c.Budgets[0])
	}
	var afterTable, afterMarkdown bytes.Buffer
	if err := Write(&afterTable, config.FormatTable, got, false); err != nil {
		t.Fatal(err)
	}
	if err := Write(&afterMarkdown, config.FormatMarkdown, got, false); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeTable.Bytes(), afterTable.Bytes()) || !bytes.Equal(beforeMarkdown.Bytes(), afterMarkdown.Bytes()) {
		t.Fatal("waived report rendering changed after a validated save/load round trip")
	}
}
