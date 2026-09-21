package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// Issue is one problem found in a suite file.
type Issue struct {
	File   string
	Line   int
	Column int
	// Field is the dotted location of the problem, such as
	// benchmarks[0].commands.jsonize.command.
	Field   string
	Message string
	// Hint tells the user how to fix the problem, when there is a clear fix.
	Hint string
}

// String renders the issue as file:line:col: field: message (hint).
func (i Issue) String() string {
	var b strings.Builder
	b.WriteString(i.File)
	if i.Line > 0 {
		fmt.Fprintf(&b, ":%d:%d", i.Line, i.Column)
	}
	b.WriteString(": ")
	if i.Field != "" {
		b.WriteString(i.Field)
		b.WriteString(": ")
	}
	b.WriteString(i.Message)
	if i.Hint != "" {
		b.WriteString("\n    hint: ")
		b.WriteString(i.Hint)
	}
	return b.String()
}

// ValidationError carries every issue found in one suite file.
type ValidationError struct {
	Issues []Issue
}

func (e *ValidationError) Error() string {
	lines := make([]string, len(e.Issues))
	for i, is := range e.Issues {
		lines[i] = is.String()
	}
	return strings.Join(lines, "\n")
}

// path is a location inside the YAML document: string keys and int indexes.
type path []any

func (p path) key(k string) path {
	out := make(path, len(p), len(p)+1)
	copy(out, p)
	return append(out, k)
}

func (p path) index(i int) path {
	out := make(path, len(p), len(p)+1)
	copy(out, p)
	return append(out, i)
}

func (p path) String() string {
	var b strings.Builder
	for _, seg := range p {
		switch s := seg.(type) {
		case int:
			b.WriteString("[" + strconv.Itoa(s) + "]")
		case string:
			if b.Len() > 0 {
				b.WriteByte('.')
			}
			b.WriteString(s)
		}
	}
	return b.String()
}

// locator finds the position of a path in the parsed document. When the exact
// node does not exist (a missing key), the closest existing ancestor is used.
type locator struct {
	file *ast.File
}

func newLocator(src []byte) locator {
	f, err := parser.ParseBytes(src, 0)
	if err != nil {
		return locator{}
	}
	return locator{file: f}
}

// position returns the line and column of p. When p names a key that does
// not exist, the position of the key that holds it is returned instead, which
// is where a missing key has to be added.
func (l locator) position(p path) (int, int) {
	if l.file == nil || len(l.file.Docs) == 0 {
		return 0, 0
	}
	node := l.file.Docs[0].Body
	line, col := 0, 0
	if node != nil {
		line, col = tokenPosition(node)
	}
	keyLine, keyCol := line, col
	for _, seg := range p {
		value, key := child(node, seg)
		if value == nil {
			return keyLine, keyCol
		}
		node = value
		line, col = tokenPosition(value)
		keyLine, keyCol = line, col
		if key != nil {
			keyLine, keyCol = tokenPosition(key)
		}
	}
	return line, col
}

// keyPosition returns the position of the key that p names, rather than of
// its value: where an unknown key has to be fixed.
func (l locator) keyPosition(p path) (int, int) {
	if len(p) == 0 || l.file == nil || len(l.file.Docs) == 0 {
		return l.position(p)
	}
	node := l.file.Docs[0].Body
	for _, seg := range p[:len(p)-1] {
		value, _ := child(node, seg)
		if value == nil {
			return l.position(p)
		}
		node = value
	}
	if _, key := child(node, p[len(p)-1]); key != nil {
		return tokenPosition(key)
	}
	return l.position(p)
}

func tokenPosition(n ast.Node) (int, int) {
	tk := n.GetToken()
	if tk == nil || tk.Position == nil {
		return 0, 0
	}
	return tk.Position.Line, tk.Position.Column
}

// child returns the value at seg inside node, and for a mapping the key node
// that names it.
func child(node ast.Node, seg any) (ast.Node, ast.Node) {
	switch n := unwrap(node).(type) {
	case *ast.MappingNode:
		key, ok := seg.(string)
		if !ok {
			return nil, nil
		}
		for _, mv := range n.Values {
			// The token value is the key without the quotes it was written with.
			if mv.Key != nil && mv.Key.GetToken() != nil && mv.Key.GetToken().Value == key {
				if mv.Value != nil && mv.Value.Type() != ast.NullType {
					return mv.Value, mv.Key
				}
				return mv.Key, mv.Key
			}
		}
	case *ast.SequenceNode:
		i, ok := seg.(int)
		if !ok || i < 0 || i >= len(n.Values) {
			return nil, nil
		}
		return n.Values[i], nil
	}
	return nil, nil
}

func unwrap(node ast.Node) ast.Node {
	for {
		switch n := node.(type) {
		case *ast.TagNode:
			node = n.Value
		case *ast.AnchorNode:
			node = n.Value
		default:
			return node
		}
	}
}

// decodeIssue turns a YAML decoding error into an Issue with a message that
// names YAML types rather than Go types.
func decodeIssue(file string, err error) Issue {
	var ne *nodeError
	if errors.As(err, &ne) {
		return Issue{File: file, Line: ne.line, Column: ne.column, Message: ne.msg}
	}
	var yerr yaml.Error
	if !errors.As(err, &yerr) {
		return Issue{File: file, Message: err.Error()}
	}
	is := Issue{File: file, Message: friendlyMessage(yerr)}
	if tk := yerr.GetToken(); tk != nil && tk.Position != nil {
		is.Line, is.Column = tk.Position.Line, tk.Position.Column
	}
	var unknown *yaml.UnknownFieldError
	if errors.As(err, &unknown) {
		is.Hint = "check the spelling against https://nao1215.github.io/himorime/configuration/; unknown keys are rejected so a typo cannot silently change a benchmark"
	}
	var dup *yaml.DuplicateKeyError
	if errors.As(err, &dup) {
		is.Hint = "remove one of the two entries"
	}
	return is
}

func friendlyMessage(err yaml.Error) string {
	var te *yaml.TypeError
	if errors.As(err, &te) {
		want := "a value of another type"
		if te.DstType != nil {
			want = yamlKind(te.DstType.String())
		}
		got := "a value"
		if te.SrcType != nil {
			got = yamlKind(te.SrcType.String())
		}
		return fmt.Sprintf("expected %s, got %s", want, got)
	}
	msg := err.GetMessage()
	msg = strings.TrimPrefix(msg, "cannot unmarshal ")
	return msg
}

func yamlKind(goType string) string {
	switch {
	case strings.HasPrefix(goType, "[]"):
		return "a list"
	case strings.HasPrefix(goType, "map["), strings.Contains(goType, "config.Raw"):
		return "a mapping"
	case strings.Contains(goType, "int"), strings.Contains(goType, "uint"):
		return "an integer"
	case strings.Contains(goType, "float"):
		return "a number"
	case strings.Contains(goType, "bool"):
		return "true or false"
	case strings.Contains(goType, "string"):
		return "a string"
	default:
		return "a value of type " + goType
	}
}
