package docgen

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/nao1215/himorime/internal/cli"
	"github.com/nao1215/himorime/internal/config"
	"github.com/nao1215/himorime/schema"
)

const root = "../.."

// docFiles are the Markdown files that may hold generated sections, example
// blocks, and himorime command lines.
func docFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(root, "website", "content", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	return append(files, filepath.Join(root, "README.md"))
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestGeneratedDocsInSync fails when generated documentation drifted from the
// code. Run `make docs` (UPDATE_DOCS=1) to rewrite it.
func TestGeneratedDocsInSync(t *testing.T) {
	t.Parallel()
	update := os.Getenv("UPDATE_DOCS") == "1"
	for _, path := range docFiles(t) {
		want, err := Sync(root, path)
		if err != nil {
			t.Fatal(err)
		}
		if got := read(t, path); got != want {
			if update {
				if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			t.Errorf("%s is out of date; run `make docs`", path)
		}
	}
}

// commandLine matches a documented himorime invocation in a console or shell
// code block: "$ himorime ..." or "himorime ..." at the start of a line.
var commandLine = regexp.MustCompile(`^(?:\$ )?himorime(?: (.*))?$`)

// TestDocumentedCommandsExist checks every himorime command line shown in the
// documentation against the real command table: the subcommand exists and
// every flag is one that subcommand accepts.
func TestDocumentedCommandsExist(t *testing.T) {
	t.Parallel()
	flagsOf := map[string]map[string]bool{}
	for _, c := range cli.Commands() {
		fs := flag.NewFlagSet(c.Name, flag.ContinueOnError)
		c.Flags(fs)
		set := map[string]bool{"help": true}
		fs.VisitAll(func(f *flag.Flag) { set[f.Name] = true })
		flagsOf[c.Name] = set
	}
	files := docFiles(t)
	examples, _ := filepath.Glob(filepath.Join(root, "examples", "*", "*.y*ml"))
	files = append(files, examples...)
	checked := 0
	for _, path := range files {
		for _, part := range commandLines(read(t, path), strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")) {
			m := commandLine.FindStringSubmatch(part)
			if m == nil {
				continue
			}
			fields := strings.Fields(m[1])
			if len(fields) == 0 || strings.HasPrefix(fields[0], "-") {
				continue
			}
			flags, ok := flagsOf[fields[0]]
			if !ok {
				t.Errorf("%s: %q uses unknown command %q", path, part, fields[0])
				continue
			}
			checked++
			for _, f := range fields[1:] {
				if !strings.HasPrefix(f, "--") || f == "--" {
					continue
				}
				name := strings.SplitN(strings.TrimPrefix(f, "--"), "=", 2)[0]
				if !flags[name] {
					t.Errorf("%s: %q uses flag --%s, which himorime %s does not have", path, part, name, fields[0])
				}
			}
		}
	}
	if checked < 20 {
		t.Fatalf("only %d documented command lines were found; the scanner is broken", checked)
	}
}

var envPrefix = regexp.MustCompile(`^(?:[A-Z_][A-Z0-9_]*=\S+ )+`)

// commandLines extracts shell command lines from console, shell and bash code
// blocks of Markdown, or from comments of a YAML file, split at "&&" and with
// leading "$ " prompts and VAR=value assignments removed.
func commandLines(text string, isYAML bool) []string {
	var out []string
	inShell := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if isYAML {
			if !strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
		} else {
			if lang, ok := strings.CutPrefix(line, "```"); ok {
				inShell = !inShell && (lang == "console" || lang == "sh" || lang == "bash" || lang == "shell")
				continue
			}
			if !inShell {
				continue
			}
		}
		line = strings.TrimSpace(strings.SplitN(line, " # ", 2)[0])
		for _, part := range strings.Split(line, " && ") {
			part = strings.TrimPrefix(strings.TrimSpace(part), "$ ")
			part = envPrefix.ReplaceAllString(part, "")
			out = append(out, strings.TrimSpace(part))
		}
	}
	return out
}

// TestCookbookRecipesAreRun ties the Cookbook to the E2E suite: every recipe
// heading has a scenario of the same name in cookbook.atago.yaml, every
// example directory is shown on the page, and every recipe link written in an
// example points at a heading that exists.
func TestCookbookRecipesAreRun(t *testing.T) {
	t.Parallel()
	page := read(t, filepath.Join(root, "website", "content", "cookbook.md"))
	headings := map[string]bool{}
	anchors := map[string]bool{}
	for _, line := range strings.Split(page, "\n") {
		if h, ok := strings.CutPrefix(line, "## "); ok {
			headings[h] = true
			anchors[anchor(h)] = true
		}
	}
	var spec struct {
		Scenarios []struct {
			Name string `yaml:"name"`
		} `yaml:"scenarios"`
	}
	if err := yaml.Unmarshal([]byte(read(t, filepath.Join(root, "test", "e2e", "atago", "cookbook.atago.yaml"))), &spec); err != nil {
		t.Fatal(err)
	}
	scenarios := map[string]bool{}
	for _, s := range spec.Scenarios {
		scenarios[s.Name] = true
	}
	for h := range headings {
		if h == "Find a recipe" {
			continue
		}
		if !scenarios[h] {
			t.Errorf("cookbook recipe %q has no scenario of the same name in test/e2e/atago/cookbook.atago.yaml", h)
		}
	}
	for s := range scenarios {
		if !headings[s] {
			t.Errorf("cookbook scenario %q has no recipe heading on the Cookbook page", s)
		}
	}

	dirs, _ := filepath.Glob(filepath.Join(root, "examples", "*", "himorime.yaml"))
	if len(dirs) < 10 {
		t.Fatalf("found only %d example suites", len(dirs))
	}
	recipeLink := regexp.MustCompile(`https://nao1215\.github\.io/himorime/cookbook/#([a-z0-9-]+)`)
	for _, file := range dirs {
		rel, _ := filepath.Rel(root, file)
		rel = filepath.ToSlash(rel)
		if !strings.Contains(page, "<!-- example: "+rel+" -->") {
			t.Errorf("%s is not shown on the Cookbook page", rel)
		}
		for _, m := range recipeLink.FindAllStringSubmatch(read(t, file), -1) {
			if !anchors[m[1]] {
				t.Errorf("%s links to cookbook anchor #%s, which no heading produces", rel, m[1])
			}
		}
		if _, err := config.Load(file); err != nil {
			t.Errorf("%s is not a valid suite: %v", rel, err)
		}
	}
}

// anchor mirrors Hugo's default heading anchors for the headings used here.
func anchor(h string) string {
	h = strings.ToLower(h)
	var sb strings.Builder
	for _, r := range h {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
		case r == ' ' || r == '-':
			sb.WriteRune('-')
		}
	}
	return sb.String()
}

