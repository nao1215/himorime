package diag

import "testing"

func TestCodes(t *testing.T) {
	all := All()
	if len(all) != 5 {
		t.Fatalf("got %d codes, want 5", len(all))
	}
	for i, c := range all {
		if c.String() == "" || c.Name == "" || c.Meaning == "" {
			t.Fatalf("code %d is incomplete: %#v", i, c)
		}
		if got, ok := Lookup(c.String()); !ok || got.Number != c.Number {
			t.Fatalf("Lookup(%q) failed", c.String())
		}
	}
	if _, ok := ForExit(1); ok {
		t.Fatal("exit 1 must not have an error code")
	}
	for _, exit := range []int{2, 3, 4, 5, 6} {
		if _, ok := ForExit(exit); !ok {
			t.Fatalf("exit %d has no code", exit)
		}
	}
}
