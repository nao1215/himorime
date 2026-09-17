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
	"syscall"
	"testing"
	"time"
)

// The test binary doubles as the child process: TestMain dispatches on
// HIMORIME_PROC_HELPER so every platform runs the same portable helper.
func TestMain(m *testing.M) {
	switch os.Getenv("HIMORIME_PROC_HELPER") {
	case "":
		// As in himorime: Run starts commands from a spawner where the
		// platform has one. Tests that must cover both paths use startPaths.
		EnableSpawner()
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
		cmd.Env = append(os.Environ(), "HIMORIME_PROC_HELPER=marker")
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
		cmd.Env = append(os.Environ(), "HIMORIME_PROC_HELPER=marker")
		cmd.Stdout = os.Stdout
		if err := cmd.Start(); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	case "burn":
		// Touch HELPER_ALLOC MiB, then spin for HELPER_BURN.
		mib, _ := strconv.Atoi(os.Getenv("HELPER_ALLOC"))
		buf := make([]byte, mib<<20)
		for i := 0; i < len(buf); i += 4096 {
			buf[i] = 1
		}
		// Spin until the process has used HELPER_BURN of CPU time, not of
		// wall-clock time: on a loaded runner a loop timed by the clock was
		// given a third of its duration on a CPU. The deadline only stops a
		// helper that cannot read its own CPU time.
		d, _ := time.ParseDuration(os.Getenv("HELPER_BURN"))
		x := 0
		for deadline := time.Now().Add(d + 10*time.Second); selfCPU() < d && time.Now().Before(deadline); {
			x++
		}
		runtime.KeepAlive(buf)
		os.Exit(0)
	case "parent":
		// Run the burn helper as a child and wait for it, doing nothing itself.
		exe, _ := os.Executable()
		cmd := exec.Command(exe)
		cmd.Env = append(os.Environ(), "HIMORIME_PROC_HELPER=burn")
		if err := cmd.Run(); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	case "pwd":
		wd, _ := os.Getwd()
		fmt.Print(wd)
		os.Exit(0)
	case "ppid":
		fmt.Print(os.Getppid())
		os.Exit(0)
	case "getenv":
		fmt.Print(os.Getenv(os.Getenv("HELPER_NAME")))
		os.Exit(0)
	case "owner":
		os.Exit(ownerHelper())
	case "marker":
		time.Sleep(4 * time.Second)
		_ = os.WriteFile(os.Getenv("HELPER_MARKER"), []byte("alive"), 0o600)
		os.Exit(0)
	default:
		os.Exit(99)
	}
}

// runFunc runs a Spec on one of the paths Run can take.
type runFunc func(context.Context, Spec) (Result, error)

// startPath is one way Run starts a command.
type startPath struct {
	name string
	run  func(t *testing.T) runFunc
}

// startPaths lists every way Run starts a command: directly, and through the
// spawner where the platform has one. Tests run as a subtest per path.
func startPaths() []startPath {
	paths := []startPath{{name: "direct", run: func(*testing.T) runFunc {
		return func(ctx context.Context, s Spec) (Result, error) {
			return run(ctx, s, nil, attach)
		}
	}}}
	if !spawnerSupported {
		return paths
	}
	return append(paths, startPath{name: "spawner", run: func(t *testing.T) runFunc {
		t.Helper()
		return func(ctx context.Context, s Spec) (Result, error) {
			res, handled, err := runSpawned(ctx, s)
			if !handled {
				t.Error("no spawner took the command")
				return res, errors.New("no spawner took the command")
			}
			return res, err
		}
	}})
}

func helper(t *testing.T, mode string, env ...string) Spec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Spec{
		Path:   exe,
		Env:    append(append(os.Environ(), "HIMORIME_PROC_HELPER="+mode), env...),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}
}

func TestRunReportsExitCode(t *testing.T) {
	t.Parallel()
	for _, path := range startPaths() {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			runWith := path.run(t)
			for _, code := range []int{0, 1, 7} {
				t.Run(strconv.Itoa(code), func(t *testing.T) {
					t.Parallel()
					res, err := runWith(context.Background(), helper(t, "exit", "HELPER_CODE="+strconv.Itoa(code)))
					if err != nil {
						t.Fatal(err)
					}
					if res.ExitCode != code || res.TimedOut || res.Canceled {
						t.Fatalf("result = %+v, want exit code %d", res, code)
					}
				})
			}
		})
	}
}