// TestDocumentedSuitesAreValid parses every YAML block in the documentation
// that is a suite (it starts with the schema comment or version: "1"), so an
// example on a page cannot use a key or form himorime rejects.
func TestDocumentedSuitesAreValid(t *testing.T) {
	t.Parallel()
	block := regexp.MustCompile("(?s)```yaml\n(.*?)```")
	count := 0
	for _, path := range docFiles(t) {
		for _, m := range block.FindAllStringSubmatch(read(t, path), -1) {
			body := m[1]
			if !strings.Contains(body, "version: \"1\"") || !strings.Contains(body, "benchmarks:") {
				continue
			}
			count++
			if _, err := config.Parse(path, filepath.Join(t.TempDir(), "himorime.yaml"), []byte(body)); err != nil {
				t.Errorf("%s: a documented suite is invalid:\n%v\n%s", path, err, body)
			}
		}
	}
	if count < 5 {
		t.Fatalf("only %d documented suites found", count)
	}
}

// publishedChannels are the distribution channels himorime is actually
// available through. A README or install page that tells people to use any
// other package manager would promise something that does not exist; add a
// channel here only once the package is published.
var publishedChannels = []string{"go install", "GitHub Releases", "brew install", "setup-himorime"}

func TestDocsDoNotPromiseUnpublishedPackages(t *testing.T) {
	t.Parallel()
	unpublished := []string{"aqua g", "mise use", "yay -S", "paru -S", "winget install", "scoop install", "apt install himorime", "nix-env", "choco install"}
	for _, path := range []string{filepath.Join(root, "README.md"), filepath.Join(root, "website", "content", "install.md")} {
		text := read(t, path)
		for _, u := range unpublished {
			if strings.Contains(text, u) {
				t.Errorf("%s mentions %q, which is not a published channel (published: %v)", path, u, publishedChannels)
			}
		}
	}
}

