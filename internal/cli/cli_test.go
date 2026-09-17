package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nao1215/himorime/internal/config"
	"github.com/nao1215/himorime/internal/exitcode"
)

// helperPath is the portable helper program from test/e2e/helper, built once
// without instrumentation. Measuring the test binary itself would be too slow
// under the race detector, where its start-up alone takes about a second and
// hides the delays the comparisons are about.
var helperPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "himorime-cli-test-")
	if err != nil {
		panic(err)
	}
	helperPath = filepath.Join(dir, "helper")
	if runtime.GOOS == "windows" {
		helperPath += ".exe"
	}
	build := exec.Command("go", "build", "-o", helperPath, "./test/e2e/helper")
	build.Dir = filepath.Join("..", "..")
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=")
	if out, err := build.CombinedOutput(); err != nil {
		_ = os.RemoveAll(dir)
		panic(fmt.Sprintf("build the helper: %v\n%s", err, out))
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	code   int
	stdout string
	stderr string
}

type env map[string]string

func run(t *testing.T, dir string, e env, args ...string) result {
	t.Helper()
	return runWith(t, dir, e, nil, args...)
}

// runWith is run with a hook that adjusts the App before it runs, such as
// replacing what the platform can measure.
func runWith(t *testing.T, dir string, e env, adjust func(*App), args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	base := os.Environ()
	for k, v := range e {
		base = append(base, k+"="+v)
	}
	app := &App{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
		LookupEnv: func(k string) (string, bool) {
			if v, ok := e[k]; ok {
				return v, true
			}
			if k == "GITHUB_ACTIONS" || k == "GITHUB_EVENT_NAME" || k == "GITHUB_EVENT_PATH" || k == "GITHUB_STEP_SUMMARY" || k == "HIMORIME_BASE_REF" || k == "NO_COLOR" {
				return "", false
			}
			return os.LookupEnv(k)
		},
		Environ:  func() []string { return base },
		Getwd:    func() (string, error) { return dir, nil },
		ReadFile: os.ReadFile,
		Now:      time.Now,
	}
	if adjust != nil {
		adjust(app)
	}
	// Commands resolve relative paths against the process working directory.
	chdir(t, dir)
	code := app.Run(context.Background(), args)
	return result{code, stdout.String(), stderr.String()}
}

