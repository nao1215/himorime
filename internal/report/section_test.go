package report

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// levelBody renders a stand-in report that shows the heading level it got.
func levelBody(level int) string {
	return strings.Repeat("#", level) + " suite\n\n| a |\n|---|\n| 1 |\n"
}

func TestReplaceSection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		doc     string
		want    string
		wantErr string
	}{
		{
			name: "markers present, no heading above",
			doc:  "intro\n<!-- himorime:begin bench -->\nold\ntext\n<!-- himorime:end bench -->\noutro\n",
			want: "intro\n<!-- himorime:begin bench -->\n\n## suite\n\n| a |\n|---|\n| 1 |\n\n<!-- himorime:end bench -->\noutro\n",
		},
		{
			name: "empty section",
			doc:  "<!-- himorime:begin bench -->\n<!-- himorime:end bench -->",
			want: "<!-- himorime:begin bench -->\n\n## suite\n\n| a |\n|---|\n| 1 |\n\n<!-- himorime:end bench -->",
		},
		{
			name: "CRLF file keeps CRLF",
			doc:  "# Title\r\n\r\n<!-- himorime:begin bench -->\r\nold\r\n<!-- himorime:end bench -->\r\n",
			want: "# Title\r\n\r\n<!-- himorime:begin bench -->\r\n\r\n## suite\r\n\r\n| a |\r\n|---|\r\n| 1 |\r\n\r\n<!-- himorime:end bench -->\r\n",
		},
		{
			name: "heading level follows the nearest heading above",
			doc:  "# Page\n\n## Benchmarks\n\ntext\n\n<!-- himorime:begin bench -->\n<!-- himorime:end bench -->\n\n## Later\n",
			want: "# Page\n\n## Benchmarks\n\ntext\n\n<!-- himorime:begin bench -->\n\n### suite\n\n| a |\n|---|\n| 1 |\n\n<!-- himorime:end bench -->\n\n## Later\n",
		},
		{
			name: "a heading inside a fenced code block is ignored",
			doc:  "## Benchmarks\n\n```sh\n##### not a heading\n```\n\n~~~\n# neither\n~~~\n<!-- himorime:begin bench -->\n<!-- himorime:end bench -->\n",
			want: "## Benchmarks\n\n```sh\n##### not a heading\n```\n\n~~~\n# neither\n~~~\n<!-- himorime:begin bench -->\n\n### suite\n\n| a |\n|---|\n| 1 |\n\n<!-- himorime:end bench -->\n",
		},
		{
			name: "front matter comments are not headings",
			doc:  "---\ntitle: x\n# comment\n---\n\n<!-- himorime:begin bench -->\n<!-- himorime:end bench -->\n",
			want: "---\ntitle: x\n# comment\n---\n\n<!-- himorime:begin bench -->\n\n## suite\n\n| a |\n|---|\n| 1 |\n\n<!-- himorime:end bench -->\n",
		},
		{
			name: "levels never go deeper than 6",
			doc:  "###### Deep\n<!-- himorime:begin bench -->\n<!-- himorime:end bench -->\n",
			want: "###### Deep\n<!-- himorime:begin bench -->\n\n###### suite\n\n| a |\n|---|\n| 1 |\n\n<!-- himorime:end bench -->\n",
		},
		{
			name: "markers inside a code block do not count",
			doc:  "```md\n<!-- himorime:begin bench -->\n<!-- himorime:end bench -->\n```\n<!-- himorime:begin bench -->\nold\n<!-- himorime:end bench -->\n",
			want: "```md\n<!-- himorime:begin bench -->\n<!-- himorime:end bench -->\n```\n<!-- himorime:begin bench -->\n\n## suite\n\n| a |\n|---|\n| 1 |\n\n<!-- himorime:end bench -->\n",
		},
		{
			name: "other sections are left alone",
			doc:  "<!-- himorime:begin other -->\nkeep\n<!-- himorime:end other -->\n<!-- himorime:begin bench -->\n<!-- himorime:end bench -->\n",
			want: "<!-- himorime:begin other -->\nkeep\n<!-- himorime:end other -->\n<!-- himorime:begin bench -->\n\n## suite\n\n| a |\n|---|\n| 1 |\n\n<!-- himorime:end bench -->\n",
		},
		{
			name:    "missing begin",
			doc:     "text\n<!-- himorime:end bench -->\n",
			wantErr: `no line "<!-- himorime:begin bench -->"`,
		},
		{
			name:    "missing end",
			doc:     "<!-- himorime:begin bench -->\ntext\n",
			wantErr: `no line "<!-- himorime:end bench -->" after the begin marker on line 1`,
		},
		{
			name:    "duplicated begin",
			doc:     "<!-- himorime:begin bench -->\n<!-- himorime:begin bench -->\n<!-- himorime:end bench -->\n",
			wantErr: `"<!-- himorime:begin bench -->" appears on lines 1 and 2; keep exactly one`,
		},
		{
			name:    "duplicated end",
			doc:     "<!-- himorime:begin bench -->\n<!-- himorime:end bench -->\n<!-- himorime:end bench -->\n",
			wantErr: `"<!-- himorime:end bench -->" appears on lines 2 and 3; keep exactly one`,
		},
		{
			name:    "end before begin",
			doc:     "<!-- himorime:end bench -->\n<!-- himorime:begin bench -->\n",
			wantErr: `the end marker on line 1 comes before the begin marker on line 2`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ReplaceSection([]byte(tt.doc), "bench", levelBody)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("got\n%q\nwant\n%q", got, tt.want)
			}
			again, err := ReplaceSection(got, "bench", levelBody)
			if err != nil || string(again) != string(got) {
				t.Fatalf("a second replacement changed the document: %v\n%q", err, again)
			}
		})
	}
}

