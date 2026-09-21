// Package gitwt prepares a Git revision for measurement in a temporary
// worktree without touching the user's working tree, index or branches.
//
// The only lasting trace a comparison could leave is the worktree's
// administrative entry under .git/worktrees; Remove deletes it, and
// AddWorktree first clears the entries of himorime's own worktrees whose
// directories are gone, so an entry orphaned by a killed process is cleared by
// the next comparison. Entries of the user's worktrees are never touched, even
// when their directories are missing. Opening a repository only reads it.
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

	"github.com/nao1215/himorime/internal/rmtree"
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
//
// The checkout is settled before it is returned: see settleIndex.
func (r *Repo) AddWorktree(ctx context.Context, tempBase, sha string) (*Worktree, error) {
	// Clear entries of himorime worktrees whose directories no longer exist,
	// such as one left behind by a himorime that was killed with SIGKILL.
	_ = r.pruneOrphans(ctx)
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
	if err := r.settleIndex(ctx, dir); err != nil {
		_ = w.Remove(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("prepare worktree for %s: %w", short(sha), err)
	}
	return w, nil
}

// settleIndex makes the index of a fresh checkout as fast to use as the
// index of a working tree that has existed for a while.
//
// Git records the index's modification time and cannot trust the cached stat
// data of a file modified in the same second: such an entry is "racily clean"
// and its content is compared again by every `git status`. A checkout writes
// its files and its index within the same second, so every entry of a new
// worktree starts out racy. A measured command that asks Git about the
// revision it runs in (a build that embeds `git describe --dirty`, a CLI that
// reads its repository) would then be slower in the base worktree than in the
// working tree for a reason that has nothing to do with the change, and it
// stays that way when the command runs Git without optional locks, which
// never rewrites the index. Rewriting the index once the clock has moved past
// the second of the checkout ends that.
func (r *Repo) settleIndex(ctx context.Context, dir string) error {
	out, err := r.run(ctx, dir, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return err
	}
	info, err := os.Stat(strings.TrimSpace(out))
	if err != nil {
		return err
	}
	if wait := time.Until(info.ModTime().Truncate(time.Second).Add(time.Second)); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	_, err = r.run(ctx, dir, "update-index", "-q", "--refresh")
	return err
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
		// Git cannot delete a directory without write permission, such as a
		// Go module cache a build left in the worktree. Its error is not
		// kept: the tree is removed below, and its entry is pruned after.
		_, _ = w.repo.run(cctx, w.repo.Top, "worktree", "remove", "--force", w.Dir)
	}
	if err := rmtree.RemoveAll(w.base); err != nil {
		errs = append(errs, err)
	}
	if err := w.repo.pruneOrphans(cctx); err != nil {
		errs = append(errs, err)
	}
	w.base = ""
	return errors.Join(errs...)
}

// pruneOrphans deletes the administrative entries of himorime worktrees whose
// directories no longer exist. `git worktree prune` is not used because it
// also deletes the entry of any worktree of the user's whose directory is
// missing for now, such as one on an unmounted disk, and with it that
// worktree's index and branch lock.
func (r *Repo) pruneOrphans(ctx context.Context) error {
	out, err := r.run(ctx, r.Top, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	admin := filepath.Join(filepath.FromSlash(strings.TrimSpace(out)), "worktrees")
	entries, err := os.ReadDir(admin)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var errs []error
	for _, e := range entries {
		entry := filepath.Join(admin, e.Name())
		gitdir, err := os.ReadFile(filepath.Join(entry, "gitdir")) //nolint:gosec // G304: a file Git keeps in its own directory
		if err != nil {
			continue
		}
		dir := filepath.Dir(filepath.FromSlash(strings.TrimSpace(string(gitdir))))
		if !isOwnWorktree(dir) {
			continue
		}
		if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) { //nolint:gosec // G703: only checks whether the checkout Git recorded still exists
			continue
		}
		if err := os.RemoveAll(entry); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// isOwnWorktree reports whether dir has the shape AddWorktree gives a
// checkout: a directory named tree inside a worktree-* directory.
func isOwnWorktree(dir string) bool {
	return filepath.Base(dir) == "tree" && strings.HasPrefix(filepath.Base(filepath.Dir(dir)), "worktree-")
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
