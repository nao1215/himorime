// Package proc starts processes so that a whole process tree can be stopped,
// and measures their wall-clock duration.
//
// On Unix every child is placed in its own process group before it executes
// anything, and stopping it sends SIGKILL to the group. On Windows every child
// is created suspended, assigned to a Job Object, and only then resumed, so no
// descendant can start outside the job; stopping it terminates the job. The
// tree is stopped on timeout, on cancellation, and also when the command
// exits: a process a command leaves running in the background is stopped with
// it on every platform, so a benchmark cannot leak processes and a temporary
// directory is never still in use when himorime removes it.
package proc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Spec describes one process to run.
type Spec struct {
	// Path is the program. When it holds no path separator it is looked up in
	// PATH (on Windows, in the PATH of Env when Env sets one).
	Path string
	Args []string
	// Script, when non-empty, runs through the platform shell instead of Path.
	Script string
	Dir    string
	// Env is the complete environment of the child.
	Env     []string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Timeout time.Duration
	// CollectUsage reads the CPU time and peak RSS of the process tree after
	// it exits. Without it, Result.Usage reports both as not collected.
	CollectUsage bool
	// MeasureMemory, with CollectUsage, says the peak RSS will be used. With
	// EnableSpawner the command is then started from the spawner, so that
	// himorime's own memory does not raise its peak RSS. The spawner costs
	// about a millisecond to start once and tens of microseconds per run, all
	// outside the measured interval, so commands whose memory is not measured
	// are started directly.
	MeasureMemory bool
}

// Result is the outcome of Run.
type Result struct {
	// Elapsed is the wall-clock time from just before the process was started
	// until it was reaped.
	Elapsed  time.Duration
	ExitCode int
	// TimedOut is set when Timeout expired and the tree was killed.
	TimedOut bool
	// Canceled is set when the context was canceled and the tree was killed.
	Canceled bool
	// Usage is the resource usage of the tree, read from the operating
	// system after the process exited and outside Elapsed.
	Usage Usage
}

// ErrStart wraps a failure to start the process at all (program not found,
// permission denied, bad working directory).
var ErrStart = errors.New("start process")

// ErrProcessTree wraps a failure to place a started process under a handle
// that covers its whole tree (a Job Object on Windows). The process is killed
// before it runs any code of its own: without the handle, neither stopping
// its descendants nor their CPU accounting could be trusted.
var ErrProcessTree = errors.New("attach the process tree")

// Clock returns the current time. Tests replace it.
type Clock func() time.Time

// Run starts the process, waits for it, and kills its process tree when the
// timeout expires or ctx is canceled. A non-zero exit status is reported in
// Result, not as an error; the error is non-nil only when the process could
// not be started or waited for.
//
// With EnableSpawner, MeasureMemory and no injected clock, the command is
// started from the spawner, which measures its elapsed time the same way.
func Run(ctx context.Context, s Spec, now Clock) (Result, error) {
	if now == nil && s.CollectUsage && s.MeasureMemory && spawnerEnabled.Load() {
		if res, handled, err := runSpawned(ctx, s); handled {
			return res, err
		}
	}
	return run(ctx, s, now, attach)
}

