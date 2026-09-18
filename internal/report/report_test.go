package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/nao1215/himorime/internal/config"
	"github.com/nao1215/himorime/internal/exitcode"
	"github.com/nao1215/himorime/internal/metric"
	"github.com/nao1215/himorime/internal/runner"
	"github.com/nao1215/himorime/internal/stats"
	"github.com/nao1215/himorime/schema"
)

func samples(center time.Duration, n int, spread float64) []time.Duration {
	out := make([]time.Duration, n)
	for i := range out {
		f := 1 + spread*(float64(i%5)-2)/2
		out[i] = time.Duration(float64(center) * f)
	}
	return out
}

func cmd(name string) config.Command {
	return config.Command{Name: name, Exec: config.Exec{Argv: []string{name, "--flag"}}}
}

func regression() config.Regression {
	d := config.MetricRegression{Metric: config.MetricMedian, MaxPercent: 10, Gate: true}
	return config.Regression{Confidence: 0.95, MinSamples: 10, MaxCV: 0.5, Latency: d, Throughput: d, CPU: d, Memory: d}
}

func runResult(name string, baseline string, cmds map[string][]time.Duration, order ...string) runner.BenchmarkResult {
	b := config.Benchmark{Name: name, Baseline: baseline, Regression: regression()}
	res := runner.BenchmarkResult{Rounds: 20}
	for _, n := range order {
		c := cmd(n)
		b.Commands = append(b.Commands, c)
		res.Commands = append(res.Commands, runner.CommandResult{Command: c, Sides: map[string]*runner.Measurement{
			runner.SideHead: {Samples: cmds[n], Warmups: 1},
		}})
	}
	res.Benchmark = b
	return res
}

func judge(mode Mode, fail bool, benches ...runner.BenchmarkResult) *Report {
	suite := &config.Suite{Name: "suite | one", Description: "demo"}
	for _, b := range benches {
		suite.Benchmarks = append(suite.Benchmarks, b.Benchmark)
	}
	r := &Report{HimorimeVersion: "v1.0.0", StartedAt: time.Unix(0, 0).UTC(), FinishedAt: time.Unix(1, 0).UTC(),
		Environment: Environment{OS: "linux", Arch: "amd64", CPUModel: "Test CPU", LogicalCPUs: 4, GoVersion: "go1.26"}}
	Judge(r, []SuiteInput{{Suite: suite, File: "himorime.yaml", Benchmarks: benches}}, Options{Mode: mode, Seed: 42, FailOnInconclusive: fail})
	return r
}

func TestJudgeRunRelativeAndBudgets(t *testing.T) {
	t.Parallel()
	b := runResult("df small", "jsonize", map[string][]time.Duration{
		"jsonize": samples(2*time.Millisecond, 20, 0),
		"jc":      samples(30*time.Millisecond, 20, 0),
	}, "jsonize", "jc")
	b.Benchmark.Budgets = []config.Budget{
		latencyBudget(t, "jsonize", metric.AggMedian, "< 20ms"),
		latencyBudget(t, "jc", metric.AggMedian, "< 20ms"),
	}
	r := judge(ModeRun, false, b)
	cs := r.Suites[0].Benchmarks[0].Commands
	if cs[0].Result != ResultPass || cs[1].Result != ResultOverBudget {
		t.Fatalf("results = %s, %s", cs[0].Result, cs[1].Result)
	}
	if *cs[0].Relative.VsBaseline != 1 || math.Abs(*cs[1].Relative.VsBaseline-15) > 1e-9 || math.Abs(*cs[1].Relative.VsFastest-15) > 1e-9 {
		t.Fatalf("relative = %+v %+v", *cs[1].Relative.VsBaseline, *cs[1].Relative.VsFastest)
	}
	if !cs[0].Budgets[0].Pass || cs[1].Budgets[0].Pass || *cs[1].Budgets[0].Actual != float64(30*time.Millisecond) || cs[1].Budgets[0].Status != BudgetFail || cs[1].Budgets[0].Unit != "ns" {
		t.Fatalf("budgets = %+v %+v", cs[0].Budgets, cs[1].Budgets)
	}
	if r.Summary.ExitCode != exitcode.Failed || r.Summary.OverBudget != 1 || r.Summary.Pass != 1 {
		t.Fatalf("summary = %+v", r.Summary)
	}
	if r.Suites[0].Benchmarks[0].Result != ResultOverBudget || r.Suites[0].Result != ResultOverBudget {
		t.Fatal("benchmark and suite results must take the worst command result")
	}
}

// latencyBudget builds a latency budget such as "< 20ms" on an aggregation.
func latencyBudget(t *testing.T, command string, agg metric.Aggregation, s string) config.Budget {
	t.Helper()
	return budget(t, command, metric.Latency, agg, s)
}

func budget(t *testing.T, command string, name metric.Name, agg metric.Aggregation, s string) config.Budget {
	t.Helper()
	def := metric.MustLookup(name)
	th, err := metric.ParseThreshold(def.Kind, def.Better, s)
	if err != nil {
		t.Fatal(err)
	}
	return config.Budget{Command: command, Metric: name, Aggregation: agg, Threshold: th}
}

func TestJudgeRunWithoutBaselineUsesFastest(t *testing.T) {
	t.Parallel()
	b := runResult("x", "", map[string][]time.Duration{
		"a": samples(4*time.Millisecond, 10, 0),
		"b": samples(2*time.Millisecond, 10, 0),
	}, "a", "b")
	r := judge(ModeRun, false, b)
	cs := r.Suites[0].Benchmarks[0].Commands
	if cs[0].Relative.VsBaseline != nil || *cs[0].Relative.VsFastest != 2 || *cs[1].Relative.VsFastest != 1 {
		t.Fatalf("relative = %+v %+v", cs[0].Relative, cs[1].Relative)
	}
}

