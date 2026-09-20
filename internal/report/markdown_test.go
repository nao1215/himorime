package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/nao1215/himorime/internal/metric"
	"github.com/nao1215/himorime/internal/runner"
)

// mixedReport has a latency-only benchmark and one that measures every
// metric, with different command names, so no geometric mean is possible.
func mixedReport(t *testing.T, adjust func(detect, csv *runner.BenchmarkResult)) *Report {
	t.Helper()
	detect := runResult("detect", "", map[string][]time.Duration{"detected": samples(2*time.Millisecond, 10, 0), "tool": samples(3*time.Millisecond, 10, 0)}, "detected", "tool")
	csv := metricsResult("csv 100k rows", 10, 40*time.Millisecond, 30*time.Millisecond, 5*time.Millisecond, 8<<20, 1000)
	if adjust != nil {
		adjust(&detect, &csv)
	}
	return judge(ModeRun, false, detect, csv)
}

func markdownOf(t *testing.T, r *Report) string {
	t.Helper()
	var out bytes.Buffer
	if err := WriteMarkdown(&out, r); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// tableAfter returns the table that follows a heading.
func tableAfter(t *testing.T, md, heading string) string {
	t.Helper()
	_, rest, ok := strings.Cut(md, heading+"\n\n")
	if !ok {
		t.Fatalf("markdown lacks %q:\n%s", heading, md)
	}
	table, _, _ := strings.Cut(rest, "\n\n")
	return table
}

func TestMarkdownOmitsRowsWithoutValues(t *testing.T) {
	t.Parallel()
	md := markdownOf(t, mixedReport(t, nil))
	latency := tableAfter(t, md, "### Latency")
	if !strings.Contains(latency, "| detect | detected |") || !strings.Contains(latency, "| csv 100k rows | tool |") {
		t.Errorf("the latency table lists every command:\n%s", latency)
	}
	for _, heading := range []string{"### Throughput", "### CPU", "### Memory"} {
		table := tableAfter(t, md, heading)
		if strings.Contains(table, "| detect |") || !strings.Contains(table, "| csv 100k rows | tool |") {
			t.Errorf("%s table:\n%s", heading, table)
		}
		if strings.Contains(table, " - | - |") {
			t.Errorf("%s table has a row without values:\n%s", heading, table)
		}
	}
}

func TestMarkdownResultColumn(t *testing.T) {
	t.Parallel()
	md := markdownOf(t, mixedReport(t, nil))
	if !strings.Contains(md, "| Benchmark | Command | Median | P95 | Mean | Stddev | Min | Max | Runs | Relative |\n|---|---|--:|--:|--:|--:|--:|--:|--:|--:|\n") {
		t.Errorf("latency header without a Result column:\n%s", md)
	}
	if !strings.Contains(md, "| Benchmark | Command | Median | Mean | Min | P95 |\n|---|---|--:|--:|--:|--:|\n") {
		t.Errorf("throughput header without a Result column:\n%s", md)
	}
	if strings.Contains(md, "Result") || strings.Contains(md, "PASS") {
		t.Errorf("nothing was judged, so no Result column is shown:\n%s", md)
	}
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "| detect | detected |") && strings.Count(line, "|") != 11 {
			t.Errorf("row has the wrong number of cells: %s", line)
		}
	}

	budgeted := markdownOf(t, mixedReport(t, func(_, csv *runner.BenchmarkResult) {
		csv.Benchmark.Budgets = append(csv.Benchmark.Budgets, latencyBudget(t, "tool", "median", "<= 1s"))
	}))
	for _, want := range []string{
		"| Benchmark | Command | Median | P95 | Mean | Stddev | Min | Max | Runs | Relative | Result |",
		"| Benchmark | Command | Median | Mean | Min | P95 | Result |",
		"| Benchmark | Command | Peak RSS (median) | Peak RSS (max) | Result |",
		"| detect | detected |",
		"### Budgets",
	} {
		if !strings.Contains(budgeted, want) {
			t.Errorf("with a budget, markdown lacks %q:\n%s", want, budgeted)
		}
	}

	failed := markdownOf(t, mixedReport(t, func(detect, _ *runner.BenchmarkResult) {
		detect.Commands[0].Sides[runner.SideHead].Failure = &runner.Failure{Kind: runner.FailExitCode, ExitCode: 1, Message: "exited with status 1"}
	}))
	if !strings.Contains(failed, "| Relative | Result |") || !strings.Contains(failed, " | ERROR |") {
		t.Errorf("with an error, the Result column is shown:\n%s", failed)
	}
}

