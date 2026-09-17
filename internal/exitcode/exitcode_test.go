package exitcode

import "testing"

func TestAllIsCompleteAndOrdered(t *testing.T) {
	t.Parallel()
	all := All()
	want := []int{OK, Failed, Config, Usage, Execution, Internal, Metric}
	if len(all) != len(want) {
		t.Fatalf("All() has %d entries, want %d", len(all), len(want))
	}
	for i, c := range all {
		if c.Code != want[i] || c.Code != i || c.Name == "" || c.Meaning == "" {
			t.Errorf("entry %d = %+v", i, c)
		}
	}
}

func TestOutcome(t *testing.T) {
	t.Parallel()
	for _, code := range []int{Failed, Execution, Metric} {
		if Outcome(code) == "" {
			t.Errorf("Outcome(%d) is empty", code)
		}
	}
	if Outcome(OK) != "" || Outcome(Config) != "" {
		t.Error("statuses reported before measuring need no outcome line")
	}
}
