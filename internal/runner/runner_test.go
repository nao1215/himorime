package runner

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nao1215/himorime/internal/config"
	"github.com/nao1215/himorime/internal/proc"
	"github.com/nao1215/himorime/internal/redact"
)

// The test binary is its own helper program: TestMain dispatches on
// RUNNER_HELPER, so the runner starts real processes on every platform.
func TestMain(m *testing.M) {
	mode := os.Getenv("RUNNER_HELPER")
	if mode == "" {
		// As in himorime, commands start from a spawner where the platform
		// has one; HIMORIME_TEST_DIRECT=1 runs the same tests with himorime
		// starting them itself. internal/proc tests both paths either way.
		if os.Getenv("HIMORIME_TEST_DIRECT") != "1" {
			proc.EnableSpawner()
		}
		os.Exit(m.Run())
	}
	args := os.Args[1:]
	switch mode {
	case "ok":
		os.Exit(0)
	case "exit":
		code, _ := strconv.Atoi(args[0])
		if len(args) > 1 {
			_, _ = io.WriteString(os.Stderr, strings.Join(args[1:], " ")+"\n")
		}
		os.Exit(code)
	case "sleep":
		d, _ := time.ParseDuration(args[0])
		time.Sleep(d)
	case "append":
		f, err := os.OpenFile(args[0], os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			os.Exit(3)
		}
		_, _ = f.WriteString(strings.Join(args[1:], " ") + "\n")
		_ = f.Close()
	case "stdin-lines":
		data, _ := io.ReadAll(os.Stdin)
		want, _ := strconv.Atoi(args[0])
		if strings.Count(string(data), "\n") != want {
			os.Exit(4)
		}
	case "print-env":
		_, _ = io.WriteString(os.Stdout, os.Getenv(args[0]))
	case "print":
		_, _ = io.WriteString(os.Stdout, strings.Join(args, " "))
	case "copy":
		data, err := os.ReadFile(args[0])
		if err != nil {
			os.Exit(5)
		}
		_ = os.WriteFile(args[1], data, 0o600)
	case "symlink":
		if err := os.Symlink(args[0], args[1]); err != nil {
			os.Exit(5)
		}
	case "background":
		exe, _ := os.Executable()
		cmd := exec.Command(exe, "1m")
		cmd.Env = append(os.Environ(), "RUNNER_HELPER=sleep")
		if err := cmd.Start(); err != nil {
			os.Exit(3)
		}
	case "pad-secret":
		// Print padding, then a secret, so the secret straddles the start of
		// the kept stderr tail.
		_, _ = io.WriteString(os.Stderr, strings.Repeat("x", 5000)+os.Getenv("SUITE_TOKEN")+strings.Repeat("y", stderrTailBytes-10))
		os.Exit(1)
	case "stdin-copy":
		data, _ := io.ReadAll(os.Stdin)
		if err := os.WriteFile(args[0], data, 0o600); err != nil {
			os.Exit(5)
		}
	case "tty":
		os.Exit(ttyHelper(args))
	case "readonly":
		// A tree whose directories cannot be written, as a Go module cache is.
		sub := filepath.Join(args[0], "ro", "sub")
		if err := os.MkdirAll(sub, 0o700); err != nil {
			os.Exit(5)
		}
		if err := os.WriteFile(filepath.Join(sub, "f"), []byte("x"), 0o400); err != nil {
			os.Exit(5)
		}
		for _, d := range []string{sub, filepath.Dir(sub)} {
			if err := os.Chmod(d, 0o500); err != nil {
				os.Exit(5)
			}
		}
	case "pwd":
		wd, _ := os.Getwd()
		_, _ = io.WriteString(os.Stdout, wd)
	default:
		os.Exit(99)
	}
	os.Exit(0)
}

type fixture struct {
	t      *testing.T
	exe    string
	dir    string
	suite  *config.Suite
	runner *Runner
	side   Side
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	tmp := t.TempDir()
	return &fixture{
		t:     t,
		exe:   exe,
		dir:   dir,
		suite: &config.Suite{Name: "test", Dir: dir, Path: filepath.Join(dir, "himorime.yaml")},
		runner: &Runner{
			TempDir:     tmp,
			ProjectRoot: dir,
			Seed:        7,
			Redactor:    redact.New(nil, "super-secret-value"),
		},
		side: Side{Name: SideHead, Root: dir, ProjectRoot: dir},
	}
}

