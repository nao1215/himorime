package buildinfo

import "testing"

func TestGetPrefersInjectedVersion(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })

	Version = "v9.9.9"
	if got := Get(); got != "v9.9.9" {
		t.Fatalf("Get() = %q, want v9.9.9", got)
	}
}

func TestGetNeverEmpty(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })

	Version = ""
	if Get() == "" {
		t.Fatal("Get() returned an empty version")
	}
	_ = Commit()
}
