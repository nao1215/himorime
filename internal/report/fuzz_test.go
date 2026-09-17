package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nao1215/yahiko/internal/config"
	"github.com/nao1215/yahiko/internal/runner"
)

// FuzzEscapeMarkdown checks that an escaped cell can never end a table row or
// open an HTML tag.
func FuzzEscapeMarkdown(f *testing.F) {
	for _, seed := range []string{"a|b", "<script>", "line\nbreak", "`code`", `\|`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := EscapeMarkdown(s)
		if strings.ContainsAny(out, "\n\r<>") {
			t.Fatalf("EscapeMarkdown(%q) = %q keeps a line break or angle bracket", s, out)
		}
		unescaped := strings.ReplaceAll(strings.ReplaceAll(out, `\\`, ""), `\|`, "")
		if strings.Contains(unescaped, "|") {
			t.Fatalf("EscapeMarkdown(%q) = %q keeps a bare pipe", s, out)
		}
	})
}

// FuzzReportFormats renders a report built from arbitrary names, messages and
// samples in every format. JSON and CSV must parse back, and nothing may panic.
func FuzzReportFormats(f *testing.F) {
	f.Add("suite", "bench", "cmd", "stderr text", int64(1000), int64(2000), true)
	f.Add("a|b", "c,\"d\"\ne", "x", "<b>", int64(0), int64(-5), false)
	f.Fuzz(func(t *testing.T, suiteName, benchName, cmdName, message string, s1, s2 int64, compare bool) {
		c := config.Command{Name: cmdName, Exec: config.Exec{Argv: []string{cmdName}}}
		samples := []time.Duration{time.Duration(s1), time.Duration(s2)}
		sides := map[string]*runner.Measurement{runner.SideHead: {Samples: samples, Failure: &runner.Failure{Kind: runner.FailExitCode, Message: message, Stderr: message}}}
		mode := ModeRun
		if compare {
			mode = ModeCompare
			sides[runner.SideBase] = &runner.Measurement{Samples: samples}
		}
		br := runner.BenchmarkResult{
			Benchmark: config.Benchmark{Name: benchName, Commands: []config.Command{c}, Regression: config.Regression{Metric: config.MetricMedian, MaxPercent: 10, Confidence: 0.95, MinSamples: 2}},
			Commands:  []runner.CommandResult{{Command: c, Sides: sides}},
		}
		r := &Report{Environment: Environment{LogicalCPUs: 1}}
		Judge(r, []SuiteInput{{Suite: &config.Suite{Name: suiteName}, File: "f.yaml", Benchmarks: []runner.BenchmarkResult{br}}}, Options{Mode: mode, Seed: 1})
		for _, format := range config.Formats() {
			var buf bytes.Buffer
			if err := Write(&buf, format, r, true); err != nil {
				t.Fatalf("%s: %v", format, err)
			}
			switch format {
			case config.FormatJSON:
				var v map[string]any
				if err := json.Unmarshal(buf.Bytes(), &v); err != nil {
					t.Fatalf("invalid JSON: %v", err)
				}
			case config.FormatCSV:
				if _, err := csv.NewReader(&buf).ReadAll(); err != nil {
					t.Fatalf("invalid CSV: %v", err)
				}
			case config.FormatTable, config.FormatMarkdown, config.FormatGitHub:
			}
		}
	})
}