// chdir is serialized by the callers: the tests in this file do not run in
// parallel because the working directory is process-wide.
func chdir(t *testing.T, dir string) {
	t.Helper()
	t.Chdir(dir)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func helperExe(t *testing.T) string {
	t.Helper()
	return filepath.ToSlash(helperPath)
}

func suite(t *testing.T, body string) string {
	t.Helper()
	exe := helperExe(t)
	return strings.ReplaceAll(body, "@EXE@", strconv.Quote(exe))
}

func TestHelpVersionAndUsageErrors(t *testing.T) {
	dir := t.TempDir()
	if r := run(t, dir, nil); r.code != exitcode.Usage || !strings.Contains(r.stderr, "Commands:") {
		t.Fatalf("no args: %+v", r)
	}
	if r := run(t, dir, nil, "frobnicate"); r.code != exitcode.Usage || !strings.Contains(r.stderr, `unknown command "frobnicate"`) {
		t.Fatalf("unknown command: %+v", r)
	}
	if r := run(t, dir, nil, "--help"); r.code != 0 || !strings.Contains(r.stdout, "compare") {
		t.Fatalf("--help: %+v", r)
	}
	if r := run(t, dir, nil, "help", "run"); r.code != 0 || !strings.Contains(r.stdout, "--filter") {
		t.Fatalf("help run: %+v", r)
	}
	if r := run(t, dir, nil, "help", "nope"); r.code != exitcode.Usage {
		t.Fatalf("help nope: %+v", r)
	}
	if r := run(t, dir, nil, "run", "--help"); r.code != 0 || !strings.Contains(r.stdout, "Usage: himorime run") || !strings.Contains(r.stdout, "--skip-tag") {
		t.Fatalf("run --help: %+v", r)
	}
	if r := run(t, dir, nil, "version"); r.code != 0 || !strings.HasPrefix(r.stdout, "himorime ") || !strings.Contains(r.stdout, runtime.GOOS+"/"+runtime.GOARCH) {
		t.Fatalf("version: %+v", r)
	}
	if r := run(t, dir, nil, "--version"); r.code != 0 {
		t.Fatalf("--version: %+v", r)
	}
	if r := run(t, dir, nil, "run", "--bogus"); r.code != exitcode.Usage || !strings.Contains(r.stderr, "flag provided but not defined: -bogus") {
		t.Fatalf("bad flag: %+v", r)
	}
	if r := run(t, dir, nil, "run", "--format", "xml"); r.code != exitcode.Usage || !strings.Contains(r.stderr, `unknown --format "xml"`) {
		t.Fatalf("bad format: %+v", r)
	}
	if r := run(t, dir, nil, "run", "--seed", "-1"); r.code != exitcode.Usage {
		t.Fatalf("bad seed: %+v", r)
	}
	for _, args := range [][]string{{"run", "--runs", "0"}, {"run", "--warmup", "-1"}, {"compare", "--runs", "x"}} {
		if r := run(t, dir, nil, args...); r.code != exitcode.Usage || !strings.Contains(r.stderr, "must be an integer") {
			t.Fatalf("%v: %+v", args, r)
		}
	}
	if r := run(t, dir, nil, "compare"); r.code != exitcode.Usage || !strings.Contains(r.stderr, "--against is required") {
		t.Fatalf("compare without --against: %+v", r)
	}
	if r := run(t, dir, nil, "run", "--filter", "("); r.code != exitcode.Usage || !strings.Contains(r.stderr, "invalid --filter") {
		t.Fatalf("bad filter: %+v", r)
	}
}

func TestInit(t *testing.T) {
	dir := t.TempDir()
	r := run(t, dir, nil, "init")
	if r.code != 0 || !strings.Contains(r.stdout, "wrote himorime.yaml") {
		t.Fatalf("init: %+v", r)
	}
	data, err := os.ReadFile(filepath.Join(dir, "himorime.yaml"))
	if err != nil || !strings.HasPrefix(string(data), "# yaml-language-server: $schema=https://raw.githubusercontent.com/nao1215/himorime/main/schema/himorime.schema.json") {
		t.Fatalf("init wrote %q, %v", data, err)
	}
	write(t, filepath.Join(dir, "himorime.yaml"), "keep me")
	r = run(t, dir, nil, "init")
	if r.code != exitcode.Usage || !strings.Contains(r.stderr, "already exists") {
		t.Fatalf("init over an existing file: %+v", r)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "himorime.yaml")); string(data) != "keep me" {
		t.Fatal("init overwrote an existing file without --force")
	}
	if r := run(t, dir, nil, "init", "--force"); r.code != 0 {
		t.Fatalf("init --force: %+v", r)
	}
	if r := run(t, dir, nil, "init", "bench/custom.himorime.yaml"); r.code != exitcode.Execution {
		t.Fatalf("init into a missing directory: %+v", r)
	}
	if r := run(t, dir, nil, "init", "a.yaml", "b.yaml"); r.code != exitcode.Usage {
		t.Fatalf("init with two paths: %+v", r)
	}
	if r := run(t, dir, nil, "validate"); r.code != 0 || !strings.Contains(r.stdout, "himorime.yaml: ok (1 benchmark, 1 command)") {
		t.Fatalf("validate the generated file: %+v", r)
	}
}

func TestInitTemplateIsValid(t *testing.T) {
	t.Parallel()
	s, err := config.Parse("init.yaml", filepath.Join(t.TempDir(), "himorime.yaml"), []byte(InitTemplate))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Benchmarks) == 0 {
		t.Fatal("the template has no benchmark")
	}
}

func TestValidateReportsEveryIssue(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "himorime.yaml"), `version: "1"
suite: {name: broken}
benchmarks:
  - name: a
    runs: "3"
    baseline: ghost
    commands:
      x: {command: [x], bogus: 1}
`)
	r := run(t, dir, nil, "validate")
	if r.code != exitcode.Config {
		t.Fatalf("validate: %+v", r)
	}
	for _, want := range []string{"himorime.yaml:5:11: benchmarks[0].runs: expected an integer, got a string", `unknown key "bogus"`, "nothing was run"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, r.stderr)
		}
	}
	if r.stdout != "" {
		t.Errorf("validation errors must not go to stdout: %q", r.stdout)
	}
	if r := run(t, dir, nil, "validate", "missing.yaml"); r.code != exitcode.Config || !strings.Contains(r.stderr, "does not exist") {
		t.Fatalf("missing file: %+v", r)
	}
	empty := t.TempDir()
	if r := run(t, empty, nil, "validate", "."); r.code != exitcode.Config || !strings.Contains(r.stderr, "contains no himorime.yaml") {
		t.Fatalf("empty directory: %+v", r)
	}
}

