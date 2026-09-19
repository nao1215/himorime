package prcomment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nao1215/himorime/internal/report"
)

const workflowPayload = `{
  "action":"completed",
  "repository":{"id":123,"full_name":"octo/bench"},
  "workflow_run":{
    "id":9001,"name":"benchmark","workflow_id":44,"run_number":12,"run_attempt":2,
    "event":"pull_request","status":"completed","conclusion":"failure","html_url":"https://github.example/octo/bench/actions/runs/9001",
    "repository":{"id":123,"full_name":"octo/bench"},
    "pull_requests":[{"number":7,"base":{"repo":{"id":123,"name":"bench","url":"https://api.github.example/repos/octo/bench"}},"head":{"repo":{"id":456,"name":"bench","url":"https://api.github.example/repos/contributor/bench"}}}]
  }
}`

func env(values map[string]string) Env {
	return FromLookup(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
}

func files(content string) func(string) ([]byte, error) {
	return func(string) ([]byte, error) { return []byte(content), nil }
}

func workflowEnv() Env {
	return env(map[string]string{
		"GITHUB_ACTIONS": "true", "GITHUB_EVENT_NAME": "workflow_run", "GITHUB_EVENT_PATH": "/event.json",
		"GITHUB_REPOSITORY": "octo/bench", "GITHUB_API_URL": "https://api.github.example", "GITHUB_TOKEN": "secret",
	})
}

func TestResolveWorkflowRun(t *testing.T) {
	t.Parallel()
	run, err := workflowEnv().ResolveWorkflowRun(files(workflowPayload))
	if err != nil || run.Repository != "octo/bench" || run.PullRequest != 7 || run.WorkflowID != 44 || run.RunNumber != 12 || run.RunAttempt != 2 {
		t.Fatalf("run = %+v, err = %v", run, err)
	}
	tests := []struct{ name, payload, event, repo, want string }{
		{"wrong event", workflowPayload, "pull_request", "octo/bench", "only available"},
		{"foreign repository", strings.Replace(workflowPayload, `"full_name":"octo/bench"`, `"full_name":"evil/repo"`, 1), "workflow_run", "octo/bench", "does not match"},
		{"not a pull request workflow", strings.Replace(workflowPayload, `"event":"pull_request"`, `"event":"push"`, 1), "workflow_run", "octo/bench", "want pull_request"},
		{"no associated pull request", strings.Replace(workflowPayload, `"pull_requests":[`, `"pull_requests":[],"ignored":[`, 1), "workflow_run", "octo/bench", "exactly one"},
		{"foreign pull request", strings.Replace(workflowPayload, `"id":123,"name":"bench"`, `"id":999,"name":"bench"`, 1), "workflow_run", "octo/bench", "does not belong"},
		{"missing base repository ID", strings.Replace(workflowPayload, `"id":123,"name":"bench"`, `"name":"bench"`, 1), "workflow_run", "octo/bench", "does not belong"},
		{"zero base repository ID", strings.Replace(workflowPayload, `"id":123,"name":"bench"`, `"id":0,"name":"bench"`, 1), "workflow_run", "octo/bench", "does not belong"},
		{"missing event repository ID", strings.Replace(workflowPayload, `"id":123,`, ``, 1), "workflow_run", "octo/bench", "does not match"},
		{"zero repository IDs", strings.ReplaceAll(workflowPayload, `"id":123`, `"id":0`), "workflow_run", "octo/bench", "does not match"},
		{"negative repository IDs", strings.ReplaceAll(workflowPayload, `"id":123`, `"id":-123`), "workflow_run", "octo/bench", "does not match"},
		{"source repository mismatch", strings.Replace(workflowPayload, `    "repository":{"id":123,`, `    "repository":{"id":999,`, 1), "workflow_run", "octo/bench", "does not match"},
		{"missing source repository ID", strings.Replace(workflowPayload, `    "repository":{"id":123,`, `    "repository":{`, 1), "workflow_run", "octo/bench", "does not match"},
		{"foreign source repository name", strings.Replace(workflowPayload, `    "repository":{"id":123,"full_name":"octo/bench"}`, `    "repository":{"id":123,"full_name":"evil/repo"}`, 1), "workflow_run", "octo/bench", "does not match"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := env(map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_EVENT_NAME": tt.event, "GITHUB_EVENT_PATH": "/e", "GITHUB_REPOSITORY": tt.repo})
			_, err := e.ResolveWorkflowRun(files(tt.payload))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRenderCommentEscapesHostileNamesAndIsBounded(t *testing.T) {
	t.Parallel()
	rep := &report.Report{Mode: report.ModeCompare, Summary: report.Summary{Regression: 1, ExitCode: 1}}
	rep.Suites = []report.Suite{{Name: "@org/team suite|<script>alert(1)</script>", Error: &report.Error{Kind: "build", Message: "failed"}, Result: report.ResultRegression, Benchmarks: []report.Benchmark{{Name: "line\nbreak", Result: report.ResultRegression, Commands: []report.Command{{Name: "[bad](javascript:x)", Result: report.ResultRegression}}}}}}
	run := WorkflowRun{RunID: 9001, WorkflowID: 44, WorkflowName: "@org/team bench [x]", RunNumber: 12, RunAttempt: 2, RunURL: "https://github.example/run/1", Conclusion: "failure"}
	body, err := renderComment(rep, run, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"<script>", "\nline\nbreak", "[bad](javascript:x)", "@org/team"} {
		if strings.Contains(body, bad) {
			t.Errorf("body contains unsafe %q:\n%s", bad, body)
		}
	}
	for _, want := range []string{"<!-- himorime:benchmark-report:v2 run_id=9001 workflow_id=44 run_number=12 run_attempt=2 -->", "execution or measurement error", "&#64;org/team", "suite\\|&lt;script&gt;", "<sub>Generated by [himorime](https://github.com/nao1215/himorime)</sub>"} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q:\n%s", want, body)
		}
	}
	if len(body) > maxCommentBytes {
		t.Fatalf("body has %d bytes", len(body))
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "<sub>Generated by [himorime](https://github.com/nao1215/himorime)</sub>") {
		t.Fatalf("attribution is not the final visible line:\n%s", body)
	}
}

func TestRenderCommentCanHideAttributionFooter(t *testing.T) {
	t.Parallel()
	rep := &report.Report{Mode: report.ModeRun, Summary: report.Summary{Pass: 1}}
	run := WorkflowRun{RunID: 9001, WorkflowID: 44, WorkflowName: "bench", RunNumber: 12, RunAttempt: 2, RunURL: "https://github.example/run/1", Conclusion: "success"}
	body, err := renderComment(rep, run, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "Generated by") || strings.Contains(body, "github.com/nao1215/himorime") {
		t.Fatalf("hidden footer remains in comment:\n%s", body)
	}
	for _, want := range []string{"<!-- himorime:benchmark-report:v2", "[View full benchmark report and logs](<https://github.example/run/1>)"} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q:\n%s", want, body)
		}
	}
}

func TestFullReportIncludesAggregatedMetrics(t *testing.T) {
	t.Parallel()
	stats := &report.MetricStats{Count: 3, Min: 9_000_000, Max: 11_000_000, Mean: 10_000_000, Median: 10_000_000, Percentiles: map[string]float64{"p95": 11_000_000}}
	measurement := &report.Measurement{Count: 3, Metrics: map[string]*report.MetricSummary{
		"latency": {Name: "latency", Group: "latency", Unit: "ns", Better: "lower", Status: report.StatusMeasured, Stats: stats},
	}}
	rep := &report.Report{Mode: report.ModeRun, Summary: report.Summary{Pass: 1}}
	rep.Suites = []report.Suite{{Name: "suite", Result: report.ResultPass, Benchmarks: []report.Benchmark{{Name: "case", Result: report.ResultPass, Commands: []report.Command{{Name: "tool", Result: report.ResultPass, Head: measurement}}}}}}
	run := WorkflowRun{RunID: 9001, WorkflowID: 44, WorkflowName: "bench", RunNumber: 12, RunAttempt: 2, RunURL: "https://github.example/run/1", Conclusion: "success"}
	body, err := renderComment(rep, run, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "10.00ms") {
		t.Fatal("passing measurement leaked into notification")
	}
	var full bytes.Buffer
	if err := report.WriteGitHubSummary(&full, rep); err != nil {
		t.Fatal(err)
	}
	body = full.String()
	for _, want := range []string{"Latency", "Median", "10.00ms"} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q:\n%s", want, body)
		}
	}
}