func TestMarkdownEmptyBenchmarkUsesErrorCell(t *testing.T) {
	t.Parallel()
	r := &Report{Suites: []Suite{{Name: "suite", Benchmarks: []Benchmark{
		{Name: "no commands"},
		{Name: "failed setup", Error: &Error{Kind: string(runner.FailSetup), Message: "setup failed"}},
	}}}}
	md := markdownOf(t, r)
	for _, name := range []string{"no commands", "failed setup"} {
		if !strings.Contains(md, "| "+name+" | - | - | - | - | - | - | - | - | - | ERROR |") {
			t.Errorf("empty benchmark %q is not rendered as an error row:\n%s", name, md)
		}
	}
	r.Mode = ModeCompare
	md = markdownOf(t, r)
	for _, name := range []string{"no commands", "failed setup"} {
		if !strings.Contains(md, "| "+name+" | - | - | - | - | - | - | - | - | ERROR |") {
			t.Errorf("empty comparison benchmark %q is not rendered as an error row:\n%s", name, md)
		}
	}
}

func TestMarkdownSuiteErrorsAndCompareRows(t *testing.T) {
	t.Parallel()
	comparison := &MetricComparison{Metric: string(metric.Latency), Better: "lower", Base: floatPtr(1), Head: floatPtr(2), Difference: floatPtr(1), Verdict: string(ResultRegression), Gate: true}
	r := &Report{
		Mode: ModeCompare,
		Suites: []Suite{
			{Name: "failed", Error: &Error{Kind: "build_failed", Message: "compiler failed"}},
			{
				Name: "compare",
				Benchmarks: []Benchmark{
					{Name: "empty"},
					{
						Name: "partial",
						Commands: []Command{{
							Name: "tool", Result: ResultRegression,
							Comparisons: map[string]*MetricComparison{string(metric.Latency): comparison},
							Budgets:     []BudgetCheck{{Metric: string(metric.Latency), Aggregation: "median", Status: BudgetFail, Operator: "<=", Limit: 1, Actual: floatPtr(2)}},
						}},
					},
				},
			},
		},
	}
	md := markdownOf(t, r)
	for _, want := range []string{"**Error:** build failed: compiler failed", "| empty | - | - | - | - | - | - | - | - | ERROR |", "| Benchmark | Command | Base | Head", "## Budgets (head)", "| partial | tool |"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
}

func TestMarkdownNotesIncludeBenchmarkAndMeasurementStderr(t *testing.T) {
	t.Parallel()
	r := judge(ModeRun, false, runResult("tool", "", map[string][]time.Duration{"tool": samples(time.Millisecond, 3, 0)}, "tool"))
	b := &r.Suites[0].Benchmarks[0]
	b.Error = &Error{Kind: "setup_failed", Message: "benchmark failed", Stderr: "benchmark-1\nbenchmark-2\nbenchmark-3\nbenchmark-4"}
	b.Commands[0].Head.Error = &Error{Kind: "exit_code", Message: "command failed", Stderr: "command-1\ncommand-2\ncommand-3\ncommand-4"}
	r.Environment.Tools = []Tool{{Name: "tool", Version: "tool 1.0"}}
	md := markdownOf(t, r)
	for _, want := range []string{`stderr: benchmark-2 \| benchmark-3 \| benchmark-4`, `stderr: command-2 \| command-3 \| command-4`, "- tool: tool 1.0"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown notes lack %q:\n%s", want, md)
		}
	}
}

