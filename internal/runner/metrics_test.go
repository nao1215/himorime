package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nao1215/yahiko/internal/config"
	"github.com/nao1215/yahiko/internal/metric"
	"github.com/nao1215/yahiko/internal/proc"
)

// fakeUsage is a collector stand-in: it runs the real process and then
// replaces the usage the platform reported, so every platform outcome can be
// tested on any machine.
func fakeUsage(u func(n int) proc.Usage) func(context.Context, proc.Spec, proc.Clock) (proc.Result, error) {
	n := 0
	return func(ctx context.Context, s proc.Spec, now proc.Clock) (proc.Result, error) {
		res, err := proc.Run(ctx, s, now)
		if s.CollectUsage {
			n++
			res.Usage = u(n)
		}
		return res, err
	}
}

func TestMeasureCollectsUsageAndWork(t *testing.T) {
	t.Parallel()
	if cpuErr, memErr := proc.Capabilities(); cpuErr != nil || memErr != nil {
		t.Skip("resource usage is not supported here")
	}
	f := newFixture(t)
	b := bench("usage", 3, f.command("a", "ok"))
	b.Metrics = config.Metrics{CPU: true, Memory: true, Throughput: &config.Work{Value: 500, Unit: "records"}, Unsupported: config.UnsupportedFail}
	res := f.runner.Measure(context.Background(), f.suite, b, []Side{f.side})
	if res.Failure != nil {
		t.Fatal(res.Failure)
	}
	m := res.Commands[0].Sides[SideHead]
	if m.Failure != nil {
		t.Fatal(m.Failure)
	}
	if len(m.Samples) != 3 || len(m.CPUUser) != 3 || len(m.CPUSystem) != 3 || len(m.PeakRSS) != 3 || len(m.Work) != 3 {
		t.Fatalf("sample counts: %+v", m)
	}
	for i := range m.Samples {
		if m.PeakRSS[i] < 1<<20 || m.Work[i] != 500 {
			t.Fatalf("run %d: peak rss %d, work %v", i, m.PeakRSS[i], m.Work[i])
		}
	}
}

func TestMeasureWithoutMetricsCollectsNoUsage(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	requested := false
	f.runner.Exec = func(ctx context.Context, s proc.Spec, now proc.Clock) (proc.Result, error) {
		requested = requested || s.CollectUsage
		return proc.Run(ctx, s, now)
	}
	res := f.runner.Measure(context.Background(), f.suite, bench("latency only", 2, f.command("a", "ok")), []Side{f.side})
	m := res.Commands[0].Sides[SideHead]
	if requested || m.CPUUser != nil || m.PeakRSS != nil || m.Work != nil {
		t.Fatalf("a latency-only benchmark collected usage: requested=%v %+v", requested, m)
	}
}

func TestMeasureUnsupportedAtRunTime(t *testing.T) {
	t.Parallel()
	unsupported := func(int) proc.Usage {
		return proc.Usage{UserCPU: time.Millisecond, SystemCPU: time.Millisecond, MemoryErr: &proc.UnsupportedError{What: "peak rss", Reason: "the command ran 2 processes"}}
	}

	f := newFixture(t)
	f.runner.Exec = fakeUsage(unsupported)
	b := bench("fail policy", 2, f.command("a", "ok"))
	b.Metrics = config.Metrics{CPU: true, Memory: true, Unsupported: config.UnsupportedFail}
	res := f.runner.Measure(context.Background(), f.suite, b, []Side{f.side})
	m := res.Commands[0].Sides[SideHead]
	if m.Failure == nil || m.Failure.Kind != FailMetricUnsupported || !strings.Contains(m.Failure.Message, "the command ran 2 processes") {
		t.Fatalf("failure = %+v", m.Failure)
	}
	if !m.Failure.Kind.IsMetricFailure() || FailExitCode.IsMetricFailure() {
		t.Fatal("IsMetricFailure")
	}

	f2 := newFixture(t)
	f2.runner.Exec = fakeUsage(unsupported)
	b2 := bench("skip policy", 3, f2.command("a", "ok"))
	b2.Metrics = config.Metrics{CPU: true, Memory: true, Unsupported: config.UnsupportedSkip}
	res2 := f2.runner.Measure(context.Background(), f2.suite, b2, []Side{f2.side})
	m2 := res2.Commands[0].Sides[SideHead]
	if m2.Failure != nil {
		t.Fatalf("skip policy failed: %+v", m2.Failure)
	}
	reason, skipped := m2.Skipped(metric.GroupMemory)
	if !skipped || !strings.Contains(reason, "2 processes") || m2.PeakRSS != nil {
		t.Fatalf("memory was not skipped: %q %v %v", reason, skipped, m2.PeakRSS)
	}
	if _, cpuSkipped := m2.Skipped(metric.GroupCPU); cpuSkipped || len(m2.CPUUser) != 3 || len(m2.Samples) != 3 {
		t.Fatalf("cpu must still be measured: %+v", m2)
	}
}

