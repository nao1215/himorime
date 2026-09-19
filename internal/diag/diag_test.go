package diag

import (
	"bytes"
	"fmt"
	"testing"
)

func TestCodes(t *testing.T) {
	all := All()
	if len(all) != 5 {
		t.Fatalf("got %d codes, want 5", len(all))
	}
	for i, c := range all {
		if c.String() == "" || c.Name == "" || c.Meaning == "" {
			t.Fatalf("code %d is incomplete: %#v", i, c)
		}
		if c.String() != fmt.Sprintf("HMR%d001", i+2) {
			t.Fatalf("unstable diagnostic code: %s", c)
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
