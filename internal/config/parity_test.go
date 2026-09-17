package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// These tests keep schema/yahiko.schema.json and the Go loader in agreement.
// The loader validates against the embedded schema first, so Go can never
// accept a document the schema rejects. What remains to check is the other
// direction and the key inventory:
//
//   - every document in testdata/parity/valid is accepted by both;
//   - every document in testdata/parity/schema is rejected by the schema,
//     validated here with a compiler that reads the file from disk;
//   - every document in testdata/parity/semantic is accepted by the schema
//     but rejected by Go with the message named in its "# expect:" line —
//     these are the rules JSON Schema cannot express;
//   - the yaml keys of the Raw structs equal the schema's properties.

func diskSchema(tb testing.TB) *jsonschema.Schema {
	tb.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "schema", "yahiko.schema.json"))
	if err != nil {
		tb.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		tb.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("disk.json", doc); err != nil {
		tb.Fatal(err)
	}
	s, err := c.Compile("disk.json")
	if err != nil {
		tb.Fatal(err)
	}
	return s
}

func schemaAccepts(tb testing.TB, s *jsonschema.Schema, src []byte) bool {
	tb.Helper()
	inst, err := yamlInstance(src)
	if err != nil {
		return false
	}
	return s.Validate(inst) == nil
}

func corpus(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "parity", dir, "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no corpus in %s: %v", dir, err)
	}
	out := map[string][]byte{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out[f] = data
	}
	return out
}

func TestSchemaParityValid(t *testing.T) {
	t.Parallel()
	s := diskSchema(t)
	for name, src := range corpus(t, "valid") {
		if !schemaAccepts(t, s, src) {
			t.Errorf("%s: rejected by the schema", name)
		}
		if _, err := Parse(name, filepath.Join(t.TempDir(), "yahiko.yaml"), src); err != nil {
			t.Errorf("%s: rejected by Go: %v", name, err)
		}
	}
}

func TestSchemaParitySchemaInvalid(t *testing.T) {
	t.Parallel()
	s := diskSchema(t)
	for name, src := range corpus(t, "schema") {
		if schemaAccepts(t, s, src) {
			t.Errorf("%s: accepted by the schema, but it is in the schema-invalid corpus", name)
		}
		if _, err := Parse(name, filepath.Join(t.TempDir(), "yahiko.yaml"), src); !IsValidation(err) {
			t.Errorf("%s: Go did not reject it with a validation error: %v", name, err)
		}
	}
}

func TestSchemaParitySemanticInvalid(t *testing.T) {
	t.Parallel()
	s := diskSchema(t)
	for name, src := range corpus(t, "semantic") {
		first, _, _ := strings.Cut(string(src), "\n")
		want, ok := strings.CutPrefix(first, "# expect: ")
		if !ok {
			t.Fatalf("%s: the first line must be '# expect: <message>'", name)
		}
		if !schemaAccepts(t, s, src) {
			t.Errorf("%s: rejected by the schema; move it to testdata/parity/schema", name)
		}
		_, err := Parse(name, filepath.Join(t.TempDir(), "yahiko.yaml"), src)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: Go error = %v, want it to contain %q", name, err, want)
		}
	}
}