// TestMeasureCollectionFailureIsNeverSkipped: a platform that supports a
// metric but fails to report it must not look like an unsupported platform,
// whatever the policy.
func TestMeasureCollectionFailureIsNeverSkipped(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.runner.Exec = fakeUsage(func(n int) proc.Usage {
		if n == 2 {
			return proc.Usage{CPUErr: errors.New("rusage missing"), MemoryErr: errors.New("rusage missing")}
		}
		return proc.Usage{UserCPU: time.Millisecond, PeakRSS: 1 << 20}
	})
	b := bench("collection", 3, f.command("a", "ok"))
	b.Metrics = config.Metrics{CPU: true, Memory: true, Unsupported: config.UnsupportedSkip}
	res := f.runner.Measure(context.Background(), f.suite, b, []Side{f.side})
	m := res.Commands[0].Sides[SideHead]
	if m.Failure == nil || m.Failure.Kind != FailMetricCollection || !strings.Contains(m.Failure.Message, "rusage missing") {
		t.Fatalf("failure = %+v", m.Failure)
	}
}

func TestMeasureUnsupportedPlatform(t *testing.T) {
	t.Parallel()
	noCPU := func() (error, error) {
		return &proc.UnsupportedError{What: "cpu time", Reason: "not on this OS"}, nil
	}
	f := newFixture(t)
	f.runner.Capabilities = noCPU
	log := filepath.Join(f.dir, "log.txt")
	b := bench("fail", 2, f.command("a", "append", log, "measured"))
	b.Setup = []config.Exec{f.helper("append", log, "setup")}
	b.Metrics = config.Metrics{CPU: true, Unsupported: config.UnsupportedFail}
	res := f.runner.Measure(context.Background(), f.suite, b, []Side{f.side})
	if res.Failure == nil || res.Failure.Kind != FailMetricUnsupported || !strings.Contains(res.Failure.Message, "not on this OS") {
		t.Fatalf("failure = %+v", res.Failure)
	}
	if _, err := os.Stat(log); err == nil {
		t.Fatal("setup or the command ran although the metric was unsupported")
	}
	problems := f.runner.UnsupportedMetrics([]config.Benchmark{b, bench("plain", 1)})
	if len(problems) != 1 || problems[0].Group != metric.GroupCPU || problems[0].Skip || problems[0].Benchmark != "fail" {
		t.Fatalf("problems = %+v", problems)
	}

	f2 := newFixture(t)
	f2.runner.Capabilities = noCPU
	b2 := bench("skip", 2, f2.command("a", "ok"))
	b2.Metrics = config.Metrics{CPU: true, Unsupported: config.UnsupportedSkip}
	res2 := f2.runner.Measure(context.Background(), f2.suite, b2, []Side{f2.side})
	m := res2.Commands[0].Sides[SideHead]
	if res2.Failure != nil || m.Failure != nil || len(m.Samples) != 2 || m.CPUUser != nil {
		t.Fatalf("skip: %+v %+v", res2.Failure, m)
	}
	if _, skipped := m.Skipped(metric.GroupCPU); !skipped {
		t.Fatal("cpu not marked skipped")
	}
}

func TestMeasureWorkFromFileSize(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	dir := filepath.Join(f.dir, "input dir")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data file.json"), []byte(strings.Repeat("x", 1234)), 0o600); err != nil {
		t.Fatal(err)
	}
	b := bench("file size", 2, f.command("a", "ok"))
	b.Metrics = config.Metrics{Throughput: &config.Work{FileSize: "input dir/data file.json", Unit: "bytes"}}
	res := f.runner.Measure(context.Background(), f.suite, b, []Side{f.side})
	m := res.Commands[0].Sides[SideHead]
	if m.Failure != nil || len(m.Work) != 2 || m.Work[0] != 1234 {
		t.Fatalf("work = %v, failure %+v", m.Work, m.Failure)
	}

	// A file produced in setup is read after setup.
	f2 := newFixture(t)
	b2 := bench("generated", 1, f2.command("a", "ok"))
	b2.Setup = []config.Exec{f2.helper("append", "${workdir}/gen.txt", "hello")}
	b2.Metrics = config.Metrics{Throughput: &config.Work{FileSize: "${workdir}/gen.txt", Unit: "bytes"}}
	res2 := f2.runner.Measure(context.Background(), f2.suite, b2, []Side{f2.side})
	if m := res2.Commands[0].Sides[SideHead]; m.Failure != nil || len(m.Work) != 1 || m.Work[0] != 6 {
		t.Fatalf("generated work = %+v", m)
	}

	for name, file := range map[string]string{"missing": "nope.json", "empty": "empty.json", "directory": "input dir"} {
		f3 := newFixture(t)
		_ = os.WriteFile(filepath.Join(f3.dir, "empty.json"), nil, 0o600)
		_ = os.MkdirAll(filepath.Join(f3.dir, "input dir"), 0o700)
		b3 := bench(name, 1, f3.command("a", "ok"))
		b3.Metrics = config.Metrics{Throughput: &config.Work{FileSize: file, Unit: "bytes"}}
		res3 := f3.runner.Measure(context.Background(), f3.suite, b3, []Side{f3.side})
		if m := res3.Commands[0].Sides[SideHead]; m.Failure == nil || m.Failure.Kind != FailMetricCollection {
			t.Errorf("%s work file: failure = %+v", name, m.Failure)
		}
	}
}
