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
// can be signaled at once.
func configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

type tree struct {
	pgid int
}

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

// ShellName names the shell `shell: true` commands run through.
func ShellName() string { return "/bin/sh -c" }

func hasSeparator(name string) bool { return strings.ContainsRune(name, '/') }

func isPathVar(k string) bool { return k == "PATH" }

func lookPathIn(name, pathList, workdir string) (string, error) {
	for _, dir := range filepath.SplitList(pathList) {
		if dir == "" {
			continue
		}
		p := filepath.Join(pathEntry(dir, workdir), name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}
