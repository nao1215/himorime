// Package gitwt prepares a Git revision for measurement in a temporary
// worktree without touching the user's working tree, index or branches.
//
// The only lasting trace a comparison could leave is the worktree's
// administrative entry under .git/worktrees; Remove deletes it, and Open runs
// `git worktree prune` first so an entry orphaned by a killed process is
// cleared on the next run.
package gitwt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrNotRepository is returned when a directory is not inside a Git work tree.
var ErrNotRepository = errors.New("not inside a Git repository")

// removeTimeout bounds cleanup that runs after an interrupt.
const removeTimeout = time.Minute

// Repo is a Git repository work tree.
type Repo struct {
	// Top is the absolute top-level directory of the work tree.
	Top string
	git string
}

// Open finds the repository containing dir.
func Open(ctx context.Context, dir string) (*Repo, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git is not installed or not in PATH: %w", err)
	}
	r := &Repo{git: git}
	out, err := r.run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, ErrNotRepository)
	}
	top, err := filepath.Abs(filepath.FromSlash(strings.TrimSpace(out)))
	if err != nil {
		return nil, err
	}
	if resolved, err := filepath.EvalSymlinks(top); err == nil {
		top = resolved
	}
	r.Top = top
	// Clear entries of worktrees whose directories no longer exist, such as
	// one left behind by a himorime that was killed with SIGKILL.
	_, _ = r.run(ctx, top, "worktree", "prune")
	return r, nil
}

// ResolveCommit resolves a revision to a full commit SHA.
func (r *Repo) ResolveCommit(ctx context.Context, ref string) (string, error) {
	if ref == "" {
		return "", errors.New("the revision is empty")
	}
	if strings.HasPrefix(ref, "-") {
		return "", fmt.Errorf("invalid revision %q: a revision cannot start with '-'", ref)
	}
	if strings.ContainsAny(ref, "\x00\n\r") {
		return "", fmt.Errorf("invalid revision %q", ref)
	}
	out, err := r.run(ctx, r.Top, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("revision %q is not a commit in %s; fetch it first (in GitHub Actions, check out with fetch-depth: 0)", ref, r.Top)
	}
	return strings.TrimSpace(out), nil
}

// HeadSHA returns the commit checked out in the work tree, or "" for a
// repository without commits.
func (r *Repo) HeadSHA(ctx context.Context) string {
	out, err := r.run(ctx, r.Top, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Dirty reports whether the work tree has uncommitted changes, untracked files
// included.
func (r *Repo) Dirty(ctx context.Context) (bool, error) {
	out, err := r.run(ctx, r.Top, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// Worktree is a temporary checkout of one commit.
type Worktree struct {
	// Dir is the absolute directory of the checkout.
	Dir  string
	SHA  string
	repo *Repo
	base string
}

// AddWorktree checks out sha into a new directory below tempBase. The
// checkout is detached, so no branch is created or moved, and the
// repository's hooks are disabled for it: a post-checkout hook is not part of
// what is being measured.
func (r *Repo) AddWorktree(ctx context.Context, tempBase, sha string) (*Worktree, error) {
	if err := os.MkdirAll(tempBase, 0o700); err != nil {
		return nil, err
	}
	parent, err := os.MkdirTemp(tempBase, "worktree-")
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(parent, "tree")
	hooks := filepath.Join(parent, "no-hooks")
	if err := os.Mkdir(hooks, 0o700); err != nil {
		_ = os.RemoveAll(parent)
		return nil, err
	}
	w := &Worktree{Dir: dir, SHA: sha, repo: r, base: parent}
	if _, err := r.run(ctx, r.Top, "-c", "core.hooksPath="+hooks, "worktree", "add", "--detach", "--quiet", dir, sha); err != nil {
		_ = w.Remove(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("create worktree for %s: %w", short(sha), err)
	}
	return w, nil
}

// Remove deletes the worktree's directory and its administrative entry. It is
// safe to call more than once and after a failed AddWorktree. It keeps going
// when the context is already canceled, so an interrupt still cleans up.
func (w *Worktree) Remove(ctx context.Context) error {
	if w == nil || w.base == "" {
		return nil
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), removeTimeout)
	defer cancel()
	var errs []error
	if _, err := os.Stat(w.Dir); err == nil {
		if _, err := w.repo.run(cctx, w.repo.Top, "worktree", "remove", "--force", w.Dir); err != nil {
			errs = append(errs, err)
		}
	}
	if err := os.RemoveAll(w.base); err != nil {
		errs = append(errs, err)
	}
	if _, err := w.repo.run(cctx, w.repo.Top, "worktree", "prune"); err != nil {
		errs = append(errs, err)
	}
	w.base = ""
	return errors.Join(errs...)
}

func (r *Repo) run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, r.git, args...) //nolint:gosec // G204: fixed git subcommands; revisions are validated by ResolveCommit
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("git %s: %w", args[0], err)
		}
		return "", fmt.Errorf("git %s: %s", args[0], firstLine(msg))
	}
	return stdout.String(), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
