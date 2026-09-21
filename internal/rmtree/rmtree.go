// Package rmtree removes himorime's own temporary directory trees: the working
// directories of benchmarks and the base worktree of a comparison.
package rmtree

import (
	"errors"
	"io/fs"
	"os"
	"time"
)

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