func TestRenderCommentReviewHierarchy(t *testing.T) {
	t.Parallel()
	r := &report.Report{Mode: report.ModeCompare, Summary: report.Summary{Commands: 1, Inconclusive: 1}}
	r.Suites = []report.Suite{{Name: "suite", Benchmarks: []report.Benchmark{{Name: "version", Commands: []report.Command{{Name: "himorime", Result: report.ResultInconclusive, Comparisons: map[string]*report.MetricComparison{
		"peak_rss": {Metric: "peak_rss", Unit: "bytes", Statistic: "median", Verdict: "inconclusive", Reason: report.ReasonAtFloor, Gate: true},
	}}}}}}}
	run := WorkflowRun{RunID: 9001, WorkflowID: 44, WorkflowName: "Benchmark", RunNumber: 12, RunAttempt: 2, RunURL: "https://github.example/run/1", Conclusion: "success"}
	body, err := renderComment(r, run, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"⚠️ 1 inconclusive metric comparison", "version / himorime", "Peak RSS", "At or below the measurement floor"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q:\n%s", want, body)
		}
	}
	for _, unwanted := range []string{"Inconclusive comparison", "**", "| Command |", "| Confidence |", "0 regressed"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("unexpected %q", unwanted)
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "| ") && strings.Count(line, "|") > 5 {
			t.Errorf("more than four columns: %s", line)
		}
	}
}