// helper returns an Exec that runs the test binary in the given mode.
func (f *fixture) helper(mode string, args ...string) config.Exec {
	return config.Exec{
		Argv:    append([]string{f.exe}, args...),
		Env:     []config.EnvVar{{Name: "RUNNER_HELPER", Value: mode}},
		Timeout: time.Minute,
	}
}

func (f *fixture) command(name, mode string, args ...string) config.Command {
	return config.Command{Name: name, Exec: f.helper(mode, args...), Stdout: config.OutputDiscard, Stderr: config.OutputDiscard, ExitCodes: []int{0}}
}

func bench(name string, runs int, commands ...config.Command) config.Benchmark {
	return config.Benchmark{
		Name: name, Runs: runs, MinRuns: 3, MaxRuns: 5, MinTime: 0, Commands: commands,
		Regression: config.Regression{MinSamples: 2},
	}
}

func TestMeasureFixedRunsInterleaved(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	order := filepath.Join(f.dir, "order.txt")
	b := bench("fixed", 4, f.command("a", "append", order, "a"), f.command("b", "append", order, "b"))
	b.Warmup = 1
	res := f.runner.Measure(context.Background(), b, []Side{f.side})
	if res.Failure != nil {
		t.Fatalf("failure: %+v", res.Failure)
	}
	if res.Rounds != 4 {
		t.Fatalf("rounds = %d", res.Rounds)
	}
	for _, c := range res.Commands {
		m := c.Sides[SideHead]
		if len(m.Samples) != 4 || m.Warmups != 1 || m.Failure != nil {
			t.Fatalf("%s: %+v", c.Command.Name, m)
		}
	}
	data, err := os.ReadFile(order)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(string(data))
	if len(lines) != 10 {
		t.Fatalf("expected 10 executions (1 warmup + 4 runs, 2 commands), got %v", lines)
	}
	// Interleaving: every round (pair of lines) holds both commands.
	for i := 0; i < len(lines); i += 2 {
		if lines[i] == lines[i+1] {
			t.Fatalf("round %d ran %s twice: %v", i/2, lines[i], lines)
		}
	}
}

func TestMeasureOrderIsDeterministicForASeed(t *testing.T) {
	t.Parallel()
	run := func(seed uint64) string {
		f := newFixture(t)
		f.runner.Seed = seed
		order := filepath.Join(f.dir, "order.txt")
		b := bench("order", 4, f.command("a", "append", order, "a"), f.command("b", "append", order, "b"), f.command("c", "append", order, "c"))
		f.runner.Measure(context.Background(), b, []Side{f.side})
		data, _ := os.ReadFile(order)
		return strings.Join(strings.Fields(string(data)), "")
	}
	first := run(11)
	if again := run(11); again != first {
		t.Fatalf("same seed, different order: %s vs %s", first, again)
	}
	different := false
	for seed := uint64(12); seed < 20; seed++ {
		if run(seed) != first {
			different = true
			break
		}
	}
	if !different {
		t.Fatal("eight other seeds all produced the same order")
	}
}

