//go:build unix

package proc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// idleSpawnerPids lists the process IDs of the idle spawners.
func idleSpawnerPids() []int {
	spawners.mu.Lock()
	defer spawners.mu.Unlock()
	pids := make([]int, 0, len(spawners.idle))
	for _, sp := range spawners.idle {
		pids = append(pids, sp.process.Pid)
	}
	return pids
}

// ownerHelper plays himorime: it runs commands one at a time, which must all
// go through one spawner, prints the spawner's process ID and, with
// HELPER_HANG, starts a long command and never returns, for the test to kill
// it.
func ownerHelper() int {
	EnableSpawner()
	exe, err := os.Executable()
	if err != nil {
		return 3
	}
	env := os.Environ()
	for range 3 {
		if _, err := Run(context.Background(), Spec{Path: exe, Env: append(env, "HIMORIME_PROC_HELPER=exit"), CollectUsage: true, MeasureMemory: true}, nil); err != nil {
			return 3
		}
	}
	pids := idleSpawnerPids()
	if len(pids) != 1 {
		return 4
	}
	fmt.Println(pids[0])
	if os.Getenv("HELPER_HANG") == "" {
		return 0
	}
	_, _ = Run(context.Background(), Spec{Path: exe, Env: append(env, "HIMORIME_PROC_HELPER=spawn"), CollectUsage: true, MeasureMemory: true}, nil)
	return 5
}

// processGone reports whether pid no longer runs; a zombie waiting for its
// new parent to reap it counts as gone.
func processGone(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return true
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	_, rest, ok := strings.Cut(string(stat), ") ")
	return ok && strings.HasPrefix(rest, "Z")
}

func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !processGone(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("the spawner %d still runs after the process that started it exited", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func startOwner(t *testing.T, env ...string) (*exec.Cmd, int) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(append(os.Environ(), "HIMORIME_PROC_HELPER=owner"), env...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("the owner printed no spawner: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	return cmd, pid
}

func TestSpawnerExitsWithHimorime(t *testing.T) {
	t.Parallel()
	owner, pid := startOwner(t)
	if err := owner.Wait(); err != nil {
		t.Fatalf("owner: %v", err)
	}
	waitGone(t, pid)
}

// TestSpawnerStopsTheCommandWhenHimorimeIsKilled: himorime killed with
// SIGKILL cannot stop the command it measures, so the spawner does, and then
// exits.
func TestSpawnerStopsTheCommandWhenHimorimeIsKilled(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "marker")
	owner, pid := startOwner(t, "HELPER_HANG=1", "HELPER_MARKER="+marker)
	// Let the spawner start the long command and its grandchild.
	time.Sleep(500 * time.Millisecond)
	_ = owner.Process.Kill()
	_ = owner.Wait()
	waitGone(t, pid)
	// The grandchild would write the marker 4s after it started.
	time.Sleep(4500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the command survived himorime and its spawner")
	}
}

// TestSpawnerReportsStartFailuresLikeADirectStart: a start failure carries
// the same message and the same errno on both paths.
func TestSpawnerReportsStartFailuresLikeADirectStart(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "missing")
	for _, s := range []Spec{
		{Path: missing, Env: os.Environ()},
		{Path: "/bin/sh", Args: []string{"-c", "true"}, Dir: missing, Env: os.Environ()},
	} {
		_, direct := run(context.Background(), s, nil, attach)
		_, handled, spawned := runSpawned(context.Background(), s)
		if !handled {
			t.Fatal("no spawner took the command")
		}
		if !errors.Is(direct, ErrStart) || !errors.Is(spawned, ErrStart) {
			t.Fatalf("errors = %v / %v, want ErrStart", direct, spawned)
		}
		if direct.Error() != spawned.Error() || !errors.Is(spawned, syscall.ENOENT) {
			t.Fatalf("spawner error %q (ENOENT: %v), want %q", spawned, errors.Is(spawned, syscall.ENOENT), direct)
		}
	}
}

func TestServeSpawnerRefusesToRunWithoutHimorime(t *testing.T) {
	t.Parallel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), SpawnerEnv+"=1")
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 || !strings.Contains(string(out), "internal use") {
		t.Fatalf("a spawner started by hand: %v, %q", err, out)
	}
}

// TestRunUsesTheSpawnerOnlyForMemory: only a command whose peak RSS is
// measured pays for the spawner; everything else is started by himorime.
func TestRunUsesTheSpawnerOnlyForMemory(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name            string
		usage, memory   bool
		fromThisProcess bool
	}{
		{"latency", false, false, true},
		{"cpu", true, false, true},
		{"memory", true, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out strings.Builder
			s := helper(t, "ppid")
			s.Stdout, s.CollectUsage, s.MeasureMemory = &out, tt.usage, tt.memory
			if _, err := Run(context.Background(), s, nil); err != nil {
				t.Fatal(err)
			}
			parent, err := strconv.Atoi(out.String())
			if err != nil {
				t.Fatal(err)
			}
			if (parent == os.Getpid()) != tt.fromThisProcess {
				t.Fatalf("parent %d, this process %d", parent, os.Getpid())
			}
		})
	}
}