func TestJudgeCommandErrorIsNotHidden(t *testing.T) {
	t.Parallel()
	b := runResult("x", "a", map[string][]time.Duration{"a": samples(time.Millisecond, 10, 0), "b": nil}, "a", "b")
	b.Commands[1].Sides[runner.SideHead].Failure = &runner.Failure{Kind: runner.FailExitCode, ExitCode: 2, Message: "exited with status 2", Stderr: "oops"}
	r := judge(ModeRun, false, b)
	cs := r.Suites[0].Benchmarks[0].Commands
	if cs[1].Result != ResultError || cs[1].Head.Error == nil || *cs[1].Head.Error.ExitCode != 2 || cs[1].Relative != nil {
		t.Fatalf("failed command = %+v", cs[1])
	}
	if r.Summary.ExitCode != exitcode.Execution || r.Summary.Error != 1 {
		t.Fatalf("summary = %+v", r.Summary)
	}
}

func TestJudgeBenchmarkFailureMarksCommands(t *testing.T) {
	t.Parallel()
	b := runResult("x", "", map[string][]time.Duration{"a": samples(time.Millisecond, 3, 0)}, "a")
	b.Failure = &runner.Failure{Kind: runner.FailInterrupted, Message: "stopped"}
	r := judge(ModeRun, false, b)
	bench := r.Suites[0].Benchmarks[0]
	if bench.Result != ResultError || bench.Commands[0].Result != ResultError || bench.Commands[0].Head.Count != 3 {
		t.Fatalf("benchmark = %+v", bench)
	}
	setupFailed := runner.BenchmarkResult{Benchmark: config.Benchmark{Name: "setup"}, Failure: &runner.Failure{Kind: runner.FailSetup, Message: "boom"}}
	r = judge(ModeRun, false, setupFailed)
	if r.Summary.Error != 1 || r.Summary.ExitCode != exitcode.Execution {
		t.Fatalf("a setup failure must count as an error: %+v", r.Summary)
	}
}

func compareResult(name string, base, head []time.Duration) runner.BenchmarkResult {
	c := cmd("tool")
	return runner.BenchmarkResult{
		Benchmark: config.Benchmark{Name: name, Commands: []config.Command{c}, Regression: regression()},
		Rounds:    len(head),
		Commands: []runner.CommandResult{{Command: c, Sides: map[string]*runner.Measurement{
			runner.SideBase: {Samples: base}, runner.SideHead: {Samples: head},
		}}},
	}
}

func TestJudgeCompareVerdictsAndExitCodes(t *testing.T) {
	t.Parallel()
	pass := compareResult("pass", samples(10*time.Millisecond, 20, 0.02), samples(10*time.Millisecond, 20, 0.02))
	regressed := compareResult("regressed", samples(10*time.Millisecond, 20, 0.02), samples(20*time.Millisecond, 20, 0.02))
	improved := compareResult("improved", samples(20*time.Millisecond, 20, 0.02), samples(10*time.Millisecond, 20, 0.02))
	few := compareResult("few", samples(10*time.Millisecond, 5, 0.02), samples(10*time.Millisecond, 5, 0.02))

	r := judge(ModeCompare, false, pass, regressed, improved)
	want := []Result{ResultPass, ResultRegression, ResultImproved}
	for i, b := range r.Suites[0].Benchmarks {
		if b.Commands[0].Result != want[i] {
			t.Errorf("%s = %s, want %s", b.Name, b.Commands[0].Result, want[i])
		}
		if b.Commands[0].Comparisons["latency"] == nil || b.Commands[0].Base == nil || b.Commands[0].Relative != nil {
			t.Errorf("%s: comparison fields = %+v", b.Name, b.Commands[0])
		}
	}
	if r.Summary.ExitCode != exitcode.Failed {
		t.Fatalf("a regression must exit 1: %+v", r.Summary)
	}

	r = judge(ModeCompare, false, pass, few)
	if r.Suites[0].Benchmarks[1].Commands[0].Result != ResultInconclusive || r.Summary.ExitCode != exitcode.OK {
		t.Fatalf("inconclusive without --fail-on-inconclusive: %+v", r.Summary)
	}
	r = judge(ModeCompare, true, pass, few)
	if r.Summary.ExitCode != exitcode.Failed || !r.Summary.FailOnInconclusive {
		t.Fatalf("inconclusive with --fail-on-inconclusive: %+v", r.Summary)
	}

	budgeted := compareResult("budget", samples(30*time.Millisecond, 20, 0.02), samples(20*time.Millisecond, 20, 0.02))
	budgeted.Benchmark.Budgets = []config.Budget{latencyBudget(t, "tool", metric.AggMedian, "< 15ms")}
	r = judge(ModeCompare, false, budgeted)
	if got := r.Suites[0].Benchmarks[0].Commands[0].Result; got != ResultOverBudget {
		t.Fatalf("an improvement over budget = %s, want over_budget", got)
	}
}

func TestJudgeCompareSkipsUncomparedCommands(t *testing.T) {
	t.Parallel()
	b := compareResult("x", samples(time.Millisecond, 20, 0), samples(time.Millisecond, 20, 0))
	other := cmd("other")
	b.Benchmark.Commands = append(b.Benchmark.Commands, other)
	b.Commands = append(b.Commands, runner.CommandResult{Command: other, Sides: map[string]*runner.Measurement{}})
	r := judge(ModeCompare, false, b)
	if n := len(r.Suites[0].Benchmarks[0].Commands); n != 1 {
		t.Fatalf("commands = %d, want only the compared one", n)
	}
}

