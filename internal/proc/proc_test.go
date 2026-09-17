package proc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as the child process: TestMain dispatches on
// YAHIKO_PROC_HELPER so every platform runs the same portable helper.
func TestMain(m *testing.M) {
	switch os.Getenv("YAHIKO_PROC_HELPER") {
	case "":
		os.Exit(m.Run())
	case "exit":
		code, _ := strconv.Atoi(os.Getenv("HELPER_CODE"))
		os.Exit(code)
	case "cat":
		_, _ = io.Copy(os.Stdout, os.Stdin)
		os.Exit(0)
	case "sleep":
		d, _ := time.ParseDuration(os.Getenv("HELPER_SLEEP"))
		time.Sleep(d)
		os.Exit(0)
	case "spawn":
		// Start a grandchild that writes a marker file after a delay, then
		// sleep. Killing only the direct child would leave the grandchild to
		// write the marker.
		exe, _ := os.Executable()
		cmd := exec.Command(exe)
		cmd.Env = append(os.Environ(), "YAHIKO_PROC_HELPER=marker")
		if err := cmd.Start(); err != nil {
			os.Exit(3)
		}
		time.Sleep(time.Minute)
		os.Exit(0)
	case "background":
		// Start a grandchild that writes a marker after a delay, inheriting
		// stdout, then exit at once.
		exe, _ := os.Executable()
		cmd := exec.Command(exe)
		cmd.Env = append(os.Environ(), "YAHIKO_PROC_HELPER=marker")
		cmd.Stdout = os.Stdout
		if err := cmd.Start(); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	case "marker":
		time.Sleep(1500 * time.Millisecond)
		_ = os.WriteFile(os.Getenv("HELPER_MARKER"), []byte("alive"), 0o600)
		os.Exit(0)
	default:
		os.Exit(99)
	}
}

func helper(t *testing.T, mode string, env ...string) Spec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Spec{
		Path:   exe,
		Env:    append(append(os.Environ(), "YAHIKO_PROC_HELPER="+mode), env...),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}
}

func TestRunReportsExitCode(t *testing.T) {
	t.Parallel()
	for _, code := range []int{0, 1, 7} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			t.Parallel()
			res, err := Run(context.Background(), helper(t, "exit", "HELPER_CODE="+strconv.Itoa(code)), nil)
			if err != nil {
				t.Fatal(err)
			}
			if res.ExitCode != code || res.TimedOut || res.Canceled {
				t.Fatalf("result = %+v, want exit code %d", res, code)
			}
		})
	}
}