func TestRunPassesStdinAndCapturesStdout(t *testing.T) {
	t.Parallel()
	for _, path := range startPaths() {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			runWith := path.run(t)
			s := helper(t, "cat")
			var out bytes.Buffer
			s.Stdin = strings.NewReader("hello himorime")
			s.Stdout = &out
			if _, err := runWith(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			if out.String() != "hello himorime" {
				t.Fatalf("stdout = %q", out.String())
			}
		})
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
	want := 250 * time.Millisecond
	if suspendedUntilAttached {
		// Four readings, 250ms apart: start, before and after attach, end.
		// The 250ms attach took while the process was suspended is not
		// latency: 750ms - 250ms.
		want = 500 * time.Millisecond
	}
	if res.Elapsed != want {
		t.Fatalf("Elapsed = %v, want %v from the injected clock", res.Elapsed, want)
	}
}

// TestRunFailsWhenTheTreeCannotBeAttached: a process that cannot be put under
// its tree handle is not measured as if its descendants could be stopped and
// accounted for. It is killed and reaped, and the error says why.
func TestRunFailsWhenTheTreeCannotBeAttached(t *testing.T) {
	t.Parallel()
	denied := errors.New("access is denied")
	s := helper(t, "sleep", "HELPER_SLEEP=1m")
	s.CollectUsage = true
	start := time.Now()
	res, err := run(context.Background(), s, nil, func(*exec.Cmd) (*tree, error) { return nil, denied })
	if !errors.Is(err, ErrProcessTree) || !errors.Is(err, denied) || errors.Is(err, ErrStart) {
		t.Fatalf("err = %v, want ErrProcessTree wrapping the cause", err)
	}
	if res.Elapsed != 0 || res.Usage.CPUErr != nil || res.ExitCode != 0 {
		t.Fatalf("a failed attach must not produce a measurement: %+v", res)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Fatalf("the process was not killed: Run took %v", elapsed)
	}
}

// TestRunShortLivedProcesses: a process that exits at once is attached and
// accounted for every time; on Windows it is suspended until it belongs to
// its job, so it cannot exit before.
func TestRunShortLivedProcesses(t *testing.T) {
	t.Parallel()
	for _, path := range startPaths() {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			runWith := path.run(t)
			cpuErr, _ := Capabilities()
			for i := range 30 {
				s := helper(t, "exit")
				s.CollectUsage = true
				res, err := runWith(context.Background(), s)
				if err != nil || res.ExitCode != 0 {
					t.Fatalf("run %d: %+v, %v", i, res, err)
				}
				if cpuErr == nil && res.Usage.CPUErr != nil {
					t.Fatalf("run %d: CPU time of a short-lived process was not collected: %v", i, res.Usage.CPUErr)
				}
			}
		})
	}
}

func TestRunTimeoutKillsTheProcessTree(t *testing.T) {
	t.Parallel()
	for _, path := range startPaths() {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			runWith := path.run(t)
			marker := filepath.Join(t.TempDir(), "marker")
			s := helper(t, "spawn", "HELPER_MARKER="+marker)
			s.Timeout = 300 * time.Millisecond

			start := time.Now()
			res, err := runWith(context.Background(), s)
			if err != nil {
				t.Fatal(err)
			}
			if !res.TimedOut {
				t.Fatalf("result = %+v, want TimedOut", res)
			}
			if time.Since(start) > 20*time.Second {
				t.Fatalf("Run returned after %v; the timeout did not stop the child", time.Since(start))
			}
			// The grandchild would write the marker 4s after it started, before Run
			// returned.
			time.Sleep(4500 * time.Millisecond)
			if _, err := os.Stat(marker); err == nil {
				t.Fatal("the grandchild survived the timeout and wrote its marker")
			}
		})
	}
}

