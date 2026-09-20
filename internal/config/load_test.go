package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nao1215/himorime/internal/metric"
)

func TestLoadAppliesDefaults(t *testing.T) {
	t.Parallel()
	s := mustParse(t, minimal(""))
	if s.Name != "test" || len(s.Benchmarks) != 1 {
		t.Fatalf("suite = %+v", s)
	}
	b := s.Benchmarks[0]
	if b.Warmup != DefaultWarmup || b.Runs != 0 || b.MinRuns != DefaultMinRuns || b.MaxRuns != DefaultMaxRuns || b.MinTime != DefaultMinTime {
		t.Errorf("measurement defaults = %+v", b)
	}
	c := b.Commands[0]
	if c.Timeout != DefaultTimeout || c.Stdout != OutputDiscard || c.Stderr != OutputDiscard || !reflect.DeepEqual(c.ExitCodes, []int{0}) {
		t.Errorf("command defaults = %+v", c)
	}
	metricDefault := MetricRegression{Metric: MetricMedian, MaxPercent: 10, Gate: true}
	want := Regression{Confidence: 0.95, MinSamples: 10, MaxCV: 0.5, Latency: metricDefault, CPU: metricDefault, Memory: metricDefault}
	if !reflect.DeepEqual(b.Regression, want) {
		t.Errorf("regression defaults = %+v, want %+v", b.Regression, want)
	}
	if filepath.Base(s.Path) != "himorime.yaml" || s.Dir != filepath.Dir(s.Path) {
		t.Errorf("paths = %s, %s", s.Path, s.Dir)
	}
}

func TestDogfoodSuiteKeepsMemoryForNonVersionBenchmarks(t *testing.T) {
	t.Parallel()
	suitePath := filepath.Join("..", "..", "bench", "himorime.yaml")
	s, err := Load(suitePath)
	if err != nil {
		t.Fatalf("Load(%q) failed: %v", suitePath, err)
	}
	wantMemory := map[string]bool{
		"version":                false,
		"validate every example": true,
		"list with a filter":     true,
		"run a tiny suite":       true,
		"collector overhead":     true,
	}
	if len(s.Benchmarks) != len(wantMemory) {
		t.Fatalf("benchmark count = %d, want %d", len(s.Benchmarks), len(wantMemory))
	}
	for _, benchmark := range s.Benchmarks {
		want, ok := wantMemory[benchmark.Name]
		if !ok {
			t.Fatalf("unexpected benchmark %q", benchmark.Name)
		}
		if benchmark.Metrics.Memory != want {
			t.Errorf("%q memory = %t, want %t", benchmark.Name, benchmark.Metrics.Memory, want)
		}
		if !benchmark.Metrics.CPU {
			t.Errorf("%q CPU collection is disabled", benchmark.Name)
		}
	}
}

func TestLoadInheritanceOrder(t *testing.T) {
	t.Parallel()
	src := `version: "1"
name: inherit
defaults:
  warmup: 3
  runs: 20
  timeout: 5s
  cwd: fixtures
  env: {A: defaults, B: defaults}
  stdout: default.out
  exit_codes: [0, 1]
  regression:
    latency: {statistic: mean, max_percent: "5%", gate: false}
    confidence: 0.9
    min_samples: 12
    max_cv: 0
benchmarks:
  - name: first
    runs: 30
    timeout: 7s
    env: {B: benchmark, C: benchmark}
    commands:
      one:
        command: [one]
        env: {C: command}
        timeout: 9s
        exit_codes: [3]
      two:
        command: [two]
    regression:
      latency: {max_percent: 15}
      commands: [one]
  - name: second
    commands:
      three:
        command: [three]
`
	s := mustParse(t, src)
	first, second := s.Benchmarks[0], s.Benchmarks[1]
	if first.Warmup != 3 || first.Runs != 30 || second.Runs != 20 {
		t.Errorf("warmup/runs = %d/%d/%d", first.Warmup, first.Runs, second.Runs)
	}
	one, two := first.Commands[0], first.Commands[1]
	if one.Timeout != 9*time.Second || two.Timeout != 7*time.Second || second.Commands[0].Timeout != 5*time.Second {
		t.Errorf("timeouts = %v %v %v", one.Timeout, two.Timeout, second.Commands[0].Timeout)
	}
	wantEnv := []EnvVar{{"A", "defaults"}, {"B", "benchmark"}, {"C", "command"}}
	if !reflect.DeepEqual(one.Env, wantEnv) {
		t.Errorf("env = %+v, want %+v", one.Env, wantEnv)
	}
	if one.Cwd != "fixtures" || one.Stdout != "default.out" || one.Stderr != OutputDiscard {
		t.Errorf("cwd/stdout/stderr = %q %q %q", one.Cwd, one.Stdout, one.Stderr)
	}
	if !reflect.DeepEqual(one.ExitCodes, []int{3}) || !reflect.DeepEqual(two.ExitCodes, []int{0, 1}) {
		t.Errorf("exit codes = %v %v", one.ExitCodes, two.ExitCodes)
	}
	r := first.Regression
	// A benchmark's latency setting overrides only the keys it writes: the
	// statistic and the gate are still inherited from defaults.
	if r.Latency.Metric != MetricMean || r.Latency.MaxPercent != 15 || r.Latency.Gate || r.Confidence != 0.9 || r.MinSamples != 12 || r.MaxCV != 0 || !reflect.DeepEqual(r.Commands, []string{"one"}) {
		t.Errorf("benchmark regression = %+v", r)
	}
	if !r.CPU.Gate || !r.Memory.Gate {
		t.Errorf("metrics without a gate setting must stay gated: %+v", r)
	}
	if second.Regression.Latency.MaxPercent != 5 || second.Regression.Commands != nil {
		t.Errorf("inherited regression = %+v", second.Regression)
	}
	if !r.Compares("one") || r.Compares("two") || !second.Regression.Compares("three") {
		t.Error("Compares does not follow regression.commands")
	}
}

