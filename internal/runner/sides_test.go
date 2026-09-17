package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nao1215/himorime/internal/config"
)

// revisionFixture lays out a comparison: the working tree (head) and a base
// worktree outside it, each holding the suite directory with its own
// data.txt, as Git would check them out.
type revisionFixture struct {
	*fixture
	base, head Side
}

func newRevisionFixture(t *testing.T) *revisionFixture {
	t.Helper()
	f := newFixture(t)
	baseTop := t.TempDir()
	baseRoot := filepath.Join(baseTop, "bench")
	headRoot := filepath.Join(f.dir, "bench")
	for dir, content := range map[string]string{baseRoot: "base\n", headRoot: "head revision\n"} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "data.txt"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return &revisionFixture{
		fixture: f,
		base:    Side{Name: SideBase, Root: baseRoot, HeadRoot: headRoot, ProjectRoot: baseTop},
		head:    Side{Name: SideHead, Root: headRoot, HeadRoot: headRoot, ProjectRoot: f.dir},
	}
}

func (rf *revisionFixture) read(t *testing.T, side Side, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(side.Root, name))
	if err != nil {
		t.Fatalf("%s: %v", side.Name, err)
	}
	return string(data)
}

// TestRevisionsRunTheirOwnFiles: a script-style command with a relative
// argument and the default working directory reads the file of the revision
// being measured, not the working tree's, and so do relative stdin and
// throughput file_size paths and hooks. ${head_root} gives both revisions the
// working tree's copy.
func TestRevisionsRunTheirOwnFiles(t *testing.T) {
	t.Parallel()
	rf := newRevisionFixture(t)
	// "copy data.txt" resolves data.txt against the process's working
	// directory, like `sh work.sh` would.
	own := rf.command("own", "copy", "data.txt", "${workdir}/own.txt")
	stdin := rf.command("stdin", "stdin-copy", "${workdir}/stdin.txt")
	b := bench("revisions", 2, own, stdin)
	b.Stdin = config.Stdin{Kind: config.StdinFile, File: "data.txt"}
	b.Metrics.Throughput = &config.Work{FileSize: "data.txt", Unit: "bytes"}
	b.Setup = []config.Exec{rf.helper("copy", "data.txt", "${workdir}/setup.txt")}
	b.Cleanup = []config.Exec{
		rf.helper("copy", "${workdir}/own.txt", "${root}/own.out"),
		rf.helper("copy", "${workdir}/stdin.txt", "${root}/stdin.out"),
		rf.helper("copy", "${workdir}/setup.txt", "${root}/setup.out"),
	}
	res := rf.runner.Measure(context.Background(), b, []Side{rf.base, rf.head})
	if res.Failure != nil {
		t.Fatalf("failure: %+v", res.Failure)
	}
	for _, side := range []Side{rf.base, rf.head} {
		want := rf.read(t, side, "data.txt")
		for _, out := range []string{"own.out", "stdin.out", "setup.out"} {
			if got := rf.read(t, side, out); got != want {
				t.Errorf("%s %s = %q, want its own data.txt %q", side.Name, out, got, want)
			}
		}
		if w := res.Commands[0].Sides[side.Name].Work; len(w) != 2 || w[0] != float64(len(want)) {
			t.Errorf("%s file_size work = %v, want %d", side.Name, w, len(want))
		}
	}

	shared := rf.command("shared", "stdin-copy", "${workdir}/shared.txt")
	sb := bench("shared", 1, shared)
	sb.Stdin = config.Stdin{Kind: config.StdinFile, File: "${head_root}/data.txt"}
	sb.Metrics.Throughput = &config.Work{FileSize: "${head_root}/data.txt", Unit: "bytes"}
	sb.Cleanup = []config.Exec{rf.helper("copy", "${workdir}/shared.txt", "${root}/shared.out")}
	pinned := rf.command("pinned", "copy", "data.txt", "${workdir}/pinned.txt")
	pinned.Cwd = "${head_root}"
	sb.Commands = append(sb.Commands, pinned)
	sb.Cleanup = append(sb.Cleanup, rf.helper("copy", "${workdir}/pinned.txt", "${root}/pinned.out"))
	res = rf.runner.Measure(context.Background(), sb, []Side{rf.base, rf.head})
	if res.Failure != nil {
		t.Fatalf("shared failure: %+v", res.Failure)
	}
	for _, side := range []Side{rf.base, rf.head} {
		for _, out := range []string{"shared.out", "pinned.out"} {
			if got := rf.read(t, side, out); got != "head revision\n" {
				t.Errorf("%s %s = %q, want the working tree's data.txt", side.Name, out, got)
			}
		}
	}
}