func TestRunPassesStdinAndCapturesStdout(t *testing.T) {
	t.Parallel()
	s := helper(t, "cat")
	var out bytes.Buffer
	s.Stdin = strings.NewReader("hello yahiko")
	s.Stdout = &out
	if _, err := Run(context.Background(), s, nil); err != nil {
		t.Fatal(err)
	}
	if out.String() != "hello yahiko" {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestRunMeasuresWithInjectedClock(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	calls := 0
	clock := func() time.Time {
		calls++
		return base.Add(time.Duration(calls-1) * 250 * time.Millisecond)
	}
	res, err := Run(context.Background(), helper(t, "exit"), clock)
	if err != nil {
		t.Fatal(err)
	}
	if res.Elapsed != 250*time.Millisecond {
		t.Fatalf("Elapsed = %v, want 250ms from the injected clock", res.Elapsed)
	}
}

func TestRunTimeoutKillsTheProcessTree(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "marker")
	s := helper(t, "spawn", "HELPER_MARKER="+marker)
	s.Timeout = 300 * time.Millisecond

	start := time.Now()
	res, err := Run(context.Background(), s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut {
		t.Fatalf("result = %+v, want TimedOut", res)
	}
	if time.Since(start) > 20*time.Second {
		t.Fatalf("Run returned after %v; the timeout did not stop the child", time.Since(start))
	}
	// The grandchild would write the marker 1.5s after it started.
	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the grandchild survived the timeout and wrote its marker")
	}
}

func TestRunCancelKillsTheProcess(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	s := helper(t, "sleep", "HELPER_SLEEP=1m")
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	res, err := Run(ctx, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Canceled {
		t.Fatalf("result = %+v, want Canceled", res)
	}
}

func TestRunAlreadyCanceledContextStartsNothing(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := Run(ctx, helper(t, "exit"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Canceled {
		t.Fatalf("result = %+v, want Canceled", res)
	}
}

func TestRunMissingProgramIsAStartError(t *testing.T) {
	t.Parallel()
	_, err := Run(context.Background(), Spec{Path: "yahiko-definitely-missing-program", Env: os.Environ()}, nil)
	if !errors.Is(err, ErrStart) {
		t.Fatalf("err = %v, want ErrStart", err)
	}
	_, err = Run(context.Background(), Spec{Env: os.Environ()}, nil)
	if !errors.Is(err, ErrStart) {
		t.Fatalf("empty program: err = %v, want ErrStart", err)
	}
}

func TestRunLooksUpProgramInChildPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := "yahiko-proc-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o700); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "PATH="+dir, "YAHIKO_PROC_HELPER=exit", "HELPER_CODE=4")
	res, err := Run(context.Background(), Spec{Path: "yahiko-proc-helper", Env: env, Stdout: io.Discard, Stderr: io.Discard}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 4 {
		t.Fatalf("exit code = %d, want 4 from the helper found through the child's PATH", res.ExitCode)
	}
}

func TestShellScript(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	res, err := Run(context.Background(), Spec{Script: "echo shell-ok", Env: os.Environ(), Stdout: &out, Stderr: io.Discard}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || !strings.Contains(out.String(), "shell-ok") {
		t.Fatalf("result = %+v, stdout = %q", res, out.String())
	}
	if ShellName() == "" {
		t.Fatal("ShellName is empty")
	}
}

func TestShellQuoteKeepsValuesLiteral(t *testing.T) {
	t.Parallel()
	value := "a b;echo injected"
	quoted, err := ShellQuote(value)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	_, err = Run(context.Background(), Spec{Script: "echo " + quoted, Env: os.Environ(), Stdout: &out, Stderr: io.Discard}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(out.String())
	got = strings.Trim(got, `"`)
	if got != value {
		t.Fatalf("echo printed %q, want the literal %q", got, value)
	}
	if runtime.GOOS != "windows" {
		q, _ := ShellQuote("it's")
		if q != `'it'\''s'` {
			t.Fatalf("ShellQuote(it's) = %s", q)
		}
	}
}

func TestShellQuoteRefusesUnquotableCmdValues(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("cmd.exe quoting only applies on Windows")
	}
	for _, v := range []string{`a"b`, "100%", "a\nb"} {
		if _, err := ShellQuote(v); err == nil {
			t.Errorf("ShellQuote(%q) accepted a value cmd.exe would reinterpret", v)
		}
	}
}

func Example_cutEnv() {
	k, v, ok := cutEnv("PATH=/usr/bin")
	fmt.Println(k, v, ok)
	// Output: PATH /usr/bin true
}

func TestRunStopsProcessesLeftBehindAndDoesNotWaitForThem(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "marker")
	s := helper(t, "background", "HELPER_MARKER="+marker)
	// yahiko always gives commands a file or the null device, never a pipe,
	// so Wait returns as soon as the command itself exits.
	out, err := os.Create(filepath.Join(t.TempDir(), "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	s.Stdout = out
	start := time.Now()
	res, err := Run(context.Background(), s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.TimedOut || res.Canceled {
		t.Fatalf("result = %+v", res)
	}
	if elapsed := time.Since(start); elapsed > 1200*time.Millisecond {
		t.Fatalf("Run waited %v for a background process", elapsed)
	}
	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a process left running by the command survived it")
	}
}

func TestLookPathResolvesRelativeEntriesAgainstTheWorkingDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := "yahiko-relative-helper"
	file := name
	if runtime.GOOS == "windows" {
		file += ".exe"
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", file), data, 0o700); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "PATH=bin", "YAHIKO_PROC_HELPER=exit", "HELPER_CODE=6")
	res, err := Run(context.Background(), Spec{Path: name, Dir: dir, Env: env, Stdout: io.Discard, Stderr: io.Discard}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 6 {
		t.Fatalf("exit code = %d, want 6 from bin/ relative to the working directory", res.ExitCode)
	}
}