func TestLoadKeepsCommandOrderAndForms(t *testing.T) {
	t.Parallel()
	src := `version: "1"
name: order
build:
  command: [go, build, -o, "${artifact}", .]
benchmarks:
  - name: forms
    baseline: zeta
    stdin: {content: "hello\n"}
    setup:
      - command: [gen, "${workdir}/in.txt"]
    prepare_each:
      - command: "rm -f ${workdir}/cache"
        shell: true
    cleanup:
      - command: [cleanup]
        cwd: "${workdir}"
    commands:
      zeta:
        command: ["${artifact}", --fast]
        budget: {latency: {median: "< 20ms", mean: "< 30ms"}}
      alpha:
        command: "cat ${workdir}/in.txt | wc -l"
        shell: true
        budget: {latency: {max: "<= 1s"}}
      mid:
        command: [mid]
`
	s := mustParse(t, src)
	b := s.Benchmarks[0]
	var names []string
	for _, c := range b.Commands {
		names = append(names, c.Name)
	}
	if !reflect.DeepEqual(names, []string{"zeta", "alpha", "mid"}) {
		t.Fatalf("command order = %v", names)
	}
	if s.Build == nil || s.Build.Timeout != DefaultBuildTimeout || b.Setup[0].Timeout != DefaultHookTimeout {
		t.Errorf("build/hook timeouts = %+v %+v", s.Build, b.Setup)
	}
	if b.Stdin.Kind != StdinContent || b.Stdin.Content != "hello\n" {
		t.Errorf("stdin = %+v", b.Stdin)
	}
	if !b.PrepareEach[0].Shell || b.Cleanup[0].Cwd != "${workdir}" {
		t.Errorf("hooks = %+v %+v", b.PrepareEach, b.Cleanup)
	}
	if b.Commands[1].Display() != "cat ${workdir}/in.txt | wc -l" || b.Commands[0].Display() != "${artifact} --fast" {
		t.Errorf("display = %q, %q", b.Commands[1].Display(), b.Commands[0].Display())
	}
	var budgets []string
	for _, bud := range b.Budgets {
		budgets = append(budgets, bud.Command+"."+string(bud.Metric)+"."+string(bud.Aggregation)+string(bud.Threshold.Op))
	}
	if !reflect.DeepEqual(budgets, []string{"zeta.latency.median<", "zeta.latency.mean<", "alpha.latency.max<="}) {
		t.Errorf("budgets = %v", budgets)
	}
	if _, ok := b.Command("mid"); !ok {
		t.Error("Command(mid) not found")
	}
	if _, ok := b.Command("nope"); ok {
		t.Error("Command(nope) found")
	}
}

func TestLoadStdinForms(t *testing.T) {
	t.Parallel()
	for src, want := range map[string]Stdin{
		"stdin: testdata/in.txt":          {Kind: StdinFile, File: "testdata/in.txt"},
		"stdin: {file: \"${workdir}/x\"}": {Kind: StdinFile, File: "${workdir}/x"},
		`stdin: {content: ""}`:            {Kind: StdinContent},
		"stdin: {content: \"a ${root}\"}": {Kind: StdinContent, Content: "a ${root}"},
	} {
		s := mustParse(t, minimal(src))
		if got := s.Benchmarks[0].Stdin; got != want {
			t.Errorf("%s => %+v, want %+v", src, got, want)
		}
	}
}

func TestLoadTerminal(t *testing.T) {
	t.Parallel()
	for src, want := range map[string]bool{
		"":                                false,
		"terminal: false":                 false,
		"terminal: true":                  true,
		"terminal: true\nstdout: out.txt": true,
		"terminal: true\nstderr: discard": true,
	} {
		s := mustParse(t, minimal(src))
		if got := s.Benchmarks[0].Terminal; got != want {
			t.Errorf("%q => Terminal %v, want %v", src, got, want)
		}
	}
	// A benchmark that discards stderr itself is not affected by defaults.stderr.
	src := strings.Replace(minimal("terminal: true\nstderr: discard"), "benchmarks:", "defaults: {stderr: err.txt}\nbenchmarks:", 1)
	if s := mustParse(t, src); !s.Benchmarks[0].Terminal {
		t.Error("Terminal = false")
	}
}

func TestLoadRejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		src   string
		want  string
		field string
	}{
		{"unknown top-level key", `version: "1"
name: x
benchmark: []
`, `unknown key "benchmark"`, "benchmark"},
		{"removed suite object", `version: "1"
name: x
suite: {name: x}
benchmarks: [{name: a, commands: {a: {command: [a]}}}]
`, `unknown key "suite"`, "suite"},
		{"missing version", "name: x\nbenchmarks: [{name: a, commands: {a: {command: [a]}}}]\n", "version is required", "version"},
		{"unsupported version", "version: \"2\"\nname: x\nbenchmarks: [{name: a, commands: {a: {command: [a]}}}]\n", "must be one of: 1", "version"},
		{"blank suite name", "version: \"1\"\nname: \" \"\nbenchmarks: [{name: a, commands: {a: {command: [a]}}}]\n", "must not be blank", "name"},
		{"no benchmarks", "version: \"1\"\nname: x\nbenchmarks: []\n", "must contain at least 1 item", "benchmarks"},
		{"typo in command key", minimal("") + "        shel: true\n", `unknown key "shel"`, "benchmarks[0].commands.tool.shel"},
		{"string command without shell", `version: "1"
name: x
benchmarks:
  - name: a
    commands:
      t: {command: "echo hi"}
`, "a string command requires shell: true", "benchmarks[0].commands.t"},
		{"list command with shell", `version: "1"
name: x
benchmarks:
  - name: a
    commands:
      t: {command: [echo, hi], shell: true}
`, "a string command requires shell: true", "benchmarks[0].commands.t"},
		{"empty program", `version: "1"
name: x
benchmarks:
  - name: a
    commands:
      t: {command: [""]}
`, "expected a list of arguments", "benchmarks[0].commands.t.command"},
		{"duration without unit", minimal("timeout: 10"), "expected a duration string", "benchmarks[0].timeout"},
		{"zero timeout", minimal("timeout: 0s"), "greater than zero", "benchmarks[0].timeout"},
		{"negative runs", minimal("runs: 0"), "must be at least 1, got 0", "benchmarks[0].runs"},
		{"number as name", minimal("") + "  - name: 42\n    commands: {x: {command: [x]}}\n", "expected a string, got a number", "benchmarks[1].name"},
		{"bad tag", minimal("tags: [\"has space\"]"), "invalid name", "benchmarks[0].tags[0]"},
		{"removed benchmark budget", minimal("budget: {tool: {median: \"< 20ms\"}}"), `unknown key "budget"`, "benchmarks[0].budget"},
		{"bad budget", minimalCommandBudget("{latency: {median: \"20ms\"}}"), "invalid budget", "benchmarks[0].commands.tool.budget.latency.median"},
		{"unknown budget metric", minimalCommandBudget("{bogus: {median: \"< 20ms\"}}"), `unknown key "bogus"`, "benchmarks[0].commands.tool.budget.bogus"},
		{"empty budget", minimalCommandBudget("{}"), "must contain at least 1 entry", "benchmarks[0].commands.tool.budget"},
		{"absolute stdin", minimal("stdin: /etc/passwd"), "absolute paths are not allowed", "benchmarks[0].stdin"},
		{"windows absolute stdin", minimal(`stdin: 'C:\data.txt'`), "absolute paths are not allowed", "benchmarks[0].stdin"},
		{"escaping stdout", minimal("stdout: ../escape.txt"), "relative path inside ${workdir}", "benchmarks[0].stdout"},
		{"bad statistic", minimal("regression: {latency: {statistic: p95}}"), "must be one of: median, mean", "benchmarks[0].regression.latency.statistic"},
		{"zero max_percent", minimal("regression: {latency: {max_percent: 0}}"), "percentage greater than 0", "benchmarks[0].regression.latency.max_percent"},
		{"latency tolerance at the top level", minimal("regression: {max_percent: 10}"), `unknown key "max_percent"`, "benchmarks[0].regression.max_percent"},
		{"gate is a boolean", minimal("regression: {cpu_total: {gate: \"no\"}}"), "expected true or false", "benchmarks[0].regression.cpu_total.gate"},
		{"low confidence", minimal("regression: {confidence: 0.3}"), "must be at least 0.5, got 0.3", "benchmarks[0].regression.confidence"},
		{"bad env name", minimal("env: {\"1X\": y}"), "invalid environment variable name", "benchmarks[0].env.1X"},
		{"bad report format", "version: \"1\"\nname: x\nbenchmarks: [{name: a, commands: {a: {command: [a]}}}]\nreport: {outputs: [{format: table, path: x.txt}]}\n", "must be one of", "report.outputs[0].format"},
		{"stderr on a terminal benchmark", minimal("terminal: true\nstderr: err.txt"), "stderr cannot be set with terminal: true", "benchmarks[0].stderr"},
		{"stderr on a terminal command", minimal("") + "        stderr: err.txt\n    terminal: true\n", "stderr cannot be set with terminal: true", "benchmarks[0].commands.tool.stderr"},
		{"defaults stderr on a terminal benchmark", strings.Replace(minimal("terminal: true"), "benchmarks:", "defaults: {stderr: err.txt}\nbenchmarks:", 1), "stderr cannot be set with terminal: true", "benchmarks[0].terminal"},
		{"terminal is a boolean", minimal("terminal: \"on\""), "expected true or false", "benchmarks[0].terminal"},
		{"yaml syntax", "version: \"1\"\nname: x\nbenchmarks: [", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseString(t, tt.src)
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Load() error = %v, want a ValidationError", err)
			}
			if !isValidation(err) {
				t.Fatal("isValidation = false")
			}
			found := false
			for _, is := range verr.Issues {
				if strings.Contains(is.Message, tt.want) && (tt.field == "" || is.Field == tt.field) {
					found = true
				}
			}
			if !found {
				t.Fatalf("no issue with message %q at %q; issues:\n%v", tt.want, tt.field, err)
			}
		})
	}
}

func TestLoadSemanticRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		extra string
		want  string
	}{
		{"unknown baseline", "baseline: nope", `baseline "nope" is not a command of this benchmark`},
		{"regression command unknown", "regression: {commands: [ghost]}", `"ghost" is not a command of this benchmark`},
		{"max below min runs", "min_runs: 20\nmax_runs: 10", "max_runs (10) is lower than min_runs (20)"},
		{"artifact without build", "setup:\n  - command: [\"${artifact}\"]", "${artifact} is used but the suite has no build section"},
		{"unknown variable", "cwd: \"${home}/x\"", "unknown variable ${home}"},
		{"variable not at start", "cwd: \"x/${root}\"", "${root} may only start a path"},
		{"head_root not at start", "stdin: \"x/${head_root}/in.txt\"", "${head_root} may only start a path"},
		{"dotdot after head_root leaving the project", "stdin: \"${head_root}/../x\"", "leaves the project"},
		{"dotdot after workdir", "stdin: \"${workdir}/../x\"", "must not contain .. after ${workdir}"},
		{"env var in path", "cwd: \"${env:HOME}\"", "${env:HOME} is not allowed in a path"},
		{"control character", "description: ok\nname: \"a\\u0001b\"", "must not contain control characters"},
		{"too long duration", "timeout: 25h", "exceeds the maximum of 24h"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := minimal(tt.extra)
			if tt.name == "control character" {
				src = `version: "1"
name: x
benchmarks:
  - name: "a\u0001b"
    commands: {t: {command: [t]}}
`
			}
			_, err := parseString(t, src)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestLoadDuplicateNames(t *testing.T) {
	t.Parallel()
	src := `version: "1"
name: x
benchmarks:
  - name: same
    commands: {a: {command: [a]}}
  - name: same
    commands: {a: {command: [a]}}
`
	_, err := parseString(t, src)
	if err == nil || !strings.Contains(err.Error(), `benchmark name "same" is already used by benchmarks[0]`) {
		t.Fatalf("error = %v", err)
	}
	dupKey := `version: "1"
name: x
benchmarks:
  - name: a
    commands:
      a: {command: [a]}
      a: {command: [b]}
`
	if _, err := parseString(t, dupKey); err == nil {
		t.Fatal("a duplicated command key was accepted")
	}
}

func TestIssuePositions(t *testing.T) {
	t.Parallel()
	src := minimal("baseline: nope")
	_, err := parseString(t, src)
	var verr *ValidationError
	if !errors.As(err, &verr) || len(verr.Issues) != 1 {
		t.Fatalf("err = %v", err)
	}
	is := verr.Issues[0]
	if is.Line != 8 || is.Column != 15 || is.Field != "benchmarks[0].baseline" || is.Hint == "" {
		t.Fatalf("issue = %+v", is)
	}
	if !strings.HasPrefix(is.String(), filepath.Join(filepath.Dir(is.File), "himorime.yaml")+":8:15: benchmarks[0].baseline:") {
		t.Fatalf("String() = %q", is.String())
	}
}

func TestLoadFileErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := Load(filepath.Join(dir, "missing.yaml")); !isValidation(err) || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("missing file: %v", err)
	}
	if _, err := Load(dir); !isValidation(err) || !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("directory: %v", err)
	}
	empty := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(empty, []byte("\n  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(empty); !isValidation(err) || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty file: %v", err)
	}
	big := filepath.Join(dir, "big.yaml")
	if err := os.WriteFile(big, make([]byte, maxFileSize+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(big); !isValidation(err) || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("big file: %v", err)
	}
}

func TestFormatsAndHelpers(t *testing.T) {
	t.Parallel()
	if len(Formats()) != 6 {
		t.Fatal("formats")
	}
	if got := joinArgv([]string{"a", "b c", `d"e`, ""}); got != `a "b c" "d\"e" ""` {
		t.Fatalf("joinArgv = %s", got)
	}
	if !hasDotDot(`a\..\b`) || hasDotDot("a..b") {
		t.Fatal("path helpers")
	}
	e := Exec{Shell: true, Script: "echo ${env:TOKEN}"}
	if e.Display() != "echo ${env:TOKEN}" {
		t.Fatal("Display must show the unexpanded script")
	}
	b := Benchmark{Tags: []string{"smoke"}}
	if !b.HasTag("smoke") || b.HasTag("slow") {
		t.Fatal("HasTag")
	}
}

