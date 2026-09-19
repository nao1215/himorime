package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/nao1215/himorime/internal/config"
	"github.com/nao1215/himorime/internal/exitcode"
	"github.com/nao1215/himorime/internal/metric"
	"github.com/nao1215/himorime/internal/runner"
)

// floorResult measures every metric of one command with a constant peak RSS
// rss, the floor of each run given by floors.
func floorResult(name string, rss int64, floors ...int64) runner.BenchmarkResult {
	b := metricsResult(name, len(floors), 100*time.Millisecond, 10*time.Millisecond, time.Millisecond, rss, 1000)
	b.Commands[0].Sides[runner.SideHead].PeakRSSFloor = floors
	return b
}

func repeat(v int64, n int) []int64 {
	out := make([]int64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func TestJudgePeakRSSFloor(t *testing.T) {
	t.Parallel()
	floors := append(repeat(5<<20, 6), repeat(2<<20, 4)...)
	r := judge(ModeRun, false, floorResult("floor", 3<<20, floors...))
	ms := r.Suites[0].Benchmarks[0].Commands[0].Head.Metrics
	rss := ms["peak_rss"]
	if rss.Floor != 5<<20 || rss.SamplesAtFloor != 6 {
		t.Fatalf("floor = %d, samples at floor = %d; want the largest floor and the 6 runs at or below theirs", rss.Floor, rss.SamplesAtFloor)
	}
	if rss.Stats.Median != 3<<20 || rss.Samples[0] != 3<<20 {
		t.Fatalf("statistics must stay those of the raw samples: %+v", rss.Stats)
	}
	for name, m := range ms {
		if name != "peak_rss" && (m.Floor != 0 || m.SamplesAtFloor != 0) {
			t.Errorf("%s has a floor: %+v", name, m)
		}
	}

	// A platform without a floor (Windows) reports 0 and no sample at it.
	r = judge(ModeRun, false, floorResult("none", 3<<20, repeat(0, 5)...))
	if rss := r.Suites[0].Benchmarks[0].Commands[0].Head.Metrics["peak_rss"]; rss.Floor != 0 || rss.SamplesAtFloor != 0 {
		t.Fatalf("no floor: %+v", rss)
	}
}

const floorNote = "≤ marks a peak RSS at or below what the process starting the command already used; the command used at most that much."

func TestRenderPeakRSSAtTheFloor(t *testing.T) {
	t.Parallel()
	atFloor := floorResult("tiny", 3<<20, repeat(5<<20, 10)...)
	above := floorResult("large", 64<<20, repeat(5<<20, 10)...)
	r := judge(ModeRun, false, atFloor, above)

	var out bytes.Buffer
	if err := WriteTerminal(&out, r, TerminalOptions{}); err != nil {
		t.Fatal(err)
	}
	term := out.String()
	tiny := lineWith(t, term, "tiny")
	if !strings.Contains(tiny, "≤ 5.00MiB") {
		t.Errorf("a peak at the floor is not shown as <= the floor: %q", tiny)
	}
	if large := lineWith(t, term, "large"); strings.Contains(large, "<=") || !strings.Contains(large, "64.00MiB") {
		t.Errorf("a peak above the floor is shown as %q", large)
	}
	if strings.Count(term, floorNote) != 1 {
		t.Errorf("terminal must explain <= once:\n%s", term)
	}

	md := markdownOf(t, r)
	if !strings.Contains(md, "| tiny | tool | ≤ 5.00MiB | ≤ 5.00MiB |") || strings.Count(md, EscapeMarkdown(floorNote)) != 1 {
		t.Errorf("markdown memory table:\n%s", md)
	}

	// Without a peak at the floor there is nothing to explain.
	r = judge(ModeRun, false, above)
	out.Reset()
	_ = WriteTerminal(&out, r, TerminalOptions{})
	if strings.Contains(out.String(), "<=") || strings.Contains(markdownOf(t, r), "at or below what the process") {
		t.Errorf("a note without a peak at the floor:\n%s", out.String())
	}
}

func lineWith(t *testing.T, text, word string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, word) && strings.Contains(strings.ToLower(line), "peak rss") {
			return line
		}
	}
	t.Fatalf("no line with %q in:\n%s", word, text)
	return ""
}

