package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nao1215/himorime/internal/config"
	"github.com/nao1215/himorime/internal/report"
	"github.com/nao1215/himorime/internal/runner"
)

type reportFailingWriter struct{}

func (reportFailingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func savedReportJSON(t *testing.T, failed bool) []byte {
	t.Helper()
	command := config.Command{Name: "tool", Exec: config.Exec{Argv: []string{"tool"}}}
	benchmark := config.Benchmark{Name: "saved", Commands: []config.Command{command}}
	measurement := &runner.Measurement{Samples: []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}}
	if failed {
		measurement.Samples = nil
		measurement.Failure = &runner.Failure{Kind: runner.FailExitCode, ExitCode: 7, Message: "saved failure"}
	}
	result := runner.BenchmarkResult{
		Benchmark: benchmark,
		Rounds:    3,
		Commands:  []runner.CommandResult{{Command: command, Sides: map[string]*runner.Measurement{runner.SideHead: measurement}}},
	}
	suite := &config.Suite{Name: "saved", Benchmarks: []config.Benchmark{benchmark}}
	r := &report.Report{
		HimorimeVersion: "test", StartedAt: time.Unix(0, 0).UTC(), FinishedAt: time.Unix(1, 0).UTC(),
		Environment: report.Environment{OS: "linux", Arch: "amd64", LogicalCPUs: 1, Tools: []report.Tool{}},
	}
	report.Judge(r, []report.SuiteInput{{Suite: suite, File: "suite.yaml", Benchmarks: []runner.BenchmarkResult{result}}}, report.Options{Mode: report.ModeRun, Seed: 42})
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestReportCommandRendersStoredFailureAndReturnsSuccess(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "result.json")
	if err := os.WriteFile(input, savedReportJSON(t, true), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	a := &App{Stdout: &stdout, Stderr: &stderr, Getwd: func() (string, error) { return dir, nil }, ReadFile: os.ReadFile}
	if code := a.Run(context.Background(), []string{"report", "result.json"}); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "exit 4") {
		t.Fatalf("stored exit code missing from output: %s", stdout.String())
	}
}

func TestReportCommandRejectsInputAliasesAndBadOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "result.json")
	data := savedReportJSON(t, false)
	if err := os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"./result.json", "hard.json", "link.json"} {
		if alias == "hard.json" {
			if err := os.Link(input, filepath.Join(dir, alias)); err != nil {
				t.Fatal(err)
			}
		} else if alias == "link.json" {
			if err := os.Symlink(input, filepath.Join(dir, alias)); err != nil {
				if runtime.GOOS == "windows" {
					t.Logf("symlink unavailable on Windows: %v", err)
					continue
				}
				t.Fatal(err)
			}
		}
		var stdout, stderr bytes.Buffer
		a := &App{Stdout: &stdout, Stderr: &stderr, Getwd: func() (string, error) { return dir, nil }, ReadFile: os.ReadFile}
		if code := a.Run(context.Background(), []string{"report", "result.json", "--output", alias}); code != 2 {
			t.Errorf("alias %s code=%d stderr=%s", alias, code, stderr.String())
		}
		if got, err := os.ReadFile(input); err != nil || !bytes.Equal(got, data) {
			t.Errorf("alias %s changed source: err=%v", alias, err)
		}
	}
	var stdout, stderr bytes.Buffer
	a := &App{Stdout: &stdout, Stderr: &stderr, Getwd: func() (string, error) { return dir, nil }, ReadFile: os.ReadFile}
	if code := a.Run(context.Background(), []string{"report", "result.json", "--output", filepath.Join(dir, "result.json", "child")}); code != 4 {
		t.Fatalf("bad output code=%d stderr=%s", code, stderr.String())
	}
}

func TestReportCommandSavesRenderedOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "result.json")
	if err := os.WriteFile(input, savedReportJSON(t, false), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	a := &App{Stdout: &stdout, Stderr: &stderr, Getwd: func() (string, error) { return dir, nil }, ReadFile: os.ReadFile}
	if code := a.Run(context.Background(), []string{"report", "result.json", "--format", "markdown", "--output", filepath.Join("nested", "report.md")}); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "nested", "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "## saved") || !strings.Contains(string(data), "1 passed · 1 benchmark") {
		t.Fatalf("saved report does not look like markdown: %s", data)
	}
	if stdout.Len() != 0 {
		t.Fatalf("saved report leaked to stdout: %q", stdout.String())
	}
	if runtime.GOOS == "windows" {
		return
	}
	// The file is written as run --output writes it: readable by others, as
	// a page for a documentation site must be, and through a symbolic link.
	if info, err := os.Stat(filepath.Join(dir, "nested", "report.md")); err != nil || info.Mode().Perm()&0o044 == 0 {
		t.Fatalf("saved report mode = %v, %v; want it readable by group and others", info.Mode(), err)
	}
	target := filepath.Join(dir, "target.md")
	if err := os.Symlink(target, filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	if code := a.Run(context.Background(), []string{"report", "result.json", "--format", "markdown", "--output", "link.md"}); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if info, err := os.Lstat(filepath.Join(dir, "link.md")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symbolic link was replaced: %v, %v", info.Mode(), err)
	}
	if data, err := os.ReadFile(target); err != nil || !strings.Contains(string(data), "## saved") {
		t.Fatalf("the link target was not written: %q, %v", data, err)
	}
}