func TestMeasureAdaptiveRunsRespectBounds(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	b := bench("adaptive", 0, f.command("a", "ok"))
	b.MinRuns, b.MaxRuns, b.MinTime = 3, 50, 0
	res := f.runner.Measure(context.Background(), b, []Side{f.side})
	if n := len(res.Commands[0].Sides[SideHead].Samples); n != 3 {
		t.Fatalf("with min_time 0 adaptive runs stop at min_runs, got %d", n)
	}

	b.MinRuns, b.MaxRuns, b.MinTime = 2, 4, time.Hour
	res = f.runner.Measure(context.Background(), b, []Side{f.side})
	if n := len(res.Commands[0].Sides[SideHead].Samples); n != 4 {
		t.Fatalf("an unreachable min_time must stop at max_runs, got %d", n)
	}

	b.MinRuns, b.MaxRuns, b.MinTime = 2, 100, 150*time.Millisecond
	slow := bench("slow", 0, f.command("s", "sleep", "40ms"))
	slow.MinRuns, slow.MaxRuns, slow.MinTime = 2, 100, 150*time.Millisecond
	res = f.runner.Measure(context.Background(), slow, []Side{f.side})
	samples := res.Commands[0].Sides[SideHead].Samples
	var total time.Duration
	for _, d := range samples {
		total += d
	}
	// Whatever the machine's speed, adaptive runs stop at the first round
	// where both minimums hold, and never above max_runs.
	if len(samples) < 2 || len(samples) > 100 || total < 150*time.Millisecond {
		t.Fatalf("adaptive stop: %d samples totalling %v", len(samples), total)
	}
	var withoutLast time.Duration
	for _, d := range samples[:len(samples)-1] {
		withoutLast += d
	}
	if len(samples) > 2 && withoutLast >= 150*time.Millisecond {
		t.Fatalf("adaptive runs continued after both minimums held: %d samples", len(samples))
	}
}

func TestMeasureRunsOverride(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	two, zero := 2, 0
	f.runner.RunsOverride, f.runner.WarmupOverride = &two, &zero
	b := bench("override", 9, f.command("a", "ok"))
	b.Warmup = 5
	res := f.runner.Measure(context.Background(), b, []Side{f.side})
	m := res.Commands[0].Sides[SideHead]
	if len(m.Samples) != 2 || m.Warmups != 0 {
		t.Fatalf("overrides ignored: %+v", m)
	}
}

func TestMeasureCommandFailureIsIsolated(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	b := bench("fail", 3, f.command("good", "ok"), f.command("bad", "exit", "3", "boom", "super-secret-value"))
	res := f.runner.Measure(context.Background(), b, []Side{f.side})
	if res.Failure != nil {
		t.Fatalf("a command failure must not be a benchmark failure: %+v", res.Failure)
	}
	good, bad := res.Commands[0].Sides[SideHead], res.Commands[1].Sides[SideHead]
	if len(good.Samples) != 3 || good.Failure != nil {
		t.Fatalf("good = %+v", good)
	}
	if bad.Failure == nil || bad.Failure.Kind != FailExitCode || bad.Failure.ExitCode != 3 || len(bad.Samples) != 0 {
		t.Fatalf("bad = %+v", bad)
	}
	if !strings.Contains(bad.Failure.Stderr, "boom") || strings.Contains(bad.Failure.Stderr, "super-secret-value") {
		t.Fatalf("stderr tail = %q (must show the output, masked)", bad.Failure.Stderr)
	}
}

func TestMeasureAllowedExitCodes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	c := f.command("grep", "exit", "1")
	c.ExitCodes = []int{0, 1}
	res := f.runner.Measure(context.Background(), bench("codes", 2, c), []Side{f.side})
	if m := res.Commands[0].Sides[SideHead]; m.Failure != nil || len(m.Samples) != 2 {
		t.Fatalf("exit 1 was allowed but failed: %+v", m)
	}
}

func TestMeasureTimeout(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	c := f.command("slow", "sleep", "1m")
	c.Timeout = 200 * time.Millisecond
	start := time.Now()
	res := f.runner.Measure(context.Background(), bench("timeout", 3, c), []Side{f.side})
	if time.Since(start) > 30*time.Second {
		t.Fatal("the timeout did not stop the command")
	}
	m := res.Commands[0].Sides[SideHead]
	if m.Failure == nil || m.Failure.Kind != FailTimeout {
		t.Fatalf("failure = %+v", m.Failure)
	}
}

func TestMeasureStartFailure(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	c := config.Command{Name: "missing", Exec: config.Exec{Argv: []string{"himorime-no-such-program"}, Timeout: time.Second}, Stdout: config.OutputDiscard, Stderr: config.OutputDiscard, ExitCodes: []int{0}}
	res := f.runner.Measure(context.Background(), bench("start", 2, c), []Side{f.side})
	if m := res.Commands[0].Sides[SideHead]; m.Failure == nil || m.Failure.Kind != FailStart {
		t.Fatalf("failure = %+v", m.Failure)
	}
}