func TestMarkdownExplainsComparisonBoundaryStates(t *testing.T) {
	t.Parallel()
	base, head, difference, actual := 10.0, 12.0, 2.0, 20.0
	r := &Report{
		Mode: ModeCompare,
		Suites: []Suite{
			{
				Name: "boundaries",
				Benchmarks: []Benchmark{
					{
						Name: "case",
						Commands: []Command{
							{
								Name:   "tool",
								Result: ResultRegression,
								Comparisons: map[string]*MetricComparison{
									string(metric.Throughput): {Metric: string(metric.Throughput), Unit: "records/s", Verdict: string(ResultRegression), DerivedFrom: string(metric.Latency), Gate: false, Base: &base, Head: &head, Difference: &difference},
									string(metric.CPUTotal):   {Metric: string(metric.CPUTotal), Unit: "ns", Verdict: VerdictSkipped, Gate: true, Reason: "unsupported on base"},
									string(metric.CPUUser):    {Metric: string(metric.CPUUser), Unit: "ns", Verdict: string(ResultRegression), Gate: false, Base: &base, Head: &head, Difference: &difference},
									string(metric.PeakRSS):    {Metric: string(metric.PeakRSS), Unit: "bytes", Verdict: string(ResultInconclusive), Gate: true, Reason: "too noisy", Base: &base, Head: &head, Difference: &difference},
								},
								Budgets: []BudgetCheck{
									{Metric: string(metric.Latency), Aggregation: "median", Unit: "ns", Operator: "<=", Limit: 10, Actual: &actual, Status: BudgetFail},
									{Metric: string(metric.PeakRSS), Aggregation: "max", Unit: "bytes", Status: BudgetSkipped, Reason: ReasonAtFloor},
								},
							},
						},
					},
				},
			},
		},
		Summary: Summary{Regression: 1, Benchmarks: 1, Commands: 1, ExitCode: 1},
	}
	md := markdownOf(t, r)
	for _, want := range []string{
		"REGRESSION (FROM LATENCY)",
		"cpu total skipped: unsupported on base",
		"cpu user regressed beyond its tolerance, but gate: false keeps it from failing the run",
		"peak rss inconclusive: too noisy",
		"budget median &lt;= 10ns not met (measured 20ns)",
		"budget peak rss max could not be assessed: the peak RSS is at or below the measurement floor",
		"(NOT GATED) marks a metric with gate: false",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
}

func TestMarkdownHasNoGeometricMeanNote(t *testing.T) {
	t.Parallel()
	r := mixedReport(t, nil)
	if GeometricMeanNote(r.Suites[0]) == "" {
		t.Fatal("the fixture must have a geometric mean note")
	}
	if md := markdownOf(t, r); strings.Contains(md, "geometric mean") {
		t.Errorf("markdown shows the geometric mean note:\n%s", md)
	}
	var summary bytes.Buffer
	if err := WriteGitHubSummary(&summary, r); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(summary.String(), "geometric mean") {
		t.Errorf("job summary shows the geometric mean note:\n%s", summary.String())
	}
	var term bytes.Buffer
	if err := WriteTerminal(&term, r, TerminalOptions{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(term.String(), "1.00x") {
		t.Errorf("the compact terminal shows a redundant ratio:\n%s", term.String())
	}
}

func TestMarkdownTools(t *testing.T) {
	t.Parallel()
	r := mixedReport(t, nil)
	md := markdownOf(t, r)
	if !strings.Contains(md, ", seed 42.\n") || !strings.HasSuffix(md, "exit 0\n") {
		t.Errorf("without tools the footer and summary are present:\n%s", md)
	}
	r.Environment.Tools = []Tool{{Name: "jc", Version: "jc version 1.25.7"}, {Name: "jo", Version: "1.9"}}
	r.Git = &Git{BaseSHA: "0123456789abcdef", HeadSHA: "fedcba9876543210", Dirty: true}
	md = markdownOf(t, r)
	if !strings.Contains(md, ", seed 42.\n\n- jc: jc version 1.25.7\n- jo: 1.9\n\n") || !strings.HasSuffix(md, "exit 0\n") {
		t.Errorf("tools footer:\n%s", md)
	}
	if !strings.Contains(md, "base 0123456789ab, head fedcba987654 with uncommitted changes") {
		t.Errorf("git revisions were not shortened in markdown:\n%s", md)
	}
	var term bytes.Buffer
	if err := WriteTerminal(&term, r, TerminalOptions{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(term.String(), "jc version") {
		t.Errorf("the terminal does not print tool versions:\n%s", term.String())
	}
}

func TestMarkdownSectionHeadingLevels(t *testing.T) {
	t.Parallel()
	r := mixedReport(t, nil)
	for level, want := range map[int][]string{
		2: {"## suite \\| one\n", "### Latency\n"},
		4: {"#### suite \\| one\n", "##### Latency\n"},
		6: {"###### suite \\| one\n", "###### Latency\n"},
	} {
		md := renderMarkdown(r, level)
		for _, w := range want {
			if !strings.HasPrefix(md, want[0]) || !strings.Contains(md, "\n"+w) && !strings.HasPrefix(md, w) {
				t.Errorf("level %d lacks %q:\n%s", level, w, md)
			}
		}
		if strings.Contains(md, "#######") {
			t.Errorf("level %d goes deeper than 6:\n%s", level, md)
		}
	}
}
