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

func TestReadPreservesWaivedUnassessedAndRSSFloorDetails(t *testing.T) {
	r := judge(ModeRun, false, runResult("details", "", map[string][]time.Duration{
		"tool": samples(2*time.Millisecond, 10, 0),
	}, "tool"))
	c := &r.Suites[0].Benchmarks[0].Commands[0]
	cpu := c.Head.Metrics["cpu_total"]
	cpu.Status, cpu.Reason = StatusUnsupported, "waived by platform policy"
	rss := c.Head.Metrics["peak_rss"]
	rss.Status = StatusMeasured
	rss.Stats = &MetricStats{Count: 1, Min: 16 << 20, Max: 16 << 20, Median: 16 << 20, Mean: 16 << 20, Percentiles: map[string]float64{"p90": 16 << 20}}
	rss.Samples = []float64{16 << 20}
	rss.Floor, rss.SamplesAtFloor = 16<<20, 1
	c.Budgets = []BudgetCheck{{Metric: "peak_rss", Aggregation: "median", Operator: "<=", Limit: 8 << 20, Unit: "bytes", Status: BudgetNoData, Reason: ReasonAtFloor}}
	c.Result = ResultMetricError
	r.Suites[0].Benchmarks[0].Result = ResultMetricError
	r.Suites[0].Result = ResultMetricError
	r.Summary.Pass, r.Summary.MetricError, r.Summary.ExitCode = 0, 1, 6
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
	if got.Summary.ExitCode != 6 || gc.Head.Metrics["cpu_total"].Status != StatusUnsupported || gc.Budgets[0].Status != BudgetNoData {
		t.Fatalf("stored status changed: metric=%+v budget=%+v", gc.Head.Metrics["cpu_total"], gc.Budgets[0])
	}
	if gc.Budgets[0].Reason != ReasonAtFloor || gc.Head.Metrics["peak_rss"].Floor != 16<<20 || gc.Head.Metrics["peak_rss"].SamplesAtFloor != 1 {
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
}
