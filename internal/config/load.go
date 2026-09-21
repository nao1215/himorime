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
	if is, ok := extraDocument(display, loc); ok {
		return nil, &ValidationError{Issues: []Issue{is}}
	}
	issues, err := schemaIssues(display, src, loc)
	if err != nil {
		return nil, err
	}
	if len(issues) > 0 {
		return nil, &ValidationError{Issues: issues}
	}
	decodeSrc, rewritten, err := resolveAliases(src, loc)
	if err != nil {
		return nil, &ValidationError{Issues: []Issue{decodeIssue(display, err)}}
	}
	var raw RawFile
	if err := yaml.UnmarshalWithOptions(decodeSrc, &raw, yaml.DisallowUnknownField()); err != nil {
		is := decodeIssue(display, err)
		if rewritten {
			// The position is in the expanded copy, not in the user's file.
			is.Line, is.Column = 0, 0
		}
		return nil, &ValidationError{Issues: []Issue{is}}
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
// anchored value, and reports whether it rewrote the document. The typed
// decoders work on syntax nodes, where an alias is only a name; expanding once
// up front lets them see the real value. A document without aliases is
// returned unchanged so decode errors keep their original line numbers.
//
// The expanded document is written as JSON, which YAML reads back with every
// string exactly as it was: a plain YAML rendering drops a tab or turns ".inf"
// into a number.
func resolveAliases(src []byte, loc locator) ([]byte, bool, error) {
	if loc.file == nil || !hasAlias(loc.file) {
		return src, false, nil
	}
	var doc any
	if err := yaml.UnmarshalWithOptions(src, &doc, yaml.UseOrderedMap()); err != nil {
		return nil, false, err
	}
	out, err := yaml.MarshalWithOptions(mergeKeys(doc), yaml.JSON())
	return out, true, err
}

// mergeKeys removes the duplicate keys a YAML merge key (<<) leaves in an
// ordered map: the decoder lists the merged keys first and the keys written
// next to <<, which take precedence, after them. The first position is kept so
// that command order follows the file.
func mergeKeys(v any) any {
	switch n := v.(type) {
	case yaml.MapSlice:
		out := make(yaml.MapSlice, 0, len(n))
		at := make(map[any]int, len(n))
		for _, item := range n {
			item.Value = mergeKeys(item.Value)
			if i, ok := at[item.Key]; ok {
				out[i].Value = item.Value
				continue
			}
			at[item.Key] = len(out)
			out = append(out, item)
		}
		return out
	case []any:
		for i := range n {
			n[i] = mergeKeys(n[i])
		}
	}
	return v
}

// extraDocument reports a second YAML document in a suite file. Only the first
// document would be read, so the benchmarks after "---" would silently never
// run.
func extraDocument(display string, loc locator) (Issue, bool) {
	if loc.file == nil {
		return Issue{}, false
	}
	seen := false
	for _, d := range loc.file.Docs {
		if d == nil || d.Body == nil {
			continue
		}
		if !seen {
			seen = true
			continue
		}
		is := Issue{
			File:    display,
			Message: "a suite file holds one YAML document; this file has another one after ---",
			Hint:    "merge the benchmarks into one benchmarks list, or move the second suite into its own file",
		}
		is.Line, is.Column = tokenPosition(d)
		return is, true
	}
	return Issue{}, false
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
