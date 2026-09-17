package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

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
	if !strings.Contains(term.String(), "no geometric mean: ") {
		t.Errorf("the terminal keeps the note:\n%s", term.String())
	}
}

func TestMarkdownTools(t *testing.T) {
	t.Parallel()
	r := mixedReport(t, nil)
	md := markdownOf(t, r)
	if !strings.HasSuffix(md, ", seed 42.\n") {
		t.Errorf("without tools the footer is one line:\n%s", md)
	}
	r.Environment.Tools = []Tool{{Name: "jc", Version: "jc version 1.25.7"}, {Name: "jo", Version: "1.9"}}
	md = markdownOf(t, r)
	if !strings.HasSuffix(md, ", seed 42.\n\n- jc: jc version 1.25.7\n- jo: 1.9\n") {
		t.Errorf("tools footer:\n%s", md)
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
