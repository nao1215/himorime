package exitcode

import "testing"

func TestAllIsCompleteAndOrdered(t *testing.T) {
	t.Parallel()
	all := All()
	want := []int{OK, Failed, Config, Usage, Execution, Internal}
	if len(all) != len(want) {
		t.Fatalf("All() has %d entries, want %d", len(all), len(want))
	}
	for i, c := range all {
		if c.Code != want[i] || c.Code != i || c.Name == "" || c.Meaning == "" {
			t.Errorf("entry %d = %+v", i, c)
		}
	}
}