func TestLoadMetrics(t *testing.T) {
	t.Parallel()
	src := `version: "1"
name: metrics
defaults:
  metrics:
    cpu: true
    unsupported: skip
  regression:
    cpu_total: {max_percent: 12}
benchmarks:
  - name: large json
    metrics:
      throughput:
        work: {file_size: "testdata/large file.json"}
      memory: true
    commands:
      tool:
        command: [tool]
        budget:
          latency: {median: "< 100ms", p95: "<= 120ms", p99.9: "<= 1s"}
          throughput: {median: ">= 50MiB/s", p5: "> 10MiB/s"}
          cpu_total: {median: "<= 70ms"}
          cpu_utilization: {max: "<= 180%"}
          peak_rss: {max: "<= 64MiB"}
      other: {command: [other]}
    regression:
      latency: {min_difference: 2ms}
      peak_rss: {max_percent: 5, min_difference: 512KiB}
  - name: records
    metrics:
      cpu: false
      throughput:
        work: {value: 100000, unit: records}
    commands:
      tool:
        command: [tool]
        budget:
          throughput: {median: ">= 1000 records/s"}
  - name: plain
    commands:
      tool: {command: [tool]}
`
	s := mustParse(t, src)
	large, records, plain := s.Benchmarks[0], s.Benchmarks[1], s.Benchmarks[2]
	if !large.Metrics.CPU || !large.Metrics.Memory || large.Metrics.Unsupported != UnsupportedSkip {
		t.Errorf("large metrics = %+v", large.Metrics)
	}
	if w := large.Metrics.Throughput; w == nil || w.FileSize != "testdata/large file.json" || w.Unit != "bytes" || w.Value != 0 {
		t.Errorf("large work = %+v", w)
	}
	if records.Metrics.CPU || records.Metrics.Memory || records.Metrics.Throughput.Value != 100000 || records.Metrics.Throughput.Unit != "records" {
		t.Errorf("records metrics = %+v", records.Metrics)
	}
	if !plain.Metrics.CPU || plain.Metrics.Memory || plain.Metrics.Throughput != nil || plain.Metrics.Collects(metric.GroupThroughput) {
		t.Errorf("plain metrics = %+v", plain.Metrics)
	}
	var got []string
	for _, b := range large.Budgets {
		got = append(got, fmt.Sprintf("%s.%s.%s %s %g", b.Command, b.Metric, b.Aggregation, b.Threshold.Op, b.Threshold.Limit))
	}
	want := []string{
		"tool.latency.median < 1e+08",
		"tool.latency.p95 <= 1.2e+08",
		"tool.latency.p99.9 <= 1e+09",
		"tool.throughput.median >= 5.24288e+07",
		"tool.throughput.p5 > 1.048576e+07",
		"tool.cpu_total.median <= 7e+07",
		"tool.cpu_utilization.max <= 180",
		"tool.peak_rss.max <= 6.7108864e+07",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("budgets:\n got %v\nwant %v", got, want)
	}
	r := large.Regression
	if r.Latency.MinDifference != 2e6 {
		t.Errorf("regression = %+v", r)
	}
	if r.CPU.MaxPercent != 12 || r.Memory.MaxPercent != 5 || r.Memory.MinDifference != 512<<10 || r.Memory.Metric != MetricMedian {
		t.Errorf("inherited regression = %+v", r)
	}
	for name, wantPct := range map[metric.Name]float64{metric.Latency: 10, metric.CPUTotal: 12, metric.PeakRSS: 5} {
		if mr, ok := r.For(name); !ok || mr.MaxPercent != wantPct {
			t.Errorf("For(%s) = %+v, %v", name, mr, ok)
		}
	}
	if _, ok := r.For(metric.CPUUtilization); ok {
		t.Error("cpu utilization must not be comparable")
	}
	if plain.Metrics.WorkUnit() != "" || records.Metrics.WorkUnit() != "records" {
		t.Error("metrics helpers")
	}
}