func TestReportCommandRejectsInvalidInputs(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "result.json")
	if err := os.WriteFile(input, savedReportJSON(t, false), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		args []string
		want int
	}{
		{name: "missing input", args: []string{"report"}, want: 3},
		{name: "too many inputs", args: []string{"report", "result.json", "other.json"}, want: 3},
		{name: "unknown format", args: []string{"report", "result.json", "--format", "csv"}, want: 3},
		{name: "missing file", args: []string{"report", "missing.json"}, want: 2},
		{name: "invalid report", args: []string{"report", "invalid.json"}, want: 2},
	}
	if err := os.WriteFile(filepath.Join(dir, "invalid.json"), []byte(`{"schema_version":"1","unexpected":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			a := &App{Stdout: &stdout, Stderr: &stderr, Getwd: func() (string, error) { return dir, nil }, ReadFile: os.ReadFile}
			if code := a.Run(context.Background(), tc.args); code != tc.want {
				t.Fatalf("code=%d stderr=%s", code, stderr.String())
			}
			if stderr.Len() == 0 {
				t.Fatal("invalid input did not produce a diagnostic")
			}
		})
	}

	writeCases := []struct {
		name string
		app  *App
		args []string
		want int
	}{
		{name: "read failure", app: &App{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getwd: func() (string, error) { return dir, nil }, ReadFile: func(string) ([]byte, error) { return nil, errors.New("read failed") }}, args: []string{"report", "result.json"}, want: 2},
		{name: "rendered output failure", app: &App{Stdout: reportFailingWriter{}, Stderr: &bytes.Buffer{}, Getwd: func() (string, error) { return dir, nil }, ReadFile: os.ReadFile}, args: []string{"report", "result.json"}, want: 4},
	}
	for _, tc := range writeCases {
		t.Run(tc.name, func(t *testing.T) {
			if code := tc.app.Run(context.Background(), tc.args); code != tc.want {
				t.Fatalf("code=%d", code)
			}
		})
	}
}

func TestReportCommandHelpAndInjectedDependencyFallbacks(t *testing.T) {
	var stdout, stderr bytes.Buffer
	a := &App{Stdout: &stdout, Stderr: &stderr}
	if code := a.Run(context.Background(), []string{"report", "--help"}); code != 0 || !strings.Contains(stdout.String(), "Usage: himorime report") {
		t.Fatalf("report help: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	a.Getwd = func() (string, error) { return "", errors.New("working directory unavailable") }
	if code := a.Run(context.Background(), []string{"report", "saved.json"}); code != 2 || !strings.Contains(stderr.String(), "working directory unavailable") {
		t.Fatalf("working-directory failure: code=%d stderr=%q", code, stderr.String())
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "saved.json")
	if err := os.WriteFile(input, savedReportJSON(t, false), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	a.Getwd = nil
	a.ReadFile = nil
	if code := a.Run(context.Background(), []string{"report", input}); code != 0 || stdout.Len() == 0 {
		t.Fatalf("default ReadFile: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestReportPathsSameWrapsOutputWorkingDirectoryError(t *testing.T) {
	getwdErr := errors.New("working directory unavailable")
	input := filepath.Join(t.TempDir(), "input.json")
	if err := os.WriteFile(input, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &App{Getwd: func() (string, error) { return "", getwdErr }}
	_, err := reportPathsSame(a, input, "result.md")
	if !errors.Is(err, getwdErr) {
		t.Fatalf("error = %v, want wrapped working-directory error", err)
	}
}

func TestWriteSavedReportReportsFilesystemErrors(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &App{Getwd: func() (string, error) { return dir, nil }}
	if err := writeSavedReport(a, filepath.Join("not-a-directory", "report.md"), []byte("report")); err == nil {
		t.Fatal("writing below a regular file unexpectedly succeeded")
	}
	existingDir := filepath.Join(dir, "existing")
	if err := os.Mkdir(existingDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := writeSavedReport(a, "existing", []byte("report")); err == nil {
		t.Fatal("replacing a directory unexpectedly succeeded")
	}
	a.Getwd = func() (string, error) { return "", errors.New("working directory unavailable") }
	if err := writeSavedReport(a, "report.md", []byte("report")); err == nil || !strings.Contains(err.Error(), "working directory unavailable") {
		t.Fatalf("working-directory error = %v", err)
	}
}

func TestReportWritersRejectMissingRoots(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	r := &report.Report{}
	if err := writeSuiteReport(missing, config.Output{Format: config.FormatJSON, Path: "report.json"}, r); err == nil {
		t.Fatal("suite report accepted a missing suite root")
	}
	if err := writeSection(filepath.Join(missing, "report.md"), "bench", r); err == nil {
		t.Fatal("section report accepted a missing output directory")
	}
}

func TestMeasurementAndFileWritersPropagateErrors(t *testing.T) {
	var stderr bytes.Buffer
	m := &measurement{
		app: &App{
			Stdout:    reportFailingWriter{},
			Stderr:    &stderr,
			LookupEnv: func(string) (string, bool) { return "", false },
		},
		cmd:   "run",
		flags: &measureFlags{format: string(config.FormatTable)},
	}
	if code := m.writeReports(&report.Report{}, nil); code != 4 || !strings.Contains(stderr.String(), "write report to stdout") {
		t.Fatalf("stdout write failure: code=%d stderr=%q", code, stderr.String())
	}

	dir := t.TempDir()
	blocked := filepath.Join(dir, "regular-file")
	if err := os.WriteFile(blocked, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(nil, filepath.Join(blocked, "nested", "report.md"), false, func(io.Writer) error { return nil }); err == nil || !strings.Contains(err.Error(), "create report directory") {
		t.Fatalf("directory creation error = %v", err)
	}
	if err := writeFile(nil, dir, false, func(io.Writer) error { return nil }); err == nil || !strings.Contains(err.Error(), "open report file") {
		t.Fatalf("open directory error = %v", err)
	}
	writeErr := errors.New("render failed")
	if err := writeFile(nil, filepath.Join(dir, "report.md"), false, func(io.Writer) error { return writeErr }); !errors.Is(err, writeErr) {
		t.Fatalf("render error = %v", err)
	}
}
