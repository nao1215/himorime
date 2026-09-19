package diag

import (
	"bytes"
	"fmt"
	"testing"
)

func TestCodes(t *testing.T) {
	all := All()
	if len(all) != 8 {
		t.Fatalf("got %d codes, want 8", len(all))
	}
	for _, c := range all {
		if c.String() == "" || c.Name == "" || c.Meaning == "" {
			t.Fatalf("incomplete code: %#v", c)
		}
	}
	for _, want := range []string{"HMR2001", "HMR3001", "HMR4001", "HMR4002", "HMR4003", "HMR4004", "HMR5001", "HMR6001"} {
		found := false
		for _, c := range all {
			if c.String() == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing diagnostic code %s", want)
		}
	}
	for _, exit := range []int{0, 1, 2, 3, 4, 5, 6, 7} {
		var out bytes.Buffer
		Print(&out, exit, "detail: %s (%d%%)", "example", 50)
		want := "detail: example (50%)\n"
		if exit >= 2 && exit <= 6 {
			want = fmt.Sprintf("HMR%d001: %s", exit, want)
		}
		if out.String() != want {
			t.Fatalf("exit %d: got %q, want %q", exit, out.String(), want)
		}
	}
}