// TestLoadRejectsMetrics covers the metric rules: names, units, work,
// directions, value types, aggregations and the settings that need a metric
// to be measured.
func TestLoadRejectsMetrics(t *testing.T) {
	t.Parallel()
	withMetrics := func(metrics, budget, regression string) string {
		src := minimal("")
		if metrics != "" {
			src += "    metrics:\n" + indent(metrics, "      ")
		}
		if budget != "" {
			src = strings.Replace(src, "        command: [tool, --version]\n", "        command: [tool, --version]\n        budget:\n"+indent(budget, "          "), 1)
		}
		if regression != "" {
			src += "    regression:\n" + indent(regression, "      ")
		}
		return src
	}
	tests := []struct {
		name  string
		src   string
		want  string
		field string
	}{
		{"unknown metric", withMetrics("gpu: true", "", ""), `unknown key "gpu"`, "benchmarks[0].metrics.gpu"},
		{"latency true is removed", withMetrics("latency: true", "", ""), `unknown key "latency"`, "benchmarks[0].metrics.latency"},
		{"latency is removed", withMetrics("latency: false", "", ""), `unknown key "latency"`, "benchmarks[0].metrics.latency"},
		{"scope is removed", withMetrics("cpu: {scope: process}", "", ""), "expected true or false", "benchmarks[0].metrics.cpu"},
		{"cpu process tree scope is removed", withMetrics("cpu: {scope: process_tree}", "", ""), "expected true or false", "benchmarks[0].metrics.cpu"},
		{"memory process tree scope is removed", withMetrics("memory: {scope: process_tree}", "", ""), "expected true or false", "benchmarks[0].metrics.memory"},
		{"collector type", withMetrics("memory: yes-please", "", ""), "expected true or false", "benchmarks[0].metrics.memory"},
		{"unsupported policy", withMetrics("unsupported: ignore", "", ""), "must be one of: fail, skip", "benchmarks[0].metrics.unsupported"},
		{"zero work", withMetrics("throughput: {work: {value: 0, unit: records}}", "", ""), "must be greater than 0, got 0", "benchmarks[0].metrics.throughput.work.value"},
		{"negative work", withMetrics("throughput: {work: {value: -5}}", "", ""), "must be greater than 0, got -5", "benchmarks[0].metrics.throughput.work.value"},
		{"string work", withMetrics("throughput: {work: {value: \"100\"}}", "", ""), "expected a number, got a string", "benchmarks[0].metrics.throughput.work.value"},
		{"work without amount", withMetrics("throughput: {work: {unit: records}}", "", ""), "work needs a value or a file_size", "benchmarks[0].metrics.throughput.work"},
		{"work with both", withMetrics("throughput: {work: {value: 1, file_size: in.json}}", "", ""), "declares both value and file_size", "benchmarks[0].metrics.throughput.work"},
		{"file size in records", withMetrics("throughput: {work: {file_size: in.json, unit: records}}", "", ""), "its unit must be bytes", "benchmarks[0].metrics.throughput.work.unit"},
		{"work in MiB", withMetrics("throughput: {work: {value: 2, unit: MiB}}", "", ""), `work unit "MiB" is a byte size multiple`, "benchmarks[0].metrics.throughput.work.unit"},
		{"invalid work unit", withMetrics("throughput: {work: {value: 2, unit: \"records/s\"}}", "", ""), "does not match the expected format", "benchmarks[0].metrics.throughput.work.unit"},
		{"absolute file size", withMetrics("throughput: {work: {file_size: /etc/passwd}}", "", ""), "absolute paths are not allowed", "benchmarks[0].metrics.throughput.work.file_size"},
		{"throughput without work", withMetrics("throughput: {}", "", ""), "work is required", "benchmarks[0].metrics.throughput.work"},
		{"throughput in defaults", "version: \"1\"\nname: x\ndefaults: {metrics: {throughput: {work: {value: 1}}}}\nbenchmarks: [{name: a, commands: {a: {command: [a]}}}]\n", `unknown key "throughput"`, "defaults.metrics.throughput"},
		{"budget without metric", withMetrics("", "cpu_total: {median: \"< 1s\"}", ""), "a budget on cpu total needs the benchmark to measure cpu", "benchmarks[0].commands.tool.budget.cpu_total.median"},
		{"throughput budget without work", withMetrics("", "throughput: {median: \">= 1 ops/s\"}", ""), "needs the benchmark to measure throughput", "benchmarks[0].commands.tool.budget.throughput.median"},
		{"latency budget as upper bound", withMetrics("", "latency: {p95: \">= 10ms\"}", ""), `write "<" or "<="`, "benchmarks[0].commands.tool.budget.latency.p95"},
		{"throughput budget as upper bound", withMetrics("throughput: {work: {value: 10}}", "throughput: {median: \"<= 5 operations/s\"}", ""), `write ">" or ">="`, "benchmarks[0].commands.tool.budget.throughput.median"},
		{"memory budget as lower bound", withMetrics("memory: true", "peak_rss: {max: \">= 1MiB\"}", ""), `write "<" or "<="`, "benchmarks[0].commands.tool.budget.peak_rss.max"},
		{"memory budget with a duration", withMetrics("memory: true", "peak_rss: {max: \"<= 10ms\"}", ""), "invalid budget", "benchmarks[0].commands.tool.budget.peak_rss.max"},
		{"cpu budget with bytes", withMetrics("cpu: true", "cpu_total: {median: \"<= 1MiB\"}", ""), "invalid budget", "benchmarks[0].commands.tool.budget.cpu_total.median"},
		{"utilization without percent", withMetrics("cpu: true", "cpu_utilization: {median: \"<= 150\"}", ""), "invalid budget", "benchmarks[0].commands.tool.budget.cpu_utilization.median"},
		{"invalid unit", withMetrics("memory: true", "peak_rss: {max: \"<= 64MB/s\"}", ""), "invalid budget", "benchmarks[0].commands.tool.budget.peak_rss.max"},
		{"lowercase unit", withMetrics("memory: true", "peak_rss: {max: \"<= 64mib\"}", ""), "invalid budget", "benchmarks[0].commands.tool.budget.peak_rss.max"},
		{"zero byte budget", withMetrics("memory: true", "peak_rss: {max: \"<= 0MiB\"}", ""), "greater than zero", "benchmarks[0].commands.tool.budget.peak_rss.max"},
		{"rate unit mismatch", withMetrics("throughput: {work: {value: 10, unit: records}}", "throughput: {median: \">= 5MiB/s\"}", ""), "the budget is in bytes/s but the declared work unit is records", "benchmarks[0].commands.tool.budget.throughput.median"},
		{"rate unit mismatch the other way", withMetrics("throughput: {work: {file_size: in.json}}", "throughput: {median: \">= 5 records/s\"}", ""), "the budget is in records/s but the declared work unit is bytes", "benchmarks[0].commands.tool.budget.throughput.median"},
		{"unsupported aggregation", withMetrics("", "latency: {stddev: \"< 1ms\"}", ""), `unknown aggregation "stddev"`, "benchmarks[0].commands.tool.budget.latency.stddev"},
		{"percentile 100", withMetrics("", "latency: {p100: \"< 1ms\"}", ""), `unknown aggregation "p100"`, "benchmarks[0].commands.tool.budget.latency.p100"},
		{"percentile 0", withMetrics("", "latency: {p0: \"< 1ms\"}", ""), `unknown aggregation "p0"`, "benchmarks[0].commands.tool.budget.latency.p0"},
		{"latency shorthand is removed", withMetrics("", "median: \"< 1s\"", ""), `unknown key "median"`, "benchmarks[0].commands.tool.budget.median"},
		{"mean shorthand is removed", withMetrics("", "mean: \"< 1s\"", ""), `unknown key "mean"`, "benchmarks[0].commands.tool.budget.mean"},
		{"min shorthand is removed", withMetrics("", "min: \"< 1s\"", ""), `unknown key "min"`, "benchmarks[0].commands.tool.budget.min"},
		{"max shorthand is removed", withMetrics("", "max: \"< 1s\"", ""), `unknown key "max"`, "benchmarks[0].commands.tool.budget.max"},
		{"nested cpu budget is removed", withMetrics("cpu: true", "cpu: {total: {median: \"<= 1s\"}}", ""), `unknown key "cpu"`, "benchmarks[0].commands.tool.budget.cpu"},
		{"nested memory budget is removed", withMetrics("memory: true", "memory: {peak_rss: {max: \"<= 1MiB\"}}", ""), `unknown key "memory"`, "benchmarks[0].commands.tool.budget.memory"},
		{"empty cpu budget", withMetrics("cpu: true", "cpu_total: {}", ""), "must contain at least 1 entry", "benchmarks[0].commands.tool.budget.cpu_total"},
		{"regression for an unmeasured metric", withMetrics("", "", "peak_rss: {max_percent: 5}"), "regression.peak_rss is set but this benchmark does not measure memory", "benchmarks[0].regression.peak_rss"},
		{"regression percent threshold", withMetrics("cpu: true", "", "cpu_total: {max_percent: 0}"), "percentage greater than 0", "benchmarks[0].regression.cpu_total.max_percent"},
		{"regression min_difference type", withMetrics("memory: true", "", "peak_rss: {min_difference: 5ms}"), "invalid byte size", "benchmarks[0].regression.peak_rss.min_difference"},
		{"latency min_difference type", withMetrics("", "", "latency: {min_difference: 1MiB}"), "invalid duration", "benchmarks[0].regression.latency.min_difference"},
		{"gate for an unmeasured metric", withMetrics("", "", "cpu_total: {gate: false}"), "regression.cpu_total is set but this benchmark does not measure cpu", "benchmarks[0].regression.cpu_total"},
		{"cpu regression name is removed", withMetrics("cpu: true", "", "cpu: {max_percent: 5}"), `unknown key "cpu"`, "benchmarks[0].regression.cpu"},
		{"memory regression name is removed", withMetrics("memory: true", "", "memory: {max_percent: 5}"), `unknown key "memory"`, "benchmarks[0].regression.memory"},
		{"independent throughput regression is rejected", withMetrics("throughput: {work: {value: 5, unit: records}}", "", "throughput: {max_percent: 10}"), `unknown key "throughput"`, "benchmarks[0].regression.throughput"},
		{"regression statistic p95", withMetrics("cpu: true", "", "cpu_total: {statistic: p95}"), "must be one of: median, mean", "benchmarks[0].regression.cpu_total.statistic"},
		{"latency metric key is removed", withMetrics("", "", "latency: {metric: mean}"), `unknown key "metric"`, "benchmarks[0].regression.latency.metric"},
		{"cpu metric key is removed", withMetrics("cpu: true", "", "cpu_total: {metric: mean}"), `unknown key "metric"`, "benchmarks[0].regression.cpu_total.metric"},
		{"memory metric key is removed", withMetrics("memory: true", "", "peak_rss: {metric: mean}"), `unknown key "metric"`, "benchmarks[0].regression.peak_rss.metric"},
		{"samples format in report", "version: \"1\"\nname: x\nbenchmarks: [{name: a, commands: {a: {command: [a]}}}]\nreport: {outputs: [{format: samples, path: x.csv}]}\n", "must be one of", "report.outputs[0].format"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseString(t, tt.src)
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Load() error = %v, want a ValidationError\n%s", err, tt.src)
			}
			for _, is := range verr.Issues {
				if strings.Contains(is.Message, tt.want) && is.Field == tt.field {
					if is.Line == 0 {
						t.Errorf("issue %q has no position", is.Message)
					}
					return
				}
			}
			t.Fatalf("no issue with message %q at %q; issues:\n%v\nsource:\n%s", tt.want, tt.field, err, tt.src)
		})
	}
}

