package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/nao1215/himorime/internal/exitcode"
)

func runComment(ctx context.Context, a *App, args []string) int {
	runner := a.RunCommentHelper
	if runner == nil {
		runner = runCommentHelper
	}
	code, err := runner(ctx, args, a.Stdin, a.Stdout, a.Stderr, a.Environ())
	if err != nil {
		fmt.Fprintf(a.Stderr, "himorime comment: %v\n", err)
		return exitcode.Execution
	}
	return code
}

func runCommentHelper(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, environ []string) (int, error) {
	path, err := findCommentHelper()
	if err != nil {
		return 0, err
	}
	cmd := exec.CommandContext(ctx, path, args...) //nolint:gosec // path is the fixed himorime-comment helper name.
	cmd.Stdin, cmd.Stdout, cmd.Stderr, cmd.Env = stdin, stdout, stderr, environ
	err = cmd.Run()
	if err == nil {
		return exitcode.OK, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 0, fmt.Errorf("run himorime-comment: %w", err)
}

func findCommentHelper() (string, error) {
	name := "himorime-comment"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if executable, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(executable), name)
		if path, pathErr := exec.LookPath(sibling); pathErr == nil {
			return path, nil
		}
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", errors.New("himorime-comment helper is not installed next to himorime or on PATH")
	}
	return path, nil
}