// run is Run with the function that attaches the tree handle, which tests
// replace to simulate a platform failure.
func run(ctx context.Context, s Spec, now Clock, attachTree func(*exec.Cmd) (*tree, error)) (Result, error) {
	if now == nil {
		now = time.Now
	}
	cmd, err := command(s)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrStart, err)
	}
	cmd.Dir = s.Dir
	cmd.Env = s.Env
	cmd.Stdin = s.Stdin
	cmd.Stdout = s.Stdout
	cmd.Stderr = s.Stderr
	// When output goes to a Go writer rather than a file, os/exec copies it
	// through a pipe and Wait also waits for every process holding that pipe.
	// Bound that wait so a stray descendant cannot hold a finished command for
	// long. himorime's runner always passes files or nil.
	cmd.WaitDelay = waitDelay
	configure(cmd)

	select {
	case <-ctx.Done():
		return Result{Canceled: true, ExitCode: -1}, nil
	default:
	}

	if s.CollectUsage {
		resetFloor()
	}
	start := now()
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrStart, err)
	}
	// Where the process starts suspended (Windows), attach puts it in its
	// tree handle before it runs anything and then resumes it, so no
	// descendant can escape. The process runs no code of its own meanwhile,
	// so the time attach takes is not part of its latency.
	attachStart := start
	if suspendedUntilAttached {
		attachStart = now()
	}
	tree, err := attachTree(cmd)
	var paused time.Duration
	if suspendedUntilAttached {
		paused = now().Sub(attachStart)
	}
	if err != nil {
		// Without the tree handle neither stopping the command's descendants
		// nor their CPU accounting could be trusted, so the process is killed
		// (where it is suspended, before it ran anything) and reaped.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return Result{}, fmt.Errorf("%w: %w", ErrProcessTree, err)
	}
	var probe *usageProbe
	if s.CollectUsage {
		probe = newUsageProbe(cmd, tree)
	}

	done := make(chan struct{})
	watcherDone := make(chan struct{})
	killed := make(chan killReason, 1)
	go func() {
		defer close(watcherDone)
		watch(ctx, s.Timeout, done, killed, func() {
			_ = tree.kill()
		})
	}()

	waitErr := cmd.Wait()
	elapsed := now().Sub(start) - paused
	var floor int64
	if s.CollectUsage {
		floor = readFloor()
	}
	close(done)
	// The watcher may be killing the tree right now; the tree handle is only
	// released after it has finished.
	<-watcherDone
	usage := notCollected()
	if probe != nil {
		// Read before the tree is closed: closing stops what the command left
		// running and releases the handles the counters are read through.
		usage = probe.collect(cmd, tree)
		usage.Floor = floor
		probe.close()
	}
	tree.close()

	res := Result{Elapsed: elapsed, ExitCode: exitCode(cmd, waitErr), Usage: usage}
	select {
	case r := <-killed:
		res.TimedOut = r == killTimeout
		res.Canceled = r == killCancel
	default:
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) {
		return res, fmt.Errorf("wait for process: %w", waitErr)
	}
	return res, nil
}

// waitDelay bounds how long Wait keeps copying output after the command
// exited.
const waitDelay = 5 * time.Second

// pathEntry resolves one PATH entry the way the child sees it: a relative
// entry is relative to the child's working directory.
func pathEntry(entry, dir string) string {
	if filepath.IsAbs(entry) {
		return entry
	}
	base := dir
	if base == "" {
		base, _ = os.Getwd()
	}
	return filepath.Join(base, entry)
}

type killReason int

const (
	killTimeout killReason = iota + 1
	killCancel
)

func watch(ctx context.Context, timeout time.Duration, done <-chan struct{}, killed chan<- killReason, kill func()) {
	var timer <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timer = t.C
	}
	select {
	case <-done:
	case <-timer:
		if !finished(done) {
			killed <- killTimeout
			kill()
		}
	case <-ctx.Done():
		if !finished(done) {
			killed <- killCancel
			kill()
		}
	}
}

func finished(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func exitCode(cmd *exec.Cmd, waitErr error) int {
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	if waitErr != nil {
		return -1
	}
	return 0
}

func command(s Spec) (*exec.Cmd, error) {
	if s.Script != "" {
		return shellCommand(s.Script)
	}
	if s.Path == "" {
		return nil, errors.New("empty program name")
	}
	path, err := lookPath(s.Path, s.Env, s.Dir)
	if err != nil {
		return nil, err
	}
	// Not CommandContext: cancellation must stop the whole process tree, which
	// watch does through the platform tree handle. CommandContext would kill
	// only the direct child.
	cmd := exec.Command(path, s.Args...) //nolint:noctx,gosec // see above; G204: running the suite's command is the purpose, as an argv without a shell
	return cmd, nil
}

// lookPath resolves a bare program name against the PATH the child will see,
// so that `env: {PATH: ...}` in a suite chooses the program as it would in a
// shell.
func lookPath(name string, env []string, dir string) (string, error) {
	if hasSeparator(name) {
		return name, nil
	}
	for i := len(env) - 1; i >= 0; i-- {
		if k, v, ok := cutEnv(env[i]); ok && isPathVar(k) {
			old, had := os.LookupEnv(k)
			if had && old == v {
				break
			}
			return lookPathIn(name, v, dir)
		}
	}
	p, err := exec.LookPath(name)
	if err != nil && errors.Is(err, exec.ErrDot) {
		return "", fmt.Errorf("%s resolves to the current directory, which is not searched; write ./%s", name, name)
	}
	return p, err
}

func cutEnv(kv string) (string, string, bool) {
	for i := 1; i < len(kv); i++ {
		if kv[i] == '=' {
			return kv[:i], kv[i+1:], true
		}
	}
	return "", "", false
}
