// Package docgen generates the parts of the documentation that restate the
// code: the command reference, the exit code table, the configuration field
// reference and defaults, and the example suites embedded in the Cookbook and
// README. Each generated part sits between markers in a Markdown file:
//
//	<!-- BEGIN GENERATED: commands -->
//	...
//	<!-- END GENERATED: commands -->
//
//	<!-- example: examples/startup/yahiko.yaml -->
//	```yaml
//	...
//	```
//
// TestGeneratedDocsInSync fails when a file differs from what Sync would
// write; `make docs` rewrites them.
package docgen

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nao1215/yahiko/internal/cli"
	"github.com/nao1215/yahiko/internal/config"
	"github.com/nao1215/yahiko/internal/exitcode"
	"github.com/nao1215/yahiko/schema"
)

var (
	generatedRE = regexp.MustCompile(`(?s)(<!-- BEGIN GENERATED: ([a-z-]+) -->\n)(.*?)(<!-- END GENERATED: ([a-z-]+) -->)`)
	exampleRE   = regexp.MustCompile("(?s)(<!-- example: ([^ ]+) -->\n)```yaml\n(.*?)```\n")
)

// Sync returns the content of the Markdown file at path with every generated
// section and example block brought up to date. root is the repository root.
func Sync(root, path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: documentation files of this repository
	if err != nil {
		return "", err
	}
	text := string(data)
	var errs []error
	text = generatedRE.ReplaceAllStringFunc(text, func(block string) string {
		m := generatedRE.FindStringSubmatch(block)
		if m[2] != m[5] {
			errs = append(errs, fmt.Errorf("%s: section %q ends with %q", path, m[2], m[5]))
			return block
		}
		body, err := Section(m[2])
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			return block
		}
		return m[1] + body + m[4]
	})
	text = exampleRE.ReplaceAllStringFunc(text, func(block string) string {
		m := exampleRE.FindStringSubmatch(block)
		example, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(m[2]))) //nolint:gosec // G304: example paths written in this repository's docs
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: example %s: %w", path, m[2], err))
			return block
		}
		return m[1] + "```yaml\n" + string(example) + "```\n"
	})
	return text, errors.Join(errs...)
}

// Section renders one generated section by name.
func Section(name string) (string, error) {
	switch name {
	case "commands":
		return commands(), nil
	case "exit-codes":
		return exitCodes(), nil
	case "config-reference":
		return configReference()
	case "defaults":
		return defaults(), nil
	case "variables":
		return variables(), nil
	}
	return "", fmt.Errorf("unknown generated section %q", name)
}