// schemaProperties returns the property names of a schema object.
func schemaProperties(t *testing.T, node map[string]any) []string {
	t.Helper()
	props, ok := node["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema node has no properties: %v", node["description"])
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func yamlKeys(v any) []string {
	rt := reflect.TypeOf(v)
	var keys []string
	for i := range rt.NumField() {
		tag := rt.Field(i).Tag.Get("yaml")
		name, _, _ := strings.Cut(tag, ",")
		if name != "" && name != "-" {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	return keys
}

func TestSchemaKeysMatchRawStructs(t *testing.T) {
	t.Parallel()
	var root map[string]any
	if err := json.Unmarshal(mustSchema(t), &root); err != nil {
		t.Fatal(err)
	}
	defs, _ := root["definitions"].(map[string]any)
	props, _ := root["properties"].(map[string]any)
	def := func(name string) map[string]any {
		d, ok := defs[name].(map[string]any)
		if !ok {
			t.Fatalf("definition %s missing", name)
		}
		return d
	}
	prop := func(name string) map[string]any {
		p, ok := props[name].(map[string]any)
		if !ok {
			t.Fatalf("property %s missing", name)
		}
		return p
	}
	report := prop("report")
	outputs := report["properties"].(map[string]any)["outputs"].(map[string]any)["items"].(map[string]any)
	stdinObject := def("benchmark")["properties"].(map[string]any)["stdin"].(map[string]any)["oneOf"].([]any)[1].(map[string]any)

	pairs := []struct {
		name   string
		schema map[string]any
		raw    any
		drop   []string
	}{
		{"top level", root, RawFile{}, nil},
		{"suite", prop("suite"), RawSuite{}, nil},
		{"defaults", prop("defaults"), RawDefaults{}, nil},
		{"exec", def("exec"), RawExec{}, nil},
		{"benchmark", def("benchmark"), RawBenchmark{}, nil},
		{"command", def("benchCommand"), RawCommand{}, nil},
		{"budget", def("budgetSet"), RawBudget{}, nil},
		{"regression (defaults)", def("regression"), RawRegression{}, []string{"commands"}},
		{"regression (benchmark)", def("regressionBenchmark"), RawRegression{}, nil},
		{"report", report, RawReport{}, nil},
		{"report output", outputs, RawOutput{}, nil},
		{"stdin", stdinObject, StdinSpec{}, nil},
	}
	for _, p := range pairs {
		want := yamlKeys(p.raw)
		if len(p.drop) > 0 {
			var kept []string
			for _, k := range want {
				if !contains(p.drop, k) {
					kept = append(kept, k)
				}
			}
			want = kept
		}
		got := schemaProperties(t, p.schema)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: schema keys %v, Go keys %v", p.name, got, want)
		}
		if ap, ok := p.schema["additionalProperties"].(bool); !ok || ap {
			t.Errorf("%s: the schema must set additionalProperties: false", p.name)
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// FuzzLoad feeds arbitrary documents to the loader. It must never panic, and
// it must never accept a document the on-disk schema rejects.
func FuzzLoad(f *testing.F) {
	for _, dir := range []string{"valid", "schema", "semantic"} {
		files, _ := filepath.Glob(filepath.Join("testdata", "parity", dir, "*.yaml"))
		for _, file := range files {
			if data, err := os.ReadFile(file); err == nil {
				f.Add(data)
			}
		}
	}
	s := diskSchema(f)
	f.Fuzz(func(t *testing.T, src []byte) {
		suite, err := Parse("fuzz.yaml", filepath.Join(os.TempDir(), "fuzz", "yahiko.yaml"), src)
		if err != nil {
			return
		}
		if suite == nil {
			t.Fatal("Parse returned neither a suite nor an error")
		}
		if !schemaAccepts(t, s, src) {
			t.Fatalf("Go accepted a document the schema rejects:\n%s", src)
		}
	})
}

// FuzzParseDuration checks the duration parser against its own pattern.
func FuzzParseDuration(f *testing.F) {
	for _, seed := range []string{"1s", "1.5ms", "1m30s", "10", "-1s", "µs", "24h", "99999999999999h"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d, err := ParseDuration(s)
		if err != nil {
			return
		}
		if !durationRE.MatchString(s) {
			t.Fatalf("ParseDuration accepted %q outside the schema pattern", s)
		}
		if d < 0 || d > maxDuration {
			t.Fatalf("ParseDuration(%q) = %v out of range", s, d)
		}
	})
}

// FuzzParseBudget checks budgets never produce a non-positive limit.
func FuzzParseBudget(f *testing.F) {
	for _, seed := range []string{"< 20ms", "<=1s", "< 0s", "20ms", ">1s"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		b, err := ParseBudget(s)
		if err != nil {
			return
		}
		if b.Limit <= 0 || !budgetRE.MatchString(s) {
			t.Fatalf("ParseBudget(%q) = %+v", s, b)
		}
	})
}

// FuzzExpand checks that expansion never panics and that a template without
// references is returned unchanged.
func FuzzExpand(f *testing.F) {
	for _, seed := range []string{"${root}/x", "$${x}", "${env:A}", "${", "plain"} {
		f.Add(seed)
	}
	vars := Vars{Artifact: "A", Root: "R", Workdir: "W", Exe: "E", LookupEnv: func(string) (string, bool) { return "V", true }}
	f.Fuzz(func(t *testing.T, s string) {
		out, err := Expand(s, vars, nil)
		if err != nil {
			return
		}
		if !strings.Contains(s, "$") && out != s {
			t.Fatalf("Expand(%q) = %q changed text without references", s, out)
		}
	})
}
