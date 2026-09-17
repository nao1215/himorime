package config

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestLoadReportSectionsAndVersions(t *testing.T) {
	t.Parallel()
	s := mustParse(t, minimal("")+`report:
  outputs:
    - {format: markdown, path: docs/compare.md, section: speed-1}
    - {format: json, path: out.json}
  versions:
    zeta: [zeta, --version]
    jc: [jc, --version]
    jo.v2: ["${root}/bin/jo", -v]
`)
	want := []Output{{Format: FormatMarkdown, Path: "docs/compare.md", Section: "speed-1"}, {Format: FormatJSON, Path: "out.json"}}
	if !reflect.DeepEqual(s.Outputs, want) {
		t.Errorf("outputs = %+v", s.Outputs)
	}
	tools := []ToolVersion{
		{Name: "zeta", Argv: []string{"zeta", "--version"}},
		{Name: "jc", Argv: []string{"jc", "--version"}},
		{Name: "jo.v2", Argv: []string{"${root}/bin/jo", "-v"}},
	}
	if !reflect.DeepEqual(s.Versions, tools) {
		t.Errorf("versions = %+v, want the order of the file", s.Versions)
	}
	if s := mustParse(t, minimal("")); len(s.Versions) != 0 {
		t.Errorf("versions without report.versions = %+v", s.Versions)
	}
}

func TestLoadRejectsReportSectionsAndVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		report string
		want   string
		field  string
	}{
		{"section with json", "outputs: [{format: json, path: x.json, section: bench}]", "section is only allowed with format: markdown", "report.outputs[0].section"},
		{"uppercase section", "outputs: [{format: markdown, path: x.md, section: Bench}]", `invalid section name "Bench"`, "report.outputs[0].section"},
		{"empty section", "outputs: [{format: markdown, path: x.md, section: \"\"}]", `invalid section name ""`, "report.outputs[0].section"},
		{"bad tool name", "versions: {\"-jc\": [jc, --version]}", `invalid name "-jc"`, "report.versions.-jc"},
		{"empty version command", "versions: {jc: []}", "expected a list of arguments", "report.versions.jc"},
		{"string version command", "versions: {jc: \"jc --version\"}", "expected a list of arguments", "report.versions.jc"},
		{"empty program", "versions: {jc: [\"\"]}", "must not be empty", "report.versions.jc[0]"},
		{"workdir in a version command", "versions: {jc: [\"${workdir}/jc\"]}", "${workdir} is not available in report.versions", "report.versions.jc[0]"},
		{"artifact in a version command", "versions: {jc: [\"${artifact}\"]}", "${artifact} is not available in report.versions", "report.versions.jc[0]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseString(t, minimal("")+"report:\n"+indent(tt.report, "  "))
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Load() error = %v, want a ValidationError", err)
			}
			found := false
			for _, is := range verr.Issues {
				if strings.Contains(is.Message, tt.want) && is.Field == tt.field {
					found = true
				}
			}
			if !found {
				t.Fatalf("no issue with message %q at %q; issues:\n%v", tt.want, tt.field, err)
			}
		})
	}
}