// TestLoadReportsAWrongWorkUnitOnce pins that a work unit himorime rejects is
// not used to build the hints of the budget and tolerance errors that follow
// it: a writer who fixes the unit must not be told to write it again.
func TestLoadReportsAWrongWorkUnitOnce(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		src   string
		want  []string
		field string
	}{
		{
			name: "byte multiple with a byte rate everywhere",
			src: addCommandBudget(minimal(`metrics:
      throughput: {work: {value: 1000, unit: KB}}
`), `{throughput: {median: ">= 1KB/s"}}`),
			want:  []string{`work unit "KB" is a byte size multiple`},
			field: "benchmarks[0].metrics.throughput.work.unit",
		},
		{
			name: "file size in records with a byte rate budget",
			src: addCommandBudget(minimal(`metrics:
      throughput: {work: {file_size: in.json, unit: records}}
`), `{throughput: {median: ">= 1MiB/s"}}`),
			want:  []string{"its unit must be bytes"},
			field: "benchmarks[0].metrics.throughput.work.unit",
		},
		{
			name: "byte multiple with a budget in another unit",
			src: addCommandBudget(minimal(`metrics:
      throughput: {work: {value: 1000, unit: KB}}
`), `{throughput: {median: ">= 1000 records/s"}}`),
			want:  []string{`work unit "KB" is a byte size multiple`, "the budget is in records/s"},
			field: "benchmarks[0].metrics.throughput.work.unit",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseString(t, tt.src)
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Load() error = %v, want a ValidationError\n%s", err, tt.src)
			}
			if len(verr.Issues) != len(tt.want) {
				t.Fatalf("issues = %d, want %d:\n%v\nsource:\n%s", len(verr.Issues), len(tt.want), err, tt.src)
			}
			for i, w := range tt.want {
				if !strings.Contains(verr.Issues[i].Message, w) {
					t.Errorf("issue %d = %q, want it to contain %q", i, verr.Issues[i].Message, w)
				}
			}
			if verr.Issues[0].Field != tt.field {
				t.Errorf("first issue field = %q, want %q", verr.Issues[0].Field, tt.field)
			}
		})
	}
}

