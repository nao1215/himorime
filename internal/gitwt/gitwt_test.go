package gitwt

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL="+emptyConfig(t), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// emptyConfig returns an empty Git configuration file, so the tests do not
// depend on the developer's or the runner's global settings.
func emptyConfig(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func newRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "version.txt"), []byte("v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "v1")
	return dir
}

func TestOpenResolveAndDirty(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repo, err := Open(ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(dir)
	if !samePath(repo.Top, resolved) {
		t.Fatalf("Top = %s, want %s", repo.Top, resolved)
	}
	head := git(t, dir, "rev-parse", "HEAD")
	if repo.HeadSHA(ctx) != head {
		t.Fatalf("HeadSHA = %s, want %s", repo.HeadSHA(ctx), head)
	}
	sha, err := repo.ResolveCommit(ctx, "main")
	if err != nil || sha != head {
		t.Fatalf("ResolveCommit(main) = %s, %v", sha, err)
	}
	if dirty, err := repo.Dirty(ctx); err != nil || dirty {
		t.Fatalf("clean repo reported dirty=%v err=%v", dirty, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if dirty, _ := repo.Dirty(ctx); !dirty {
		t.Fatal("an untracked file must count as dirty")
	}
	for _, bad := range []string{"", "--output=/tmp/x", "no-such-branch", "main\nHEAD"} {
		if _, err := repo.ResolveCommit(ctx, bad); err == nil {
			t.Errorf("ResolveCommit(%q) succeeded", bad)
		}
	}
	if _, err := Open(ctx, t.TempDir()); err == nil {
		t.Fatal("Open outside a repository succeeded")
	}
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func TestWorktreeLifecycleLeavesNoTrace(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	ctx := context.Background()
	base := git(t, dir, "rev-parse", "HEAD")
	// Make the working tree dirty and move the branch, as a developer would.
	if err := os.WriteFile(filepath.Join(dir, "version.txt"), []byte("v2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "commit", "-q", "-am", "v2")
	if err := os.WriteFile(filepath.Join(dir, "version.txt"), []byte("v3 uncommitted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	statusBefore := git(t, dir, "status", "--porcelain")
	branchBefore := git(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
	headBefore := git(t, dir, "rev-parse", "HEAD")

	repo, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	wt, err := repo.AddWorktree(ctx, tmp, base)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(wt.Dir, "version.txt"))
	// The worktree is checked out with the runner's own Git configuration,
	// which converts line endings to CRLF on Windows.
	if err != nil || strings.ReplaceAll(string(data), "\r\n", "\n") != "v1\n" {
		t.Fatalf("worktree content = %q, %v", data, err)
	}
	if !strings.Contains(git(t, dir, "worktree", "list"), filepath.Base(wt.Dir)) {
		t.Fatal("the worktree is not registered")
	}
	if err := wt.Remove(ctx); err != nil {
		t.Fatal(err)
	}
	if err := wt.Remove(ctx); err != nil {
		t.Fatalf("a second Remove must be a no-op: %v", err)
	}
	if _, err := os.Stat(wt.Dir); !os.IsNotExist(err) {
		t.Fatal("the worktree directory still exists")
	}
	if list := git(t, dir, "worktree", "list", "--porcelain"); strings.Count(list, "worktree ") != 1 {
		t.Fatalf("worktree entries remain:\n%s", list)
	}
	if git(t, dir, "status", "--porcelain") != statusBefore || git(t, dir, "rev-parse", "--abbrev-ref", "HEAD") != branchBefore || git(t, dir, "rev-parse", "HEAD") != headBefore {
		t.Fatal("the working tree, branch or HEAD changed")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "version.txt")); string(data) != "v3 uncommitted\n" {
		t.Fatal("uncommitted changes were lost")
	}
	if branches := git(t, dir, "branch", "--list"); strings.Count(branches, "\n") != 0 {
		t.Fatalf("a branch was created:\n%s", branches)
	}
}

func TestWorktreeRemovedAfterCancellation(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	repo, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.AddWorktree(context.Background(), t.TempDir(), repo.HeadSHA(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := wt.Remove(ctx); err != nil {
		t.Fatalf("Remove with a canceled context: %v", err)
	}
	if list := git(t, dir, "worktree", "list", "--porcelain"); strings.Count(list, "worktree ") != 1 {
		t.Fatalf("worktree entries remain after an interrupt:\n%s", list)
	}
}

func TestAddWorktreeFailureCleansUp(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	repo, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if _, err := repo.AddWorktree(context.Background(), tmp, strings.Repeat("0", 40)); err == nil {
		t.Fatal("a worktree for a missing commit was created")
	}
	entries, _ := os.ReadDir(tmp)
	if len(entries) != 0 {
		t.Fatalf("a failed AddWorktree left %d entries behind", len(entries))
	}
}

func TestOpenPrunesStaleWorktrees(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	stale := filepath.Join(t.TempDir(), "stale")
	git(t, dir, "worktree", "add", "--detach", "-q", stale, "HEAD")
	if err := os.RemoveAll(stale); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if list := git(t, dir, "worktree", "list", "--porcelain"); strings.Contains(list, "stale") {
		t.Fatalf("a worktree killed mid-run was not pruned:\n%s", list)
	}
}
