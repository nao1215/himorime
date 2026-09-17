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
	for _, impl := range []string{"scanner", "readall", "parallel"} {
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

// TestParallelMatchesScanner counts inputs whose words straddle chunk
// boundaries with every worker count.
func TestParallelMatchesScanner(t *testing.T) {
	t.Parallel()
	inputs := []string{"", "a", " a ", "ab cd ef\ngh", strings.Repeat("word ", 101) + "\n  tail", strings.Repeat("x", 50) + " " + strings.Repeat("y", 49)}
	for _, in := range inputs {
		want, err := countScanner(strings.NewReader(in))
		if err != nil {
			t.Fatal(err)
		}
		for workers := 1; workers <= 9; workers++ {
			if got := countParallel([]byte(in), workers); got != want {
				t.Errorf("countParallel(%q, %d) = %+v, want %+v", in, workers, got, want)
			}
		}
	}
}

func TestGenerateFormats(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for format, first := range map[string]string{"text": "entry 0 ", "csv": "id,name,note,score\n0,fox 0,", "jsonl": `{"id": 0, "name": "fox 0"`} {
		path := filepath.Join(dir, format)
		if err := run([]string{"-gen", "3", "-gen-format", format, "-o", path}, nil, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(path)
		if !strings.HasPrefix(string(data), first) {
			t.Errorf("%s fixture starts %q", format, data)
		}
	}
	if err := run([]string{"-gen", "3", "-gen-format", "xml", "-o", filepath.Join(dir, "x")}, nil, &bytes.Buffer{}); err == nil {
		t.Error("an unknown fixture format was accepted")
	}
}
