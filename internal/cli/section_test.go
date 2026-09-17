package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nao1215/himorime/internal/exitcode"
)

const oneBenchmark = `version: "1"
suite: {name: docs}
defaults: {warmup: 0, runs: 3}
benchmarks:
  - name: quick
    commands:
      helper:
        command: [@EXE@, sleep, 1ms]
`

const sectionDoc = "# Tool\n\n## Benchmarks\n\nWritten by hand.\n\n<!-- himorime:begin bench -->\nold table\n<!-- himorime:end bench -->\n\n## License\n\nMIT\n"

func TestRunUpdatesAMarkdownSection(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "himorime.yaml"), suite(t, oneBenchmark))
	write(t, filepath.Join(dir, "doc.md"), sectionDoc)
	r := run(t, dir, nil, "run", "--quiet", "--seed", "5", "--format", "markdown", "--output", "doc.md", "--section", "bench")
	if r.code != 0 || r.stdout != "" || r.stderr != "" {
		t.Fatalf("run --section: %+v", r)
	}
	data, err := os.ReadFile(filepath.Join(dir, "doc.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	before, rest, ok := strings.Cut(doc, "<!-- himorime:begin bench -->\n")
	body, after, ok2 := strings.Cut(rest, "<!-- himorime:end bench -->")
	if !ok || !ok2 || before != "# Tool\n\n## Benchmarks\n\nWritten by hand.\n\n" || after != "\n\n## License\n\nMIT\n" {
		t.Fatalf("text outside the section changed:\n%s", doc)
	}
	if !strings.HasPrefix(body, "\n### docs\n\n| Benchmark | Command | Median |") || !strings.HasSuffix(body, ", seed 5.\n\n") || strings.Contains(body, "old table") {
		t.Fatalf("section:\n%s", body)
	}

	// A document without the markers is an error that leaves it untouched.
	write(t, filepath.Join(dir, "plain.md"), "# Plain\n")
	r = run(t, dir, nil, "run", "--quiet", "--format", "markdown", "--output", "plain.md", "--section", "bench")
	if r.code != exitcode.Execution || !strings.Contains(r.stderr, `plain.md: section "bench": no line "<!-- himorime:begin bench -->"`) {
		t.Fatalf("missing markers: %+v", r)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "plain.md")); string(data) != "# Plain\n" {
		t.Fatalf("a failed update changed the file: %q", data)
	}
	r = run(t, dir, nil, "run", "--quiet", "--format", "markdown", "--output", "missing.md", "--section", "bench")
	if r.code != exitcode.Execution || !strings.Contains(r.stderr, `missing.md: section "bench": the file does not exist`) {
		t.Fatalf("missing file: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, "missing.md")); err == nil {
		t.Fatal("a missing document was created")
	}
}

func TestSuiteOutputUpdatesAMarkdownSection(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "himorime.yaml"), suite(t, oneBenchmark)+"report:\n  outputs:\n    - {format: markdown, path: docs/doc.md, section: bench}\n")
	write(t, filepath.Join(dir, "docs", "doc.md"), sectionDoc)
	r := run(t, dir, nil, "run", "--quiet", "--format", "json", "--output", "out.json")
	if r.code != 0 {
		t.Fatalf("run: %+v", r)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "docs", "doc.md"))
	if !strings.Contains(string(data), "<!-- himorime:begin bench -->\n\n### docs\n") || !strings.HasSuffix(string(data), "<!-- himorime:end bench -->\n\n## License\n\nMIT\n") {
		t.Fatalf("doc:\n%s", data)
	}
}

