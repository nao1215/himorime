package report

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/nao1215/himorime/internal/config"
	"github.com/nao1215/himorime/internal/exitcode"
	"github.com/nao1215/himorime/internal/metric"
	"github.com/nao1215/himorime/internal/proc"
	"github.com/nao1215/himorime/internal/runner"
)

// metricsResult builds a one-command benchmark measuring every metric with
// constant samples: latency lat, CPU user/system, peak RSS rss and work.
func metricsResult(name string, n int, lat, user, system time.Duration, rss int64, work float64) runner.BenchmarkResult {
	c := cmd("tool")
	m := &runner.Measurement{}
	for range n {
		m.Samples = append(m.Samples, lat)
		m.CPUUser = append(m.CPUUser, user)
		m.CPUSystem = append(m.CPUSystem, system)
		m.PeakRSS = append(m.PeakRSS, rss)
		m.Work = append(m.Work, work)
	}
	b := config.Benchmark{
		Name: name, Commands: []config.Command{c}, Regression: regressionAll(),
		Metrics: config.Metrics{CPU: true, Memory: true, Throughput: &config.Work{Value: work, Unit: "records"}, Unsupported: config.UnsupportedFail},
	}
	return runner.BenchmarkResult{Benchmark: b, Rounds: n, Commands: []runner.CommandResult{{Command: c, Sides: map[string]*runner.Measurement{runner.SideHead: m}}}}
}

func regressionAll() config.Regression {
	return regression()
}

func TestJudgeMetricsSummaries(t *testing.T) {
	t.Parallel()
	b := metricsResult("all", 10, 100*time.Millisecond, 120*time.Millisecond, 30*time.Millisecond, 64<<20, 1000)
	b.Benchmark.Budgets = []config.Budget{latencyBudget(t, "tool", "p99.5", "<= 1s")}
	r := judge(ModeRun, false, b)
	c := r.Suites[0].Benchmarks[0].Commands[0]
	ms := c.Head.Metrics
	check := func(name string, median float64, unit string) {
		t.Helper()
		m := ms[name]
		if m == nil || m.Status != StatusMeasured || m.Stats == nil || m.Stats.Median != median || m.Unit != unit || len(m.Samples) != 10 {
			t.Errorf("%s = %+v", name, m)
		}
	}
	check("latency", 1e8, "ns")
	check("throughput", 10000, "records/s")
	check("cpu_user", 1.2e8, "ns")
	check("cpu_system", 3e7, "ns")
	check("cpu_total", 1.5e8, "ns")
	check("cpu_utilization", 150, "percent")
	check("peak_rss", 64<<20, "bytes")
	if ms["throughput"].Better != "higher" || ms["latency"].Better != "lower" || ms["cpu_utilization"].Better != "neutral" || ms["peak_rss"].Scope != "process_tree" {
		t.Error("directions or scopes are wrong")
	}
	if w := ms["throughput"].Work; w == nil || *w.Value != 1000 || w.Unit != "records" {
		t.Errorf("work = %+v", w)
	}
	for _, p := range []string{"p90", "p95", "p99", "p99.5"} {
		if _, ok := ms["latency"].Stats.Percentiles[p]; !ok {
			t.Errorf("latency lacks %s: %v", p, ms["latency"].Stats.Percentiles)
		}
	}
	// A budget's percentile is reported for every metric of the command, so
	// the tables of one command always have the same columns.
	if _, ok := ms["cpu_total"].Stats.Percentiles["p99.5"]; !ok {
		t.Error("the budget percentile p99.5 is missing from cpu_total")
	}
}

