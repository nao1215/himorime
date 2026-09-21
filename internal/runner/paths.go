package runner

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nao1215/himorime/internal/rmtree"
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
	return rmtree.RemoveAll(absDir)
}