// TestSchemaURLIsConsistent keeps the schema address the same in the schema
// itself, the Go constant, the init template and the documentation.
func TestSchemaURLIsConsistent(t *testing.T) {
	t.Parallel()
	if !strings.Contains(string(schema.Suite), `"$id": "`+schema.SuiteURL+`"`) {
		t.Errorf("schema/himorime.schema.json $id is not %s", schema.SuiteURL)
	}
	if !strings.Contains(string(schema.Report), `"$id": "`+schema.ReportURL+`"`) {
		t.Errorf("schema/report.schema.json $id is not %s", schema.ReportURL)
	}
	if !strings.Contains(cli.InitTemplate, "$schema="+schema.SuiteURL) {
		t.Error("the init template does not reference the schema URL")
	}
	for _, p := range []string{filepath.Join(root, "README.md"), filepath.Join(root, "website", "content", "configuration.md")} {
		if !strings.Contains(read(t, p), schema.SuiteURL) {
			t.Errorf("%s does not mention the schema URL", p)
		}
	}
}

// TestWorkflowActionsArePinned requires every third-party action to be pinned
// to a full commit SHA with the version in a comment.
func TestWorkflowActionsArePinned(t *testing.T) {
	t.Parallel()
	files, _ := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	files = append(files, filepath.Join(root, "examples", "github-actions", "benchmark.yml"))
	if len(files) < 5 {
		t.Fatalf("found only %d workflows", len(files))
	}
	uses := regexp.MustCompile(`uses:\s*(\S+)(.*)`)
	pinned := regexp.MustCompile(`^[^@]+@[0-9a-f]{40}$`)
	for _, f := range files {
		for _, m := range uses.FindAllStringSubmatch(read(t, f), -1) {
			if strings.HasPrefix(m[1], "./") {
				continue
			}
			if !pinned.MatchString(m[1]) || !strings.Contains(m[2], "# v") {
				t.Errorf("%s: %s is not pinned to a commit SHA with a version comment", f, m[1])
			}
		}
	}
}

// TestWorkflowPermissionsAreReadOnly reserves writes for releasing and the
// separately validated benchmark job's automatic comments.
func TestWorkflowPermissionsAreReadOnly(t *testing.T) {
	t.Parallel()
	files, _ := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	write := regexp.MustCompile(`(?m)^\s+([a-z-]+):\s*write`)
	for _, f := range files {
		text := read(t, f)
		if !strings.Contains(text, "\npermissions:") {
			t.Errorf("%s does not declare top-level permissions", f)
		}
		if filepath.Base(f) == "release.yml" {
			continue
		}
		for _, m := range write.FindAllStringSubmatch(text, -1) {
			if filepath.Base(f) != "benchmark.yml" || m[1] != "pull-requests" {
				t.Errorf("%s grants unexpected %s: write", f, m[1])
			}
		}
		if strings.Contains(text, "pull_request_target") {
			t.Errorf("%s uses pull_request_target", f)
		}
	}
}