func TestGeometricMeanNoteDoesNotDependOnBenchmarkOrder(t *testing.T) {
	t.Parallel()
	pair := func(name string) runner.BenchmarkResult {
		return runResult(name, "a", map[string][]time.Duration{"a": samples(time.Millisecond, 10, 0), "b": samples(2*time.Millisecond, 10, 0)}, "a", "b")
	}
	single := func(name, command string) runner.BenchmarkResult {
		return runResult(name, "", map[string][]time.Duration{command: samples(time.Millisecond, 10, 0)}, command)
	}
	// A comparison missing a command in one case is explained in either order.
	for _, order := range [][]runner.BenchmarkResult{
		{pair("small"), single("partial", "a")},
		{single("partial", "a"), pair("small")},
	} {
		if note := GeometricMeanNote(judge(ModeRun, false, order...).Suites[0]); note != `command "b" is missing from benchmark "partial"` {
			t.Errorf("order %s, %s: note = %q", order[0].Benchmark.Name, order[1].Benchmark.Name, note)
		}
	}
	// A suite whose benchmarks share no command is not a comparison: no note,
	// whichever benchmark comes first.
	for _, order := range [][]runner.BenchmarkResult{
		{single("version", "tool"), pair("inspect")},
		{pair("inspect"), single("version", "tool")},
	} {
		r := judge(ModeRun, false, order...)
		if r.Suites[0].GeometricMean != nil || GeometricMeanNote(r.Suites[0]) != "" {
			t.Errorf("order %s, %s: geometric mean %+v, note %q", order[0].Benchmark.Name, order[1].Benchmark.Name, r.Suites[0].GeometricMean, GeometricMeanNote(r.Suites[0]))
		}
	}
}

func TestGeometricMeanRun(t *testing.T) {
	t.Parallel()
	mk := func(name string, a, b time.Duration) runner.BenchmarkResult {
		return runResult(name, "a", map[string][]time.Duration{"a": samples(a, 10, 0), "b": samples(b, 10, 0)}, "a", "b")
	}
	r := judge(ModeRun, false, mk("small", time.Millisecond, 2*time.Millisecond), mk("large", time.Millisecond, 8*time.Millisecond))
	g := r.Suites[0].GeometricMean
	if g == nil || g.Reference != "baseline" || g.Cases != 2 || g.Values[0].Ratio != 1 || math.Abs(g.Values[1].Ratio-4) > 1e-9 {
		t.Fatalf("geometric mean = %+v (%s)", g, r.Suites[0].GeometricMeanUnavailable)
	}

	missing := runResult("partial", "a", map[string][]time.Duration{"a": samples(time.Millisecond, 10, 0)}, "a")
	r = judge(ModeRun, false, mk("small", time.Millisecond, 2*time.Millisecond), missing)
	if r.Suites[0].GeometricMean != nil || !strings.Contains(r.Suites[0].GeometricMeanUnavailable, `command "b" is missing from benchmark "partial"`) {
		t.Fatalf("missing case: %+v %q", r.Suites[0].GeometricMean, r.Suites[0].GeometricMeanUnavailable)
	}

	failed := mk("failed", time.Millisecond, time.Millisecond)
	failed.Commands[1].Sides[runner.SideHead].Failure = &runner.Failure{Kind: runner.FailTimeout, Message: "timed out"}
	r = judge(ModeRun, false, mk("small", time.Millisecond, 2*time.Millisecond), failed)
	if r.Suites[0].GeometricMean != nil || !strings.Contains(r.Suites[0].GeometricMeanUnavailable, "did not complete") {
		t.Fatalf("a timeout must suppress the geometric mean: %q", r.Suites[0].GeometricMeanUnavailable)
	}

	single := judge(ModeRun, false, mk("only", time.Millisecond, 2*time.Millisecond))
	if single.Suites[0].GeometricMean != nil || GeometricMeanNote(single.Suites[0]) != "" {
		t.Fatal("one case must not produce a geometric mean, nor a note about it")
	}
	var out bytes.Buffer
	_ = WriteTerminal(&out, judge(ModeRun, false, mk("small", time.Millisecond, 2*time.Millisecond), missing), TerminalOptions{})
	if !strings.Contains(out.String(), `no geometric mean: command "b" is missing from benchmark "partial"`) {
		t.Fatalf("the terminal report must explain the missing score:\n%s", out.String())
	}
	noBaseline := runResult("nb", "", map[string][]time.Duration{"a": samples(time.Millisecond, 10, 0), "b": samples(3*time.Millisecond, 10, 0)}, "a", "b")
	r = judge(ModeRun, false, mk("small", time.Millisecond, 2*time.Millisecond), noBaseline)
	if g := r.Suites[0].GeometricMean; g == nil || g.Reference != "fastest" {
		t.Fatalf("mixed baselines fall back to fastest: %+v", g)
	}
}

func TestGeometricMeanCompare(t *testing.T) {
	t.Parallel()
	r := judge(ModeCompare, false,
		compareResult("a", samples(10*time.Millisecond, 20, 0), samples(20*time.Millisecond, 20, 0)),
		compareResult("b", samples(10*time.Millisecond, 20, 0), samples(5*time.Millisecond, 20, 0)))
	g := r.Suites[0].GeometricMean
	if g == nil || g.Reference != "base" || math.Abs(g.Values[0].Ratio-1) > 1e-9 {
		t.Fatalf("compare geometric mean = %+v", g)
	}
}