func TestHooksRunInOrderAndCleanupAlwaysRuns(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	log := filepath.Join(f.dir, "hooks.txt")
	b := bench("hooks", 2, f.command("a", "ok"))
	b.Warmup = 1
	b.Setup = []config.Exec{f.helper("append", log, "setup")}
	b.PrepareEach = []config.Exec{f.helper("append", log, "prepare")}
	b.Cleanup = []config.Exec{f.helper("append", log, "cleanup")}
	res := f.runner.Measure(context.Background(), b, []Side{f.side})
	if res.Failure != nil {
		t.Fatal(res.Failure)
	}
	data, _ := os.ReadFile(log)
	want := "setup prepare prepare prepare cleanup"
	if got := strings.Join(strings.Fields(string(data)), " "); got != want {
		t.Fatalf("hook order = %q, want %q", got, want)
	}

	// A failing setup skips measuring but still cleans up.
	f2 := newFixture(t)
	log2 := filepath.Join(f2.dir, "hooks.txt")
	b2 := bench("setup-fails", 2, f2.command("a", "append", log2, "measured"))
	b2.Setup = []config.Exec{f2.helper("exit", "9")}
	b2.Cleanup = []config.Exec{f2.helper("append", log2, "cleanup")}
	res2 := f2.runner.Measure(context.Background(), b2, []Side{f2.side})
	if res2.Failure == nil || res2.Failure.Kind != FailSetup || res2.Failure.ExitCode != 9 {
		t.Fatalf("failure = %+v", res2.Failure)
	}
	data2, _ := os.ReadFile(log2)
	if got := strings.TrimSpace(string(data2)); got != "cleanup" {
		t.Fatalf("after a failed setup the log is %q, want only cleanup", got)
	}

	// A failing prepare_each fails the command, and cleanup still runs.
	f3 := newFixture(t)
	log3 := filepath.Join(f3.dir, "hooks.txt")
	b3 := bench("prepare-fails", 2, f3.command("a", "ok"))
	b3.PrepareEach = []config.Exec{f3.helper("exit", "2")}
	b3.Cleanup = []config.Exec{f3.helper("append", log3, "cleanup")}
	res3 := f3.runner.Measure(context.Background(), b3, []Side{f3.side})
	if m := res3.Commands[0].Sides[SideHead]; m.Failure == nil || m.Failure.Kind != FailPrepareEach {
		t.Fatalf("failure = %+v", m.Failure)
	}
	if data3, _ := os.ReadFile(log3); strings.TrimSpace(string(data3)) != "cleanup" {
		t.Fatal("cleanup did not run after prepare_each failed")
	}

	// A failing cleanup is a benchmark failure.
	f4 := newFixture(t)
	b4 := bench("cleanup-fails", 1, f4.command("a", "ok"))
	b4.Cleanup = []config.Exec{f4.helper("exit", "5")}
	if res4 := f4.runner.Measure(context.Background(), b4, []Side{f4.side}); res4.Failure == nil || res4.Failure.Kind != FailCleanup {
		t.Fatalf("failure = %+v", res4.Failure)
	}
}

func TestCancellationRunsCleanup(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	log := filepath.Join(f.dir, "cleanup.txt")
	b := bench("cancel", 1000, f.command("slow", "sleep", "100ms"))
	b.Cleanup = []config.Exec{f.helper("append", log, "cleanup")}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(400 * time.Millisecond)
		cancel()
	}()
	res := f.runner.Measure(ctx, b, []Side{f.side})
	if res.Failure == nil || res.Failure.Kind != FailInterrupted {
		t.Fatalf("failure = %+v", res.Failure)
	}
	if data, _ := os.ReadFile(log); strings.TrimSpace(string(data)) != "cleanup" {
		t.Fatal("cleanup did not run after cancellation")
	}
	entries, _ := os.ReadDir(filepath.Join(f.runner.TempDir, SideHead))
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "workdir-") {
			t.Fatalf("workdir %s was left behind", e.Name())
		}
	}
}

