package runner

import (
	"context"
	"testing"

	"github.com/nao1215/himorime/internal/proc"
)

func TestProgressReportsWarmupsAndFixedRounds(t *testing.T) {
	f := newFixture(t)
	b := bench("fixed progress", 2, f.command("ok", "ok"))
	b.Warmup = 1
	var got []Progress
	inExec := false
	f.runner.Exec = func(ctx context.Context, s proc.Spec, now proc.Clock) (proc.Result, error) {
		inExec = true
		defer func() { inExec = false }()
		return proc.Run(ctx, s, now)
	}
	f.runner.Progress = func(p Progress) {
		if inExec {
			t.Errorf("progress callback ran inside command execution: %+v", p)
		}
		got = append(got, p)
	}
	res := f.runner.Measure(context.Background(), b, []Side{f.side})
	if res.Failure != nil {
		t.Fatalf("failure: %+v", res.Failure)
	}
	if len(got) != 4 {
		t.Fatalf("progress callbacks = %d, want initial, warmup and two rounds", len(got))
	}
	if got[0].Warmups != 0 || got[0].Rounds != 0 || got[0].Runs != 2 || got[0].WarmupTotal != 1 {
		t.Fatalf("initial progress = %+v", got[0])
	}
	last := got[len(got)-1]
	if last.Warmups != 1 || last.Rounds != 2 || last.Runs != 2 {
		t.Fatalf("final progress = %+v", last)
	}
}

func TestProgressAdaptiveOmitsUnknownTotalAndSurvivesFailure(t *testing.T) {
	f := newFixture(t)
	b := bench("adaptive progress", 0, f.command("fail", "exit", "7"))
	b.MinRuns, b.MaxRuns = 1, 2
	var got []Progress
	f.runner.Progress = func(p Progress) { got = append(got, p) }
	res := f.runner.Measure(context.Background(), b, []Side{f.side})
	if res.Failure != nil {
		t.Fatalf("unexpected benchmark failure: %+v", res.Failure)
	}
	if len(got) < 2 || got[0].Runs != 0 || got[len(got)-1].Runs != 0 {
		t.Fatalf("adaptive progress = %+v", got)
	}
	if got[0].Benchmark != "adaptive progress" {
		t.Fatalf("benchmark = %q", got[0].Benchmark)
	}
}

func TestProgressAdaptiveReportsCompletedRounds(t *testing.T) {
	f := newFixture(t)
	b := bench("adaptive progress", 0, f.command("ok", "ok"))
	b.MinRuns, b.MaxRuns = 1, 2
	var got []Progress
	f.runner.Progress = func(p Progress) { got = append(got, p) }
	res := f.runner.Measure(context.Background(), b, []Side{f.side})
	if res.Failure != nil {
		t.Fatalf("failure: %+v", res.Failure)
	}
	if len(got) < 2 || got[len(got)-1].Rounds != 1 || got[len(got)-1].Runs != 0 {
		t.Fatalf("adaptive progress = %+v", got)
	}
}
