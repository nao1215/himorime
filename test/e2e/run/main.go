// Command run executes the end-to-end suite. It builds himorime (or copies the
// binary named by HIMORIME_BINARY) and the portable helper into a temporary
// directory put first on PATH, then runs the atago specs under
// test/e2e/atago against them. It is written in Go so that the same entry
// point works on Linux, macOS and Windows without a POSIX shell.
//
//	go run ./test/e2e/run [atago run flags] [spec paths]
//
// The suite must not modify the repository: the Git status is compared before
// and after the run, and any difference fails it.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func run(args []string) (int, error) {
	root, err := repoRoot()
	if err != nil {
		return 1, err
	}
	atago, err := exec.LookPath("atago")
	if err != nil {
		return 127, errors.New("atago is not installed: go install github.com/nao1215/atago@v0.25.0")
	}
	bin, err := os.MkdirTemp("", "himorime-e2e-bin-")
	if err != nil {
		return 1, err
	}
	defer os.RemoveAll(bin)

	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	if prebuilt := os.Getenv("HIMORIME_BINARY"); prebuilt != "" {
		if err := copyFile(prebuilt, filepath.Join(bin, "himorime"+exe)); err != nil {
			return 1, fmt.Errorf("copy HIMORIME_BINARY: %w", err)
		}
	} else if err := goBuild(root, filepath.Join(bin, "himorime"+exe), "."); err != nil {
		return 1, err
	}
	if err := goBuild(root, filepath.Join(bin, "e2ehelper"+exe), "./test/e2e/helper"); err != nil {
		return 1, err
	}

	before, err := gitStatus(root)
	if err != nil {
		return 1, err
	}

	atagoArgs := []string{"run"}
	if !hasParallel(args) {
		// Several scenarios measure CPU time and utilization, which drop when
		// other scenarios compete for the CPUs. atago v0.25.0 runs four
		// scenarios per CPU by default; the suite keeps one per CPU, the
		// concurrency its thresholds were set under.
		atagoArgs = append(atagoArgs, "--parallel", strconv.Itoa(runtime.NumCPU()))
	}
	atagoArgs = append(atagoArgs, args...)
	if !hasSpecPath(args) {
		atagoArgs = append(atagoArgs, filepath.Join(root, "test", "e2e", "atago"))
	}
	cmd := exec.Command(atago, atagoArgs...)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	reports := os.Getenv("HIMORIME_E2E_REPORT_DIR")
	if reports == "" {
		reports, err = os.MkdirTemp("", "himorime-e2e-reports-")
		if err != nil {
			return 1, err
		}
	}
	reports, err = filepath.Abs(reports)
	if err != nil {
		return 1, err
	}
	fmt.Fprintln(os.Stderr, "E2E diagnostic reports:", reports)
	cmd.Env = append(os.Environ(),
		"HIMORIME_E2E_REPORT_DIR="+reports,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HIMORIME_REPO="+root,
		"NO_COLOR=1",
		// A run inside GitHub Actions must not pick up the job's real event.
		"GITHUB_ACTIONS=", "GITHUB_EVENT_NAME=", "GITHUB_EVENT_PATH=", "GITHUB_STEP_SUMMARY=", "HIMORIME_BASE_REF=",
	)
	runErr := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		code = exitErr.ExitCode()
	} else if runErr != nil {
		return 1, runErr
	}

	after, err := gitStatus(root)
	if err != nil {
		return 1, err
	}
	if changed := statusDiff(before, after); changed != "" {
		return 1, fmt.Errorf("the E2E suite modified the repository:\n%s", changed)
	}
	return code, nil
}

// hasParallel reports whether the caller chose the concurrency.
func hasParallel(args []string) bool {
	for _, a := range args {
		if a == "--parallel" || strings.HasPrefix(a, "--parallel=") {
			return true
		}
	}
	return false
}

func hasSpecPath(args []string) bool {
	for _, a := range args {
		if strings.HasSuffix(a, ".atago.yaml") || strings.Contains(a, "e2e") {
			return true
		}
	}
	return false
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("run this from inside the himorime repository")
		}
		dir = parent
	}
}

func goBuild(root, out, pkg string) error {
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Dir = root
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build %s: %w", pkg, err)
	}
	return nil
}

func gitStatus(root string) (string, error) {
	cmd := exec.Command("git", "status", "--porcelain", "--untracked-files=all")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git status: %w", err)
	}
	return string(out), nil
}

// statusDiff lists the `git status` lines present in only one of the two
// snapshots.
func statusDiff(before, after string) string {
	set := func(s string) map[string]bool {
		m := map[string]bool{}
		for _, l := range strings.Split(s, "\n") {
			if l != "" {
				m[l] = true
			}
		}
		return m
	}
	b, a := set(before), set(after)
	var out []string
	for l := range a {
		if !b[l] {
			out = append(out, "+ "+l)
		}
	}
	for l := range b {
		if !a[l] {
			out = append(out, "- "+l)
		}
	}
	return strings.Join(out, "\n")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