func TestJudgeNotRequestedAndUnsupportedAreNotZero(t *testing.T) {
	t.Parallel()
	b := runResult("plain", "", map[string][]time.Duration{"a": samples(time.Millisecond, 5, 0)}, "a")
	b.Benchmark.Metrics = config.Metrics{Memory: true, CPU: true, Unsupported: config.UnsupportedSkip}
	m := b.Commands[0].Sides[runner.SideHead]
	m.CPUUser = []time.Duration{1, 1, 1, 1, 1}
	m.CPUSystem = []time.Duration{1, 1, 1, 1, 1}
	m.Unsupported = map[metric.Group]string{metric.GroupMemory: "peak rss is not supported: 2 processes"}
	b.Benchmark.Budgets = []config.Budget{budget(t, "a", metric.PeakRSS, metric.AggMax, "<= 1MiB")}
	r := judge(ModeRun, false, b)
	c := r.Suites[0].Benchmarks[0].Commands[0]
	rss := c.Head.Metrics["peak_rss"]
	if rss.Status != StatusUnsupported || rss.Stats != nil || len(rss.Samples) != 0 || !strings.Contains(rss.Reason, "2 processes") {
		t.Fatalf("peak rss = %+v", rss)
	}
	if tp := c.Head.Metrics["throughput"]; tp.Status != StatusNotRequested || tp.Stats != nil {
		t.Fatalf("throughput = %+v", tp)
	}
	bc := c.Budgets[0]
	if bc.Status != BudgetSkipped || bc.Pass || bc.Actual != nil {
		t.Fatalf("a budget on an unsupported metric = %+v", bc)
	}
	if c.Result != ResultPass || r.Summary.ExitCode != exitcode.OK {
		t.Fatalf("a skipped budget must neither fail nor error: %s %+v", c.Result, r.Summary)
	}
	var out bytes.Buffer
	_ = WriteTerminal(&out, r, TerminalOptions{})
	for _, want := range []string{"memory not measured: peak rss is not supported: 2 processes", "SKIPPED", "UNSUPPORTED", "budget peak rss max skipped"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("terminal lacks %q:\n%s", want, out.String())
		}
	}
}

// TestJudgeBudgetDirections checks every metric's budget against a value on
// both sides of its limit, with the direction each metric improves in.
func TestJudgeBudgetDirections(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		metric metric.Name
		agg    metric.Aggregation
		expr   string
		pass   bool
	}{
		{"latency under", metric.Latency, "median", "<= 150ms", true},
		{"latency over", metric.Latency, "p95", "< 50ms", false},
		{"throughput above floor", metric.Throughput, "median", ">= 5000 records/s", true},
		{"throughput below floor", metric.Throughput, "min", "> 20000 records/s", false},
		{"cpu under", metric.CPUTotal, "median", "<= 200ms", true},
		{"cpu over", metric.CPUUser, "max", "< 100ms", false},
		{"utilization above 100", metric.CPUUtilization, "median", ">= 140%", true},
		{"utilization cap", metric.CPUUtilization, "median", "<= 100%", false},
		{"memory under", metric.PeakRSS, "max", "<= 64MiB", true},
		{"memory over", metric.PeakRSS, "max", "< 64MiB", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := metricsResult("b", 10, 100*time.Millisecond, 120*time.Millisecond, 30*time.Millisecond, 64<<20, 1000)
			b.Benchmark.Budgets = []config.Budget{budget(t, "tool", tt.metric, tt.agg, tt.expr)}
			r := judge(ModeRun, false, b)
			c := r.Suites[0].Benchmarks[0].Commands[0]
			if c.Budgets[0].Pass != tt.pass {
				t.Fatalf("budget = %+v, want pass=%v", c.Budgets[0], tt.pass)
			}
			wantResult, wantExit := ResultPass, exitcode.OK
			if !tt.pass {
				wantResult, wantExit = ResultOverBudget, exitcode.Failed
			}
			if c.Result != wantResult || r.Summary.ExitCode != wantExit {
				t.Fatalf("result = %s, exit %d", c.Result, r.Summary.ExitCode)
			}
		})
	}
}