const twoBenchmarks = `version: "1"
suite: {name: cli}
defaults: {warmup: 0, runs: 3}
benchmarks:
  - name: fast one
    tags: [smoke]
    stdin: {content: "x"}
    baseline: quick
    commands:
      quick:
        command: [@EXE@, sleep, 1ms]
      slower:
        command: [@EXE@, sleep, 30ms]
  - name: slow one
    tags: [slow]
    stdin: fixtures/in.txt
    commands:
      quick:
        command: [@EXE@, sleep, 1ms]
`

func TestListAndSelection(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "himorime.yaml"), suite(t, twoBenchmarks))
	r := run(t, dir, nil, "list")
	if r.code != 0 {
		t.Fatalf("list: %+v", r)
	}
	lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "SUITE") || !strings.Contains(lines[1], "fast one") || !strings.Contains(lines[1], "quick*") || !strings.Contains(lines[3], "fixtures/in.txt") {
		t.Fatalf("list output:\n%s", r.stdout)
	}
	r = run(t, dir, nil, "list", "--format", "json", "--tag", "slow")
	var entries []ListEntry
	if err := json.Unmarshal([]byte(r.stdout), &entries); err != nil || len(entries) != 1 || entries[0].Benchmark != "slow one" {
		t.Fatalf("list --format json --tag slow: %v %+v", err, r)
	}
	r = run(t, dir, nil, "list", "--skip-tag", "slow", "--filter", "fast")
	if strings.Count(r.stdout, "\n") != 3 {
		t.Fatalf("list --skip-tag: %s", r.stdout)
	}
	if r := run(t, dir, nil, "list", "--format", "yaml"); r.code != exitcode.Usage {
		t.Fatalf("list --format yaml: %+v", r)
	}
	if r := run(t, dir, nil, "run", "--tag", "nothing"); r.code != exitcode.Usage || !strings.Contains(r.stderr, "no benchmark matches") {
		t.Fatalf("empty selection: %+v", r)
	}
}

