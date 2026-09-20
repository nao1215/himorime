package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

func locatorNode(t *testing.T, src string) ast.Node {
	t.Helper()
	f, err := parser.ParseBytes([]byte(src), 0)
	if err != nil {
		t.Fatal(err)
	}
	return f.Docs[0].Body
}

func TestLocatorHandlesMissingPathsAndWrappedValues(t *testing.T) {
	t.Parallel()
	if line, col := (locator{}).position(path{"missing"}); line != 0 || col != 0 {
		t.Fatalf("empty locator position = %d:%d", line, col)
	}
	l := newLocator([]byte("root:\n  list:\n    - value\n  empty: null\nanchor: &item {value: one}\nalias: *item\n"))
	if line, col := l.position(path{"root", "list", 0}); line != 3 || col == 0 {
		t.Errorf("list item position = %d:%d", line, col)
	}
	if line, col := l.position(path{"root", "list", 8}); line != 2 || col == 0 {
		t.Errorf("missing list item position = %d:%d", line, col)
	}
	if line, col := l.position(path{"root", "missing"}); line != 1 || col == 0 {
		t.Errorf("missing mapping key position = %d:%d", line, col)
	}
	if line, col := l.keyPosition(path{"root", "list"}); line != 2 || col == 0 {
		t.Errorf("list key position = %d:%d", line, col)
	}
	if line, col := l.keyPosition(path{"root", "missing"}); line != 1 || col == 0 {
		t.Errorf("missing key position = %d:%d", line, col)
	}
	if line, col := l.keyPosition(path{"missing", "nested"}); line != 1 || col == 0 {
		t.Errorf("missing ancestor key position = %d:%d", line, col)
	}
	if line, col := l.keyPosition(nil); line != 1 || col == 0 {
		t.Errorf("empty path position = %d:%d", line, col)
	}

	body := locatorNode(t, "map:\n  empty: null\n  value: &item one\n")
	if value, key := child(body, "map"); value == nil || key == nil {
		t.Fatalf("map child = %v, %v", value, key)
	} else {
		if value, key := child(value, "empty"); value == nil || key == nil {
			t.Fatalf("null child = %v, %v", value, key)
		}
	}
	if value, key := child(body, 0); value != nil || key != nil {
		t.Fatal("mapping accepted an integer segment")
	}
	seq := locatorNode(t, "items: [one]\n")
	items, _ := child(seq, "items")
	if value, key := child(items, 0); value == nil || key != nil {
		t.Fatalf("sequence child = %v, %v", value, key)
	}
	for _, seg := range []any{"0", -1, 1} {
		if value, key := child(items, seg); value != nil || key != nil {
			t.Fatalf("invalid sequence segment %v returned %v, %v", seg, value, key)
		}
	}
	anchored := locatorNode(t, "value: &item one\n")
	anchorValue, _ := child(anchored, "value")
	if _, ok := anchorValue.(*ast.AnchorNode); !ok {
		t.Fatalf("anchored value type = %T", anchorValue)
	}
	if got := unwrap(anchorValue); got == nil {
		t.Fatal("unwrap returned nil for an anchored value")
	}
	if got := unwrap(nil); got != nil {
		t.Fatalf("unwrap(nil) = %v", got)
	}
}

func TestIssueDecodingMessages(t *testing.T) {
	t.Parallel()
	if got := (&nodeError{msg: "bad"}).Error(); got != "bad" {
		t.Fatalf("nodeError.Error() = %q", got)
	}
	for _, typ := range []string{"[]string", "map[string]any", "config.RawFile", "int", "uint64", "float64", "bool", "string", "custom.Type"} {
		if got := yamlKind(typ); got == "" {
			t.Errorf("yamlKind(%q) is empty", typ)
		}
	}
	if got := yamlKind("[]string"); got != "a list" || yamlKind("config.RawFile") != "a mapping" || yamlKind("int") != "an integer" || yamlKind("float64") != "a number" || yamlKind("bool") != "true or false" || yamlKind("string") != "a string" {
		t.Errorf("yamlKind built-ins = %q", got)
	}

	var dst struct {
		Count int `yaml:"count"`
	}
	err := yaml.UnmarshalWithOptions([]byte("count: nope\n"), &dst, yaml.DisallowUnknownField())
	if err == nil {
		t.Fatal("invalid integer was accepted")
	}
	is := decodeIssue("suite.yaml", err)
	if !strings.Contains(is.Message, "expected an integer") || is.Line == 0 || is.Column == 0 {
		t.Fatalf("decode issue = %+v", is)
	}
	if got := decodeIssue("suite.yaml", errors.New("plain failure")); got.Message != "plain failure" {
		t.Fatalf("plain decode issue = %+v", got)
	}
}