func TestFormatHelpers(t *testing.T) {
	t.Parallel()
	for ns, want := range map[int64]string{
		850: "850ns", 1500: "1.50µs", 1_820_000: "1.82ms", 28_410_000: "28.41ms", 3_400_000_000: "3.40s", 125_000_000_000: "125.00s",
	} {
		if got := FormatDuration(ns); got != want {
			t.Errorf("FormatDuration(%d) = %q, want %q", ns, got, want)
		}
	}
	if FormatRatio(15.613) != "15.61x" || FormatRatio(math.NaN()) != "-" {
		t.Error("FormatRatio")
	}
	if FormatChange(2.74) != "+2.7%" || FormatChange(-12) != "-12.0%" || FormatChange(0.01) != "+0.0%" {
		t.Error("FormatChange")
	}
	// CONFIDENCE is the probability that decided the verdict, so it never
	// reads as a doubt next to the verdict it backs.
	c := &MetricComparison{Better: "lower", ChangePercent: 15.6, ProbRegression: 0.981, MaxPercent: 10, Verdict: "regression"}
	if formatMetricConfidence(c) != "98.1%" || FormatMetricTolerance(c) != "+10%" {
		t.Errorf("confidence/tolerance = %s %s", formatMetricConfidence(c), FormatMetricTolerance(c))
	}
	c = &MetricComparison{Better: "lower", ChangePercent: 2.7, ProbRegression: 0.01, MaxPercent: 7.5, Verdict: "pass"}
	if formatMetricConfidence(c) != "99.0%" || FormatMetricTolerance(c) != "+7.5%" || FormatMetricTolerance(nil) != "-" {
		t.Errorf("pass confidence = %s", formatMetricConfidence(c))
	}
	c = &MetricComparison{Better: "lower", ChangePercent: -30, ProbImprovement: 0.99, Verdict: "improved"}
	if formatMetricConfidence(c) != "99.0%" {
		t.Error("improvement confidence")
	}
	// Throughput degrades when it drops: the tolerance points down, and a
	// drop is judged by the probability of a regression.
	c = &MetricComparison{Better: "higher", ChangePercent: -20, ProbRegression: 0.97, ProbImprovement: 0, MaxPercent: 8, Verdict: "regression"}
	if formatMetricConfidence(c) != "97.0%" || FormatMetricTolerance(c) != "-8%" {
		t.Errorf("throughput confidence/tolerance = %s %s", formatMetricConfidence(c), FormatMetricTolerance(c))
	}
	// Too close to call: the highest of the three probabilities, which is
	// below the required one.
	c = &MetricComparison{Better: "lower", ChangePercent: 9, ProbRegression: 0.4, ProbImprovement: 0, Verdict: "inconclusive", Reason: stats.ReasonTooClose}
	if formatMetricConfidence(c) != "60.0%" {
		t.Errorf("too close confidence = %s", formatMetricConfidence(c))
	}
	c = &MetricComparison{Better: "lower", ChangePercent: 30, ProbRegression: 0.2, ProbImprovement: 0.7, Verdict: "inconclusive", Reason: stats.ReasonTooClose}
	if formatMetricConfidence(c) != "80.0%" {
		t.Errorf("too close confidence = %s", formatMetricConfidence(c))
	}
	// A verdict the probabilities did not decide shows none of them.
	for _, c := range []*MetricComparison{
		{Better: "lower", ChangePercent: 40, ProbRegression: 0.3, Verdict: "pass", Reason: stats.ReasonBelowMinDiff},
		{Better: "lower", ChangePercent: 40, ProbRegression: 0.99, Verdict: "inconclusive", Reason: stats.ReasonNoisy},
		{Better: "lower", ChangePercent: 40, ProbRegression: 0.99, Verdict: "inconclusive", Reason: stats.ReasonFewSamples},
		{Better: "lower", Verdict: "inconclusive", Reason: stats.ReasonNoSamples},
		{Better: "lower", ChangePercent: 40, ProbRegression: 0.99, Verdict: "inconclusive", Reason: ReasonAtFloor},
		{Better: "higher", ChangePercent: -40, ProbRegression: 0.99, Verdict: "inconclusive", Reason: "the work differs between the revisions: 1 in the base, 2 in the head"},
		{Better: "lower", Verdict: VerdictSkipped, Reason: "failed"},
	} {
		if got := formatMetricConfidence(c); got != "-" {
			t.Errorf("confidence of %s (%s) = %q, want -", c.Verdict, c.Reason, got)
		}
	}
}

func TestTerminalRunTable(t *testing.T) {
	t.Parallel()
	b := runResult("df small", "jsonize", map[string][]time.Duration{
		"jsonize": samples(1820*time.Microsecond, 10, 0),
		"jc":      samples(28410*time.Microsecond, 10, 0),
	}, "jsonize", "jc")
	r := judge(ModeRun, false, b)
	var out bytes.Buffer
	if err := WriteTerminal(&out, r, TerminalOptions{}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"BENCHMARK  COMMAND   MEDIAN     MEAN  STDDEV  RELATIVE  RESULT",
		"df small   jsonize   1.82ms   1.82ms     0ns     1.00x  PASS",
		"df small   jc       28.41ms  28.41ms     0ns    15.61x  PASS",
		"2 passed · 1 benchmark · seed 42 · exit 0",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("table lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Error("colors without Color")
	}
	var colored bytes.Buffer
	_ = WriteTerminal(&colored, r, TerminalOptions{Color: true})
	if !strings.Contains(colored.String(), "\x1b[32mPASS\x1b[0m") {
		t.Errorf("colored table lacks a green PASS:\n%q", colored.String())
	}
}

func TestTerminalCompareTable(t *testing.T) {
	t.Parallel()
	r := judge(ModeCompare, false,
		compareResult("df small", samples(1840*time.Microsecond, 20, 0), samples(1890*time.Microsecond, 20, 0)),
		compareResult("df large", samples(14200*time.Microsecond, 20, 0), samples(16410*time.Microsecond, 20, 0)))
	var out bytes.Buffer
	if err := WriteTerminal(&out, r, TerminalOptions{}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"BENCHMARK     BASE     HEAD      DIFF  CHANGE  CONFIDENCE  TOLERANCE  RESULT",
		"df small    1.84ms   1.89ms  +50.00µs   +2.7%      100.0%       +10%  PASS",
		"df large   14.20ms  16.41ms   +2.21ms  +15.6%      100.0%       +10%  REGRESSION",
		"geometric mean over 2 cases (head relative to base): head/base 1.09x",
		"1 passed, 1 regressed",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("table lacks %q:\n%s", want, got)
		}
	}
}

