package report

import (
	"math"
	"testing"
	"time"

	"github.com/nao1215/himorime/internal/config"
	"github.com/nao1215/himorime/internal/metric"
	"github.com/nao1215/himorime/internal/runner"
)

func TestThroughputUsesLatencyInference(t *testing.T) {
	t.Parallel()
	for _, statistic := range []config.Metric{config.MetricMedian, config.MetricMean} {
		for _, gate := range []bool{false, true} {
			b := throughputCompareResult("derived", 1000, 1000, samples(100*time.Millisecond, 20, 0.1), samples(120*time.Millisecond, 20, 0.1))
			b.Benchmark.Regression.Latency.Metric = statistic
			b.Benchmark.Regression.Latency.Gate = gate
			r := judge(ModeCompare, true, b)
			c := r.Suites[0].Benchmarks[0].Commands[0]
			lat, tp := c.Comparisons["latency"], c.Comparisons["throughput"]
			if tp.Verdict != lat.Verdict || tp.Reason != lat.Reason || tp.ProbRegression != lat.ProbRegression || tp.ProbImprovement != lat.ProbImprovement || tp.Gate || tp.DerivedFrom != "latency" {
				t.Fatalf("latency %+v, throughput %+v", lat, tp)
			}
			if math.Abs(*tp.Base**lat.Base-1000e9) > 1e-3 || math.Abs(*tp.Head**lat.Head-1000e9) > 1e-3 {
				t.Fatal("rate is not work / selected latency statistic")
			}
			if tp.CILowPercent != reciprocalChange(lat.CIHighPercent) || tp.CIHighPercent != reciprocalChange(lat.CILowPercent) {
				t.Fatal("interval endpoints must reverse under the reciprocal")
			}
			wantNotGated := 0
			if !gate {
				wantNotGated = 1
			}
			if r.Summary.NotGated.Total() != wantNotGated {
				t.Fatalf("duplicate inference counted: %+v", r.Summary)
			}
		}
	}
}

func TestVaryingWorkIsNotAReciprocalLatencyComparison(t *testing.T) {
	t.Parallel()
	b := throughputCompareResult("varying", 1000, 1000, samples(100*time.Millisecond, 20, 0), samples(100*time.Millisecond, 20, 0))
	// Equal extrema are not proof of equal work in every execution.
	for _, side := range b.Commands[0].Sides {
		side.Work[0] = 500
	}
	r := judge(ModeCompare, true, b)
	tp := r.Suites[0].Benchmarks[0].Commands[0].Comparisons["throughput"]
	if tp.Verdict != VerdictSkipped || tp.Base != nil || tp.ProbRegression != 0 || r.Summary.Inconclusive != 0 || r.Summary.Skipped != 1 || r.Summary.ExitCode != 0 {
		t.Fatalf("comparison %+v, summary %+v", tp, r.Summary)
	}
}

func TestDerivedThroughputCannotTurnZeroLatencyIntoAZeroRate(t *testing.T) {
	t.Parallel()
	for _, durations := range [][2]time.Duration{{0, 0}, {0, time.Second}, {time.Second, 0}} {
		b := throughputCompareResult("zero", 1000, 1000, samples(durations[0], 20, 0), samples(durations[1], 20, 0))
		r := judge(ModeCompare, false, b)
		tp := r.Suites[0].Benchmarks[0].Commands[0].Comparisons["throughput"]
		if tp.Verdict != VerdictSkipped || tp.Base != nil || tp.Head != nil || tp.ProbRegression != 0 {
			t.Fatalf("zero latency must not fabricate a finite rate: %+v", tp)
		}
	}
}

func TestThroughputBudgetStillGatesWithLatencyDisabled(t *testing.T) {
	t.Parallel()
	b := throughputCompareResult("budget", 1000, 1000, samples(time.Second, 20, 0), samples(2*time.Second, 20, 0))
	b.Benchmark.Regression.Latency.Gate = false
	b.Benchmark.Budgets = []config.Budget{budget(t, "tool", metric.Throughput, "median", ">= 750B/s")}
	r := judge(ModeCompare, false, b)
	if r.Summary.OverBudget != 1 || r.Summary.ExitCode != 1 {
		t.Fatalf("rate budget did not fail: %+v", r.Summary)
	}
}

func TestBothFloorsAreSkippedBeforeInference(t *testing.T) {
	t.Parallel()
	for _, gate := range []bool{false, true} {
		b := pairSides("floor", floorResult("x", 3<<20, repeat(5<<20, 20)...), floorResult("x", 4<<20, repeat(8<<20, 20)...))
		b.Benchmark.Regression.Memory.Gate = gate
		r := judge(ModeCompare, true, b)
		mc := r.Suites[0].Benchmarks[0].Commands[0].Comparisons["peak_rss"]
		if mc.Verdict != VerdictSkipped || mc.Base != nil || mc.Head != nil || mc.Difference != nil || mc.ProbRegression != 0 || r.Summary.Inconclusive != 0 || r.Summary.Skipped != 1 || r.Summary.ExitCode != 0 {
			t.Fatalf("comparison %+v, summary %+v", mc, r.Summary)
		}
		// A failed absolute memory budget must still control the exit status.
		b.Commands[0].Sides[runner.SideHead].PeakRSS = repeat(64<<20, 20)
		b.Benchmark.Budgets = []config.Budget{budget(t, "tool", metric.PeakRSS, "max", "<= 32MiB")}
		if r := judge(ModeCompare, true, b); r.Summary.ExitCode != 1 {
			t.Fatalf("budget ignored: %+v", r.Summary)
		}
	}
}
