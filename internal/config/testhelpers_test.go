package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nao1215/himorime/schema"
)

func isValidation(err error) bool {
	var v *ValidationError
	return errors.As(err, &v)
}

func mustSchema(t *testing.T) []byte {
	t.Helper()
	return schema.Suite
}

// parseString loads suite source written to a temporary himorime.yaml.
func parseString(t *testing.T, src string) (*Suite, error) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "himorime.yaml")
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(p)
}

func mustParse(t *testing.T, src string) *Suite {
	t.Helper()
	s, err := parseString(t, src)
	if err != nil {
		t.Fatalf("Load() failed:\n%v\nsource:\n%s", err, src)
	}
	return s
}

// minimal is a valid suite; tests append benchmark keys through a template.
func minimal(benchmarkExtra string) string {
	return `version: "1"
name: test
benchmarks:
  - name: bench
    commands:
      tool:
        command: [tool, --version]
` + indent(benchmarkExtra, "    ")
}

func minimalCommandBudget(budget string) string {
	return addCommandBudget(minimal(""), budget)
}

func addCommandBudget(src, budget string) string {
	return strings.Replace(src, "        command: [tool, --version]\n", "        command: [tool, --version]\n        budget: "+budget+"\n", 1)
}

func indent(s, prefix string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n") + "\n"
}