func TestStdinFixtureReopenedEveryRun(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.dir, "in.txt"), []byte("a\nb\nc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := bench("stdin", 3, f.command("count", "stdin-lines", "3"))
	b.Warmup = 1
	b.Stdin = config.Stdin{Kind: config.StdinFile, File: "in.txt"}
	res := f.runner.Measure(context.Background(), b, []Side{f.side})
	if m := res.Commands[0].Sides[SideHead]; m.Failure != nil || len(m.Samples) != 3 {
		t.Fatalf("every run must read the whole fixture: %+v", m)
	}

	inline := bench("inline", 2, f.command("count", "stdin-lines", "2"))
	inline.Stdin = config.Stdin{Kind: config.StdinContent, Content: "x\ny\n"}
	res = f.runner.Measure(context.Background(), inline, []Side{f.side})
	if m := res.Commands[0].Sides[SideHead]; m.Failure != nil {
		t.Fatalf("inline stdin: %+v", m.Failure)
	}

	generated := bench("generated", 2, f.command("count", "stdin-lines", "1"))
	generated.Setup = []config.Exec{f.helper("append", "${workdir}/gen.txt", "line")}
	generated.Stdin = config.Stdin{Kind: config.StdinFile, File: "${workdir}/gen.txt"}
	res = f.runner.Measure(context.Background(), generated, []Side{f.side})
	if res.Failure != nil {
		t.Fatalf("a fixture written by setup: %+v", res.Failure)
	}

	missing := bench("missing", 2, f.command("count", "stdin-lines", "1"))
	missing.Stdin = config.Stdin{Kind: config.StdinFile, File: "nope.txt"}
	if res := f.runner.Measure(context.Background(), missing, []Side{f.side}); res.Failure == nil || res.Failure.Kind != FailSetup {
		t.Fatalf("missing fixture: %+v", res.Failure)
	}
}

func TestVariablesEnvAndCwd(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	if err := os.Mkdir(filepath.Join(f.dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	f.runner.Environ = func() []string { return append(os.Environ(), "FROM_HOST=host-value") }
	c := f.command("env", "print-env", "MIXED")
	c.Env = append(c.Env, config.EnvVar{Name: "MIXED", Value: "${env:FROM_HOST}:${exe}:ok"})
	c.Stdout = "out/env.txt"
	pwd := f.command("pwd", "pwd")
	pwd.Cwd = "sub"
	pwd.Stdout = "pwd.txt"
	b := bench("vars", 1, c, pwd)
	copyOut := func(name string) config.Exec {
		return config.Exec{Argv: []string{f.exe, "${workdir}/" + name, filepath.Join(f.dir, filepath.Base(name))}, Env: []config.EnvVar{{Name: "RUNNER_HELPER", Value: "copy"}}, Timeout: time.Minute}
	}
	b.Cleanup = []config.Exec{copyOut("out/env.txt"), copyOut("pwd.txt")}
	res := f.runner.Measure(context.Background(), b, []Side{f.side})
	if res.Failure != nil {
		t.Fatal(res.Failure)
	}
	env, _ := os.ReadFile(filepath.Join(f.dir, "env.txt"))
	if want := "host-value:" + exeSuffix() + ":ok"; string(env) != want {
		t.Fatalf("env = %q, want %q", env, want)
	}
	wd, _ := os.ReadFile(filepath.Join(f.dir, "pwd.txt"))
	realSub, _ := filepath.EvalSymlinks(filepath.Join(f.dir, "sub"))
	if got, _ := filepath.EvalSymlinks(string(wd)); got != realSub {
		t.Fatalf("cwd = %q, want %q", got, realSub)
	}

	missing := f.command("env", "print-env", "X")
	missing.Env = append(missing.Env, config.EnvVar{Name: "X", Value: "${env:HIMORIME_SURELY_UNSET}"})
	res = f.runner.Measure(context.Background(), bench("unset", 1, missing), []Side{f.side})
	if m := res.Commands[0].Sides[SideHead]; m.Failure == nil || !strings.Contains(m.Failure.Message, "HIMORIME_SURELY_UNSET is not set") {
		t.Fatalf("unset variable: %+v", m.Failure)
	}
}

func TestOutputFileKeepsLastRun(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	c := f.command("print", "print", "hello-from-run")
	c.Stdout = "logs/stdout.txt"
	b := bench("output", 2, c)
	b.Cleanup = []config.Exec{{Argv: []string{f.exe, "${workdir}/logs/stdout.txt", filepath.Join(f.dir, "kept.txt")}, Env: []config.EnvVar{{Name: "RUNNER_HELPER", Value: "copy"}}, Timeout: time.Minute}}
	res := f.runner.Measure(context.Background(), b, []Side{f.side})
	if res.Failure != nil {
		t.Fatal(res.Failure)
	}
	if data, _ := os.ReadFile(filepath.Join(f.dir, "kept.txt")); string(data) != "hello-from-run" {
		t.Fatalf("captured stdout = %q", data)
	}
}

func TestPathConfinement(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	outside := t.TempDir()
	c := f.command("a", "ok")
	c.Cwd = "${root}/link"
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(f.dir, "link")); err != nil {
			t.Fatal(err)
		}
		res := f.runner.Measure(context.Background(), bench("symlink", 1, c), []Side{f.side})
		m := res.Commands[0].Sides[SideHead]
		if m.Failure == nil || m.Failure.Kind != FailPath {
			t.Fatalf("a cwd symlinked outside the project was accepted: %+v", m.Failure)
		}
	}
	if _, err := confine(filepath.Join(f.dir, "a", "..", "..", "x"), f.dir); err == nil {
		t.Fatal("confine accepted a path above its root")
	}
	if p, err := confine(filepath.Join(f.dir, "new", "file.txt"), f.dir); err != nil || !strings.HasSuffix(p, filepath.Join("new", "file.txt")) {
		t.Fatalf("confine of a not-yet-existing path = %q, %v", p, err)
	}
}