// TestLoadRejectsARelativePathThatLeavesTheProject pins that validation
// answers the same question the run answers: a relative path whose .. leaves
// the repository holding the suite, or the suite's own directory when it is
// not in a repository, is rejected before anything is built or measured.
func TestLoadRejectsARelativePathThatLeavesTheProject(t *testing.T) {
	t.Parallel()
	write := func(t *testing.T, dir, src string) (*Suite, error) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "himorime.yaml")
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		return Load(p)
	}
	suite := func(cwd string) string {
		return "version: \"1\"\nname: x\nbenchmarks:\n  - name: a\n    commands:\n      tool: {command: [tool], cwd: " + cwd + "}\n"
	}

	t.Run("outside a repository", func(t *testing.T) {
		t.Parallel()
		_, err := write(t, t.TempDir(), suite("../.."))
		var verr *ValidationError
		if !errors.As(err, &verr) {
			t.Fatalf("Load() error = %v, want a ValidationError", err)
		}
		is := verr.Issues[0]
		if is.Field != "benchmarks[0].commands.tool.cwd" || !strings.Contains(is.Message, "leaves the project") {
			t.Errorf("issue = %+v, want the escaping path", is)
		}
		if is.Line == 0 {
			t.Errorf("issue %q has no position", is.Message)
		}
	})

	t.Run("inside a repository", func(t *testing.T) {
		t.Parallel()
		top := t.TempDir()
		if err := os.MkdirAll(filepath.Join(top, ".git"), 0o750); err != nil {
			t.Fatal(err)
		}
		if _, err := write(t, filepath.Join(top, "bench"), suite("..")); err != nil {
			t.Errorf("a .. that stays in the repository must be accepted: %v", err)
		}
		if _, err := write(t, filepath.Join(top, "bench2"), suite("../..")); err == nil {
			t.Error("a .. that leaves the repository must be rejected")
		}
		for _, cwd := range []string{`"${head_root}/.."`, `"${root}/../testdata"`} {
			if _, err := write(t, filepath.Join(top, "bench3"), suite(cwd)); err != nil {
				t.Errorf("%s stays in the repository and must be accepted: %v", cwd, err)
			}
		}
		for _, cwd := range []string{`"${head_root}/../.."`, `"${root}/../../x"`} {
			if _, err := write(t, filepath.Join(top, "bench4"), suite(cwd)); err == nil || !strings.Contains(err.Error(), "leaves the project") {
				t.Errorf("%s leaves the repository and must be rejected, got %v", cwd, err)
			}
		}
	})

	t.Run("every path setting", func(t *testing.T) {
		t.Parallel()
		for _, src := range []string{
			"version: \"1\"\nname: x\nbuild: {command: [go, build], cwd: ../..}\nbenchmarks:\n  - name: a\n    commands: {tool: {command: [tool]}}\n",
			"version: \"1\"\nname: x\nbenchmarks:\n  - name: a\n    stdin: ../../in.txt\n    commands: {tool: {command: [tool]}}\n",
			"version: \"1\"\nname: x\nbenchmarks:\n  - name: a\n    metrics: {throughput: {work: {file_size: ../../in.txt}}}\n    commands: {tool: {command: [tool]}}\n",
			"version: \"1\"\nname: x\nbenchmarks:\n  - name: a\n    commands: {tool: {command: [tool]}}\nreport: {outputs: [{format: json, path: ../../out.json}]}\n",
		} {
			if _, err := write(t, t.TempDir(), src); err == nil {
				t.Errorf("accepted an escaping path:\n%s", src)
			}
		}
	})
}

func TestResolveHelperErrorsAndImplicitWorkUnit(t *testing.T) {
	t.Parallel()
	v := &validator{dir: t.TempDir(), projectRoot: t.TempDir()}
	if got, unit := v.quantity(path{"budget"}, metric.KindDuration, "not-a-duration"); got != 0 || unit != "" || len(v.issues) != 1 {
		t.Fatalf("invalid quantity = %v %q, issues = %+v", got, unit, v.issues)
	}
	if got := refName(Ref{Name: "env", Env: "TOKEN"}); got != "env:TOKEN" || refName(Ref{Name: VarRoot}) != VarRoot {
		t.Fatalf("refName = %q", got)
	}
	if (&validator{}).leavesProject("../outside") {
		t.Fatal("leavesProject without roots reported an escape")
	}
	value := 5.0
	w := v.resolveWork(path{"metrics"}, RawWork{Value: &value})
	if w == nil || w.Value != value || w.Unit != "operations" {
		t.Fatalf("implicit work unit = %+v", w)
	}
	before := len(v.issues)
	if _, ok := v.resolveBudget("tool", budgetEntry{metric: metric.Latency, agg: "unknown", expr: "< 1ms", path: path{"budget"}}, Metrics{}); ok || len(v.issues) != before+1 {
		t.Fatalf("invalid aggregation issues = %+v", v.issues)
	}
}