// TestFileMissingFromBaseSuggestsHeadRoot: a fixture added in the working
// tree does not exist in the base revision; the failure says so and names
// ${head_root}.
func TestFileMissingFromBaseSuggestsHeadRoot(t *testing.T) {
	t.Parallel()
	rf := newRevisionFixture(t)
	if err := os.WriteFile(filepath.Join(rf.head.Root, "new.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := bench("new fixture", 1, rf.command("a", "ok"))
	b.Stdin = config.Stdin{Kind: config.StdinFile, File: "new.txt"}
	res := rf.runner.Measure(context.Background(), b, []Side{rf.base, rf.head})
	if res.Failure == nil || res.Failure.Kind != FailSetup || !strings.Contains(res.Failure.Message, "does not exist in the base revision") || !strings.Contains(res.Failure.Message, "${head_root}") {
		t.Fatalf("failure = %+v", res.Failure)
	}
	if res = rf.runner.Measure(context.Background(), b, []Side{rf.head}); res.Failure != nil {
		t.Fatalf("the head alone has the fixture: %+v", res.Failure)
	}
}

// TestAdaptiveComparisonReachesMinSamples: adaptive runs of a comparison
// continue until regression.min_samples, even when min_runs and min_time are
// already met, so the verdict is not inconclusive for lack of samples. A plain
// run keeps min_runs.
func TestAdaptiveComparisonReachesMinSamples(t *testing.T) {
	t.Parallel()
	rf := newRevisionFixture(t)
	b := bench("adaptive", 0, rf.command("a", "ok"))
	// min_time 0 makes the stop depend on counts alone.
	b.MinRuns, b.MaxRuns, b.MinTime = 2, 10, 0
	b.Regression.MinSamples = 5
	res := rf.runner.Measure(context.Background(), b, []Side{rf.base, rf.head})
	for _, side := range []string{SideBase, SideHead} {
		if n := len(res.Commands[0].Sides[side].Samples); n != 5 {
			t.Errorf("%s: %d samples, want min_samples 5", side, n)
		}
	}
	res = rf.runner.Measure(context.Background(), b, []Side{rf.head})
	if n := len(res.Commands[0].Sides[SideHead].Samples); n != 2 {
		t.Errorf("a plain run judges no comparison and stops at min_runs: %d samples", n)
	}

	fixed := bench("fixed", 3, rf.command("a", "ok"))
	fixed.Regression.MinSamples = 5
	res = rf.runner.Measure(context.Background(), fixed, []Side{rf.base, rf.head})
	if n := len(res.Commands[0].Sides[SideBase].Samples); n != 3 {
		t.Errorf("fixed runs are never extended: %d samples", n)
	}
}

func TestMinRunsFor(t *testing.T) {
	t.Parallel()
	b := config.Benchmark{MinRuns: 2, Regression: config.Regression{MinSamples: 5}}
	if got := minRunsFor(b, 1); got != 2 {
		t.Errorf("plain run = %d, want min_runs", got)
	}
	if got := minRunsFor(b, 2); got != 5 {
		t.Errorf("comparison = %d, want min_samples", got)
	}
	b.MinRuns = 8
	if got := minRunsFor(b, 2); got != 8 {
		t.Errorf("comparison with min_runs above min_samples = %d", got)
	}
}