func TestRemoveTempRefusesOutsidePaths(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(keep, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{outside, base, filepath.Join(base, ".."), ""} {
		if err := removeTemp(base, target); err == nil {
			t.Errorf("removeTemp(%q) was allowed", target)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("a file outside the temporary directory was removed")
	}
	inside := filepath.Join(base, "work")
	if err := os.MkdirAll(filepath.Join(inside, "deep"), 0o700); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(inside, "deep", "escape")); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeTemp(base, inside); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("removing a directory followed a symlink out of it")
	}
	if err := removeTemp(base, filepath.Join(base, "does-not-exist")); err != nil {
		t.Fatalf("removing a missing directory: %v", err)
	}
}

func TestRemoveTempRemovesReadOnlyDirectoriesWithoutLeavingIt(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("on Windows a directory without write permission does not stop removing what it holds")
	}
	base := t.TempDir()
	outside := t.TempDir()
	if err := os.Chmod(outside, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(outside, 0o700) })
	inside := filepath.Join(base, "work")
	sub := filepath.Join(inside, "ro", "sub")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "f"), []byte("x"), 0o400); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(sub, "escape")); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{sub, filepath.Dir(sub)} {
		if err := os.Chmod(d, 0o500); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeTemp(base, inside); err != nil {
		t.Fatalf("removeTemp: %v", err)
	}
	if _, err := os.Lstat(inside); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s is still there: %v", inside, err)
	}
	if info, err := os.Stat(outside); err != nil || info.Mode().Perm() != 0o500 {
		t.Fatalf("the directory a symlink pointed to changed: %v, %v", info.Mode(), err)
	}
}

func TestMeasureRemovesAWorkdirHoldingReadOnlyDirectories(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("on Windows a directory without write permission does not stop removing what it holds")
	}
	f := newFixture(t)
	b := bench("readonly", 2, f.command("ok", "ok"))
	b.Setup = []config.Exec{f.helper("readonly", "${workdir}")}
	res := f.runner.Measure(context.Background(), b, []Side{f.side})
	if res.Failure != nil {
		t.Fatalf("failure: %+v", res.Failure)
	}
	entries, _ := os.ReadDir(filepath.Join(f.runner.TempDir, SideHead))
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "workdir-") {
			t.Fatalf("workdir %s was left behind", e.Name())
		}
	}
}