func TestMarkdownEscaping(t *testing.T) {
	t.Parallel()
	in := "a|b `c` *d* _e_ [f] <script>\nnext\\"
	want := `a\|b \` + "`" + `c\` + "`" + ` \*d\* \_e\_ \[f\] &lt;script&gt; next\\`
	if got := EscapeMarkdown(in); got != want {
		t.Fatalf("EscapeMarkdown = %q\nwant           %q", got, want)
	}
	b := runResult("pipe | name", "", map[string][]time.Duration{"x": samples(time.Millisecond, 10, 0)}, "x")
	r := judge(ModeRun, false, b)
	var out bytes.Buffer
	if err := WriteMarkdown(&out, r); err != nil {
		t.Fatal(err)
	}
	md := out.String()
	if !strings.Contains(md, `## suite \| one`) || !strings.Contains(md, `| pipe \| name | x |`) {
		t.Fatalf("markdown:\n%s", md)
	}
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "| pipe") && strings.Count(strings.ReplaceAll(line, `\|`, ""), "|") != 11 {
			t.Fatalf("row has the wrong number of cells: %s", line)
		}
	}
}

func TestCSV(t *testing.T) {
	t.Parallel()
	b := runResult("comma, \"quoted\"\nname", "", map[string][]time.Duration{"x": samples(time.Millisecond, 10, 0)}, "x")
	b.Benchmark.Budgets = []config.Budget{latencyBudget(t, "x", "p95", "<= 1s")}
	r := judge(ModeRun, false, b)
	var out bytes.Buffer
	if err := WriteCSV(&out, r); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&out).ReadAll()
	if err != nil {
		t.Fatalf("the CSV does not parse back: %v", err)
	}
	if len(rows[0]) != len(CSVHeader) || rows[1][csvCol("benchmark")] != "comma, \"quoted\"\nname" {
		t.Fatalf("rows = %q", rows)
	}
	find := func(rows [][]string, want map[string]string) []string {
		for _, row := range rows[1:] {
			match := true
			for col, v := range want {
				if row[csvCol(col)] != v {
					match = false
				}
			}
			if match {
				return row
			}
		}
		t.Fatalf("no row matches %v in %q", want, rows)
		return nil
	}
	median := find(rows, map[string]string{"record": "stat", "side": "head", "metric": "latency", "statistic": "median"})
	if median[csvCol("value")] != "1000000" || median[csvCol("unit")] != "ns" || median[csvCol("status")] != "measured" {
		t.Fatalf("median row = %q", median)
	}
	find(rows, map[string]string{"record": "stat", "metric": "latency", "statistic": "p99"})
	bud := find(rows, map[string]string{"record": "budget", "metric": "latency", "statistic": "p95"})
	if bud[csvCol("operator")] != "<=" || bud[csvCol("limit")] != "1000000000" || bud[csvCol("verdict")] != "pass" {
		t.Fatalf("budget row = %q", bud)
	}
	for _, row := range rows {
		if len(row) != len(CSVHeader) {
			t.Fatalf("ragged row %q", row)
		}
		for _, cell := range row {
			if strings.HasPrefix(cell, "{") || strings.HasPrefix(cell, "[") {
				t.Fatalf("a cell holds structured data: %q", cell)
			}
		}
	}

	cr := judge(ModeCompare, false, compareResult("c", samples(time.Millisecond, 20, 0), samples(time.Millisecond, 20, 0)))
	out.Reset()
	if err := WriteCSV(&out, cr); err != nil {
		t.Fatal(err)
	}
	rows, _ = csv.NewReader(&out).ReadAll()
	find(rows, map[string]string{"record": "stat", "side": "base", "metric": "latency", "statistic": "median"})
	cmp := find(rows, map[string]string{"record": "comparison", "metric": "latency"})
	if cmp[csvCol("base")] != "1000000" || cmp[csvCol("difference")] != "0" || cmp[csvCol("change_percent")] == "" || cmp[csvCol("verdict")] != "pass" {
		t.Fatalf("comparison row = %q", cmp)
	}
}

func TestSamplesCSV(t *testing.T) {
	t.Parallel()
	b := runResult("s", "", map[string][]time.Duration{"x": {time.Millisecond, 2 * time.Millisecond}}, "x")
	b.Benchmark.Metrics = config.Metrics{Memory: true, Throughput: &config.Work{Value: 10, Unit: "records"}}
	m := b.Commands[0].Sides[runner.SideHead]
	m.PeakRSS = []int64{1 << 20, 2 << 20}
	m.Work = []float64{10, 10}
	r := judge(ModeRun, false, b)
	var out bytes.Buffer
	if err := WriteSamplesCSV(&out, r); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&out).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		SamplesCSVHeader,
		{"suite | one", "s", "x", "head", "1", "latency", "ns", "1000000"},
		{"suite | one", "s", "x", "head", "2", "latency", "ns", "2000000"},
		{"suite | one", "s", "x", "head", "1", "throughput", "records/s", "10000"},
		{"suite | one", "s", "x", "head", "2", "throughput", "records/s", "5000"},
		{"suite | one", "s", "x", "head", "1", "peak_rss", "bytes", "1048576"},
		{"suite | one", "s", "x", "head", "2", "peak_rss", "bytes", "2097152"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %q", rows)
	}
	for i := range want {
		if strings.Join(rows[i], ",") != strings.Join(want[i], ",") {
			t.Errorf("row %d = %q, want %q", i, rows[i], want[i])
		}
	}
}

func TestGitHubSummary(t *testing.T) {
	t.Parallel()
	r := judge(ModeCompare, false,
		compareResult("ok", samples(10*time.Millisecond, 20, 0), samples(10*time.Millisecond, 20, 0)),
		compareResult("slow", samples(10*time.Millisecond, 20, 0), samples(30*time.Millisecond, 20, 0)))
	var out bytes.Buffer
	if err := WriteGitHubSummary(&out, r); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"## ❌ himorime benchmark comparison: performance regression", "| Benchmark | Command | Base | Head |", "| slow | tool |", "REGRESSION", "seed 42"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary lacks %q:\n%s", want, s)
		}
	}
	ok := judge(ModeRun, false, runResult("x", "", map[string][]time.Duration{"a": samples(time.Millisecond, 10, 0)}, "a"))
	out.Reset()
	_ = WriteGitHubSummary(&out, ok)
	if !strings.HasPrefix(out.String(), "## ✅ himorime benchmarks: no regression") {
		t.Fatalf("summary = %s", out.String())
	}
}

func reportSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema.Report))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schema.ReportURL, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(schema.ReportURL)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestJSONMatchesReportSchema(t *testing.T) {
	t.Parallel()
	s := reportSchema(t)
	failing := runResult("fails", "a", map[string][]time.Duration{"a": samples(time.Millisecond, 10, 0), "b": nil}, "a", "b")
	failing.Commands[1].Sides[runner.SideHead].Failure = &runner.Failure{Kind: runner.FailExitCode, ExitCode: 1, Message: "exited"}
	budgeted := runResult("budget", "", map[string][]time.Duration{"a": samples(time.Millisecond, 10, 0)}, "a")
	budgeted.Benchmark.Budgets = []config.Budget{latencyBudget(t, "a", metric.AggMean, "<= 1s")}
	reports := map[string]*Report{
		"run":     judge(ModeRun, false, failing, budgeted),
		"compare": judge(ModeCompare, true, compareResult("c", samples(time.Millisecond, 20, 0.1), samples(time.Millisecond, 20, 0.1))),
		"empty":   judge(ModeRun, false),
	}
	reports["compare"].Git = &Git{HeadSHA: "abc", BaseRef: "main", BaseSHA: "def", BaseSource: "--against", Dirty: true}
	reports["run"].Environment.Tools = []Tool{{Name: "jc", Version: "jc version 1.25.7"}, {Name: "jo", Version: "1.9"}}
	buildFailed := &Report{Environment: Environment{LogicalCPUs: 1}}
	Judge(buildFailed, []SuiteInput{{Suite: &config.Suite{Name: "b"}, File: "y.yaml", BuildFailure: &runner.Failure{Kind: runner.FailBuild, Message: "no"}}}, Options{Mode: ModeCompare})
	reports["build failed"] = buildFailed
	for name, r := range reports {
		var buf bytes.Buffer
		if err := WriteJSON(&buf, r); err != nil {
			t.Fatal(err)
		}
		inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Validate(inst); err != nil {
			t.Errorf("%s report does not match schema/report.schema.json: %v\n%s", name, err, buf.String())
		}
	}
	if buildFailed.Summary.ExitCode != exitcode.Execution || buildFailed.Suites[0].Result != ResultError {
		t.Fatalf("build failure summary = %+v", buildFailed.Summary)
	}
}

func TestJSONKeepsRawNanoseconds(t *testing.T) {
	t.Parallel()
	raw := []time.Duration{1_234_567, 1_234_568, 999}
	r := judge(ModeRun, false, runResult("raw", "", map[string][]time.Duration{"a": raw}, "a"))
	var buf bytes.Buffer
	if err := WriteJSON(&buf, r); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		SchemaVersion string `json:"schema_version"`
		Suites        []struct {
			Benchmarks []struct {
				Commands []struct {
					Head map[string]json.RawMessage `json:"head"`
				} `json:"commands"`
			} `json:"benchmarks"`
		} `json:"suites"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	head := decoded.Suites[0].Benchmarks[0].Commands[0].Head
	var metrics map[string]struct {
		Unit    string    `json:"unit"`
		Samples []float64 `json:"samples"`
		Stats   struct {
			Median float64 `json:"median"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(head["metrics"], &metrics); err != nil {
		t.Fatal(err)
	}
	lat := metrics["latency"]
	// Samples up to 24h in nanoseconds stay below 2^53, so a JSON number
	// carries them exactly.
	if decoded.SchemaVersion != "1" || lat.Unit != "ns" || len(lat.Samples) != 3 || lat.Samples[0] != 1_234_567 || lat.Samples[2] != 999 || lat.Stats.Median != 1_234_567 {
		t.Fatalf("decoded = %+v", lat)
	}
	// Latency is described once, under metrics; no duplicate top-level
	// copies remain.
	for _, gone := range []string{"samples_ns", "median_ns", "mean_ns", "stddev_ns", "min_ns", "max_ns", "cv"} {
		if _, ok := head[gone]; ok {
			t.Errorf("head still has %s", gone)
		}
	}
	if strings.Contains(buf.String(), "hostname") || strings.Contains(buf.String(), "PATH") {
		t.Fatal("the report must not carry host or environment details")
	}
}

