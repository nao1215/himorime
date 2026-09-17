package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/nao1215/himorime/schema"
)

var (
	compileOnce   sync.Once
	suiteSchema   *jsonschema.Schema
	errCompile    error
	schemaPrinter = message.NewPrinter(language.English)
)

// SuiteSchema returns the compiled suite schema embedded in the binary.
func SuiteSchema() (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema.Suite))
		if err != nil {
			errCompile = fmt.Errorf("parse embedded suite schema: %w", err)
			return
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource(schema.SuiteURL, doc); err != nil {
			errCompile = fmt.Errorf("load embedded suite schema: %w", err)
			return
		}
		suiteSchema, errCompile = c.Compile(schema.SuiteURL)
	})
	return suiteSchema, errCompile
}

// schemaIssues validates YAML source against the suite schema and returns
// one issue per problem, positioned in the YAML file.
func schemaIssues(file string, src []byte, loc locator) ([]Issue, error) {
	sch, err := SuiteSchema()
	if err != nil {
		return nil, err
	}
	inst, err := yamlInstance(src)
	if err != nil {
		return []Issue{decodeIssue(file, err)}, nil
	}
	err = sch.Validate(inst)
	if err == nil {
		return nil, nil
	}
	var verr *jsonschema.ValidationError
	if !errors.As(err, &verr) {
		return nil, err
	}
	var issues []Issue
	seen := map[string]bool{}
	collectSchemaIssues(verr, func(p path, msg, hint string) {
		key := p.String() + "\x00" + msg
		if seen[key] {
			return
		}
		seen[key] = true
		line, col := loc.position(p)
		if strings.HasPrefix(msg, "unknown key ") || strings.HasPrefix(msg, "invalid name ") || strings.HasPrefix(msg, "invalid environment variable name ") {
			line, col = loc.keyPosition(p)
		}
		issues = append(issues, Issue{File: file, Line: line, Column: col, Field: p.String(), Message: msg, Hint: hint})
	})
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Line != issues[j].Line {
			return issues[i].Line < issues[j].Line
		}
		return issues[i].Column < issues[j].Column
	})
	return issues, nil
}

// yamlInstance decodes YAML into the JSON data model the schema validator
// expects: string-keyed maps, slices, strings, booleans, nil and json.Number.
func yamlInstance(src []byte) (any, error) {
	var doc any
	if err := yaml.UnmarshalWithOptions(src, &doc); err != nil {
		return nil, err
	}
	return jsonValue(doc), nil
}

func jsonValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = jsonValue(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[fmt.Sprint(k)] = jsonValue(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = jsonValue(val)
		}
		return out
	case int:
		return json.Number(strconv.FormatInt(int64(x), 10))
	case int64:
		return json.Number(strconv.FormatInt(x, 10))
	case uint64:
		return json.Number(strconv.FormatUint(x, 10))
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return fmt.Sprint(x)
		}
		return json.Number(strconv.FormatFloat(x, 'g', -1, 64))
	case string, bool, nil:
		return x
	case time.Time:
		return x.Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(x)
	}
}

// definitionMessages give a readable message for schema definitions whose raw
// errors (oneOf branches, long patterns) would say little to a user. noun is
// used when the value has the wrong type altogether.
var definitionMessages = []struct {
	fragment string
	noun     string
	message  string
	hint     string
}{
	{"/definitions/shellRule", "", "a string command requires shell: true, and shell: true requires a string command", "write command as a list such as [git, --version], or add shell: true to a string command"},
	{"/definitions/command", "a list of arguments or a string", "expected a list of arguments whose first element is the program, or a non-empty string used with shell: true", "write the command as a list such as [git, --version]"},
	{"/definitions/versionCommand", "a list of arguments", "expected a list of arguments whose first element is the program", "write the command as a list such as [jc, --version]"},
	{"/definitions/positiveDuration", "a duration string such as 500ms", "must be a duration greater than zero, such as 500ms or 2s", ""},
	{"/definitions/duration", "a duration string such as 500ms", "invalid duration: write a number with a unit, such as 500ms, 2s or 1m30s (units: ns, us, µs, ms, s, m, h)", ""},
	{"/definitions/budget", `a budget string such as "< 20ms"`, `invalid budget: write "<" or "<=" and a duration greater than zero, such as "< 20ms"; latency and CPU time are better when lower`, ""},
	{"/definitions/bytesBudget", `a budget string such as "<= 64MiB"`, `invalid budget: write "<" or "<=" and a byte size greater than zero, such as "<= 64MiB" (units: B, KB, MB, GB, TB, KiB, MiB, GiB, TiB); memory is better when lower`, ""},
	{"/definitions/rateBudget", `a budget string such as ">= 50MiB/s"`, `invalid throughput budget: write ">" or ">=" and a rate in the declared work unit, such as ">= 50MiB/s" or ">= 1000 records/s"; throughput is better when higher`, ""},
	{"/definitions/percentBudget", `a budget string such as "<= 150%"`, `invalid budget: write "<", "<=", ">" or ">=" and a percentage, such as "<= 150%"`, ""},
	{"/definitions/bytes", "a byte size string such as 1MiB", "invalid byte size: write a number and a unit, such as 1MiB (units: B, KB, MB, GB, TB, KiB, MiB, GiB, TiB)", ""},
	{"/definitions/rate", `a rate string such as "100 records/s"`, `invalid rate: write a number, the declared work unit and /s, such as 1MiB/s or "100 records/s"`, ""},
	{"/definitions/collector", "true, false or a mapping with scope", "", ""},
	{"/definitions/percent", `a percentage such as 10 or "10%"`, `expected a percentage greater than 0 and at most 1000, such as 10 or "10%"`, ""},
	{"/definitions/relativePath", "a relative path", "must be a relative path that stays inside its base directory, without variables", ""},
	{"/definitions/output", `"discard" or a relative path`, `must be "discard" or a relative path inside ${workdir}`, ""},
	{"/definitions/pathTemplate", "a path", "must be a relative path, or start with ${root} or ${workdir}; absolute paths are not allowed", ""},
	{"/properties/stdin", "a fixture path or a mapping with file or content", "", ""},
}