func TestMeasureSaysTheWorkingDirectoryIsMissing(t *testing.T) {
	t.Parallel()
	t.Run("command", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		c := f.command("pwd", "ok")
		c.Cwd = "${workdir}/nope"
		res := f.runner.Measure(context.Background(), bench("cwd", 1, c), []Side{f.side})
		m := res.Commands[0].Sides[SideHead]
		if m.Failure == nil || m.Failure.Kind != FailStart || !strings.Contains(m.Failure.Message, "(cwd ${workdir}/nope) does not exist") {
			t.Fatalf("failure = %+v, want a start failure naming the missing working directory", m.Failure)
		}
		if strings.Contains(m.Failure.Message, "hook") {
			t.Fatalf("a command's failure talks about hooks: %s", m.Failure.Message)
		}
	})
	t.Run("setup in the benchmark's cwd", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		b := bench("cwd", 1, f.command("ok", "ok"))
		b.Setup = []config.Exec{f.helper("ok")}
		b.Setup[0].Cwd = "${workdir}/sub"
		res := f.runner.Measure(context.Background(), b, []Side{f.side})
		if res.Failure == nil || res.Failure.Kind != FailSetup || !strings.Contains(res.Failure.Message, "(cwd ${workdir}/sub) does not exist") || !strings.Contains(res.Failure.Message, "needs cwd: ${workdir}") {
			t.Fatalf("failure = %+v, want a setup failure naming the directory and how to create it", res.Failure)
		}
	})
}

func TestBuild(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	art := filepath.Join(f.runner.TempDir, "artifacts", "head", "artifact")
	side := f.side
	side.Artifact = art
	f.suite.Build = ptr(f.helper("append", "${artifact}", "built"))
	if fail := f.runner.Build(context.Background(), f.suite, side); fail != nil {
		t.Fatalf("build: %+v", fail)
	}
	if _, err := os.Stat(art); err != nil {
		t.Fatal("the build did not write the artifact")
	}

	f.suite.Build = ptr(f.helper("ok"))
	side.Artifact = filepath.Join(f.runner.TempDir, "artifacts", "other", "artifact")
	if fail := f.runner.Build(context.Background(), f.suite, side); fail == nil || !strings.Contains(fail.Message, "did not write ${artifact}") {
		t.Fatalf("a build that writes nothing: %+v", fail)
	}

	f.suite.Build = ptr(f.helper("exit", "7", "compile error"))
	if fail := f.runner.Build(context.Background(), f.suite, side); fail == nil || fail.Kind != FailBuild || fail.ExitCode != 7 || !strings.Contains(fail.Stderr, "compile error") {
		t.Fatalf("a failing build: %+v", fail)
	}

	f.suite.Build = nil
	if fail := f.runner.Build(context.Background(), f.suite, side); fail != nil {
		t.Fatalf("no build: %+v", fail)
	}
}

func ptr[T any](v T) *T { return &v }

func TestShellCommand(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	c := config.Command{
		Name:      "sh",
		Exec:      config.Exec{Shell: true, Script: "echo ${root}", Timeout: time.Minute},
		Stdout:    config.OutputDiscard,
		Stderr:    config.OutputDiscard,
		ExitCodes: []int{0},
	}
	res := f.runner.Measure(context.Background(), bench("shell", 2, c), []Side{f.side})
	if m := res.Commands[0].Sides[SideHead]; m.Failure != nil || len(m.Samples) != 2 {
		t.Fatalf("shell command: %+v", m)
	}
}

func TestCompareSidesOnlyMeasureComparedCommands(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	base := f.side
	base.Name = SideBase
	b := bench("sides", 2, f.command("mine", "ok"), f.command("theirs", "ok"))
	b.Regression.Commands = []string{"mine"}
	res := f.runner.Measure(context.Background(), b, []Side{base, f.side})
	if len(res.Commands[0].Sides) != 2 || len(res.Commands[1].Sides) != 0 {
		t.Fatalf("sides = %v / %v", res.Commands[0].Sides, res.Commands[1].Sides)
	}
	b.Budgets = []config.Budget{{Command: "theirs"}}
	res = f.runner.Measure(context.Background(), b, []Side{base, f.side})
	m := res.Commands[1].Sides[SideHead]
	if len(res.Commands[1].Sides) != 1 || m == nil || len(m.Samples) != 2 || m.Failure != nil {
		t.Fatalf("excluded command's head budget must still be measured: %+v", res.Commands[1])
	}
}