func TestRenderCommentOmitsLongSuiteDescription(t *testing.T) {
	t.Parallel()
	rep := &report.Report{Mode: report.ModeRun, Summary: report.Summary{Pass: 1}}
	rep.Suites = []report.Suite{{Name: "suite", Description: strings.Repeat("x", maxCommentBytes), Result: report.ResultPass, Benchmarks: []report.Benchmark{{Name: "case", Result: report.ResultPass, Commands: []report.Command{{Name: "tool", Result: report.ResultPass}}}}}}
	run := WorkflowRun{RunID: 9001, WorkflowID: 44, WorkflowName: "bench", RunNumber: 12, RunAttempt: 2, RunURL: "https://github.example/run/1", Conclusion: "success"}
	body, err := renderComment(rep, run, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, strings.Repeat("x", 120)) || strings.Contains(body, "| Suite") || len(body) > maxCommentBytes {
		t.Fatalf("comment was not compact (%d bytes):\n%s", len(body), body)
	}
}

func TestRenderCommentGolden(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"pass", "improved", "inconclusive", "regression", "budget", "metric-error", "error", "mixed"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := commentFixture(name)
			before, _ := json.Marshal(r)
			got, err := renderComment(r, commentFixtureRun(), false)
			if err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(r)
			if string(before) != string(after) {
				t.Fatal("rendering mutated the judged report")
			}
			var full bytes.Buffer
			if err := report.WriteGitHubSummary(&full, r); err != nil {
				t.Fatal(err)
			}
			for prefix, body := range map[string]string{"comment": got, "report": full.String()} {
				path := filepath.Join("testdata", prefix+"-"+name+".md")
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if body != string(want) {
					t.Errorf("output differs from %s:\n%s", path, body)
				}
			}
			after, _ = json.Marshal(r)
			if string(before) != string(after) {
				t.Fatal("full report mutated judgement")
			}
			if strings.Contains(got, "**") {
				t.Fatal("bold formatting is not allowed")
			}
			if strings.Contains(got, "<details>") {
				t.Fatal("notification must not contain disclosures")
			}
			intro := strings.SplitN(got, "<details>", 2)[0]
			if name == "pass" && (strings.Contains(intro, "| ") || strings.Contains(intro, "Needs attention") || strings.Contains(intro, "PASS")) {
				t.Fatalf("passing comment is noisy:\n%s", intro)
			}
		})
	}
}

func TestRenderCommentIsOnlyAProblemNotification(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"pass", "inconclusive", "regression", "error"} {
		r := commentFixture(kind)
		body, err := renderComment(r, commentFixtureRun(), false)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"<details>", "PASS", "Decision evidence", "Run metadata", "metric rows", "required confidence"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s: notification contains %q", kind, forbidden)
			}
		}
		if kind == "pass" && (strings.Contains(body, "| ") || len(body) > 450) {
			t.Errorf("passing notification is not quiet:\n%s", body)
		}
	}
}