func collectSchemaIssues(e *jsonschema.ValidationError, emit func(path, string, string)) {
	walkSchemaError(e, path{}, emit)
}

func walkSchemaError(e *jsonschema.ValidationError, parent path, emit func(path, string, string)) {
	p := instancePath(e.InstanceLocation)
	if pn, ok := e.ErrorKind.(*kind.PropertyNames); ok {
		// The library reports the parent object's location unreliably for
		// propertyNames; the enclosing group's location is correct. An object
		// reached from the root through properties alone, such as
		// report.versions, has no enclosing group, and its schema pointer
		// names the location instead.
		at := parent
		if fixed, ok := propertiesPath(e.SchemaURL); ok {
			at = fixed
		}
		emit(at.key(pn.Property), propertyNameMessage(e.SchemaURL, pn.Property), "")
		return
	}
	if definitionIssue(e, p, emit) {
		return
	}
	switch e.ErrorKind.(type) {
	case *kind.OneOf, *kind.AnyOf:
		matching := branchesOfRightType(e)
		if len(matching) == 0 {
			emit(p, "has the wrong type", "")
			return
		}
		for _, c := range matching {
			walkSchemaError(c, p, emit)
		}
		return
	}
	if len(e.Causes) > 0 {
		for _, c := range e.Causes {
			walkSchemaError(c, p, emit)
		}
		return
	}
	leafIssue(e, p, emit)
}

// propertiesPath turns a schema location such as
// #/properties/report/properties/versions/propertyNames into the document path
// report.versions. It reports false for any location that passes through
// something other than properties.
func propertiesPath(schemaURL string) (path, bool) {
	_, fragment, ok := strings.Cut(schemaURL, "#/")
	if !ok {
		return nil, false
	}
	segs := strings.Split(strings.TrimSuffix(fragment, "/propertyNames"), "/")
	if len(segs) == 0 || len(segs)%2 != 0 {
		return nil, false
	}
	p := path{}
	for i := 0; i < len(segs); i += 2 {
		if segs[i] != "properties" {
			return nil, false
		}
		p = p.key(segs[i+1])
	}
	return p, true
}

func propertyNameMessage(schemaURL, name string) string {
	if strings.Contains(schemaURL, "Budgets/") || strings.HasSuffix(schemaURL, "Budgets") {
		return fmt.Sprintf("unknown aggregation %q: use min, max, mean, median or a percentile from p1 to p99.9, such as p95", name)
	}
	if strings.Contains(schemaURL, "/definitions/env") {
		return fmt.Sprintf("invalid environment variable name %q: use letters, digits and underscores, not starting with a digit", name)
	}
	return fmt.Sprintf("invalid name %q: use letters, digits, '.', '_' and '-', starting with a letter or digit", name)
}

// definitionIssue emits the friendly message of a known schema definition. It
// reports whether it handled e.
func definitionIssue(e *jsonschema.ValidationError, p path, emit func(path, string, string)) bool {
	for _, d := range definitionMessages {
		if !strings.HasSuffix(e.SchemaURL, d.fragment) {
			continue
		}
		if typeMismatch(e, e.InstanceLocation) {
			if d.noun == "" {
				return false
			}
			emit(p, "expected "+d.noun, "")
			return true
		}
		if d.message == "" {
			return false
		}
		emit(p, d.message, d.hint)
		return true
	}
	return false
}

// branchesOfRightType drops the oneOf/anyOf branches whose only complaint is
// that the value has another JSON type: they say nothing about what is wrong.
func branchesOfRightType(e *jsonschema.ValidationError) []*jsonschema.ValidationError {
	var matching []*jsonschema.ValidationError
	for _, c := range e.Causes {
		if !typeMismatch(c, e.InstanceLocation) {
			matching = append(matching, c)
		}
	}
	return matching
}

