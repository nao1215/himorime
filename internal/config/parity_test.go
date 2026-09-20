package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"github.com/nao1215/himorime/internal/metric"
)

// These tests keep schema/himorime.schema.json and the Go loader in agreement.
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
	data, err := os.ReadFile(filepath.Join("..", "..", "schema", "himorime.schema.json"))
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
		if _, err := Parse(name, filepath.Join(t.TempDir(), "himorime.yaml"), src); err != nil {
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
		if _, err := Parse(name, filepath.Join(t.TempDir(), "himorime.yaml"), src); !isValidation(err) {
			t.Errorf("%s: Go did not reject it with a validation error: %v", name, err)
		}
	}
}

func TestRemovedSyntaxSchemaParity(t *testing.T) {
	t.Parallel()
	s := diskSchema(t)
	base := `version: "1"
name: x
benchmarks:
  - name: a
    commands:
      tool:
        command: [tool]
`
	tests := []struct {
		name  string
		extra string
		field string
	}{
		{"suite object", "suite: {name: x}\n", "suite"},
		{"benchmark budget", "    budget: {tool: {median: \"< 20ms\"}}\n", "benchmarks[0].budget"},
		{"latency metric", "    metrics: {latency: true}\n", "benchmarks[0].metrics.latency"},
		{"cpu scope", "    metrics: {cpu: {scope: process_tree}}\n", "benchmarks[0].metrics.cpu"},
		{"memory scope", "    metrics: {memory: {scope: process_tree}}\n", "benchmarks[0].metrics.memory"},
		{"nested cpu budget", "        budget: {cpu: {total: {median: \"<= 1ms\"}}}\n", "benchmarks[0].commands.tool.budget.cpu"},
		{"nested memory budget", "        budget: {memory: {peak_rss: {max: \"<= 1MiB\"}}}\n", "benchmarks[0].commands.tool.budget.memory"},
		{"cpu regression", "    regression: {cpu: {max_percent: 5}}\n", "benchmarks[0].regression.cpu"},
		{"memory regression", "    regression: {memory: {max_percent: 5}}\n", "benchmarks[0].regression.memory"},
		{"latency metric setting", "    regression: {latency: {metric: mean}}\n", "benchmarks[0].regression.latency.metric"},
		{"cpu metric setting", "    regression: {cpu_total: {metric: mean}}\n", "benchmarks[0].regression.cpu_total.metric"},
		{"memory metric setting", "    regression: {peak_rss: {metric: mean}}\n", "benchmarks[0].regression.peak_rss.metric"},
		{"mean shorthand", "        budget: {mean: \"< 1s\"}\n", "benchmarks[0].commands.tool.budget.mean"},
		{"median shorthand", "        budget: {median: \"< 1s\"}\n", "benchmarks[0].commands.tool.budget.median"},
		{"min shorthand", "        budget: {min: \"< 1s\"}\n", "benchmarks[0].commands.tool.budget.min"},
		{"max shorthand", "        budget: {max: \"< 1s\"}\n", "benchmarks[0].commands.tool.budget.max"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := []byte(base + tt.extra)
			if schemaAccepts(t, s, src) {
				t.Fatal("disk schema accepted removed syntax")
			}
			_, err := Parse(tt.name, filepath.Join(t.TempDir(), "himorime.yaml"), src)
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Parse() error = %v, want ValidationError", err)
			}
			for _, issue := range verr.Issues {
				if issue.Field == tt.field {
					return
				}
			}
			t.Fatalf("Parse() fields did not include %q: %v", tt.field, err)
		})
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
		_, err := Parse(name, filepath.Join(t.TempDir(), "himorime.yaml"), src)
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
		{"defaults", prop("defaults"), RawDefaults{}, nil},
		{"exec", def("exec"), RawExec{}, nil},
		{"benchmark", def("benchmark"), RawBenchmark{}, nil},
		{"command", def("benchCommand"), RawCommand{}, nil},
		{"metrics", def("metrics"), RawMetrics{}, nil},
		{"metrics (defaults)", def("metricsDefaults"), RawMetrics{}, []string{"throughput"}},
		{"throughput", def("metrics")["properties"].(map[string]any)["throughput"].(map[string]any), RawThroughput{}, nil},
		{"work", def("work"), RawWork{}, nil},
		{"cpu regression", def("cpuRegression"), RawMetricRegression{}, nil},
		{"memory regression", def("memoryRegression"), RawMetricRegression{}, nil},
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