func TestFullReportPreservesMeasurementProvenanceAndPerMetricSettings(t *testing.T) {
	t.Parallel()
	r := commentFixture("pass")
	c := &r.Suites[0].Benchmarks[0].Commands[0]
	c.Comparisons["peak_rss"].RequiredConfidence = .99
	c.Head = &report.Measurement{Metrics: map[string]*report.MetricSummary{
		"peak_rss": {Status: report.StatusMeasured, Source: "wait4", ProcessAggregation: "max_process", Reason: "not simultaneous tree memory"},
	}}
	var out bytes.Buffer
	if err := report.WriteGitHubSummary(&out, r); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	for _, want := range []string{"95.0%", "99.0%", "wait4", "max\\_process", "not simultaneous tree memory"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q:\n%s", want, body)
		}
	}
}

func commentFixtureRun() WorkflowRun {
	return WorkflowRun{Repository: "octo/bench", WorkflowID: 44, WorkflowName: "Benchmark", RunID: 9001, RunNumber: 12, RunAttempt: 2, RunURL: "https://github.com/octo/bench/actions/runs/9001", Conclusion: "success"}
}

func TestRenderCommentSpecialResults(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"strict inconclusive", "non-gating", "derived", "skipped", "improved", "unknown metric", "empty"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			r := commentFixture("pass")
			c := &r.Suites[0].Benchmarks[0].Commands[0]
			var want, absent string
			switch kind {
			case "strict inconclusive":
				r = commentFixture("inconclusive")
				r.Summary.FailOnInconclusive, r.Summary.ExitCode = true, 1
				want, absent = "fail-on-inconclusive is enabled", "Checks passed"
			case "non-gating":
				c.Comparisons["latency"].Verdict, c.Comparisons["latency"].Gate = "regression", false
				r.Summary.NotGated.Regression = 1
				want, absent = "Latency (not gated)", "❌"
			case "derived":
				mc := *c.Comparisons["latency"]
				mc.Metric, mc.Unit, mc.DerivedFrom, mc.Gate = "throughput", "records/s", "latency", false
				c.Comparisons["throughput"] = &mc
				want, absent = "No regressions", "Throughput"
			case "skipped":
				c.Comparisons["peak_rss"] = &report.MetricComparison{Metric: "peak_rss", Verdict: "skipped", Reason: report.ReasonAtFloor, Gate: true}
				r.Summary.Skipped = 1
				want, absent = "1 skipped check", "0.00MiB"
			case "improved":
				c.Comparisons["latency"].Verdict, c.Result = "improved", report.ResultImproved
				r.Summary.Pass, r.Summary.Improved = 1, 1
				want, absent = "No regressions", "improved"
			case "unknown metric":
				mc := *c.Comparisons["latency"]
				mc.Metric, mc.Unit, mc.Verdict = "future_metric", "widgets", "inconclusive"
				c.Comparisons[mc.Metric] = &mc
				want, absent = "future\\_metric", "panic"
			case "empty":
				r = &report.Report{}
				want, absent = "No measurements", "No regressions"
			}
			body, err := renderComment(r, commentFixtureRun(), false)
			if err != nil || !strings.Contains(body, want) || strings.Contains(body, absent) {
				t.Fatalf("err=%v; want %q, not %q:\n%s", err, want, absent, body)
			}
		})
	}
}

