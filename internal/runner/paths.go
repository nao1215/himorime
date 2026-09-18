package runner

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// within reports whether target is root or lies below it. Both must be clean
// absolute paths.
func within(root, target string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

// realPath resolves symlinks in p. When p does not exist yet, its closest
// existing ancestor is resolved and the missing tail appended, so a path that
// a setup hook is about to create is still checked against where it will land.
func realPath(p string) (string, error) {
	p = filepath.Clean(p)
	var tail []string
	cur := p
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			parts := append([]string{resolved}, tail...)
			return filepath.Join(parts...), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		// EvalSymlinks also reports ErrNotExist for a symbolic link whose
		// target is missing. Treating that link as a path yet to be created
		// would let a write follow it anywhere, so it is refused.
		if info, lerr := os.Lstat(cur); lerr == nil && info.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("%s is a symbolic link to a missing target", cur)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p, nil
		}
		tail = append([]string{filepath.Base(cur)}, tail...)
		cur = parent
	}
}

// confine resolves p (symlinks included) and checks that it stays inside one
// of roots. roots are resolved the same way first. The resolved path is
// returned.
func confine(p string, roots ...string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	resolved, err := realPath(abs)
	if err != nil {
		return "", err
	}
	for _, r := range roots {
		if r == "" {
			continue
		}
		rr, err := realPath(r)
		if err != nil {
			continue
		}
		if within(rr, resolved) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("path %s resolves outside the project and the benchmark workdir", p)
}

// removeTemp deletes a directory himorime created under base. It refuses any
// path that is not strictly inside base, and it does not follow a symlink in
// place of the directory, so a cleanup can never recurse outside the
// temporary area.
func removeTemp(base, dir string) error {
	if base == "" || dir == "" {
		return errors.New("refusing to remove an unnamed directory")
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if absBase == absDir || !within(absBase, absDir) {
		return fmt.Errorf("refusing to remove %s: it is not inside %s", dir, base)
	}
	info, err := os.Lstat(absDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return os.Remove(absDir)
	}
	return RemoveAll(absDir)
}

// RemoveAll removes one of himorime's own temporary directory trees,
// retrying briefly. On Windows a file a just-terminated process had open
// stays locked for a moment after the process is gone, and removing it fails
// with a sharing violation. A tree holding directories without write
// permission, as a Go module cache does, is made writable and removed.
func RemoveAll(dir string) error {
	var err error
	for attempt := range 10 {
		if err = os.RemoveAll(dir); err == nil {
			return nil
		}
		if errors.Is(err, fs.ErrPermission) {
			makeWritable(dir)
			if err = os.RemoveAll(dir); err == nil {
				return nil
			}
		}
		time.Sleep(time.Duration(attempt+1) * 50 * time.Millisecond) //nolint:forbidigo // a bounded retry for a lock the OS releases asynchronously
	}
	return err
}

// makeWritable gives the owner write and search permission on every
// directory of the tree, and write permission on every file, so the tree can
// be removed. Every change goes through an os.Root of the tree, and WalkDir
// reports a directory before it reads it and never follows a symbolic link,
// so nothing outside the tree changes.
func makeWritable(dir string) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return
	}
	defer root.Close()
	_ = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.Type()&fs.ModeSymlink != 0 {
			return nil //nolint:nilerr // best effort: the removal that follows reports what is left
		}
		info, err := d.Info()
		if err != nil {
			return nil //nolint:nilerr // as above
		}
		add := fs.FileMode(0o200)
		if d.IsDir() {
			add = 0o700
		}
		if info.Mode().Perm()&add != add {
			_ = root.Chmod(path, info.Mode().Perm()|add)
		}
		return nil
	})
}