func TestUpdateMarkdownSection(t *testing.T) {
	t.Parallel()
	b := runResult("csv", "", map[string][]time.Duration{"a": samples(time.Millisecond, 10, 0)}, "a")
	r := judge(ModeRun, false, b)
	r.Environment.Tools = []Tool{{Name: "jc", Version: "jc version 1.25.7"}}

	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	const before = "# Compare\n\n## Speed\n\nHand-written text.\n\n<!-- himorime:begin speed -->\nstale\n<!-- himorime:end speed -->\n\n## Notes\n"
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := UpdateMarkdownSection(root, "doc.md", "speed", r); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(first)
	head, rest, ok := strings.Cut(text, "<!-- himorime:begin speed -->\n")
	if !ok || head != "# Compare\n\n## Speed\n\nHand-written text.\n\n" {
		t.Fatalf("text before the section changed:\n%s", text)
	}
	body, tail, ok := strings.Cut(rest, "<!-- himorime:end speed -->")
	if !ok || tail != "\n\n## Notes\n" {
		t.Fatalf("text after the section changed:\n%s", text)
	}
	if !strings.HasPrefix(body, "\n### suite \\| one\n\n") || !strings.HasSuffix(body, "- jc: jc version 1.25.7\n\n") || strings.Contains(body, "stale") {
		t.Fatalf("section body:\n%s", body)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 && runtime.GOOS != "windows" {
		t.Errorf("file mode changed: %v %v", info.Mode(), err)
	}
	if err := UpdateMarkdownSection(root, "doc.md", "speed", r); err != nil {
		t.Fatal(err)
	}
	if second, _ := os.ReadFile(path); string(second) != text {
		t.Fatalf("a second update with the same report changed the file:\n%s", second)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temporary files were left behind: %v", entries)
	}

	err = UpdateMarkdownSection(root, "missing.md", "speed", r)
	if !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), `section "speed": the file does not exist`) {
		t.Fatalf("missing file: %v", err)
	}
	broken := filepath.Join(dir, "broken.md")
	if err := os.WriteFile(broken, []byte("<!-- himorime:begin speed -->\nkeep me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = UpdateMarkdownSection(root, "broken.md", "speed", r)
	if err == nil || !strings.Contains(err.Error(), `section "speed": no line "<!-- himorime:end speed -->"`) {
		t.Fatalf("missing end marker: %v", err)
	}
	if data, _ := os.ReadFile(broken); string(data) != "<!-- himorime:begin speed -->\nkeep me\n" {
		t.Fatalf("a failed update changed the file: %q", data)
	}
}

func TestUpdateMarkdownSectionLeavesTheFileOnWriteError(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not stop file creation on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	r := judge(ModeRun, false, runResult("x", "", map[string][]time.Duration{"a": samples(time.Millisecond, 10, 0)}, "a"))
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	const doc = "<!-- himorime:begin bench -->\nold\n<!-- himorime:end bench -->\n"
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := UpdateMarkdownSection(root, "doc.md", "bench", r); err == nil || !strings.Contains(err.Error(), `section "bench"`) {
		t.Fatalf("err = %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != doc {
		t.Fatalf("the document was modified: %q", data)
	}
}

func TestValidSectionName(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{"bench": true, "a-1": true, "0x": true, "": false, "-a": false, "Bench": false, "a_b": false, "a b": false} {
		if got := ValidSectionName(name); got != want {
			t.Errorf("ValidSectionName(%q) = %v, want %v", name, got, want)
		}
	}
}
