package rmtree

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRemoveAllRemovesAReadOnlyTree(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "tree")
	cache := filepath.Join(dir, "pkg@v1")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "go.mod"), []byte("module pkg\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(cache, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the tree remains: %v", err)
	}
	if err := RemoveAll(dir); err != nil {
		t.Fatalf("removing a missing tree: %v", err)
	}
}