// TestComparePeakRSSAtTheFloor: a side at its floor is compared as if it
// used the whole floor. Only the verdict that assumption cannot overstate
// stands: a regression away from a base at its floor, an improvement down to
// a head at its floor. Other one-sided cases are inconclusive; two sides
// below their floors are skipped.
func TestComparePeakRSSAtTheFloor(t *testing.T) {
	t.Parallel()
	floor := repeat(5<<20, 20)
	tests := []struct {
		name       string
		base, head runner.BenchmarkResult
		verdict    string
	}{
		{"from the floor to far above", floorResult("x", 3<<20, floor...), floorResult("x", 64<<20, floor...), "regression"},
		{"from far above to the floor", floorResult("x", 64<<20, floor...), floorResult("x", 5<<20, floor...), "improved"},
		{"from the floor to just above", floorResult("x", 3<<20, floor...), floorResult("x", 5200<<10, floor...), "inconclusive"},
		{"from just above to the floor", floorResult("x", 5200<<10, floor...), floorResult("x", 4<<20, floor...), "inconclusive"},
		{"both at the floor", floorResult("x", 3<<20, floor...), floorResult("x", 4<<20, floor...), "skipped"},
		{"both above", floorResult("x", 32<<20, floor...), floorResult("x", 64<<20, floor...), "regression"},
		{"no floor", floorResult("x", 3<<20, repeat(0, 20)...), floorResult("x", 6<<20, repeat(0, 20)...), "regression"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := judge(ModeCompare, false, pairSides("x", tt.base, tt.head))
			c := r.Suites[0].Benchmarks[0].Commands[0]
			mc := c.Comparisons["peak_rss"]
			if mc.Verdict != tt.verdict {
				t.Fatalf("verdict = %s (%s), want %s", mc.Verdict, mc.Reason, tt.verdict)
			}
			if tt.verdict == "inconclusive" {
				if mc.Reason != ReasonAtFloor || c.Result != ResultInconclusive {
					t.Fatalf("reason %q, result %s", mc.Reason, c.Result)
				}
				if lat := c.Comparisons["latency"]; lat.Reason == ReasonAtFloor {
					t.Fatal("the floor made latency inconclusive")
				}
				var out bytes.Buffer
				_ = WriteTerminal(&out, r, TerminalOptions{})
				if !strings.Contains(out.String(), "the peak RSS is at or below the measurement floor") {
					t.Errorf("terminal does not say why:\n%s", out.String())
				}
			}
			if tt.verdict == VerdictSkipped && (mc.Reason != ReasonAtFloor || c.Result != ResultPass || r.Summary.Skipped != 1) {
				t.Fatalf("both-floor comparison = %+v, command result %s, summary %+v", mc, c.Result, r.Summary)
			}
		})
	}
}