func TestRenderCommentBoundsAndPrioritizesLateFailure(t *testing.T) {
	t.Parallel()
	r := commentFixture("inconclusive")
	bench := r.Suites[0].Benchmarks[0]
	for i := 0; i < 200; i++ {
		r.Suites[0].Benchmarks = append(r.Suites[0].Benchmarks, bench)
	}
	r.Suites[0].Benchmarks = append(r.Suites[0].Benchmarks, report.Benchmark{Name: "last failing case", Commands: []report.Command{{Name: "himorime", Result: report.ResultError, Head: &report.Measurement{Error: &report.Error{Kind: "exit", Message: "last case failed"}}}}})
	r.Summary.Error = 1
	body, err := renderComment(r, commentFixtureRun(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > maxCommentBytes || !strings.Contains(body, "last case failed") || !strings.Contains(body, "Showing 10 of 202 problems") {
		t.Fatalf("bad bounded result (%d bytes):\n%s", len(body), body)
	}
	if strings.Count(body, "<details>") != strings.Count(body, "</details>") {
		t.Fatal("truncation broke disclosures")
	}
}

func commentFixture(kind string) *report.Report {
	r := &report.Report{Mode: report.ModeCompare, HimorimeVersion: "v0.2.2", Seed: 42, Environment: report.Environment{OS: "linux", Arch: "amd64", CPUModel: "Example CPU", LogicalCPUs: 4}, Git: &report.Git{BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40)}, Summary: report.Summary{Commands: 2, Pass: 2}}
	s := report.Suite{Name: "CLI performance"}
	for _, name := range []string{"version", "validate"} {
		c := report.Command{Name: "himorime", Result: report.ResultPass, Comparisons: map[string]*report.MetricComparison{}, Base: &report.Measurement{Metrics: map[string]*report.MetricSummary{}}, Head: &report.Measurement{Metrics: map[string]*report.MetricSummary{}}}
		for _, m := range []struct {
			name, unit string
			base, head float64
		}{{"latency", "ns", 10e6, 10.1e6}, {"cpu_total", "ns", 12e6, 12.1e6}, {"peak_rss", "bytes", 32 << 20, 33 << 20}} {
			base, head, diff := m.base, m.head, m.head-m.base
			for _, side := range []struct {
				measurement *report.Measurement
				value       float64
			}{{c.Base, base}, {c.Head, head}} {
				v := side.value
				side.measurement.Count = 20
				side.measurement.Metrics[m.name] = &report.MetricSummary{Name: m.name, Unit: m.unit, Status: report.StatusMeasured, Source: "example", Stats: fixtureStats(v)}
			}
			c.Comparisons[m.name] = &report.MetricComparison{Metric: m.name, Unit: m.unit, Statistic: "median", Better: "lower", Base: &base, Head: &head, Difference: &diff, ChangePercent: diff / base * 100, CILowPercent: -2, CIHighPercent: 4, MaxPercent: 10, MinDifference: 1e6, RequiredConfidence: .95, MinSamples: 10, MaxCV: .5, Verdict: "pass", Reason: "the difference is smaller than min_difference", Gate: true}
		}
		c.Comparisons["peak_rss"].MinDifference = 2 << 20
		s.Benchmarks = append(s.Benchmarks, report.Benchmark{Name: name, Commands: []report.Command{c}})
	}
	r.Suites = []report.Suite{s}
	c := &r.Suites[0].Benchmarks[0].Commands[0]
	switch kind {
	case "inconclusive":
		c.Result = report.ResultInconclusive
		mc := c.Comparisons["peak_rss"]
		mc.Verdict, mc.Reason = "inconclusive", report.ReasonAtFloor
		base, head, diff := float64(16<<20), float64(17<<20), float64(1<<20)
		mc.Base, mc.Head, mc.Difference, mc.ChangePercent, mc.CILowPercent, mc.CIHighPercent = &base, &head, &diff, 6.25, 5, 7
		c.Base.Metrics["peak_rss"].Floor, c.Head.Metrics["peak_rss"].Floor = 16<<20, 16<<20
		c.Base.Metrics["peak_rss"].Stats, c.Head.Metrics["peak_rss"].Stats = fixtureStats(base), fixtureStats(head)
		r.Summary.Pass, r.Summary.Inconclusive = 1, 1
	case "regression", "budget":
		c.Result = report.ResultRegression
		mc := c.Comparisons["latency"]
		head, diff := 15e6, 5e6
		mc.Head, mc.Difference, mc.ChangePercent, mc.CILowPercent, mc.CIHighPercent, mc.ProbRegression, mc.Verdict, mc.Reason = &head, &diff, 50, 45, 55, .99, "regression", ""
		c.Head.Metrics["latency"].Stats = fixtureStats(head)
		other := &r.Suites[0].Benchmarks[1].Commands[0]
		actual := 20e6
		other.Result = report.ResultOverBudget
		other.Budgets = []report.BudgetCheck{{Metric: "cpu_total", Aggregation: "p95", Unit: "ns", Operator: "<=", Limit: 18e6, Actual: &actual, Status: report.BudgetFail}}
		r.Summary.Pass, r.Summary.Regression, r.Summary.OverBudget, r.Summary.ExitCode = 1, 1, 0, 1
		if kind == "budget" {
			*c = commentFixture("pass").Suites[0].Benchmarks[0].Commands[0]
			r.Summary.Regression, r.Summary.OverBudget = 0, 1
		} else {
			other.Budgets = nil
			other.Result = report.ResultPass
		}
	case "improved":
		c.Result = report.ResultImproved
		mc := c.Comparisons["latency"]
		head, diff := 5e6, -5e6
		mc.Head, mc.Difference, mc.ChangePercent, mc.CILowPercent, mc.CIHighPercent, mc.Verdict, mc.Reason = &head, &diff, -50, -55, -45, "improved", ""
		c.Head.Metrics["latency"].Stats = fixtureStats(head)
		r.Summary.Pass, r.Summary.Improved = 1, 1
	case "metric-error":
		c.Result = report.ResultMetricError
		c.Head = &report.Measurement{Metrics: map[string]*report.MetricSummary{"cpu_total": {Status: report.StatusFailed, Reason: "CPU counter unavailable"}}}
		c.Comparisons["cpu_total"].Verdict, c.Comparisons["cpu_total"].Reason = report.VerdictSkipped, "CPU counter unavailable"
		r.Summary.Pass, r.Summary.MetricError, r.Summary.ExitCode = 1, 1, 6
	case "mixed":
		r = commentFixture("regression")
		for _, name := range []string{"budget", "inconclusive", "metric-error", "error"} {
			r.Suites[0].Benchmarks = append(r.Suites[0].Benchmarks, commentFixture(name).Suites[0].Benchmarks...)
		}
		r.Summary = report.Summary{Commands: 10, Pass: 5, Regression: 1, OverBudget: 1, Inconclusive: 1, MetricError: 1, Error: 1, ExitCode: 4}
	case "error":
		c.Result, c.Comparisons = report.ResultError, nil
		c.Head = &report.Measurement{Error: &report.Error{Kind: "exit_code", Message: "command exited before producing samples"}}
		r.Suites = append(r.Suites, report.Suite{Name: "integration", Result: report.ResultError, Error: &report.Error{Kind: "build", Message: "compiler failed"}})
		r.Summary.Pass, r.Summary.Error, r.Summary.ExitCode = 1, 2, 4
	}
	return r
}

func fixtureStats(v float64) *report.MetricStats {
	return &report.MetricStats{Count: 20, Median: v, Mean: v, Min: v, Max: v, Percentiles: map[string]float64{"p90": v, "p95": v, "p99": v}}
}

func TestCommentClientPaginatesAndUpdatesOwnedComment(t *testing.T) {
	t.Parallel()
	const token = "top-secret-token"
	var requests []string
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodPost:
			return response(http.StatusCreated, `{"id":90}`), nil
		case r.Method == http.MethodGet && r.URL.Query().Get("page") == "1":
			comments := make([]issueComment, 100)
			var b strings.Builder
			if err := json.NewEncoder(&b).Encode(comments); err != nil {
				t.Fatal(err)
			}
			return response(http.StatusOK, b.String()), nil
		case r.Method == http.MethodGet && r.URL.Query().Get("page") == "2":
			return response(http.StatusOK, `[{ 
              "id":81,"body":"<!-- himorime:benchmark-report:v1 workflow_id=44 run_number=11 run_attempt=1 -->",
              "user":{"login":"github-actions[bot]","type":"Bot"}
		    },{
		      "id":90,"body":"<!-- himorime:benchmark-report:v2 run_id=9001 workflow_id=44 run_number=12 run_attempt=1 -->",
		      "user":{"login":"github-actions[bot]","type":"Bot"}
		    }]`), nil
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/issues/comments/81"):
			return response(http.StatusNoContent, ""), nil
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			return response(http.StatusNotFound, ""), nil
		}
	})}
	run := WorkflowRun{Repository: "octo/bench", PullRequest: 7, RunID: 9001, WorkflowID: 44, RunNumber: 12, RunAttempt: 1}
	c := CommentClient{HTTP: httpClient, APIURL: "https://api.example/api/v3", Token: token, Repository: "octo/bench"}
	stale, err := c.Upsert(context.Background(), run, "body")
	if err != nil || stale {
		t.Fatalf("stale = %v, err = %v", stale, err)
	}
	if len(requests) != 4 || !strings.Contains(requests[0], "/api/v3/repos/octo/bench/issues/7/comments") || !strings.Contains(requests[1], "page=1") || !strings.Contains(requests[2], "page=2") || !strings.Contains(requests[3], "/api/v3/repos/octo/bench/issues/comments/81") {
		t.Fatalf("requests = %v", requests)
	}
}