func TestSchemaValueConversionAndPaths(t *testing.T) {
	t.Parallel()
	if _, err := yamlInstance([]byte("[")); err == nil {
		t.Fatal("invalid YAML was accepted")
	}
	when := time.Date(2026, time.September, 20, 1, 2, 3, 4, time.UTC)
	got := jsonValue(map[any]any{
		"int": int(2), "int64": int64(3), "uint": uint64(4), "float": 1.25,
		"nan": math.NaN(), "time": when, "list": []any{true}, "raw": struct{ A int }{A: 1},
	})
	m, ok := got.(map[string]any)
	if !ok || m["int"] != json.Number("2") || m["int64"] != json.Number("3") || m["uint"] != json.Number("4") || m["float"] != json.Number("1.25") || m["nan"] != "NaN" || m["time"] != when.Format(time.RFC3339Nano) || m["list"].([]any)[0] != true || m["raw"] != "{1}" {
		t.Fatalf("jsonValue = %#v", got)
	}
	for _, tt := range []struct {
		url  string
		want string
		ok   bool
	}{
		{"https://example.test#/properties/report/properties/versions/propertyNames", "report.versions", true},
		{"#/properties/report/properties", "", false},
		{"#/properties/report/items/name", "", false},
		{"not-a-fragment", "", false},
	} {
		p, ok := propertiesPath(tt.url)
		if ok != tt.ok || (ok && p.String() != tt.want) {
			t.Errorf("propertiesPath(%q) = %q, %t; want %q, %t", tt.url, p, ok, tt.want, tt.ok)
		}
	}
	for _, tt := range []struct {
		url, want string
	}{
		{"#/definitions/env", "invalid environment variable name"},
		{"#/definitions/Budgets", "unknown aggregation"},
		{"#/definitions/other", "invalid name"},
	} {
		if got := propertyNameMessage(tt.url, "bad"); !strings.Contains(got, tt.want) {
			t.Errorf("propertyNameMessage(%q) = %q", tt.url, got)
		}
	}
	for _, tt := range []struct {
		in, want string
	}{
		{"benchmarks.0.tags.1", "benchmarks[0].tags[1]"},
		{"report.versions.jc.2", "report.versions.jc[2]"},
		{"commands.1", "commands.1"},
		{"commands.x", "commands.x"},
	} {
		if got := instancePath(strings.Split(tt.in, ".")).String(); got != tt.want {
			t.Errorf("instancePath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	for _, name := range []string{"benchmarks", "tags", "setup", "prepare_each", "cleanup", "outputs", "exit_codes", "command", "commands_list"} {
		if !isArrayKey(name) {
			t.Errorf("isArrayKey(%q) = false", name)
		}
	}
	if isArrayKey("commands") {
		t.Error("commands is a mapping, not an array")
	}
}

func TestSchemaMessageHelpers(t *testing.T) {
	t.Parallel()
	patterns := []struct{ want, got string }{
		{`invalid name`, patternMessage(&kind.Pattern{Want: `^[A-Za-z0-9][A-Za-z0-9_.-]*$`, Got: "bad!"})},
		{"must not be blank", patternMessage(&kind.Pattern{Want: `\S`, Got: ""})},
		{"invalid section name", patternMessage(&kind.Pattern{Want: `^[a-z0-9][a-z0-9-]*$`, Got: "Bad"})},
		{"does not match", patternMessage(&kind.Pattern{Want: `custom`, Got: "bad"})},
	}
	for _, tt := range patterns {
		if !strings.Contains(tt.got, tt.want) {
			t.Errorf("pattern message %q lacks %q", tt.got, tt.want)
		}
	}
	for _, tt := range []struct {
		kind jsonschema.ErrorKind
		want string
	}{
		{&kind.Enum{Want: []any{"a", "b"}}, "must be one of: a, b"},
		{&kind.Const{Want: "x"}, "must be x"},
		{&kind.MinItems{Want: 1}, "must contain at least 1 item"},
		{&kind.MinItems{Want: 2}, "must contain at least 2 items"},
		{&kind.MinProperties{Want: 1}, "must contain at least 1 entry"},
		{&kind.MaxProperties{Want: 2}, "must contain at most 2 entries"},
		{&kind.MinLength{Want: 1}, "must not be empty"},
		{&kind.Minimum{Want: big.NewRat(2, 1), Got: big.NewRat(1, 1)}, "must be at least 2, got 1"},
		{&kind.Maximum{Want: big.NewRat(2, 1), Got: big.NewRat(3, 1)}, "must be at most 2, got 3"},
		{&kind.ExclusiveMinimum{Want: big.NewRat(2, 1), Got: big.NewRat(3, 1)}, "must be greater than 2, got 3"},
	} {
		if got := rangeMessage(tt.kind); got != tt.want {
			t.Errorf("rangeMessage(%T) = %q, want %q", tt.kind, got, tt.want)
		}
	}
	for _, tt := range []struct {
		in   string
		want int
		ok   bool
	}{{"", 0, false}, {"1", 1, true}, {"123456789", 123456789, true}, {"1234567890", 0, false}, {"x", 0, false}} {
		if got, ok := atoiStrict(tt.in); got != tt.want || ok != tt.ok {
			t.Errorf("atoiStrict(%q) = %d, %t", tt.in, got, ok)
		}
	}
	if got := ratString(nil); got != "?" || ratString(big.NewRat(2, 1)) != "2" || ratString(big.NewRat(1, 2)) != "0.5" {
		t.Errorf("ratString results = %q", got)
	}
	if got := jsonTypeWords([]string{"string", "array", "null", "unknown"}); got != "a string or a list or an empty value or unknown" {
		t.Errorf("jsonTypeWords = %q", got)
	}
	if got := plural(1, "item"); got != "1 item" || plural(2, "item") != "2 items" || plural(2, "category") != "2 categories" {
		t.Errorf("plural results = %q", got)
	}
}

func TestFlatBudgetKeysMatchMetricDefinitions(t *testing.T) {
	t.Parallel()
	var root map[string]any
	if err := json.Unmarshal(mustSchema(t), &root); err != nil {
		t.Fatal(err)
	}
	defs := root["definitions"].(map[string]any)
	command := defs["benchCommand"].(map[string]any)
	props := command["properties"].(map[string]any)
	budget := props["budget"].(map[string]any)["properties"].(map[string]any)
	want := make([]string, 0, len(metric.Defs()))
	for _, d := range metric.Defs() {
		want = append(want, string(d.Name))
	}
	sort.Strings(want)
	got := make([]string, 0, len(budget))
	for name := range budget {
		got = append(got, name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("flat budget keys = %v, metric definitions = %v", got, want)
	}
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
		suite, err := Parse("fuzz.yaml", filepath.Join(os.TempDir(), "fuzz", "himorime.yaml"), src)
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
		if !regexp.MustCompile(metric.DurationPattern).MatchString(s) {
			t.Fatalf("ParseDuration accepted %q outside the schema pattern", s)
		}
		if d < 0 || d > metric.MaxDuration {
			t.Fatalf("ParseDuration(%q) = %v out of range", s, d)
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