func TestRunTableJSONAndOutputs(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "fixtures", "in.txt"), "data\n")
	write(t, filepath.Join(dir, "himorime.yaml"), suite(t, twoBenchmarks)+"report:\n  outputs:\n    - {format: markdown, path: out/report.md}\n    - {format: csv, path: out/report.csv}\n")
	r := run(t, dir, nil, "run", "--tag", "smoke", "--seed", "99", "--quiet")
	if r.code != 0 {
		t.Fatalf("run: %+v", r)
	}
	for _, want := range []string{"BENCHMARK", "fast one", "quick", "slower", "PASS", "seed 99"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, r.stdout)
		}
	}
	if strings.Contains(r.stdout, "slow one") || r.stderr != "" {
		t.Errorf("selection or --quiet ignored: stdout=%s stderr=%s", r.stdout, r.stderr)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "out", "report.md")); err != nil || !strings.Contains(string(data), "| fast one | quick |") {
		t.Fatalf("markdown output: %q %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "out", "report.csv")); err != nil || !strings.HasPrefix(string(data), "suite,benchmark,command") {
		t.Fatalf("csv output: %q %v", data, err)
	}

	r = run(t, dir, nil, "run", "himorime.yaml", "--format", "json", "--output", "result.json")
	if r.code != 0 || r.stdout != "" || !strings.Contains(r.stderr, `benchmark "slow one"`) {
		t.Fatalf("run --output: %+v", r)
	}
	var rep struct {
		Mode   string `json:"mode"`
		Suites []struct {
			Benchmarks []struct {
				Commands []struct {
					Head struct {
						Count   int `json:"count"`
						Metrics map[string]struct {
							Samples []float64 `json:"samples"`
						} `json:"metrics"`
					} `json:"head"`
				} `json:"commands"`
			} `json:"benchmarks"`
		} `json:"suites"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "result.json"))
	if err != nil || json.Unmarshal(data, &rep) != nil {
		t.Fatalf("result.json: %v\n%s", err, data)
	}
	if rep.Mode != "run" || len(rep.Suites[0].Benchmarks) != 2 || len(rep.Suites[0].Benchmarks[0].Commands[0].Head.Metrics["latency"].Samples) != 3 {
		t.Fatalf("report = %+v", rep)
	}
}

func TestRunExitCodes(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "budget.himorime.yaml"), suite(t, `version: "1"
suite: {name: budget}
defaults: {warmup: 0, runs: 3}
benchmarks:
  - name: too slow
    commands:
      tool:
        command: [@EXE@, sleep, 60ms]
    budget:
      tool: {median: "< 5ms"}
`))
	r := run(t, dir, nil, "run", "budget.himorime.yaml", "--quiet")
	if r.code != exitcode.Failed || !strings.Contains(r.stdout, "OVER BUDGET") || !strings.Contains(r.stdout, "budget median < 5.00ms not met") {
		t.Fatalf("budget: %+v", r)
	}

	write(t, filepath.Join(dir, "fail.himorime.yaml"), suite(t, `version: "1"
suite: {name: fail}
defaults: {warmup: 0, runs: 2}
benchmarks:
  - name: broken
    commands:
      tool:
        command: [@EXE@, exit, "2", "failing with ${env:SECRET_TOKEN}"]
`))
	r = run(t, dir, env{"SECRET_TOKEN": "hunter2-very-secret"}, "run", "fail.himorime.yaml", "--quiet")
	if r.code != exitcode.Execution || !strings.Contains(r.stdout, "ERROR") || !strings.Contains(r.stdout, "exited with status 2") {
		t.Fatalf("command failure: %+v", r)
	}
	if strings.Contains(r.stdout+r.stderr, "hunter2-very-secret") || !strings.Contains(r.stdout, "failing with ***") {
		t.Fatalf("a secret leaked or stderr was not shown:\n%s", r.stdout)
	}

	write(t, filepath.Join(dir, "timeout.himorime.yaml"), suite(t, `version: "1"
suite: {name: timeout}
defaults: {warmup: 0, runs: 2}
benchmarks:
  - name: hangs
    commands:
      tool:
        command: [@EXE@, sleep, 1m]
        timeout: 200ms
`))
	start := time.Now()
	r = run(t, dir, nil, "run", "timeout.himorime.yaml", "--quiet")
	if r.code != exitcode.Execution || !strings.Contains(r.stdout, "timed out after 200ms") || time.Since(start) > 30*time.Second {
		t.Fatalf("timeout: %+v", r)
	}

	r = run(t, dir, nil, "run", filepath.Join(dir, "missing.yaml"))
	if r.code != exitcode.Config {
		t.Fatalf("missing suite: %+v", r)
	}

	if r := run(t, dir, nil, "run", "budget.himorime.yaml", "--quiet", "--output", filepath.Join(dir, "no-such-dir", "x", "\x00bad")); r.code != exitcode.Execution {
		t.Fatalf("an unwritable report must fail: %+v", r)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_GLOBAL="+cfg, "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

const compareSuite = `version: "1"
suite: {name: compare}
build:
  command: [@EXE@, copy, delay.txt, "${artifact}"]
defaults:
  warmup: 0
  runs: 10
benchmarks:
  - name: sleepy
    commands:
      app:
        command: [@EXE@, sleep-from, "${artifact}"]
`

func compareRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, filepath.Join(dir, "delay.txt"), "20ms\n")
	write(t, filepath.Join(dir, "himorime.yaml"), suite(t, compareSuite))
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "base")
	return dir
}

func TestCompare(t *testing.T) {
	dir := compareRepo(t)
	r := run(t, dir, nil, "compare", "--against", "main", "--quiet")
	if r.code != 0 || !strings.Contains(r.stdout, "PASS") || !strings.Contains(r.stdout, "CHANGE") {
		t.Fatalf("unchanged compare: %+v", r)
	}

	write(t, filepath.Join(dir, "delay.txt"), "120ms\n")
	r = run(t, dir, nil, "compare", "--against", "main", "--format", "json")
	if r.code != exitcode.Failed {
		t.Fatalf("regression: %+v", r)
	}
	var rep struct {
		Mode string `json:"mode"`
		Git  struct {
			Dirty   bool   `json:"dirty"`
			BaseRef string `json:"base_ref"`
			BaseSHA string `json:"base_sha"`
		} `json:"git"`
		Summary struct {
			Regression int `json:"regression"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil {
		t.Fatalf("%v\n%s", err, r.stdout)
	}
	if rep.Mode != "compare" || !rep.Git.Dirty || rep.Git.BaseRef != "main" || len(rep.Git.BaseSHA) != 40 || rep.Summary.Regression != 1 {
		t.Fatalf("report = %+v", rep)
	}
	if !strings.Contains(r.stderr, "including uncommitted changes") {
		t.Errorf("stderr lacks the dirty note:\n%s", r.stderr)
	}
	if status := git(t, dir, "status", "--porcelain"); status != "M delay.txt" {
		t.Fatalf("the working tree changed: %q", status)
	}
	if list := git(t, dir, "worktree", "list", "--porcelain"); strings.Count(list, "worktree ") != 1 {
		t.Fatalf("a worktree was left behind:\n%s", list)
	}

	if r := run(t, dir, nil, "compare", "--against", "no-such-ref", "--quiet"); r.code != exitcode.Execution || !strings.Contains(r.stderr, "is not a commit") {
		t.Fatalf("unknown ref: %+v", r)
	}
	notRepo := t.TempDir()
	write(t, filepath.Join(notRepo, "himorime.yaml"), suite(t, compareSuite))
	if r := run(t, notRepo, nil, "compare", "--against", "main", "--quiet"); r.code != exitcode.Execution || !strings.Contains(r.stderr, "Git repository") {
		t.Fatalf("outside git: %+v", r)
	}
	if r := run(t, dir, nil, "compare", "--against", "main", "--runs", "3", "--quiet"); r.code != exitcode.Usage || !strings.Contains(r.stderr, "--runs 3 is lower than regression.min_samples") {
		t.Fatalf("--runs below min_samples: %+v", r)
	}
	write(t, filepath.Join(dir, "few.himorime.yaml"), strings.Replace(suite(t, compareSuite), "runs: 10", "runs: 3", 1))
	if r := run(t, dir, nil, "compare", "--against", "main", "few.himorime.yaml"); r.code != exitcode.Config || !strings.Contains(r.stderr, "lower than regression.min_samples") {
		t.Fatalf("too few runs for a comparison: %+v", r)
	}
}

func TestCIGitHubActions(t *testing.T) {
	dir := compareRepo(t)
	base := git(t, dir, "rev-parse", "HEAD")
	eventPath := filepath.Join(t.TempDir(), "event.json")
	write(t, eventPath, `{"pull_request":{"base":{"sha":"`+base+`"}}}`)
	summary := filepath.Join(t.TempDir(), "summary.md")
	write(t, summary, "previous step\n")
	gh := env{
		"GITHUB_ACTIONS": "true", "GITHUB_EVENT_NAME": "pull_request",
		"GITHUB_EVENT_PATH": eventPath, "GITHUB_STEP_SUMMARY": summary,
	}
	r := run(t, dir, gh, "ci")
	if r.code != 0 {
		t.Fatalf("ci: %+v", r)
	}
	if strings.Contains(r.stdout, "\x1b[") {
		t.Fatal("ci output must not be colored")
	}
	data, err := os.ReadFile(summary)
	if err != nil || !strings.HasPrefix(string(data), "previous step\n## ✅ himorime benchmark comparison") || !strings.Contains(string(data), "| sleepy | app |") {
		t.Fatalf("summary = %q, %v", data, err)
	}
	if !strings.Contains(r.stderr, "comparing base "+base[:12]) {
		t.Fatalf("stderr = %s", r.stderr)
	}

	gh["GITHUB_EVENT_NAME"] = "pull_request_target"
	if r := run(t, dir, gh, "ci"); r.code != exitcode.Usage || !strings.Contains(r.stderr, "refusing to run for pull_request_target") {
		t.Fatalf("pull_request_target: %+v", r)
	}
	if r := run(t, dir, nil, "ci"); r.code != exitcode.Usage || !strings.Contains(r.stderr, "HIMORIME_BASE_REF") {
		t.Fatalf("ci outside actions: %+v", r)
	}
	if r := run(t, dir, env{"HIMORIME_BASE_REF": "main"}, "ci", "--quiet"); r.code != 0 {
		t.Fatalf("ci with HIMORIME_BASE_REF: %+v", r)
	}
	write(t, filepath.Join(dir, "delay.txt"), "150ms\n")
	if r := run(t, dir, nil, "ci", "--against", "main", "--quiet"); r.code != exitcode.Failed || !strings.Contains(r.stdout, "REGRESSION") {
		t.Fatalf("ci regression: %+v", r)
	}
}

func TestCompletion(t *testing.T) {
	dir := t.TempDir()
	for _, shell := range Shells() {
		r := run(t, dir, nil, "completion", shell)
		if r.code != 0 || !strings.Contains(r.stdout, "himorime") || !strings.Contains(r.stdout, "compare") || !strings.Contains(r.stdout, "fail-on-inconclusive") {
			t.Errorf("completion %s: code %d\n%s", shell, r.code, r.stdout)
		}
	}
	if r := run(t, dir, nil, "completion", "tcsh"); r.code != exitcode.Usage {
		t.Fatalf("unsupported shell: %+v", r)
	}
	if r := run(t, dir, nil, "completion"); r.code != exitcode.Usage {
		t.Fatalf("no shell: %+v", r)
	}
}

func TestBashCompletionLoads(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil || runtime.GOOS == "windows" {
		t.Skip("bash is not available")
	}
	var buf bytes.Buffer
	if err := WriteCompletion(&buf, "bash"); err != nil {
		t.Fatal(err)
	}
	script := buf.String() + "\nCOMP_WORDS=(himorime com); COMP_CWORD=1; _himorime; echo \"${COMPREPLY[@]}\"\n"
	out, err := exec.Command(bash, "-c", script).Output()
	if err != nil {
		t.Fatalf("bash rejected the completion script: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "compare completion" {
		t.Fatalf("completing 'com' = %q", got)
	}
}

func TestParseFlagsAnywhere(t *testing.T) {
	fs, v := newFlagSet("run")
	var stdout, stderr bytes.Buffer
	operands, _, ok := parseFlags(fs, []string{"a.yaml", "--format", "json", "b.yaml", "--", "--weird.yaml"}, &stdout, &stderr)
	f, _ := v.(*measureFlags)
	if !ok || f.format != "json" || strings.Join(operands, " ") != "a.yaml b.yaml --weird.yaml" {
		t.Fatalf("operands = %v, format = %s, ok = %v", operands, f.format, ok)
	}
	var tags stringList
	if err := tags.Set("a, b"); err != nil || tags.String() != "a,b" {
		t.Fatalf("stringList = %v %v", tags, err)
	}
	if err := tags.Set("a,,b"); err == nil {
		t.Fatal("an empty tag was accepted")
	}
}

func TestSuitePaths(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "himorime.yaml"), "x")
	write(t, filepath.Join(dir, "b.himorime.yaml"), "x")
	write(t, filepath.Join(dir, "other.yaml"), "x")
	got, err := suitePaths([]string{dir, filepath.Join(dir, "himorime.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || filepath.Base(got[0]) != "b.himorime.yaml" || filepath.Base(got[1]) != "himorime.yaml" {
		t.Fatalf("suitePaths = %v", got)
	}
	if got, _ := suitePaths(nil); len(got) != 1 || got[0] != "himorime.yaml" {
		t.Fatalf("default = %v", got)
	}
}

func TestCommandsDocumented(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, c := range Commands() {
		if c.Name == "" || c.Usage == "" || c.Short == "" || c.Long == "" || c.run == nil || c.Flags == nil {
			t.Errorf("incomplete command %+v", c.Name)
		}
		if seen[c.Name] {
			t.Errorf("duplicate command %s", c.Name)
		}
		seen[c.Name] = true
	}
	for _, name := range []string{"init", "validate", "list", "run", "compare", "ci", "version", "completion"} {
		if !seen[name] {
			t.Errorf("required command %s is missing", name)
		}
	}
}

func TestMainEntryPoint(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"version"}, strings.NewReader(""), &stdout, &stderr); code != 0 || !strings.HasPrefix(stdout.String(), "himorime ") {
		t.Fatalf("Main(version) = %d, %q, %q", code, stdout.String(), stderr.String())
	}
	if isTerminal(&stdout) {
		t.Fatal("a buffer is not a terminal")
	}
	if cleanupFailed(exitcode.OK) != exitcode.Execution || cleanupFailed(exitcode.Failed) != exitcode.Failed {
		t.Fatal("cleanupFailed must not replace an existing failure")
	}
}