func leafIssue(e *jsonschema.ValidationError, p path, emit func(path, string, string)) {
	switch k := e.ErrorKind.(type) {
	case *kind.AdditionalProperties:
		for _, name := range k.Properties {
			emit(p.key(name), fmt.Sprintf("unknown key %q", name),
				"check the spelling against https://nao1215.github.io/himorime/configuration/; unknown keys are rejected so a typo cannot silently change a benchmark")
		}
	case *kind.Required:
		for _, name := range k.Missing {
			emit(p.key(name), fmt.Sprintf("%s is required", name), "")
		}
	case *kind.Type:
		hint := ""
		if len(k.Want) == 1 && k.Want[0] == "string" {
			hint = "quote the value to make it a string"
		}
		emit(p, fmt.Sprintf("expected %s, got %s", jsonTypeWords(k.Want), jsonTypeWord(k.Got)), hint)
	case *kind.Pattern:
		emit(p, patternMessage(k), "")
	default:
		emit(p, rangeMessage(e.ErrorKind), "")
	}
}

func patternMessage(k *kind.Pattern) string {
	switch k.Want {
	case `^[A-Za-z0-9][A-Za-z0-9_.-]*$`:
		return fmt.Sprintf("invalid name %q: use letters, digits, '.', '_' and '-', starting with a letter or digit", k.Got)
	case `\S`:
		return "must not be blank"
	case `^[a-z0-9][a-z0-9-]*$`:
		return fmt.Sprintf("invalid section name %q: use lowercase letters, digits and '-', starting with a letter or digit", k.Got)
	default:
		return fmt.Sprintf("%q does not match the expected format", k.Got)
	}
}

func rangeMessage(ek jsonschema.ErrorKind) string {
	switch k := ek.(type) {
	case *kind.Enum:
		return "must be one of: " + enumWords(k.Want)
	case *kind.Const:
		return fmt.Sprintf("must be %v", k.Want)
	case *kind.MinItems:
		return fmt.Sprintf("must contain at least %s", plural(k.Want, "item"))
	case *kind.MinProperties:
		return fmt.Sprintf("must contain at least %s", plural(k.Want, "entry"))
	case *kind.MaxProperties:
		return fmt.Sprintf("must contain at most %s", plural(k.Want, "entry"))
	case *kind.MinLength:
		return "must not be empty"
	case *kind.Minimum:
		return fmt.Sprintf("must be at least %s, got %s", ratString(k.Want), ratString(k.Got))
	case *kind.Maximum:
		return fmt.Sprintf("must be at most %s, got %s", ratString(k.Want), ratString(k.Got))
	case *kind.ExclusiveMinimum:
		return fmt.Sprintf("must be greater than %s, got %s", ratString(k.Want), ratString(k.Got))
	default:
		return ek.LocalizedString(schemaPrinter)
	}
}

// typeMismatch reports whether every leaf of e below loc is a type error at
// loc itself: the value had the wrong JSON type for that branch altogether.
func typeMismatch(e *jsonschema.ValidationError, loc []string) bool {
	if len(e.Causes) == 0 {
		_, isType := e.ErrorKind.(*kind.Type)
		return isType && sameLoc(e.InstanceLocation, loc)
	}
	for _, c := range e.Causes {
		if !typeMismatch(c, loc) {
			return false
		}
	}
	return true
}

func sameLoc(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func instancePath(loc []string) path {
	p := path{}
	for i, seg := range loc {
		// The value of a report.versions entry is an argument list too.
		if n, ok := atoiStrict(seg); ok && i > 0 && (isArrayKey(loc[i-1]) || i > 2 && loc[i-2] == "versions" && loc[i-3] == "report") {
			p = p.index(n)
			continue
		}
		p = p.key(seg)
	}
	return p
}

// isArrayKey reports whether a key holds a list in the suite format, so the
// segment after it is an index rather than a mapping key such as a command
// named "1".
func isArrayKey(k string) bool {
	switch k {
	case "benchmarks", "tags", "setup", "prepare_each", "cleanup", "outputs", "exit_codes", "command", "commands_list":
		return true
	}
	return false
}

func atoiStrict(s string) (int, bool) {
	if s == "" || len(s) > 9 {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	if strings.HasSuffix(word, "y") {
		return fmt.Sprintf("%d %sies", n, strings.TrimSuffix(word, "y"))
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func ratString(r *big.Rat) string {
	if r == nil {
		return "?"
	}
	if r.IsInt() {
		return r.Num().String()
	}
	f, _ := r.Float64()
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func jsonTypeWords(want []string) string {
	words := make([]string, len(want))
	for i, w := range want {
		words[i] = jsonTypeWord(w)
	}
	return strings.Join(words, " or ")
}

func jsonTypeWord(t string) string {
	switch t {
	case "object":
		return "a mapping"
	case "array":
		return "a list"
	case "string":
		return "a string"
	case "integer":
		return "an integer"
	case "number":
		return "a number"
	case "boolean":
		return "true or false"
	case "null":
		return "an empty value"
	}
	return t
}

func enumWords(values []any) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprint(v)
	}
	return strings.Join(parts, ", ")
}