func TestBudgetOnPeakRSSAtTheFloor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		expr   string
		agg    metric.Aggregation
		status string
		reason string
	}{
		// The floor itself is within the budget, so the true peak is too.
		{"<= 8MiB", metric.AggMax, BudgetPass, ReasonAtFloor},
		{"< 5MiB", metric.AggMedian, BudgetSkipped, ReasonAtFloor},
		// The floor is over the budget: the true peak may or may not be.
		{"<= 4MiB", metric.AggMax, BudgetSkipped, ReasonAtFloor},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			t.Parallel()
			b := floorResult("x", 3<<20, repeat(5<<20, 10)...)
			b.Benchmark.Budgets = append(b.Benchmark.Budgets, budget(t, "tool", metric.PeakRSS, tt.agg, tt.expr))
			r := judge(ModeRun, false, b)
			c := r.Suites[0].Benchmarks[0].Commands[0]
			bc := c.Budgets[0]
			if bc.Status != tt.status || bc.Reason != tt.reason || bc.Pass != (tt.status == BudgetPass) {
				t.Fatalf("budget %s = %+v", tt.expr, bc)
			}
			if bc.Actual == nil || *bc.Actual != 3<<20 {
				t.Fatalf("actual = %v, want the measured statistic", bc.Actual)
			}
			wantResult := ResultPass
			wantExit := exitcode.OK
			if tt.status == BudgetSkipped {
				wantResult = ResultMetricError
				wantExit = exitcode.Metric
			}
			wantSkipped := 0
			if tt.status == BudgetSkipped {
				wantSkipped = 1
			}
			if c.Result != wantResult || r.Summary.Skipped != wantSkipped || r.Summary.ExitCode != wantExit {
				t.Fatalf("result %s, skipped %d", c.Result, r.Summary.Skipped)
			}
			var out bytes.Buffer
			_ = WriteTerminal(&out, r, TerminalOptions{})
			if !strings.Contains(out.String(), "≤ 5.00MiB") {
				t.Errorf("budget table does not show the floor:\n%s", out.String())
			}
			if tt.status == BudgetSkipped && (!strings.Contains(out.String(), "COULD NOT BE ASSESSED") || !strings.Contains(out.String(), "the peak RSS is at or below the measurement floor")) {
				t.Errorf("terminal does not say why the budget was skipped:\n%s", out.String())
			}
			if tt.status == BudgetSkipped {
				md := markdownOf(t, r)
				if !strings.Contains(md, "COULD NOT BE ASSESSED") || !strings.Contains(md, "could not be assessed: the peak RSS is at or below the measurement floor") {
					t.Errorf("markdown does not say why the budget was unassessed:\n%s", md)
				}
				var summary bytes.Buffer
				if err := WriteGitHubSummary(&summary, r); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(summary.String(), "COULD NOT BE ASSESSED") || !strings.Contains(summary.String(), "the peak RSS is at or below the measurement floor") {
					t.Errorf("job summary does not say why the budget was unassessed:\n%s", summary.String())
				}
			}
		})
	}

	// Above the floor a budget is judged as always.
	b := floorResult("x", 64<<20, repeat(5<<20, 10)...)
	b.Benchmark.Budgets = append(b.Benchmark.Budgets, budget(t, "tool", metric.PeakRSS, metric.AggMax, "<= 32MiB"))
	if bc := judge(ModeRun, false, b).Suites[0].Benchmarks[0].Commands[0].Budgets[0]; bc.Status != BudgetFail || bc.Reason != "" {
		t.Fatalf("a budget above the floor = %+v", bc)
	}
}

func TestFloorBudgetMetricErrorSurvivesComparison(t *testing.T) {
	t.Parallel()
	base := floorResult("x", 3<<20, repeat(5<<20, 20)...)
	head := floorResult("x", 3<<20, repeat(5<<20, 20)...)
	head.Benchmark.Budgets = []config.Budget{budget(t, "tool", metric.PeakRSS, metric.AggMax, "<= 4MiB")}
	r := judge(ModeCompare, false, pairSides("x", base, head))
	c := &r.Suites[0].Benchmarks[0].Commands[0]
	if c.Result != ResultMetricError || c.Comparisons == nil || c.Comparisons["latency"] == nil || r.Summary.ExitCode != exitcode.Metric {
		t.Fatalf("floor budget = %s, summary %+v", c.Result, r.Summary)
	}
	// Verify a repeated comparison also preserves the required-budget error.
	compareMetrics(c, pairSides("x", base, head), head.Benchmark, 42)
	if c.Result != ResultMetricError {
		t.Fatalf("comparison overwrote metric error with %s", c.Result)
	}
	var out bytes.Buffer
	if err := WriteAnnotations(&out, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "title=himorime%3A required budget could not be assessed") ||
		!strings.Contains(out.String(), "budget peak rss max could not be assessed: the peak RSS is at or below the measurement floor") {
		t.Fatalf("floor annotation = %s", out.String())
	}
}