func commands() string {
	var sb strings.Builder
	for _, c := range cli.Commands() {
		fmt.Fprintf(&sb, "### %s\n\n", c.Name)
		fmt.Fprintf(&sb, "```text\n%s\n```\n\n", c.Usage)
		sb.WriteString(c.Long + "\n\n")
		fs := flag.NewFlagSet(c.Name, flag.ContinueOnError)
		c.Flags(fs)
		docs := cli.FlagDocs(fs)
		if len(docs) == 0 {
			continue
		}
		sb.WriteString("| Flag | Description |\n|---|---|\n")
		for _, f := range docs {
			fmt.Fprintf(&sb, "| `%s` | %s |\n", f.Synopsis, escapeCell(f.Usage))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func exitCodes() string {
	var sb strings.Builder
	sb.WriteString("| Code | Name | Meaning |\n|---|---|---|\n")
	for _, c := range exitcode.All() {
		fmt.Fprintf(&sb, "| `%d` | %s | %s |\n", c.Code, c.Name, escapeCell(c.Meaning))
	}
	return sb.String()
}

func defaults() string {
	rows := [][2]string{
		{"warmup", fmt.Sprint(config.DefaultWarmup)},
		{"runs", "unset (adaptive)"},
		{"min_runs", fmt.Sprint(config.DefaultMinRuns)},
		{"max_runs", fmt.Sprint(config.DefaultMaxRuns)},
		{"min_time", duration(config.DefaultMinTime)},
		{"timeout (commands)", duration(config.DefaultTimeout)},
		{"timeout (setup, prepare_each, cleanup)", duration(config.DefaultHookTimeout)},
		{"timeout (build)", duration(config.DefaultBuildTimeout)},
		{"stdout, stderr", config.OutputDiscard},
		{"exit_codes", "[0]"},
		{"regression.metric", string(config.DefaultMetric)},
		{"regression.max_percent", fmt.Sprint(config.DefaultMaxPercent)},
		{"regression.confidence", fmt.Sprint(config.DefaultConfidence)},
		{"regression.min_samples", fmt.Sprint(config.DefaultMinSamples)},
		{"regression.max_cv", fmt.Sprint(config.DefaultMaxCV)},
	}
	var sb strings.Builder
	sb.WriteString("| Setting | Default |\n|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&sb, "| `%s` | `%s` |\n", r[0], r[1])
	}
	return sb.String()
}

func duration(d time.Duration) string {
	s := d.String()
	s = strings.TrimSuffix(s, "0s")
	s = strings.TrimSuffix(s, "0m")
	return s
}

func variables() string {
	rows := [][2]string{
		{"${artifact}", "The file the build step writes. In a comparison each revision has its own. Only available when the suite has a build section. On Windows it ends in .exe."},
		{"${root}", "The directory holding the suite file, inside the tree being measured: the working tree, or the temporary worktree of the base revision."},
		{"${workdir}", "A fresh, empty directory created for each benchmark (and each revision in a comparison) and removed after cleanup. Not available in build."},
		{"${exe}", "`.exe` on Windows, empty elsewhere."},
		{"${env:NAME}", "The environment variable NAME. A variable that is not set is an execution error."},
		{"$${", "A literal `${`."},
	}
	var sb strings.Builder
	sb.WriteString("| Variable | Expands to |\n|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&sb, "| `%s` | %s |\n", r[0], r[1])
	}
	return sb.String()
}

// configReference renders one table per object of the suite schema, from the
// descriptions in schema/yahiko.schema.json, keeping the schema's key order.
func configReference() (string, error) {
	var root map[string]any
	if err := json.Unmarshal(schema.Suite, &root); err != nil {
		return "", err
	}
	defs, _ := root["definitions"].(map[string]any)
	sections := []struct {
		title   string
		pointer []string
	}{
		{"Top level", nil},
		{"suite", []string{"properties", "suite"}},
		{"defaults", []string{"properties", "defaults"}},
		{"build, setup, prepare_each, cleanup", []string{"definitions", "exec"}},
		{"benchmarks[]", []string{"definitions", "benchmark"}},
		{"benchmarks[].commands.NAME", []string{"definitions", "benchCommand"}},
		{"benchmarks[].budget.NAME", []string{"definitions", "budgetSet"}},
		{"regression", []string{"definitions", "regressionBenchmark"}},
		{"report", []string{"properties", "report"}},
	}
	var sb strings.Builder
	for _, sec := range sections {
		node := root
		for _, key := range sec.pointer {
			node = asMap(asMap(node)[key])
		}
		if node == nil {
			return "", fmt.Errorf("schema section %s is missing", sec.title)
		}
		keys, err := propertyOrder(append(append([]string{}, sec.pointer...), "properties"))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&sb, "### %s\n\n", sec.title)
		required := map[string]bool{}
		if req, ok := node["required"].([]any); ok {
			for _, r := range req {
				required[fmt.Sprint(r)] = true
			}
		}
		p := asMap(node["properties"])
		sb.WriteString("| Key | Required | Description |\n|---|---|---|\n")
		for _, k := range keys {
			req := ""
			if required[k] {
				req = "yes"
			}
			fmt.Fprintf(&sb, "| `%s` | %s | %s |\n", k, req, escapeCell(describe(asMap(p[k]), defs)))
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// propertyOrder returns the keys of the JSON object at pointer inside the
// suite schema in the order they are written. encoding/json maps lose that
// order, so the object is walked token by token.
func propertyOrder(pointer []string) ([]string, error) {
	raw := json.RawMessage(schema.Suite)
	for _, key := range pointer {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, err
		}
		next, ok := obj[key]
		if !ok {
			return nil, fmt.Errorf("schema has no %s", strings.Join(pointer, "/"))
		}
		raw = next
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := tok.(string)
		keys = append(keys, key)
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

func describe(node, defs map[string]any) string {
	if d, ok := node["description"].(string); ok && d != "" {
		return d
	}
	if ref, ok := node["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/definitions/")
		if d, ok := asMap(defs[name])["description"].(string); ok {
			return d
		}
	}
	return ""
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func escapeCell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ")
}
