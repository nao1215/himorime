package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImplementationsAgree(t *testing.T) {
	t.Parallel()
	input := "one two\nthree  four five\n\nsix"
	for _, impl := range []string{"scanner", "readall"} {
		var out bytes.Buffer
		if err := run([]string{"-impl", impl}, strings.NewReader(input), &out); err != nil {
			t.Fatal(err)
		}
		if got := out.String(); got != "3 6 29\n" {
			t.Errorf("%s: %q", impl, got)
		}
	}
	if err := run([]string{"-impl", "nope"}, strings.NewReader(""), &bytes.Buffer{}); err == nil {
		t.Error("an unknown implementation was accepted")
	}
}

func TestCacheAndGenerate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fixture := filepath.Join(dir, "in.txt")
	if err := run([]string{"-gen", "10", "-o", fixture}, nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		var out bytes.Buffer
		if err := run([]string{"-cache", filepath.Join(dir, "cache"), fixture}, nil, &out); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(out.String(), "10 ") {
			t.Fatalf("output = %q", out.String())
		}
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "cache"))
	if len(entries) != 1 {
		t.Fatalf("cache entries = %d", len(entries))
	}
	if err := run([]string{"-cache", filepath.Join(dir, "cache"), "-purge"}, nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cache")); !os.IsNotExist(err) {
		t.Fatal("-purge left the cache")
	}
	if err := run([]string{"-purge"}, nil, &bytes.Buffer{}); err == nil {
		t.Error("-purge without -cache was accepted")
	}
	if err := run([]string{"-gen", "1"}, nil, &bytes.Buffer{}); err == nil {
		t.Error("-gen without -o was accepted")
	}
	var v bytes.Buffer
	if err := run([]string{"-version"}, nil, &v); err != nil || !strings.HasPrefix(v.String(), "wordcount") {
		t.Fatal("-version")
	}
}