func TestNoDataBudgetKeepsExecutionError(t *testing.T) {
	t.Parallel()
	noData := runResult("no data", "", map[string][]time.Duration{"tool": nil}, "tool")
	noData.Benchmark.Budgets = []config.Budget{latencyBudget(t, "tool", metric.AggMedian, "<= 1s")}
	r := judge(ModeRun, false, noData)
	c := r.Suites[0].Benchmarks[0].Commands[0]
	if c.Budgets[0].Status != BudgetNoData || c.Result != ResultMetricError || r.Summary.ExitCode != exitcode.Metric {
		t.Fatalf("no-data budget = %+v, result %s, summary %+v", c.Budgets[0], c.Result, r.Summary)
	}
	execution := runResult("execution", "", map[string][]time.Duration{"tool": nil}, "tool")
	execution.Benchmark.Budgets = noData.Benchmark.Budgets
	execution.Commands[0].Sides[runner.SideHead].Failure = &runner.Failure{Kind: runner.FailExitCode, ExitCode: 1, Message: "exited"}
	r = judge(ModeRun, false, execution)
	c = r.Suites[0].Benchmarks[0].Commands[0]
	if c.Budgets[0].Status != BudgetNoData || c.Result != ResultError || r.Summary.ExitCode != exitcode.Execution {
		t.Fatalf("execution no-data budget = %+v, result %s, summary %+v", c.Budgets[0], c.Result, r.Summary)
	}
}

func TestBenchmarkExecutionErrorOutranksFloorBudget(t *testing.T) {
	t.Parallel()
	b := floorResult("cleanup", 3<<20, repeat(5<<20, 10)...)
	b.Benchmark.Budgets = []config.Budget{budget(t, "tool", metric.PeakRSS, metric.AggMax, "<= 4MiB")}
	b.Failure = &runner.Failure{Kind: runner.FailCleanup, Message: "cleanup failed"}
	r := judge(ModeRun, false, b)
	c := r.Suites[0].Benchmarks[0].Commands[0]
	if c.Result != ResultError || r.Summary.Error != 1 || r.Summary.MetricError != 0 || r.Summary.ExitCode != exitcode.Execution {
		t.Fatalf("benchmark execution error = %s, summary %+v", c.Result, r.Summary)
	}
}

func TestPeakRSSFloorInJSONAndCSV(t *testing.T) {
	t.Parallel()
	b := floorResult("x", 3<<20, repeat(5<<20, 10)...)
	b.Benchmark.Budgets = append(b.Benchmark.Budgets, budget(t, "tool", metric.PeakRSS, metric.AggMax, "<= 4MiB"))
	for name, r := range map[string]*Report{
		"run":     judge(ModeRun, false, b),
		"compare": judge(ModeCompare, false, pairSides("x", floorResult("x", 3<<20, repeat(5<<20, 20)...), floorResult("x", 64<<20, repeat(5<<20, 20)...))),
	} {
		var buf bytes.Buffer
		if err := WriteJSON(&buf, r); err != nil {
			t.Fatal(err)
		}
		inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		if err := reportSchema(t).Validate(inst); err != nil {
			t.Errorf("%s report does not match the schema: %v", name, err)
		}
		var decoded struct {
			Suites []struct {
				Benchmarks []struct {
					Commands []struct {
						Head struct {
							Metrics map[string]map[string]any `json:"metrics"`
						} `json:"head"`
					} `json:"commands"`
				} `json:"benchmarks"`
			} `json:"suites"`
		}
		if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
			t.Fatal(err)
		}
		rss := decoded.Suites[0].Benchmarks[0].Commands[0].Head.Metrics["peak_rss"]
		if name == "run" && (rss["floor"] != float64(5<<20) || rss["samples_at_floor"] != float64(10)) {
			t.Errorf("JSON peak_rss = floor %v, samples_at_floor %v", rss["floor"], rss["samples_at_floor"])
		}
	}

	var buf bytes.Buffer
	if err := WriteCSV(&buf, judge(ModeRun, false, b)); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, row := range rows[1:] {
		if row[csvCol("record")] == recordStat && row[csvCol("metric")] == "peak_rss" {
			got[row[csvCol("statistic")]] = row[csvCol("value")]
		}
		if row[csvCol("record")] == recordStat && row[csvCol("metric")] == "latency" && row[csvCol("statistic")] == "floor" {
			t.Error("latency has a floor row")
		}
	}
	if got["floor"] != "5242880" || got["samples_at_floor"] != "10" {
		t.Fatalf("peak_rss stat rows = %v", got)
	}
}
