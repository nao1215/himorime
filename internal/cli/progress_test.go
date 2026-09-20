package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nao1215/himorime/internal/exitcode"
	"github.com/nao1215/himorime/internal/runner"
)

func TestProgressRendererThrottlesAndClosesLine(t *testing.T) {
	var out bytes.Buffer
	now := time.Unix(0, 0)
	p := newProgressRenderer(&out, func() time.Time { return now })
	p.Update(runner.Progress{Benchmark: "bench", WarmupTotal: 1, Runs: 3})
	first := out.String()
	now = now.Add(50 * time.Millisecond)
	p.Update(runner.Progress{Benchmark: "bench", Warmups: 1, WarmupTotal: 1, Runs: 3})
	if out.String() != first {
		t.Fatalf("redraw before throttle interval: %q", out.String())
	}
	now = now.Add(50 * time.Millisecond)
	p.Update(runner.Progress{Benchmark: "bench", Warmups: 1, WarmupTotal: 1, Rounds: 1, Runs: 3})
	if !strings.Contains(out.String(), "warmup 1/1") || !strings.Contains(out.String(), "measured 1/3") {
		t.Fatalf("updated progress = %q", out.String())
	}
	p.Close()
	if !strings.HasSuffix(out.String(), "\033[2K\n") {
		t.Fatalf("progress line was not cleared with a final newline: %q", out.String())
	}
	p.Close()
}

func TestProgressRendererRedrawsAroundLogs(t *testing.T) {
	var out bytes.Buffer
	p := newProgressRenderer(&out, time.Now)
	p.Update(runner.Progress{Benchmark: "bench", Runs: 1})
	if _, err := p.Write([]byte("himorime: suite log\n")); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "\r\033[2Khimorime: suite log\n") || !strings.HasSuffix(text, "measured 0/1") {
		t.Fatalf("log interleaving = %q", text)
	}
	p.Close()
}

func TestProgressTextAdaptiveDoesNotInventTotal(t *testing.T) {
	text := progressText(runner.Progress{Benchmark: "adaptive", WarmupTotal: 2, Warmups: 2, Rounds: 7})
	if strings.Contains(text, "measured 7/") || !strings.Contains(text, "measured 7") {
		t.Fatalf("adaptive text = %q", text)
	}
}

type failOnWrite struct {
	calls int
	fail  int
}

func (w *failOnWrite) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == w.fail {
		return 0, errors.New("write failed")
	}
	return len(p), nil
}

func TestProgressRendererPropagatesLogWriteErrors(t *testing.T) {
	for _, fail := range []int{1, 2} {
		out := &failOnWrite{fail: fail}
		p := newProgressRenderer(out, nil)
		p.active = true
		if _, err := p.Write([]byte("log\n")); err == nil {
			t.Errorf("write %d failure was swallowed", fail)
		}
	}

	out := &failOnWrite{fail: 1}
	p := newProgressRenderer(out, nil)
	p.Close()
	if n, err := p.Write([]byte("after close")); err == nil || n != 0 {
		t.Fatalf("closed renderer write = %d, %v", n, err)
	}
	p.Update(runner.Progress{Benchmark: "ignored"})
}

func TestNonTTYLogHasNoANSI(t *testing.T) {
	var out bytes.Buffer
	m := &measurement{logw: &out}
	m.logf("plain log")
	if strings.Contains(out.String(), "\033[") {
		t.Fatalf("non-TTY log contains ANSI: %q", out.String())
	}
}

type cancelOnProgressWriter struct {
	out    io.Writer
	cancel context.CancelFunc
	once   sync.Once
}

func (w *cancelOnProgressWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "himorime: benchmark") {
		w.once.Do(w.cancel)
	}
	return w.out.Write(p)
}

func TestTTYProgressClosesOnCancellation(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "himorime.yaml"), suite(t, `version: "1"
name: progress-cancel
defaults: {warmup: 0, runs: 2}
benchmarks:
  - name: cancellable
    commands:
      tool:
        command: [@EXE@, sleep, 1s]
`))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := &App{
		Stdin: strings.NewReader(""), Stdout: &stdout,
		Stderr:    &cancelOnProgressWriter{out: &stderr, cancel: cancel},
		LookupEnv: os.LookupEnv, Environ: os.Environ, Getwd: os.Getwd,
		ReadFile: os.ReadFile, Now: time.Now, StderrIsTerminal: true,
	}
	chdir(t, dir)
	if code := app.Run(ctx, []string{"run"}); code != exitcode.Execution {
		t.Fatalf("cancellation code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "\033[2K\n") {
		t.Fatalf("cancellation did not clear the progress line: %q", stderr.String())
	}
}

func TestTTYProgressUsesStderrAndRespectsQuietAndJSON(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "himorime.yaml"), suite(t, twoBenchmarks))
	terminal := func(a *App) { a.StderrIsTerminal = true }

	r := runWith(t, dir, nil, terminal, "run", "--tag", "smoke", "--runs", "2")
	if r.code != 0 || !strings.Contains(r.stderr, `benchmark "fast one"`) || !strings.HasSuffix(r.stderr, "\033[2K\n") {
		t.Fatalf("TTY progress: %+v", r)
	}
	if strings.Contains(r.stdout, "warmup") {
		t.Fatalf("progress leaked to stdout: %s", r.stdout)
	}

	r = runWith(t, dir, nil, terminal, "run", "--quiet", "--tag", "smoke")
	if r.code != 0 || r.stderr != "" {
		t.Fatalf("quiet TTY progress: %+v", r)
	}
	failure := suite(t, `version: "1"
name: progress-failure
defaults: {warmup: 0, runs: 1}
benchmarks:
  - name: broken
    commands:
      tool:
        command: [@EXE@, exit, "2"]
`)
	write(t, filepath.Join(dir, "failure.himorime.yaml"), failure)
	r = runWith(t, dir, nil, terminal, "run", "failure.himorime.yaml")
	if r.code != exitcode.Execution || !strings.Contains(r.stderr, "\033[2K\n") {
		t.Fatalf("failed TTY progress: %+v", r)
	}

	r = runWith(t, dir, nil, terminal, "run", "--format", "json", "--tag", "smoke", "--runs", "1")
	var report map[string]any
	if r.code != 0 || json.Unmarshal([]byte(r.stdout), &report) != nil {
		t.Fatalf("JSON TTY progress: %+v", r)
	}
}
