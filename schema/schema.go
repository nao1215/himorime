// Package schema embeds the published JSON Schemas.
//
// himorime.schema.json describes suite files. himorime validates every suite
// against it before decoding, so what an editor accepts through the YAML
// language server and what himorime accepts cannot drift apart.
// report.schema.json describes the JSON report.
package schema

import _ "embed"

// Suite is the JSON Schema of a suite file.
//
//go:embed himorime.schema.json
var Suite []byte

// Report is the JSON Schema of the JSON report.
//
//go:embed report.schema.json
var Report []byte

// SuiteURL is the $id of the suite schema, referenced by `himorime init`.
const SuiteURL = "https://raw.githubusercontent.com/nao1215/himorime/main/schema/himorime.schema.json"

// ReportURL is the $id of the report schema.
const ReportURL = "https://raw.githubusercontent.com/nao1215/himorime/main/schema/report.schema.json"
