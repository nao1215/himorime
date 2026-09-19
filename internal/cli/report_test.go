package cli

import (
	"bytes"
	"context"
	"encoding/json"
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