func TestRunCancelKillsTheProcess(t *testing.T) {
	t.Parallel()
	for _, path := range startPaths() {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			runWith := path.run(t)
			ctx, cancel := context.WithCancel(context.Background())
			s := helper(t, "sleep", "HELPER_SLEEP=1m")
			go func() {
				time.Sleep(200 * time.Millisecond)
				cancel()
			}()
			res, err := runWith(ctx, s)
			if err != nil {
				t.Fatal(err)
			}
			if !res.Canceled {
				t.Fatalf("result = %+v, want Canceled", res)
			}
		})
	}
}

func TestRunAlreadyCanceledContextStartsNothing(t *testing.T) {
	t.Parallel()
	for _, path := range startPaths() {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			runWith := path.run(t)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			res, err := runWith(ctx, helper(t, "exit"))
			if err != nil {
				t.Fatal(err)
			}
			if !res.Canceled {
				t.Fatalf("result = %+v, want Canceled", res)
			}
		})
	}
}

func TestRunMissingProgramIsAStartError(t *testing.T) {
	t.Parallel()
	for _, path := range startPaths() {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			runWith := path.run(t)
			_, err := runWith(context.Background(), Spec{Path: "himorime-definitely-missing-program", Env: os.Environ()})
			if !errors.Is(err, ErrStart) {
				t.Fatalf("err = %v, want ErrStart", err)
			}
			_, err = runWith(context.Background(), Spec{Env: os.Environ()})
			if !errors.Is(err, ErrStart) {
				t.Fatalf("empty program: err = %v, want ErrStart", err)
			}
		})
	}
}

func TestRunLooksUpProgramInChildPath(t *testing.T) {
	t.Parallel()
	for _, path := range startPaths() {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			runWith := path.run(t)
			dir := t.TempDir()
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			name := "himorime-proc-helper"
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
			env := append(os.Environ(), "PATH="+dir, "HIMORIME_PROC_HELPER=exit", "HELPER_CODE=4")
			// A parallel test forking while the copy was open for writing holds the
			// file until its child execs, and executing it meanwhile fails with
			// ETXTBSY; that is the test's race, not the lookup's, so it is retried.
			var res Result
			for range 50 {
				res, err = runWith(context.Background(), Spec{Path: "himorime-proc-helper", Env: env, Stdout: io.Discard, Stderr: io.Discard})
				if !errors.Is(err, syscall.ETXTBSY) {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.ExitCode != 4 {
				t.Fatalf("exit code = %d, want 4 from the helper found through the child's PATH", res.ExitCode)
			}
		})
	}
}

func TestShellScript(t *testing.T) {
	t.Parallel()
	for _, path := range startPaths() {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			runWith := path.run(t)
			var out bytes.Buffer
			res, err := runWith(context.Background(), Spec{Script: "echo shell-ok", Env: os.Environ(), Stdout: &out, Stderr: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			if res.ExitCode != 0 || !strings.Contains(out.String(), "shell-ok") {
				t.Fatalf("result = %+v, stdout = %q", res, out.String())
			}
			if ShellName() == "" {
				t.Fatal("ShellName is empty")
			}
		})
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
	for _, path := range startPaths() {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			runWith := path.run(t)
			marker := filepath.Join(t.TempDir(), "marker")
			s := helper(t, "background", "HELPER_MARKER="+marker)
			// himorime always gives commands a file or the null device, never a pipe,
			// so Wait returns as soon as the command itself exits.
			out, err := os.Create(filepath.Join(t.TempDir(), "stdout"))
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			s.Stdout = out
			start := time.Now()
			res, err := runWith(context.Background(), s)
			if err != nil {
				t.Fatal(err)
			}
			if res.ExitCode != 0 || res.TimedOut || res.Canceled {
				t.Fatalf("result = %+v", res)
			}
			// The grandchild writes its marker 4s after it starts, so waiting for it
			// would take that long. The bound leaves room for starting a
			// race-instrumented helper on a shared macOS runner, which has taken over
			// a second on its own.
			if elapsed := time.Since(start); elapsed > 3*time.Second {
				t.Fatalf("Run waited %v for a background process", elapsed)
			}
			time.Sleep(4500 * time.Millisecond)
			if _, err := os.Stat(marker); err == nil {
				t.Fatal("a process left running by the command survived it")
			}
		})
	}
}

