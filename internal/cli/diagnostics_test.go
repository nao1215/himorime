package cli

import (
	"testing"

	"github.com/nao1215/himorime/internal/diag"
	"github.com/nao1215/himorime/internal/runner"
)

func TestFailureCauseUsesRunnerKind(t *testing.T) {
	for _, tt := range []struct {
		kind runner.FailureKind
		want diag.Code
	}{
		{runner.FailBuild, diag.Command},
		{runner.FailExitCode, diag.Command},
		{runner.FailInterrupted, diag.Interrupted},
		{runner.FailInternal, diag.Internal},
		{runner.FailMetricCollection, diag.Metric},
		{runner.FailureKind("future_failure"), diag.Internal},
	} {
		if got := failureCause(tt.kind); got != tt.want {
			t.Errorf("failureCause(%q) = %s, want %s", tt.kind, got, tt.want)
		}
	}
}

func TestRecordFailureKeepsInterruptionCause(t *testing.T) {
	m := &measurement{}
	m.recordFailure(&runner.Failure{Kind: runner.FailInterrupted, Message: "anything"})
	m.recordFailure(&runner.Failure{Kind: runner.FailExitCode, Message: "anything else"})
	if m.failureCause != diag.Interrupted {
		t.Fatalf("failure cause = %s, want %s", m.failureCause, diag.Interrupted)
	}
}

func TestRecordFailurePrefersExecutionOverMetric(t *testing.T) {
	for _, order := range [][2]runner.FailureKind{{runner.FailMetricCollection, runner.FailExitCode}, {runner.FailExitCode, runner.FailMetricCollection}} {
		m := &measurement{}
		m.recordFailure(&runner.Failure{Kind: order[0]})
		m.recordFailure(&runner.Failure{Kind: order[1]})
		if m.failureCause != diag.Command {
			t.Errorf("failure order %q then %q = %s, want %s", order[0], order[1], m.failureCause, diag.Command)
		}
	}
	m := &measurement{}
	m.recordFailure(&runner.Failure{Kind: runner.FailMetricCollection})
	m.recordFailure(&runner.Failure{Kind: runner.FailInternal})
	if m.failureCause != diag.Internal {
		t.Fatalf("metric then internal = %s, want %s", m.failureCause, diag.Internal)
	}
}