func TestSectionFlagUsageErrors(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "himorime.yaml"), suite(t, oneBenchmark))
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"run", "--format", "markdown", "--section", "bench"}, "--section needs --output FILE"},
		{[]string{"run", "--format", "json", "--output", "doc.md", "--section", "bench"}, `--section needs --format markdown, not "json"`},
		{[]string{"run", "--output", "doc.md", "--section", "bench"}, `--section needs --format markdown, not "table"`},
		{[]string{"compare", "--against", "HEAD", "--format", "markdown", "--output", "doc.md", "--section", "Bench"}, `invalid --section "Bench"`},
		{[]string{"ci", "--format", "csv", "--output", "doc.md", "--section", "bench"}, "--section needs --format markdown"},
	} {
		r := run(t, dir, nil, tt.args...)
		if r.code != exitcode.Usage || !strings.Contains(r.stderr, tt.want) {
			t.Errorf("%v: %+v, want exit %d and %q", tt.args, r, exitcode.Usage, tt.want)
		}
	}
}

func TestRunRecordsToolVersions(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "himorime.yaml"), suite(t, oneBenchmark+`report:
  versions:
    stdout-tool: [@EXE@, print, "\n   stdout-tool version 1.25.7  \nsecond line", "ignored"]
    stderr-tool: [@EXE@, exit, "0", "stderr-tool 2.0"]
    locale: [@EXE@, getenv, LC_ALL]
`))
	r := run(t, dir, nil, "run", "--quiet", "--format", "json")
	if r.code != 0 {
		t.Fatalf("run: %+v", r)
	}
	var rep struct {
		Environment struct {
			Tools []map[string]string `json:"tools"`
		} `json:"environment"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil {
		t.Fatal(err)
	}
	tools := rep.Environment.Tools
	if len(tools) != 3 || tools[0]["name"] != "stdout-tool" || tools[0]["version"] != "stdout-tool version 1.25.7" || tools[1]["name"] != "stderr-tool" || tools[1]["version"] != "stderr-tool 2.0" || tools[2]["version"] != "C" {
		t.Fatalf("tools = %v", tools)
	}

	r = run(t, dir, nil, "run", "--quiet", "--format", "markdown")
	if !strings.HasSuffix(r.stdout, ".\n\n- stdout-tool: stdout-tool version 1.25.7\n- stderr-tool: stderr-tool 2.0\n- locale: C\n") {
		t.Fatalf("markdown footer:\n%s", r.stdout)
	}
	r = run(t, dir, nil, "run", "--quiet")
	if strings.Contains(r.stdout, "1.25.7") {
		t.Fatalf("the table prints no versions:\n%s", r.stdout)
	}

	write(t, filepath.Join(dir, "himorime.yaml"), suite(t, oneBenchmark))
	r = run(t, dir, nil, "run", "--quiet", "--format", "json")
	if r.code != 0 || !strings.Contains(r.stdout, `"tools": []`) {
		t.Fatalf("no versions: %+v", r)
	}
}

func TestFailingVersionCommandExits4(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "himorime.yaml"), suite(t, oneBenchmark+`report:
  versions:
    broken: [@EXE@, exit, "3", "no such flag"]
`))
	r := run(t, dir, nil, "run", "--quiet", "--format", "json", "--output", "out.json")
	if r.code != exitcode.Execution || !strings.Contains(r.stderr, "exit 4") {
		t.Fatalf("failing version command: %+v", r)
	}
	data, err := os.ReadFile(filepath.Join(dir, "out.json"))
	if err != nil || !strings.Contains(string(data), `"kind": "setup_failed"`) || !strings.Contains(string(data), "report.versions.broken exited with status 3") || !strings.Contains(string(data), "no such flag") {
		t.Fatalf("report: %s %v", data, err)
	}

	write(t, filepath.Join(dir, "himorime.yaml"), suite(t, oneBenchmark+`report:
  versions:
    silent: [@EXE@, sleep, 1ms]
`))
	r = run(t, dir, nil, "run", "--quiet")
	if r.code != exitcode.Execution || !strings.Contains(r.stdout, "error: setup failed: report.versions.silent printed nothing") {
		t.Fatalf("silent version command: %+v", r)
	}
}
