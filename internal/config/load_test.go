package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
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
	want := Regression{Metric: MetricMedian, MaxPercent: 10, Confidence: 0.95, MinSamples: 10, MaxCV: 0.5}
	if !reflect.DeepEqual(b.Regression, want) {
		t.Errorf("regression defaults = %+v, want %+v", b.Regression, want)
	}
	if filepath.Base(s.Path) != "yahiko.yaml" || s.Dir != filepath.Dir(s.Path) {
		t.Errorf("paths = %s, %s", s.Path, s.Dir)
	}
}

func TestLoadInheritanceOrder(t *testing.T) {
	t.Parallel()
	src := `version: "1"
suite: {name: inherit}
defaults:
  warmup: 3
  runs: 20
  timeout: 5s
  cwd: fixtures
  env: {A: defaults, B: defaults}
  stdout: default.out
  exit_codes: [0, 1]
  regression:
    metric: mean
    max_percent: "5%"
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
      max_percent: 15
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
	if r.Metric != MetricMean || r.MaxPercent != 15 || r.Confidence != 0.9 || r.MinSamples != 12 || r.MaxCV != 0 || !reflect.DeepEqual(r.Commands, []string{"one"}) {
		t.Errorf("benchmark regression = %+v", r)
	}
	if second.Regression.MaxPercent != 5 || second.Regression.Commands != nil {
		t.Errorf("inherited regression = %+v", second.Regression)
	}
	if !r.Compares("one") || r.Compares("two") || !second.Regression.Compares("three") {
		t.Error("Compares does not follow regression.commands")
	}
}

func TestLoadKeepsCommandOrderAndForms(t *testing.T) {
	t.Parallel()
	src := `version: "1"
suite: {name: order}
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
      alpha:
        command: "cat ${workdir}/in.txt | wc -l"
        shell: true
      mid:
        command: [mid]
    budget:
      alpha: {max: "<= 1s"}
      zeta: {median: "< 20ms", mean: "< 30ms"}
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
		budgets = append(budgets, bud.Command+"."+string(bud.Metric)+bud.Expr.Operator())
	}
	if !reflect.DeepEqual(budgets, []string{"zeta.mean<", "zeta.median<", "alpha.max<="}) {
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

func TestLoadRejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		src   string
		want  string
		field string
	}{
		{"unknown top-level key", `version: "1"
suite: {name: x}
benchmark: []
`, `unknown key "benchmark"`, "benchmark"},
		{"missing version", "suite: {name: x}\nbenchmarks: [{name: a, commands: {a: {command: [a]}}}]\n", "version is required", "version"},
		{"unsupported version", "version: \"2\"\nsuite: {name: x}\nbenchmarks: [{name: a, commands: {a: {command: [a]}}}]\n", "must be one of: 1", "version"},
		{"blank suite name", "version: \"1\"\nsuite: {name: \" \"}\nbenchmarks: [{name: a, commands: {a: {command: [a]}}}]\n", "must not be blank", "suite.name"},
		{"no benchmarks", "version: \"1\"\nsuite: {name: x}\nbenchmarks: []\n", "must contain at least 1 item", "benchmarks"},
		{"typo in command key", minimal("") + "        shel: true\n", `unknown key "shel"`, "benchmarks[0].commands.tool.shel"},
		{"string command without shell", `version: "1"
suite: {name: x}
benchmarks:
  - name: a
    commands:
      t: {command: "echo hi"}
`, "a string command requires shell: true", "benchmarks[0].commands.t"},
		{"list command with shell", `version: "1"
suite: {name: x}
benchmarks:
  - name: a
    commands:
      t: {command: [echo, hi], shell: true}
`, "a string command requires shell: true", "benchmarks[0].commands.t"},
		{"empty program", `version: "1"
suite: {name: x}
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
		{"bad budget", minimal("budget: {tool: {median: \"20ms\"}}"), "invalid budget", "benchmarks[0].budget.tool.median"},
		{"unknown budget metric", minimal("budget: {tool: {p99: \"< 20ms\"}}"), `unknown key "p99"`, "benchmarks[0].budget.tool.p99"},
		{"empty budget", minimal("budget: {tool: {}}"), "must contain at least 1 entry", "benchmarks[0].budget.tool"},
		{"absolute stdin", minimal("stdin: /etc/passwd"), "absolute paths are not allowed", "benchmarks[0].stdin"},
		{"windows absolute stdin", minimal(`stdin: 'C:\data.txt'`), "absolute paths are not allowed", "benchmarks[0].stdin"},
		{"escaping stdout", minimal("stdout: ../escape.txt"), "relative path inside ${workdir}", "benchmarks[0].stdout"},
		{"bad metric", minimal("regression: {metric: p95}"), "must be one of: median, mean", "benchmarks[0].regression.metric"},
		{"zero max_percent", minimal("regression: {max_percent: 0}"), "percentage greater than 0", "benchmarks[0].regression.max_percent"},
		{"low confidence", minimal("regression: {confidence: 0.3}"), "must be at least 0.5, got 0.3", "benchmarks[0].regression.confidence"},
		{"bad env name", minimal("env: {\"1X\": y}"), "invalid environment variable name", "benchmarks[0].env.1X"},
		{"bad report format", "version: \"1\"\nsuite: {name: x}\nbenchmarks: [{name: a, commands: {a: {command: [a]}}}]\nreport: {outputs: [{format: table, path: x.txt}]}\n", "must be one of", "report.outputs[0].format"},
		{"yaml syntax", "version: \"1\"\nsuite: {name: x\n", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseString(t, tt.src)
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Load() error = %v, want a ValidationError", err)
			}
			if !IsValidation(err) {
				t.Fatal("IsValidation = false")
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
		{"budget for unknown command", "budget: {ghost: {median: \"< 1s\"}}", `budget refers to unknown command "ghost"`},
		{"regression command unknown", "regression: {commands: [ghost]}", `"ghost" is not a command of this benchmark`},
		{"max below min runs", "min_runs: 20\nmax_runs: 10", "max_runs (10) is lower than min_runs (20)"},
		{"artifact without build", "setup:\n  - command: [\"${artifact}\"]", "${artifact} is used but the suite has no build section"},
		{"unknown variable", "cwd: \"${home}/x\"", "unknown variable ${home}"},
		{"variable not at start", "cwd: \"x/${root}\"", "${root} may only start a path"},
		{"dotdot after variable", "stdin: \"${workdir}/../x\"", "must not contain .."},
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
suite: {name: x}
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
suite: {name: x}
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
suite: {name: x}
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
	if is.Line != 9 || is.Column != 15 || is.Field != "benchmarks[0].baseline" || is.Hint == "" {
		t.Fatalf("issue = %+v", is)
	}
	if !strings.HasPrefix(is.String(), filepath.Join(filepath.Dir(is.File), "yahiko.yaml")+":9:15: benchmarks[0].baseline:") {
		t.Fatalf("String() = %q", is.String())
	}
}

func TestLoadFileErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := Load(filepath.Join(dir, "missing.yaml")); !IsValidation(err) || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("missing file: %v", err)
	}
	if _, err := Load(dir); !IsValidation(err) || !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("directory: %v", err)
	}
	empty := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(empty, []byte("\n  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(empty); !IsValidation(err) || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty file: %v", err)
	}
	big := filepath.Join(dir, "big.yaml")
	if err := os.WriteFile(big, make([]byte, maxFileSize+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(big); !IsValidation(err) || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("big file: %v", err)
	}
}

func TestFormatsAndHelpers(t *testing.T) {
	t.Parallel()
	if len(Formats()) != 5 {
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