func TestRunSuitesFromTwoRepositories(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	base := t.TempDir()
	var paths []string
	for _, name := range []string{"a", "b"} {
		dir := filepath.Join(base, name)
		git(t, base, "init", "-q", dir)
		write(t, filepath.Join(dir, "fixtures", "in.txt"), "one\n")
		write(t, filepath.Join(dir, "himorime.yaml"), suite(t, `version: "1"
suite: {name: repo}
defaults: {warmup: 0, runs: 2}
benchmarks:
  - name: count
    stdin: fixtures/in.txt
    commands:
      helper: {command: [@EXE@, count, "1"]}
`))
		paths = append(paths, filepath.Join(dir, "himorime.yaml"))
	}
	r := run(t, base, nil, append([]string{"run", "--quiet"}, paths...)...)
	if r.code != 0 {
		t.Fatalf("suites from two repositories: %+v", r)
	}
}

func TestRunUnsupportedMetrics(t *testing.T) {
	dir := t.TempDir()
	body := `version: "1"
suite: {name: unsupported}
defaults: {warmup: 0, runs: 2}
benchmarks:
  - name: needs memory
    metrics: {memory: true, cpu: true%s}
    setup:
      - command: [@EXE@, write, "${root}/setup-ran"]
    commands:
      tool: {command: [@EXE@, sleep, 1ms]}
    budget:
      tool:
        memory: {peak_rss: {max: "<= 1GiB"}}
`
	write(t, filepath.Join(dir, "fail.himorime.yaml"), suite(t, fmt.Sprintf(body, "")))
	write(t, filepath.Join(dir, "skip.himorime.yaml"), suite(t, fmt.Sprintf(body, ", unsupported: skip")))
	noMemory := func(a *App) {
		a.Capabilities = func() (error, error) {
			return nil, errors.New("peak rss is not supported: no rusage on this test platform")
		}
	}

	r := runWith(t, dir, nil, noMemory, "run", "fail.himorime.yaml", "--quiet")
	if r.code != exitcode.Metric || !strings.Contains(r.stderr, `benchmark "needs memory": metrics.memory cannot be measured on this platform: peak rss is not supported`) ||
		!strings.Contains(r.stderr, "metrics.unsupported: skip") || r.stdout != "" {
		t.Fatalf("fail policy: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, "setup-ran")); err == nil {
		t.Fatal("setup ran although a requested metric was unsupported")
	}

	r = runWith(t, dir, nil, noMemory, "run", "skip.himorime.yaml", "--quiet", "--format", "json")
	if r.code != exitcode.OK {
		t.Fatalf("skip policy: %+v", r)
	}
	var rep struct {
		Suites []struct {
			Benchmarks []struct {
				Commands []struct {
					Head struct {
						Metrics map[string]struct {
							Status string `json:"status"`
							Reason string `json:"reason"`
						} `json:"metrics"`
					} `json:"head"`
					Budgets []struct {
						Status string `json:"status"`
					} `json:"budgets"`
				} `json:"commands"`
			} `json:"benchmarks"`
		} `json:"suites"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil {
		t.Fatal(err)
	}
	c := rep.Suites[0].Benchmarks[0].Commands[0]
	if c.Head.Metrics["peak_rss"].Status != "unsupported" || !strings.Contains(c.Head.Metrics["peak_rss"].Reason, "no rusage") ||
		c.Head.Metrics["cpu_total"].Status != "measured" || c.Budgets[0].Status != "skipped" {
		t.Fatalf("skip policy report: %+v", c)
	}
}

func TestAnnotationsOnlyInGitHubActions(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "himorime.yaml"), suite(t, `version: "1"
suite: {name: annotations}
defaults: {warmup: 0, runs: 2}
benchmarks:
  - name: slow
    commands:
      tool: {command: [@EXE@, sleep, 30ms]}
    budget:
      tool: {median: "< 1ms"}
`))
	r := run(t, dir, env{"GITHUB_ACTIONS": "true"}, "run", "--quiet")
	if r.code != exitcode.Failed || !strings.Contains(r.stderr, "::error file=himorime.yaml,title=himorime%3A performance budget exceeded::slow / tool: latency median budget < 1.00ms, measured") {
		t.Fatalf("in GitHub Actions: %+v", r)
	}
	if !strings.Contains(r.stderr, "himorime: exit 1: performance check failed") {
		t.Fatalf("no outcome line: %q", r.stderr)
	}
	r = run(t, dir, nil, "run", "--quiet")
	if r.code != exitcode.Failed || strings.Contains(r.stderr, "::error") || strings.Contains(r.stdout, "::error") {
		t.Fatalf("outside GitHub Actions: %+v", r)
	}
}