func TestJudgeMultipleMetricBudgets(t *testing.T) {
	t.Parallel()
	b := metricsResult("b", 10, 100*time.Millisecond, 120*time.Millisecond, 30*time.Millisecond, 64<<20, 1000)
	b.Benchmark.Budgets = []config.Budget{
		latencyBudget(t, "tool", "median", "<= 1s"),
		budget(t, "tool", metric.Throughput, "median", ">= 1 records/s"),
		budget(t, "tool", metric.CPUTotal, "median", "<= 1s"),
		budget(t, "tool", metric.PeakRSS, "max", "<= 32MiB"),
	}
	r := judge(ModeRun, false, b)
	c := r.Suites[0].Benchmarks[0].Commands[0]
	var statuses []string
	for _, bc := range c.Budgets {
		statuses = append(statuses, bc.Metric+"="+bc.Status)
	}
	if strings.Join(statuses, " ") != "latency=pass throughput=pass cpu_total=pass peak_rss=fail" || c.Result != ResultOverBudget {
		t.Fatalf("statuses = %v, result %s", statuses, c.Result)
	}
	var out bytes.Buffer
	_ = WriteTerminal(&out, r, TerminalOptions{})
	text := out.String()
	for _, want := range []string{
		"latency\nBENCHMARK  COMMAND",
		"\nthroughput\nBENCHMARK  COMMAND            MEDIAN              MEAN               MIN  RESULT",
		"b          tool     10.00k records/s  10.00k records/s  10.00k records/s  PASS",
		"\ncpu\nBENCHMARK  COMMAND      USER   SYSTEM     TOTAL  UTILIZATION  RESULT",
		"b          tool     120.00ms  30.00ms  150.00ms       150.0%  PASS",
		"\nmemory\nBENCHMARK  COMMAND  PEAK RSS       MAX  RESULT",
		"b          tool     64.00MiB  64.00MiB  OVER BUDGET",
		"\nbudgets\nBENCHMARK  COMMAND  METRIC",
		"peak rss max",
		"<= 32.00MiB",
		"FAIL",
		"b / tool: budget peak rss max <= 32.00MiB not met (measured 64.00MiB)",
		"above 100% means more than one CPU was busy",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("terminal lacks %q:\n%s", want, text)
		}
	}
	out.Reset()
	_ = WriteMarkdown(&out, r)
	for _, want := range []string{"### Latency", "### Throughput", "### CPU", "### Memory", "### Budgets", "| Benchmark | Command | Metric | Budget | Measured | Result |", "| b | tool | peak rss max | &lt;= 32.00MiB | 64.00MiB | FAIL |"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("markdown lacks %q:\n%s", want, out.String())
		}
	}
}

func pairSides(name string, base, head runner.BenchmarkResult) runner.BenchmarkResult {
	b := head
	b.Benchmark.Name = name
	b.Commands[0].Sides[runner.SideBase] = base.Commands[0].Sides[runner.SideHead]
	return b
}

// TestJudgeCompareDirections: throughput is judged as higher-is-better and
// latency, CPU and memory as lower-is-better, with the same tolerance.
func TestJudgeCompareDirections(t *testing.T) {
	t.Parallel()
	base := metricsResult("x", 20, 100*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond, 64<<20, 1000)
	// Twice as slow (throughput halves), same CPU, half the memory.
	head := metricsResult("x", 20, 200*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond, 32<<20, 1000)
	r := judge(ModeCompare, false, pairSides("x", base, head))
	c := r.Suites[0].Benchmarks[0].Commands[0]
	want := map[string]string{"latency": "regression", "throughput": "regression", "cpu_total": "pass", "peak_rss": "improved"}
	for name, verdict := range want {
		mc := c.Comparisons[name]
		if mc == nil || mc.Verdict != verdict {
			t.Errorf("%s = %+v, want %s", name, mc, verdict)
		}
	}
	if tp := c.Comparisons["throughput"]; tp.ChangePercent != -50 || tp.Better != "higher" || *tp.Difference != -5000 {
		t.Errorf("throughput comparison = %+v", tp)
	}
	if _, ok := c.Comparisons["cpu_utilization"]; ok {
		t.Error("cpu utilization must not be compared")
	}
	if c.Result != ResultRegression {
		t.Fatalf("result = %s", c.Result)
	}

	// Faster head: throughput rises, which is an improvement, not a regression.
	faster := metricsResult("x", 20, 50*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond, 64<<20, 1000)
	r = judge(ModeCompare, false, pairSides("y", metricsResult("x", 20, 100*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond, 64<<20, 1000), faster))
	c = r.Suites[0].Benchmarks[0].Commands[0]
	if c.Comparisons["throughput"].Verdict != "improved" || c.Comparisons["latency"].Verdict != "improved" || c.Result != ResultImproved {
		t.Fatalf("faster head: throughput %s, latency %s, result %s", c.Comparisons["throughput"].Verdict, c.Comparisons["latency"].Verdict, c.Result)
	}

	// A memory-only regression fails the command while latency passes.
	memBase := metricsResult("x", 20, 100*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond, 32<<20, 1000)
	memHead := metricsResult("x", 20, 100*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond, 48<<20, 1000)
	r = judge(ModeCompare, false, pairSides("m", memBase, memHead))
	c = r.Suites[0].Benchmarks[0].Commands[0]
	if c.Comparisons["peak_rss"].Verdict != "regression" || c.Comparisons["latency"].Verdict != "pass" || c.Result != ResultRegression || r.Summary.ExitCode != exitcode.Failed {
		t.Fatalf("memory regression: %+v %s", c.Comparisons["peak_rss"], c.Result)
	}
	var out bytes.Buffer
	_ = WriteTerminal(&out, r, TerminalOptions{})
	for _, want := range []string{"latency\nBENCHMARK", "\npeak rss\nBENCHMARK", "32.00MiB  48.00MiB  +16.00MiB  +50.0%", "REGRESSION", "\nthroughput\n", "(FROM LATENCY)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("terminal lacks %q:\n%s", want, out.String())
		}
	}
	out.Reset()
	_ = WriteGitHubSummary(&out, r)
	for _, want := range []string{"| m / tool | Peak RSS | 32.00MiB | 48.00MiB | +16.00MiB | +50.0% |"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("summary lacks %q:\n%s", want, out.String())
		}
	}
}