func TestBenchmarkCommentWorkflow(t *testing.T) {
	t.Parallel()
	for _, obsolete := range []string{".github/workflows/comment-benchmark.yml", "examples/github-actions/comment.yml"} {
		if _, err := os.Stat(filepath.Join(root, obsolete)); !os.IsNotExist(err) {
			t.Errorf("obsolete reporter still exists: %s", obsolete)
		}
	}
	for _, path := range []string{".github/workflows/benchmark.yml", "examples/github-actions/benchmark.yml"} {
		text := read(t, filepath.Join(root, path))
		var wf struct {
			On          map[string]any    `yaml:"on"`
			Permissions map[string]string `yaml:"permissions"`
			Jobs        map[string]struct {
				Permissions map[string]string `yaml:"permissions"`
				Steps       []struct {
					Uses string            `yaml:"uses"`
					Run  string            `yaml:"run"`
					If   string            `yaml:"if"`
					With map[string]string `yaml:"with"`
					Env  map[string]string `yaml:"env"`
				} `yaml:"steps"`
			} `yaml:"jobs"`
		}
		if err := yaml.Unmarshal([]byte(text), &wf); err != nil {
			t.Fatal(err)
		}
		if _, ok := wf.On["pull_request"]; !ok {
			t.Fatal("benchmark must run on pull requests")
		}
		if wf.Permissions["contents"] != "read" || len(wf.Permissions) != 1 {
			t.Fatal("top-level permissions must stay read-only")
		}
		for _, job := range wf.Jobs {
			if job.Permissions["contents"] != "read" || job.Permissions["pull-requests"] != "write" || len(job.Permissions) != 2 {
				t.Fatal("only benchmark job may publish comments")
			}
			var installed, measured, uploaded bool
			for _, step := range job.Steps {
				if strings.HasPrefix(step.Uses, "nao1215/setup-himorime@") {
					installed = step.Uses == "nao1215/setup-himorime@30710690b8b2dc8c61df8bf289f5bb899af3fbfa"
				}
				if strings.Contains(step.Run, "himorime ci ") {
					measured = installed && strings.Contains(step.Run, `--output "$RUNNER_TEMP/himorime.json"`) && strings.Contains(step.Run, "--format json")
				}
				if strings.HasPrefix(step.Uses, "actions/upload-artifact@") {
					uploaded = step.If == "always()" && step.With["name"] == "himorime-report" && step.With["path"] == "${{ runner.temp }}/himorime.json"
				}
				if step.Env["GITHUB_TOKEN"] != "" || step.Env["GH_TOKEN"] != "" {
					t.Fatal("measured commands must not receive the publication token")
				}
				if strings.Contains(step.Run, "himorime comment") {
					t.Fatal("comment command must not be needed")
				}
			}
			if !installed || !measured || !uploaded {
				t.Fatalf("%s: setup, report and artifact contract is incomplete", path)
			}
		}
	}
}

// TestFuzzTargetsAreScheduled keeps the fuzz workflow's matrix complete.
func TestFuzzTargetsAreScheduled(t *testing.T) {
	t.Parallel()
	found := map[string]bool{}
	fuzzFunc := regexp.MustCompile(`(?m)^func (Fuzz[A-Za-z0-9_]+)\(`)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "website") {
			return filepath.SkipDir
		}
		if strings.HasSuffix(path, "_test.go") {
			for _, m := range fuzzFunc.FindAllStringSubmatch(read(t, path), -1) {
				found[m[1]] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	workflow := read(t, filepath.Join(root, ".github", "workflows", "fuzz.yml"))
	var names []string
	for name := range found {
		names = append(names, name)
		if !strings.Contains(workflow, "name: "+name+" }") {
			t.Errorf("fuzz target %s is not in .github/workflows/fuzz.yml", name)
		}
	}
	sort.Strings(names)
	if len(names) < 4 {
		t.Fatalf("found only %v", names)
	}
}

func TestDuration(t *testing.T) {
	t.Parallel()
	for d, want := range map[time.Duration]string{
		time.Minute: "1m", 2 * time.Second: "2s", 5 * time.Minute: "5m", 10 * time.Minute: "10m", time.Hour: "1h", 90 * time.Second: "1m30s", 500 * time.Millisecond: "500ms",
	} {
		if got := duration(d); got != want {
			t.Errorf("duration(%v) = %q, want %q", d, got, want)
		}
	}
}