func TestWriteDispatch(t *testing.T) {
	t.Parallel()
	r := judge(ModeRun, false, runResult("x", "", map[string][]time.Duration{"a": samples(time.Millisecond, 10, 0)}, "a"))
	for _, f := range config.Formats() {
		var buf bytes.Buffer
		if err := Write(&buf, f, r, false); err != nil || buf.Len() == 0 {
			t.Errorf("Write(%s) = %v, %d bytes", f, err, buf.Len())
		}
	}
	if err := Write(&bytes.Buffer{}, "xml", r, false); err == nil {
		t.Error("an unknown format was accepted")
	}
	if err := WriteJSON(failingWriter{}, r); err == nil {
		t.Error("a write error was swallowed")
	}
	if err := WriteCSV(failingWriter{}, r); err == nil {
		t.Error("a CSV write error was swallowed")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWrite }

var errWrite = &json.UnsupportedValueError{Str: "write failed"}

func TestResultOrder(t *testing.T) {
	t.Parallel()
	if worst(ResultPass, ResultError) != ResultError || worst(ResultRegression, ResultInconclusive) != ResultRegression || worst(ResultImproved, ResultPass) != ResultImproved {
		t.Fatal("severity order")
	}
	if verdictLabel(ResultOverBudget) != "OVER BUDGET" {
		t.Fatal("label")
	}
}

// newSuiteReport judges a comparison of an existing suite and of a suite the
// base revision does not have, measured in the head only, within its budget
// or over it.
func newSuiteReport(t *testing.T, budget string) *Report {
	t.Helper()
	existing := compareResult("ok", samples(10*time.Millisecond, 20, 0), samples(10*time.Millisecond, 20, 0))
	fresh := runResult("fresh", "", map[string][]time.Duration{"app": samples(5*time.Millisecond, 10, 0)}, "app")
	fresh.Benchmark.Budgets = []config.Budget{latencyBudget(t, "app", metric.AggMedian, budget)}
	r := &Report{Environment: Environment{LogicalCPUs: 1}}
	Judge(r, []SuiteInput{
		{Suite: &config.Suite{Name: "old", Benchmarks: []config.Benchmark{existing.Benchmark}}, File: "himorime.yaml", Benchmarks: []runner.BenchmarkResult{existing}},
		{Suite: &config.Suite{Name: "added", Benchmarks: []config.Benchmark{fresh.Benchmark}}, File: "bench/himorime.yaml", NewInHead: true, Benchmarks: []runner.BenchmarkResult{fresh}},
	}, Options{Mode: ModeCompare, Seed: 7})
	return r
}

func TestJudgeSuiteNewInHead(t *testing.T) {
	t.Parallel()
	r := newSuiteReport(t, "<= 1s")
	s := r.Suites[1]
	if !s.NewInHead || s.Error != nil || s.Result != ResultPass || len(s.Benchmarks) != 1 || s.GeometricMean != nil {
		t.Fatalf("new suite = %+v", s)
	}
	c := s.Benchmarks[0].Commands[0]
	if c.Base != nil || c.Comparisons != nil || c.Head == nil || len(c.Budgets) != 1 || c.Budgets[0].Status != BudgetPass {
		t.Fatalf("a new suite's command is judged as in a plain run: %+v", c)
	}
	if r.Suites[0].NewInHead || len(r.Suites[0].Benchmarks) != 1 {
		t.Fatalf("existing suite = %+v", r.Suites[0])
	}
	if r.Summary.Suites != 2 || r.Summary.NewSuites != 1 || r.Summary.Benchmarks != 2 || r.Summary.Commands != 2 || r.Summary.Pass != 2 || r.Summary.ExitCode != exitcode.OK {
		t.Fatalf("summary = %+v", r.Summary)
	}

	// An exceeded budget of the new suite fails the comparison.
	r = newSuiteReport(t, "<= 1ms")
	if r.Suites[1].Result != ResultOverBudget || r.Summary.OverBudget != 1 || r.Summary.ExitCode != exitcode.Failed {
		t.Fatalf("over budget: suite %+v, summary %+v", r.Suites[1], r.Summary)
	}

	// So does its failed build.
	only := &Report{}
	Judge(only, []SuiteInput{{Suite: &config.Suite{Name: "added"}, File: "bench/himorime.yaml", NewInHead: true, BuildFailure: &runner.Failure{Kind: runner.FailBuild, Message: "exit status 2"}}}, Options{Mode: ModeCompare})
	if !only.Suites[0].NewInHead || only.Suites[0].Result != ResultError || only.Summary.NewSuites != 1 || only.Summary.ExitCode != exitcode.Execution {
		t.Fatalf("a new suite whose build failed = %+v", only.Summary)
	}
}

func TestRenderSuiteNewInHead(t *testing.T) {
	t.Parallel()
	r := newSuiteReport(t, "<= 1s")
	const line = "new in this revision: bench does not exist in the base revision, so only this revision is measured and its budgets are checked"

	var term bytes.Buffer
	if err := WriteTerminal(&term, r, TerminalOptions{}); err != nil {
		t.Fatal(err)
	}
	text := term.String()
	_, addedText, found := strings.Cut(text, "suite: added")
	if !found {
		t.Fatalf("terminal lacks the new suite:\n%s", text)
	}
	addedText = "suite: added" + addedText
	for _, want := range []string{
		"suite: added (bench/himorime.yaml)\n" + line + "\nlatency\nBENCHMARK  COMMAND  MEDIAN",
		"\nbudgets\n",
		"PASS",
	} {
		if !strings.Contains(addedText, want) {
			t.Errorf("terminal lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(addedText, "CONFIDENCE") {
		t.Errorf("a new suite has nothing to compare:\n%s", text)
	}
	if !strings.HasSuffix(text, "2 passed · 2 benchmarks · 1 suite new in this revision · seed 7 · exit 0\n") {
		t.Errorf("terminal summary line:\n%s", text)
	}

	var md bytes.Buffer
	if err := WriteMarkdown(&md, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md.String(), "## added\n\nNew in this revision: bench does not exist in the base revision, so only this revision is measured and its budgets are checked.\n\n") ||
		!strings.Contains(md.String(), "| fresh | app |") || strings.Contains(md.String(), "**") {
		t.Errorf("markdown:\n%s", md.String())
	}

	var summary bytes.Buffer
	if err := WriteGitHubSummary(&summary, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary.String(), " · 1 suite new in this revision\n") || !strings.Contains(summary.String(), "New in this revision: bench") {
		t.Errorf("job summary:\n%s", summary.String())
	}
	summary.Reset()
	if err := WriteGitHubSummary(&summary, newSuiteReport(t, "<= 1ms")); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(summary.String(), "## ❌ himorime benchmark comparison: budget exceeded\n") {
		t.Errorf("job summary of a new suite over its budget:\n%s", summary.String())
	}

	var ann bytes.Buffer
	if err := WriteAnnotations(&ann, r); err != nil {
		t.Fatal(err)
	}
	if ann.String() != "::notice file=bench/himorime.yaml,title=himorime%3A suite new in this revision::suite added: "+line+"\n" {
		t.Errorf("annotations = %q", ann.String())
	}

	var out bytes.Buffer
	if err := WriteCSV(&out, r); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&out).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	var added [][]string
	for _, row := range rows[1:] {
		if row[csvCol("suite")] == "added" {
			added = append(added, row)
		}
	}
	if len(added) < 2 || added[0][csvCol("record")] != "new_in_head" || added[0][csvCol("result")] != "pass" || added[0][csvCol("reason")] != line {
		t.Fatalf("csv rows of the new suite = %q", added)
	}
	var budgets int
	for _, row := range added[1:] {
		switch row[csvCol("record")] {
		case "comparison":
			t.Errorf("a new suite has no comparison rows: %q", row)
		case "budget":
			budgets++
		}
		if row[csvCol("side")] == "base" {
			t.Errorf("a new suite has no base rows: %q", row)
		}
	}
	if budgets != 1 {
		t.Errorf("csv budget rows of the new suite = %d, want 1", budgets)
	}

	var js bytes.Buffer
	if err := WriteJSON(&js, r); err != nil {
		t.Fatal(err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(js.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if err := reportSchema(t).Validate(inst); err != nil {
		t.Errorf("report does not match schema/report.schema.json: %v\n%s", err, js.String())
	}
	var decoded struct {
		Suites []struct {
			NewInHead *bool `json:"new_in_head"`
		} `json:"suites"`
		Summary struct {
			NewSuites *int `json:"new_suites"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(js.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Suites[0].NewInHead == nil || *decoded.Suites[0].NewInHead || !*decoded.Suites[1].NewInHead || decoded.Summary.NewSuites == nil || *decoded.Summary.NewSuites != 1 {
		t.Errorf("json:\n%s", js.String())
	}
}

// throughputCompareResult builds a comparison whose throughput divides the
// work of each side by its latency, as a suite with metrics.throughput.work
// does.
func throughputCompareResult(name string, baseWork, headWork float64, base, head []time.Duration) runner.BenchmarkResult {
	c := cmd("tool")
	b := config.Benchmark{Name: name, Commands: []config.Command{c}, Regression: regression()}
	b.Metrics = config.Metrics{Throughput: &config.Work{FileSize: "input.txt", Unit: "bytes"}}
	work := func(w float64, n int) []float64 {
		out := make([]float64, n)
		for i := range out {
			out[i] = w
		}
		return out
	}
	return runner.BenchmarkResult{
		Benchmark: b,
		Rounds:    len(head),
		Commands: []runner.CommandResult{{Command: c, Sides: map[string]*runner.Measurement{
			runner.SideBase: {Samples: base, Work: work(baseWork, len(base))},
			runner.SideHead: {Samples: head, Work: work(headWork, len(head))},
		}}},
	}
}

func TestMetricSummaryRecordsTheWorkThatWasMeasured(t *testing.T) {
	t.Parallel()
	b := throughputCompareResult("work", 1000, 250, samples(10*time.Millisecond, 20, 0.02), samples(10*time.Millisecond, 20, 0.02))
	r := judge(ModeCompare, false, b)
	c := r.Suites[0].Benchmarks[0].Commands[0]
	for _, tc := range []struct {
		side *Measurement
		want float64
		name string
	}{{c.Base, 1000, "base"}, {c.Head, 250, "head"}} {
		w := tc.side.Metrics["throughput"].Work
		if w == nil {
			t.Fatalf("%s: throughput has no work", tc.name)
		}
		if w.MeasuredMin == nil || w.MeasuredMax == nil {
			t.Fatalf("%s: work = %+v, want the measured amount", tc.name, w)
		}
		if *w.MeasuredMin != tc.want || *w.MeasuredMax != tc.want {
			t.Errorf("%s: measured work = %v..%v, want %v", tc.name, *w.MeasuredMin, *w.MeasuredMax, tc.want)
		}
	}
}

func TestCompareRefusesAVerdictWhenTheWorkDiffers(t *testing.T) {
	t.Parallel()
	b := throughputCompareResult("shrunk", 1000, 250, samples(10*time.Millisecond, 20, 0.02), samples(10*time.Millisecond, 20, 0.02))
	r := judge(ModeCompare, false, b)
	c := r.Suites[0].Benchmarks[0].Commands[0]
	tp := c.Comparisons["throughput"]
	if tp == nil {
		t.Fatal("no throughput comparison")
	}
	if tp.Verdict != string(stats.VerdictInconclusive) {
		t.Errorf("verdict = %s, want inconclusive; a smaller input is not a slower program", tp.Verdict)
	}
	if !strings.Contains(tp.Reason, "1000") || !strings.Contains(tp.Reason, "250") {
		t.Errorf("reason = %q, want both amounts of work", tp.Reason)
	}
	if lat := c.Comparisons["latency"]; lat == nil || lat.Verdict != string(stats.VerdictPass) {
		t.Errorf("latency comparison = %+v, want it untouched", lat)
	}
	if r.Summary.ExitCode != exitcode.OK {
		t.Errorf("exit code = %d, want 0; the throughput change is not a regression", r.Summary.ExitCode)
	}
}

func TestCompareKeepsTheVerdictWhenTheWorkIsTheSame(t *testing.T) {
	t.Parallel()
	b := throughputCompareResult("slower", 1000, 1000, samples(10*time.Millisecond, 20, 0.02), samples(20*time.Millisecond, 20, 0.02))
	r := judge(ModeCompare, false, b)
	tp := r.Suites[0].Benchmarks[0].Commands[0].Comparisons["throughput"]
	if tp == nil || tp.Verdict != string(stats.VerdictRegression) {
		t.Fatalf("throughput comparison = %+v, want a regression", tp)
	}
	if r.Summary.ExitCode != exitcode.Failed {
		t.Errorf("exit code = %d, want 1", r.Summary.ExitCode)
	}
}