func TestJudgeCompareMinDifferenceAndSkipped(t *testing.T) {
	t.Parallel()
	base := metricsResult("x", 20, 100*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond, 32<<20, 1000)
	head := metricsResult("x", 20, 100*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond, 36<<20, 1000)
	b := pairSides("x", base, head)
	b.Benchmark.Regression.Memory.MinDifference = 8 << 20
	head.Commands[0].Sides[runner.SideHead].Unsupported = nil
	b.Commands[0].Sides[runner.SideBase].Unsupported = nil
	r := judge(ModeCompare, false, b)
	c := r.Suites[0].Benchmarks[0].Commands[0]
	if mc := c.Comparisons["peak_rss"]; mc.Verdict != "pass" || mc.MinDifference != 8<<20 {
		t.Fatalf("a +4MiB change under min_difference 8MiB = %+v", mc)
	}

	skipped := pairSides("s", metricsResult("x", 20, 100*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond, 32<<20, 1000),
		metricsResult("x", 20, 100*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond, 64<<20, 1000))
	skipped.Benchmark.Metrics.Unsupported = config.UnsupportedSkip
	skipped.Commands[0].Sides[runner.SideBase].Unsupported = map[metric.Group]string{metric.GroupMemory: "no"}
	skipped.Commands[0].Sides[runner.SideBase].PeakRSS = nil
	r = judge(ModeCompare, false, skipped)
	c = r.Suites[0].Benchmarks[0].Commands[0]
	if mc := c.Comparisons["peak_rss"]; mc.Verdict != VerdictSkipped || mc.Base != nil || c.Result != ResultPass {
		t.Fatalf("a comparison with an unsupported side = %+v, result %s", mc, c.Result)
	}
}

func TestJudgeMetricErrors(t *testing.T) {
	t.Parallel()
	b := metricsResult("collect", 3, time.Millisecond, time.Millisecond, time.Millisecond, 1<<20, 10)
	b.Commands[0].Sides[runner.SideHead].Failure = &runner.Failure{Kind: runner.FailMetricCollection, Metric: metric.GroupMemory, Message: "metrics.memory: rusage missing"}
	r := judge(ModeRun, false, b)
	c := r.Suites[0].Benchmarks[0].Commands[0]
	if c.Result != ResultMetricError || r.Summary.MetricError != 1 || r.Summary.ExitCode != exitcode.Metric {
		t.Fatalf("result %s, summary %+v", c.Result, r.Summary)
	}
	if rss := c.Head.Metrics["peak_rss"]; rss.Status != StatusFailed || rss.Stats != nil || c.Head.Error.Metric != "memory" {
		t.Fatalf("peak rss = %+v, error %+v", rss, c.Head.Error)
	}

	unsupported := runner.BenchmarkResult{Benchmark: config.Benchmark{Name: "unsupported"}, Failure: &runner.Failure{Kind: runner.FailMetricUnsupported, Metric: metric.GroupCPU, Message: "metrics.cpu was requested"}}
	r = judge(ModeRun, false, unsupported)
	if r.Summary.MetricError != 1 || r.Summary.Error != 0 || r.Summary.ExitCode != exitcode.Metric || r.Suites[0].Benchmarks[0].Result != ResultMetricError {
		t.Fatalf("summary = %+v", r.Summary)
	}

	// An execution error outranks a metric error.
	exec := runResult("exec", "", map[string][]time.Duration{"a": nil}, "a")
	exec.Commands[0].Sides[runner.SideHead].Failure = &runner.Failure{Kind: runner.FailExitCode, ExitCode: 1, Message: "exited"}
	r = judge(ModeRun, false, exec, unsupported)
	if r.Summary.ExitCode != exitcode.Execution {
		t.Fatalf("summary = %+v", r.Summary)
	}
	var out bytes.Buffer
	_ = WriteGitHubSummary(&out, judge(ModeRun, false, unsupported))
	if !strings.Contains(out.String(), "metric\\_unsupported") || !strings.Contains(out.String(), "| Exit code | 6 |") {
		t.Fatalf("summary = %s", out.String())
	}
}

