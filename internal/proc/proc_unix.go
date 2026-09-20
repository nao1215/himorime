//go:build !windows

package proc

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// configure places the child in a new process group so that the whole tree
// can be signaled at once. The child joins the group between fork and exec,
// before the command runs, so no descendant can start outside it. On a
// terminal the child starts a new session instead, which is also a new
// process group, and takes its standard input as the controlling terminal.
func configure(cmd *exec.Cmd, terminal bool) {
	if terminal {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// suspendedUntilAttached is false: the process group already exists when the
// command starts, and attach only records it.
const suspendedUntilAttached = false

type tree struct {
	pgid int
}

// attach records the process group. It cannot fail: Start would have failed
// if the group could not be created.
func attach(cmd *exec.Cmd) (*tree, error) {
	return &tree{pgid: cmd.Process.Pid}, nil
}

func (t *tree) kill() error {
	err := syscall.Kill(-t.pgid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// close stops whatever the command left running in its process group.
func (t *tree) close() {
	_ = t.kill()
}

func shellCommand(script string) (*exec.Cmd, error) {
	return exec.Command("/bin/sh", "-c", script), nil //nolint:noctx,gosec // shell: true is an explicit opt-in; substituted values are quoted by ShellQuote
}

// ShellQuote quotes s for /bin/sh so that it is one literal word.
func ShellQuote(s string) (string, error) {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'", nil
}

func hasSeparator(name string) bool { return strings.ContainsRune(name, '/') }

func isPathVar(k string) bool { return k == "PATH" }

func lookPathIn(name, pathList, workdir string) (string, error) {
	for _, dir := range filepath.SplitList(pathList) {
		if dir == "" {
			continue
		}
		p := filepath.Join(pathEntry(dir, workdir), name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 { //nolint:gosec // G703: a benchmark's PATH may name executables outside the suite.
			return p, nil
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}
