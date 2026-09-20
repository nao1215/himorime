package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/nao1215/himorime/schema"
)

var (
	savedSchemaOnce sync.Once
	savedSchema     *jsonschema.Schema
	errSavedSchema  error
)

func savedReportSchema() (*jsonschema.Schema, error) {
	savedSchemaOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema.Report))
		if err != nil {
			errSavedSchema = fmt.Errorf("parse embedded report schema: %w", err)
			return
		}
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource(schema.ReportURL, doc); err != nil {
			errSavedSchema = fmt.Errorf("load embedded report schema: %w", err)
			return
		}
		savedSchema, errSavedSchema = compiler.Compile(schema.ReportURL)
	})
	return savedSchema, errSavedSchema
}

// Read validates and decodes one saved JSON report. Validation uses the
// schema embedded in the binary, so rendering never silently accepts a
// report from an incompatible format.
func Read(data []byte) (*Report, error) {
	var version struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &version); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if version.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("unsupported report schema version %q (supported version is %q)", version.SchemaVersion, SchemaVersion)
	}
	sch, err := savedReportSchema()
	if err != nil {
		return nil, err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if err := sch.Validate(instance); err != nil {
		return nil, fmt.Errorf("report does not match schema: %w", err)
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("decode report: %w", err)
	}
	return &r, nil
}