func TestJSONWithMetricsMatchesReportSchema(t *testing.T) {
	t.Parallel()
	s := reportSchema(t)
	run := metricsResult("run", 10, 100*time.Millisecond, 120*time.Millisecond, 30*time.Millisecond, 64<<20, 1000)
	run.Benchmark.Budgets = []config.Budget{budget(t, "tool", metric.PeakRSS, "p95", "<= 1GiB"), budget(t, "tool", metric.Throughput, "min", ">= 1 records/s")}
	failed := metricsResult("failed", 3, time.Millisecond, time.Millisecond, time.Millisecond, 1<<20, 10)
	failed.Commands[0].Sides[runner.SideHead].Failure = &runner.Failure{Kind: runner.FailMetricCollection, Metric: metric.GroupCPU, Message: "no"}
	skipped := metricsResult("skipped", 3, time.Millisecond, time.Millisecond, time.Millisecond, 1<<20, 10)
	skipped.Commands[0].Sides[runner.SideHead].Unsupported = map[metric.Group]string{metric.GroupMemory: "no"}
	fileWork := metricsResult("file", 3, time.Millisecond, time.Millisecond, time.Millisecond, 1<<20, 10)
	fileWork.Benchmark.Metrics.Throughput = &config.Work{FileSize: "in.json", Unit: "bytes"}
	cmp := pairSides("cmp", metricsResult("x", 20, 100*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond, 32<<20, 1000),
		metricsResult("x", 20, 110*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond, 32<<20, 1000))
	for name, r := range map[string]*Report{
		"run":     judge(ModeRun, false, run, failed, skipped, fileWork),
		"compare": judge(ModeCompare, false, cmp),
	} {
		var buf bytes.Buffer
		if err := WriteJSON(&buf, r); err != nil {
			t.Fatal(err)
		}
		inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Validate(inst); err != nil {
			t.Errorf("%s report does not match schema/report.schema.json: %v", name, err)
		}
	}
}

func TestAnnotations(t *testing.T) {
	t.Parallel()
	over := metricsResult("over", 10, 100*time.Millisecond, 120*time.Millisecond, 30*time.Millisecond, 64<<20, 1000)
	over.Benchmark.Budgets = []config.Budget{budget(t, "tool", metric.PeakRSS, "max", "<= 32MiB"), latencyBudget(t, "tool", "median", "<= 1s")}
	failed := runResult("fail,ed", "", map[string][]time.Duration{"a": nil}, "a")
	failed.Commands[0].Sides[runner.SideHead].Failure = &runner.Failure{Kind: runner.FailExitCode, ExitCode: 3, Message: "exited with status 3\n::error::injected"}
	unsupported := runner.BenchmarkResult{Benchmark: config.Benchmark{Name: "unsupported"}, Failure: &runner.Failure{Kind: runner.FailMetricUnsupported, Metric: metric.GroupCPU, Message: "metrics.cpu was requested"}}
	r := judge(ModeRun, false, over, failed, unsupported)
	var out bytes.Buffer
	if err := WriteAnnotations(&out, r); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	want := []string{
		"::error file=himorime.yaml,title=himorime%3A performance budget exceeded::over / tool: peak rss max budget <= 32.00MiB, measured 64.00MiB",
		"::error file=himorime.yaml,title=himorime%3A benchmark could not run::fail,ed / a: exit code: exited with status 3 ::error::injected",
		"::error file=himorime.yaml,title=himorime%3A metric could not be measured::unsupported: metric unsupported: metrics.cpu was requested",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("annotations:\n%s\nwant:\n%s", out.String(), strings.Join(want, "\n"))
	}

	reg := pairSides("m", metricsResult("x", 20, 100*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond, 32<<20, 1000),
		metricsResult("x", 20, 100*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond, 48<<20, 1000))
	out.Reset()
	_ = WriteAnnotations(&out, judge(ModeCompare, false, reg))
	if got := strings.TrimSpace(out.String()); got != "::error file=himorime.yaml,title=himorime%3A performance regression::m / tool: peak rss 32.00MiB -> 48.00MiB (+16.00MiB, +50.0%25; tolerance +10%25)" {
		t.Fatalf("regression annotation = %q", got)
	}
	if escapeData("100%\r\nx") != "100%25%0D%0Ax" || escapeProperty("a:b,c") != "a%3Ab%2Cc" {
		t.Fatal("escaping")
	}
}

