package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// maxFileSize bounds a suite file. A benchmark suite is hand-written text; a
// file this large is a mistake, and refusing it keeps a hostile document from
// exhausting memory before validation even starts.
const maxFileSize = 4 << 20

// Load reads, validates and resolves the suite file at path. A problem with the
// file's content is returned as a *ValidationError listing every issue found; a
// problem reading the file is returned as a plain error.
func Load(path string) (*Suite, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", path, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, &ValidationError{Issues: []Issue{{
				File:    path,
				Message: "suite file does not exist",
				Hint:    "create one with `himorime init`, or pass the path of an existing suite",
			}}}
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if info.IsDir() {
		return nil, &ValidationError{Issues: []Issue{{File: path, Message: "is a directory, not a suite file"}}}
	}
	if info.Size() > maxFileSize {
		return nil, &ValidationError{Issues: []Issue{{File: path, Message: fmt.Sprintf("suite file is larger than %d bytes", maxFileSize)}}}
	}
	src, err := os.ReadFile(abs) //nolint:gosec // G304: the suite file the user named; its size was checked above
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return Parse(path, abs, src)
}

// Parse validates and resolves suite source. display is the path shown in
// messages; abs is the absolute path the suite's relative paths resolve from.
func Parse(display, abs string, src []byte) (*Suite, error) {
	if len(bytes.TrimSpace(src)) == 0 {
		return nil, &ValidationError{Issues: []Issue{{
			File:    display,
			Message: "suite file is empty",
			Hint:    "a suite needs at least version, name and one benchmark; `himorime init` writes a runnable example",
		}}}
	}
	loc := newLocator(src)
	issues, err := schemaIssues(display, src, loc)
	if err != nil {
		return nil, err
	}
	if len(issues) > 0 {
		return nil, &ValidationError{Issues: issues}
	}
	decodeSrc, err := resolveAliases(src, loc)
	if err != nil {
		return nil, &ValidationError{Issues: []Issue{decodeIssue(display, err)}}
	}
	var raw RawFile
	if err := yaml.UnmarshalWithOptions(decodeSrc, &raw, yaml.DisallowUnknownField()); err != nil {
		return nil, &ValidationError{Issues: []Issue{decodeIssue(display, err)}}
	}
	dir := filepath.Dir(abs)
	v := &validator{file: display, loc: loc, dir: dir, projectRoot: projectRoot(dir)}
	suite := v.resolve(&raw)
	if len(v.issues) > 0 {
		return nil, &ValidationError{Issues: v.issues}
	}
	suite.Path = abs
	suite.Dir = filepath.Dir(abs)
	return suite, nil
}

// projectRoot is the directory a relative path of the suite may not leave:
// the top of the repository holding the suite, or the suite's own directory
// when it is not in a repository. It is the same directory the run confines
// paths to, found without asking Git, so that validation and the run agree.
func projectRoot(dir string) string {
	for cur := dir; ; {
		if _, err := os.Lstat(filepath.Join(cur, ".git")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return dir
		}
		cur = parent
	}
}

// resolveAliases returns src with every YAML alias replaced by a copy of its
// anchored value. The typed decoders work on syntax nodes, where an alias is
// only a name; expanding once up front lets them see the real value. A
// document without aliases is returned unchanged so decode errors keep their
// original line numbers.
func resolveAliases(src []byte, loc locator) ([]byte, error) {
	if loc.file == nil || !hasAlias(loc.file) {
		return src, nil
	}
	var doc any
	if err := yaml.UnmarshalWithOptions(src, &doc, yaml.UseOrderedMap()); err != nil {
		return nil, err
	}
	return yaml.Marshal(doc)
}

type aliasFinder struct{ found bool }

func (a *aliasFinder) Visit(node ast.Node) ast.Visitor {
	if _, ok := node.(*ast.AliasNode); ok {
		a.found = true
		return nil
	}
	return a
}

func hasAlias(f *ast.File) bool {
	finder := &aliasFinder{}
	for _, d := range f.Docs {
		ast.Walk(finder, d)
	}
	return finder.found
}