func TestLookPathResolvesRelativeEntriesAgainstTheWorkingDirectory(t *testing.T) {
	t.Parallel()
	for _, path := range startPaths() {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			runWith := path.run(t)
			dir := t.TempDir()
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			name := "himorime-relative-helper"
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
			env := append(os.Environ(), "PATH=bin", "HIMORIME_PROC_HELPER=exit", "HELPER_CODE=6")
			// The copy can be busy for a moment, as in TestRunLooksUpProgramInChildPath.
			var res Result
			for range 50 {
				res, err = runWith(context.Background(), Spec{Path: name, Dir: dir, Env: env, Stdout: io.Discard, Stderr: io.Discard})
				if !errors.Is(err, syscall.ETXTBSY) {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.ExitCode != 6 {
				t.Fatalf("exit code = %d, want 6 from bin/ relative to the working directory", res.ExitCode)
			}
		})
	}
}

// TestRunCollectsUsage checks the collected values against what the helper
// did, with bounds wide enough for a loaded CI machine: the values must be
// present, in the right unit, and move in the right direction.
func TestRunCollectsUsage(t *testing.T) {
	t.Parallel()
	for _, path := range startPaths() {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			runWith := path.run(t)
			if cpuErr, memErr := Capabilities(); cpuErr != nil || memErr != nil {
				t.Skipf("usage is not supported here: %v, %v", cpuErr, memErr)
			}
			s := helper(t, "burn", "HELPER_BURN=150ms", "HELPER_ALLOC=48")
			s.CollectUsage = true
			res, err := runWith(context.Background(), s)
			if err != nil || res.ExitCode != 0 {
				t.Fatalf("run = %+v, %v", res, err)
			}
			u := res.Usage
			if u.CPUErr != nil || u.MemoryErr != nil {
				t.Fatalf("usage errors: cpu %v, memory %v", u.CPUErr, u.MemoryErr)
			}
			total := u.UserCPU + u.SystemCPU
			if total < 50*time.Millisecond {
				t.Errorf("a 150ms busy loop used only %v of CPU", total)
			}
			if total > res.Elapsed*time.Duration(runtime.NumCPU())+50*time.Millisecond {
				t.Errorf("cpu time %v is impossible in %v on %d CPUs", total, res.Elapsed, runtime.NumCPU())
			}
			const allocated = 48 << 20
			if u.PeakRSS < allocated || u.PeakRSS > 4<<30 {
				t.Errorf("peak rss = %d bytes after touching %d bytes", u.PeakRSS, allocated)
			}
		})
	}
}

func TestRunWithoutCollectUsageReportsNothing(t *testing.T) {
	t.Parallel()
	for _, path := range startPaths() {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			runWith := path.run(t)
			res, err := runWith(context.Background(), helper(t, "exit"))
			if err != nil {
				t.Fatal(err)
			}
			if res.Usage.CPUErr == nil || res.Usage.MemoryErr == nil || res.Usage.UserCPU != 0 || res.Usage.PeakRSS != 0 {
				t.Fatalf("usage was reported without being requested: %+v", res.Usage)
			}
		})
	}
}

// TestRunUsageCoversChildren runs a parent that waits for a child doing the
// work. The CPU time and memory must include the child: a process-only
// measurement would see an idle parent.
func TestRunUsageCoversChildren(t *testing.T) {
	t.Parallel()
	for _, path := range startPaths() {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			runWith := path.run(t)
			if cpuErr, memErr := Capabilities(); cpuErr != nil || memErr != nil {
				t.Skipf("usage is not supported here: %v, %v", cpuErr, memErr)
			}
			s := helper(t, "parent", "HELPER_BURN=150ms", "HELPER_ALLOC=64")
			s.CollectUsage = true
			res, err := runWith(context.Background(), s)
			if err != nil || res.ExitCode != 0 {
				t.Fatalf("run = %+v, %v", res, err)
			}
			u := res.Usage
			if u.CPUErr != nil {
				t.Fatalf("cpu: %v", u.CPUErr)
			}
			if total := u.UserCPU + u.SystemCPU; total < 50*time.Millisecond {
				t.Errorf("the child's 150ms busy loop is missing from the tree's CPU time %v", total)
			}
			if runtime.GOOS == "windows" {
				if !IsUnsupported(u.MemoryErr) || u.Processes != 2 {
					t.Fatalf("peak rss of a two-process tree on Windows = %d, %v (processes %d); want unsupported", u.PeakRSS, u.MemoryErr, u.Processes)
				}
				return
			}
			if u.MemoryErr != nil {
				t.Fatalf("memory: %v", u.MemoryErr)
			}
			if u.PeakRSS < 64<<20 {
				t.Errorf("peak rss %d misses the child's 64MiB", u.PeakRSS)
			}
		})
	}
}