// TestJudgeCompareGate: a metric with gate: false is compared and reported,
// but its regression or inconclusive verdict never decides the result, is
// never shown as a plain PASS or REGRESSION, and is counted apart.
func TestJudgeCompareGate(t *testing.T) {
	t.Parallel()
	// Latency doubles, CPU time and memory are unchanged.
	base := metricsResult("x", 20, 100*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond, 32<<20, 1000)
	head := metricsResult("x", 20, 200*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond, 32<<20, 1000)
	b := pairSides("latency info", base, head)
	b.Benchmark.Metrics.Throughput = nil
	b.Benchmark.Regression.Latency.Gate = false

	r := judge(ModeCompare, true, b)
	c := r.Suites[0].Benchmarks[0].Commands[0]
	lat := c.Comparisons["latency"]
	if lat.Verdict != "regression" || lat.Gate || !c.Comparisons["cpu_total"].Gate || c.Comparisons["cpu_total"].Verdict != "pass" {
		t.Fatalf("comparisons = latency %+v, cpu %+v", lat, c.Comparisons["cpu_total"])
	}
	if c.Result != ResultPass || r.Summary.ExitCode != exitcode.OK || r.Summary.Regression != 0 {
		t.Fatalf("a regression of a metric that is not gated failed the run: result %s, summary %+v", c.Result, r.Summary)
	}
	if want := (VerdictCounts{Regression: 1}); r.Summary.NotGated != want {
		t.Fatalf("not gated = %+v, want %+v", r.Summary.NotGated, want)
	}

	var out bytes.Buffer
	_ = WriteTerminal(&out, r, TerminalOptions{})
	for _, want := range []string{"REGRESSION (NOT GATED)", "(NOT GATED) marks a metric with gate: false", "not gated: 1 regressed", "exit 0"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("terminal lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "REGRESSION\n") {
		t.Errorf("a not gated regression is shown as a plain REGRESSION:\n%s", out.String())
	}
	out.Reset()
	_ = WriteGitHubSummary(&out, r)
	if !strings.Contains(out.String(), "| Exit code | 0 |") || !strings.Contains(out.String(), "REGRESSION (NOT GATED)") {
		t.Errorf("summary:\n%s", out.String())
	}
	out.Reset()
	_ = WriteAnnotations(&out, r)
	if got := strings.TrimSpace(out.String()); !strings.HasPrefix(got, "::notice file=himorime.yaml,title=himorime%3A performance regression (not gated)::latency info / tool: latency") {
		t.Errorf("annotations = %q", got)
	}
	out.Reset()
	_ = WriteCSV(&out, r)
	rows, err := csv.NewReader(&out).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows[1:] {
		if row[csvCol("record")] == "comparison" && row[csvCol("metric")] == "latency" {
			found = row[csvCol("gate")] == "false" && row[csvCol("verdict")] == "regression"
		}
	}
	if !found {
		t.Errorf("no latency comparison row with gate false in %q", rows)
	}

	// The same change with latency gated fails the run.
	gated := pairSides("latency gated", base, head)
	gated.Benchmark.Metrics.Throughput = nil
	r = judge(ModeCompare, false, gated)
	if c := r.Suites[0].Benchmarks[0].Commands[0]; c.Result != ResultRegression || r.Summary.ExitCode != exitcode.Failed || r.Summary.NotGated.Total() != 0 {
		t.Fatalf("gated: result %s, summary %+v", c.Result, r.Summary)
	}
}

// TestFailOnInconclusiveIgnoresNotGated: --fail-on-inconclusive applies to
// gated comparisons only.
func TestFailOnInconclusiveIgnoresNotGated(t *testing.T) {
	t.Parallel()
	noisy := func(name string, gate bool) runner.BenchmarkResult {
		base := runResult(name, "", map[string][]time.Duration{"a": samples(100*time.Millisecond, 20, 0.8)}, "a")
		head := runResult(name, "", map[string][]time.Duration{"a": samples(100*time.Millisecond, 20, 0.8)}, "a")
		b := pairSides(name, base, head)
		b.Benchmark.Regression.MaxCV = 0.1
		b.Benchmark.Regression.Latency.Gate = gate
		return b
	}
	r := judge(ModeCompare, true, noisy("info", false))
	if r.Summary.ExitCode != exitcode.OK || r.Summary.Inconclusive != 0 || r.Summary.NotGated.Inconclusive != 1 {
		t.Fatalf("not gated inconclusive with --fail-on-inconclusive: %+v", r.Summary)
	}
	r = judge(ModeCompare, true, noisy("gated", true))
	if r.Summary.ExitCode != exitcode.Failed || r.Summary.Inconclusive != 1 {
		t.Fatalf("gated inconclusive with --fail-on-inconclusive: %+v", r.Summary)
	}
}

// TestSkippedChecksAreCounted: a budget and a comparison skipped for an
// unsupported metric do not fail the run, but are counted and shown.
func TestSkippedChecksAreCounted(t *testing.T) {
	t.Parallel()
	skipped := pairSides("s", metricsResult("x", 20, 100*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond, 32<<20, 1000),
		metricsResult("x", 20, 100*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond, 32<<20, 1000))
	skipped.Benchmark.Metrics.Unsupported = config.UnsupportedSkip
	skipped.Benchmark.Budgets = []config.Budget{budget(t, "tool", metric.PeakRSS, "max", "<= 1GiB")}
	for _, side := range skipped.Commands[0].Sides {
		side.Unsupported = map[metric.Group]string{metric.GroupMemory: "no"}
		side.PeakRSS = nil
	}
	r := judge(ModeCompare, false, skipped)
	if r.Summary.Skipped != 2 || r.Summary.ExitCode != exitcode.OK {
		t.Fatalf("summary = %+v", r.Summary)
	}
	var out bytes.Buffer
	_ = WriteTerminal(&out, r, TerminalOptions{})
	if !strings.Contains(out.String(), "2 checks skipped") || !strings.Contains(out.String(), "SKIPPED") {
		t.Errorf("terminal:\n%s", out.String())
	}
}

// TestMetricCollectionIsRecorded: every metric says where its values come
// from and how the processes of the tree are combined.
func TestMetricCollectionIsRecorded(t *testing.T) {
	t.Parallel()
	r := judge(ModeRun, false, metricsResult("all", 10, 100*time.Millisecond, 120*time.Millisecond, 30*time.Millisecond, 64<<20, 1000))
	ms := r.Suites[0].Benchmarks[0].Commands[0].Head.Metrics
	cpu, mem := proc.CPUCollection(), proc.MemoryCollection()
	for name, want := range map[string][2]string{
		"latency":         {SourceWallClock, proc.AggregationNone},
		"throughput":      {SourceDeclaredWork, proc.AggregationNone},
		"cpu_total":       {cpu.Source, cpu.ProcessAggregation},
		"cpu_utilization": {cpu.Source, cpu.ProcessAggregation},
		"peak_rss":        {mem.Source, mem.ProcessAggregation},
	} {
		if got := [2]string{ms[name].Source, ms[name].ProcessAggregation}; got != want {
			t.Errorf("%s collection = %v, want %v", name, got, want)
		}
	}
	if mem.ProcessAggregation == proc.AggregationMaxProcess {
		var out bytes.Buffer
		_ = WriteTerminal(&out, r, TerminalOptions{})
		if !strings.Contains(out.String(), "not the combined memory of processes running at the same time") {
			t.Errorf("terminal does not explain how peak RSS combines processes:\n%s", out.String())
		}
	}
}