func TestCommentClientRejectsStaleRunAndDoesNotLeakToken(t *testing.T) {
	t.Parallel()
	const token = "never-print-me"
	var mutations []string
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mutations = append(mutations, r.Method)
		if r.Method == http.MethodPost {
			return response(http.StatusCreated, `{"id":2}`), nil
		}
		if r.Method == http.MethodGet {
			return response(http.StatusOK, `[{"id":1,"body":"<!-- himorime:benchmark-report:v2 run_id=9002 workflow_id=99 run_number=1 run_attempt=1 -->","user":{"login":"github-actions[bot]","type":"Bot"}}]`), nil
		}
		return response(http.StatusNoContent, ""), nil
	})}
	run := WorkflowRun{Repository: "octo/bench", PullRequest: 7, RunID: 9001, WorkflowID: 44, RunNumber: 12, RunAttempt: 9}
	c := CommentClient{HTTP: httpClient, APIURL: "https://api.example", Token: token, Repository: "octo/bench"}
	stale, err := c.Upsert(context.Background(), run, "body")
	if err != nil || !stale || fmt.Sprint(mutations) != "[POST GET DELETE]" {
		t.Fatalf("stale = %v, mutations = %v, err = %v", stale, mutations, err)
	}

	c.HTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusForbidden, token), nil
	})}
	_, err = c.Upsert(context.Background(), run, "body")
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("err = %q; token must not be exposed", err)
	}
}

