//go:build !linux && !darwin

package runner

import (
	"context"
	"strings"
	"testing"
)

func ttyHelper([]string) int { return 99 }

func TestMeasureOnATerminalNamesTheSystemWithoutOne(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	b := bench("terminal", 3, f.command("tty", "tty", "0"))
	b.Terminal = true
	res := f.runner.Measure(context.Background(), b, []Side{f.side})
	if res.Failure == nil || res.Failure.Kind != FailStart || !strings.Contains(res.Failure.Message, "pseudo-terminal") {
		t.Fatalf("failure = %+v, want a start failure that names the pseudo-terminal", res.Failure)
	}
}