func TestSelection(t *testing.T) {
	t.Parallel()
	bs := []config.Benchmark{
		{Name: "df small", Tags: []string{"smoke", "parser"}},
		{Name: "df large", Tags: []string{"parser", "slow"}},
		{Name: "startup"},
	}
	names := func(sel Selection) string {
		var out []string
		for _, b := range sel.Select(bs) {
			out = append(out, b.Name)
		}
		return strings.Join(out, ",")
	}
	tests := []struct {
		sel  Selection
		want string
	}{
		{Selection{}, "df small,df large,startup"},
		{Selection{Filter: regexp.MustCompile("^df")}, "df small,df large"},
		{Selection{Tags: []string{"smoke"}}, "df small"},
		{Selection{Tags: []string{"smoke", "slow"}}, "df small,df large"},
		{Selection{SkipTags: []string{"slow"}}, "df small,startup"},
		{Selection{Tags: []string{"parser"}, SkipTags: []string{"slow"}}, "df small"},
		{Selection{Filter: regexp.MustCompile("large"), Tags: []string{"smoke"}}, ""},
	}
	for _, tt := range tests {
		if got := names(tt.sel); got != tt.want {
			t.Errorf("%+v selected %q, want %q", tt.sel, got, tt.want)
		}
	}
}

func TestHookWithABackgroundProcessDoesNotBlock(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	b := bench("background", 1, f.command("a", "ok"))
	b.Setup = []config.Exec{f.helper("background")}
	start := time.Now()
	res := f.runner.Measure(context.Background(), b, []Side{f.side})
	if res.Failure != nil {
		t.Fatal(res.Failure)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("setup blocked for %v on a background process", elapsed)
	}
}

func TestSecretsPassedThroughSuiteEnvAreMaskedAcrossTheTailBoundary(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.runner.Redactor = redact.New(nil)
	f.runner.Environ = func() []string { return append(os.Environ(), "HOST_PAT=pat-value-0123456789abcdef") }
	c := f.command("leaky", "pad-secret")
	c.Env = append(c.Env, config.EnvVar{Name: "SUITE_TOKEN", Value: "${env:HOST_PAT}"})
	res := f.runner.Measure(context.Background(), bench("secret", 1, c), []Side{f.side})
	m := res.Commands[0].Sides[SideHead]
	if m.Failure == nil {
		t.Fatal("the command should fail")
	}
	for _, part := range []string{"pat-value-0123456789abcdef", "0123456789abcdef", "89abcdef"} {
		if strings.Contains(m.Failure.Stderr, part) {
			t.Fatalf("stderr tail leaks %q: ...%s", part, m.Failure.Stderr[:40])
		}
	}
	if len(m.Failure.Stderr) > stderrTailBytes {
		t.Fatalf("tail is %d bytes, more than %d", len(m.Failure.Stderr), stderrTailBytes)
	}
}

func TestDanglingSymlinkIsNotFollowed(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges on Windows")
	}
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "created-outside.txt")
	link := filepath.Join(root, "out.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := confine(link, root); err == nil {
		t.Fatal("a dangling symlink pointing outside was accepted")
	}
	f := newFixture(t)
	c := f.command("print", "print", "escape")
	c.Stdout = "out.txt"
	b := bench("dangling", 1, c)
	b.Setup = []config.Exec{{Argv: []string{f.exe, outside, "${workdir}/out.txt"}, Env: []config.EnvVar{{Name: "RUNNER_HELPER", Value: "symlink"}}, Timeout: time.Minute}}
	res := f.runner.Measure(context.Background(), b, []Side{f.side})
	if m := res.Commands[0].Sides[SideHead]; m.Failure == nil || m.Failure.Kind != FailPath {
		t.Fatalf("failure = %+v", m.Failure)
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("output was written through a dangling symlink")
	}
}