func TestCommentClientDoesNotUpdateAnUnownedMarker(t *testing.T) {
	t.Parallel()
	created := false
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			created = true
			return response(http.StatusCreated, `{"id":2}`), nil
		}
		if r.Method == http.MethodGet {
			return response(http.StatusOK, `[{"id":1,"body":"<!-- himorime:benchmark-report:v1 workflow_id=44 run_number=99 run_attempt=1 -->","user":{"login":"attacker","type":"User"}}]`), nil
		}
		return response(http.StatusBadRequest, ""), nil
	})}
	run := WorkflowRun{Repository: "octo/bench", PullRequest: 7, RunID: 9001, WorkflowID: 44, RunNumber: 12, RunAttempt: 1}
	c := CommentClient{HTTP: httpClient, APIURL: "https://api.example", Token: "token", Repository: "octo/bench"}
	if stale, err := c.Upsert(context.Background(), run, "body"); err != nil || stale || !created {
		t.Fatalf("stale = %v, created = %v, err = %v", stale, created, err)
	}
}

func TestCommentClientUpdatesLatestOwnedCommentAndDeletesLegacyAndOtherWorkflows(t *testing.T) {
	t.Parallel()
	var mutations []string
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			mutations = append(mutations, r.Method+" "+r.URL.Path)
			return response(http.StatusCreated, `{"id":14}`), nil
		}
		if r.Method == http.MethodGet {
			return response(http.StatusOK, `[
              {"id":10,"body":"<!-- himorime:benchmark-report:v1 workflow_id=44 run_number=8 run_attempt=1 -->","user":{"login":"github-actions[bot]","type":"Bot"}},
              {"id":11,"body":"<!-- himorime:benchmark-report:v2 run_id=8999 workflow_id=88 run_number=2 run_attempt=1 -->","user":{"login":"github-actions[bot]","type":"Bot"}},
              {"id":12,"body":"<!-- himorime:benchmark-report:v2 run_id=8998 workflow_id=44 run_number=9 run_attempt=1 -->","user":{"login":"github-actions[bot]","type":"Bot"}},
		      {"id":13,"body":"<!-- himorime:benchmark-report:v2 run_id=9999 workflow_id=44 run_number=99 run_attempt=1 -->","user":{"login":"somebody","type":"User"}},
		      {"id":14,"body":"<!-- himorime:benchmark-report:v2 run_id=9001 workflow_id=44 run_number=12 run_attempt=1 -->","user":{"login":"github-actions[bot]","type":"Bot"}}
            ]`), nil
		}
		mutations = append(mutations, r.Method+" "+r.URL.Path)
		return response(http.StatusNoContent, ""), nil
	})}
	run := WorkflowRun{Repository: "octo/bench", PullRequest: 7, RunID: 9001, WorkflowID: 44, RunNumber: 12, RunAttempt: 1}
	c := CommentClient{HTTP: httpClient, APIURL: "https://api.example", Token: "token", Repository: "octo/bench"}
	stale, err := c.Upsert(context.Background(), run, "body")
	if err != nil || stale {
		t.Fatalf("stale = %v, err = %v", stale, err)
	}
	want := []string{
		"POST /repos/octo/bench/issues/7/comments",
		"DELETE /repos/octo/bench/issues/comments/10",
		"DELETE /repos/octo/bench/issues/comments/11",
		"DELETE /repos/octo/bench/issues/comments/12",
	}
	if fmt.Sprint(mutations) != fmt.Sprint(want) {
		t.Fatalf("mutations = %v, want %v", mutations, want)
	}
}