func TestUnsupportedError(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("wrapped: %w", &UnsupportedError{What: "peak rss", Reason: "because"})
	if !IsUnsupported(err) || IsUnsupported(errNotCollected) {
		t.Fatal("IsUnsupported")
	}
	if !strings.Contains(err.Error(), "peak rss is not supported: because") {
		t.Fatal(err)
	}
}

// TestRunPassesFilesAsStandardStreams: himorime's runner gives commands files,
// which reach the command as its own descriptors on both paths.
func TestRunPassesFilesAsStandardStreams(t *testing.T) {
	t.Parallel()
	for _, path := range startPaths() {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			runWith := path.run(t)
			dir := t.TempDir()
			in := filepath.Join(dir, "stdin")
			if err := os.WriteFile(in, []byte("fixture line\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			stdin, err := os.Open(in)
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			stdout, err := os.Create(filepath.Join(dir, "stdout"))
			if err != nil {
				t.Fatal(err)
			}
			defer stdout.Close()
			s := helper(t, "cat")
			s.Stdin, s.Stdout = stdin, stdout
			res, err := runWith(context.Background(), s)
			if err != nil || res.ExitCode != 0 {
				t.Fatalf("run = %+v, %v", res, err)
			}
			got, err := os.ReadFile(stdout.Name())
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "fixture line\n" {
				t.Fatalf("stdout file = %q", got)
			}
		})
	}
}

// TestRunDefaultsToHimorimesWorkingDirectoryAndEnvironment: a Spec without
// Dir or Env runs where himorime runs, with himorime's environment as it is
// now, whichever process starts it.
func TestRunDefaultsToHimorimesWorkingDirectoryAndEnvironment(t *testing.T) {
	t.Setenv("HIMORIME_PROC_HELPER", "getenv")
	t.Setenv("HELPER_NAME", "HIMORIME_PROC_TEST_INHERITED")
	t.Setenv("HIMORIME_PROC_TEST_INHERITED", "inherited-value")
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range startPaths() {
		t.Run(path.name, func(t *testing.T) {
			runWith := path.run(t)
			var out bytes.Buffer
			if _, err := runWith(context.Background(), Spec{Path: exe, Stdout: &out}); err != nil {
				t.Fatal(err)
			}
			if out.String() != "inherited-value" {
				t.Fatalf("the child saw %q, want the variable himorime has", out.String())
			}
			out.Reset()
			s := helper(t, "pwd")
			s.Stdout = &out
			if _, err := runWith(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			if out.String() != wd {
				t.Fatalf("working directory = %q, want %q", out.String(), wd)
			}
		})
	}
}

// TestRunReportsAFloorWithUsage: the floor is read only when usage is
// collected, and only where the process starting a command raises it.
func TestRunReportsAFloorWithUsage(t *testing.T) {
	t.Parallel()
	for _, path := range startPaths() {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			runWith := path.run(t)
			s := helper(t, "exit")
			res, err := runWith(context.Background(), s)
			if err != nil {
				t.Fatal(err)
			}
			if res.Usage.Floor != 0 {
				t.Fatalf("floor %d reported without collecting usage", res.Usage.Floor)
			}
			s.CollectUsage = true
			res, err = runWith(context.Background(), s)
			if err != nil {
				t.Fatal(err)
			}
			_, memErr := Capabilities()
			switch {
			case runtime.GOOS == "windows":
				if res.Usage.Floor != 0 {
					t.Fatalf("floor on Windows = %d, want 0", res.Usage.Floor)
				}
			case memErr == nil && res.Usage.Floor <= 0:
				t.Fatalf("floor = %d, want the starting process's peak RSS", res.Usage.Floor)
			}
		})
	}
}