func TestCommentClientReturnsDeletionFailureForRetry(t *testing.T) {
	t.Parallel()
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodPost:
			return response(http.StatusCreated, `{"id":22}`), nil
		case http.MethodGet:
			return response(http.StatusOK, `[
                  {"id":20,"body":"<!-- himorime:benchmark-report:v2 run_id=9000 workflow_id=44 run_number=11 run_attempt=1 -->","user":{"login":"github-actions[bot]","type":"Bot"}},
		          {"id":21,"body":"<!-- himorime:benchmark-report:v1 workflow_id=44 run_number=10 run_attempt=1 -->","user":{"login":"github-actions[bot]","type":"Bot"}},
		          {"id":22,"body":"<!-- himorime:benchmark-report:v2 run_id=9001 workflow_id=44 run_number=12 run_attempt=1 -->","user":{"login":"github-actions[bot]","type":"Bot"}}
                ]`), nil
		case http.MethodDelete:
			return response(http.StatusServiceUnavailable, "retry later"), nil
		default:
			return response(http.StatusBadRequest, ""), nil
		}
	})}
	run := WorkflowRun{Repository: "octo/bench", PullRequest: 7, RunID: 9001, WorkflowID: 44, RunNumber: 12, RunAttempt: 1}
	c := CommentClient{HTTP: httpClient, APIURL: "https://api.example", Token: "token", Repository: "octo/bench"}
	stale, err := c.Upsert(context.Background(), run, "body")
	if err == nil || stale || !strings.Contains(err.Error(), "delete old himorime comment 20") || !strings.Contains(err.Error(), "503") {
		t.Fatalf("stale = %v, err = %v", stale, err)
	}
}

func TestCommentClientKeepsDeterministicIDForDuplicateRun(t *testing.T) {
	t.Parallel()
	var deleted []string
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodPost:
			return response(http.StatusCreated, `{"id":30}`), nil
		case http.MethodGet:
			return response(http.StatusOK, `[
                  {"id":30,"body":"<!-- himorime:benchmark-report:v2 run_id=9001 workflow_id=44 run_number=12 run_attempt=2 -->","user":{"login":"github-actions[bot]","type":"Bot"}},
                  {"id":31,"body":"<!-- himorime:benchmark-report:v2 run_id=9001 workflow_id=44 run_number=12 run_attempt=2 -->","user":{"login":"github-actions[bot]","type":"Bot"}}
                ]`), nil
		case http.MethodDelete:
			deleted = append(deleted, r.URL.Path)
			return response(http.StatusNoContent, ""), nil
		default:
			return response(http.StatusBadRequest, ""), nil
		}
	})}
	run := WorkflowRun{Repository: "octo/bench", PullRequest: 7, RunID: 9001, WorkflowID: 44, RunNumber: 12, RunAttempt: 2}
	c := CommentClient{HTTP: httpClient, APIURL: "https://api.example", Token: "token", Repository: "octo/bench"}
	stale, err := c.Upsert(context.Background(), run, "body")
	if err != nil || !stale || fmt.Sprint(deleted) != "[/repos/octo/bench/issues/comments/30]" {
		t.Fatalf("stale = %v, deleted = %v, err = %v", stale, deleted, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
