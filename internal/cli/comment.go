package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/nao1215/himorime/internal/exitcode"
	"github.com/nao1215/himorime/internal/ghactions"
	"github.com/nao1215/himorime/internal/report"
	"github.com/nao1215/himorime/schema"
)

const maxReportBytes = 16 * 1024 * 1024

func runComment(ctx context.Context, a *App, args []string) int {
	fs, settings := newFlagSet("comment")
	flags, _ := settings.(*commentFlags)
	operands, code, ok := parseFlags(fs, args, a.Stdout, a.Stderr)
	if !ok {
		return code
	}
	if len(operands) != 1 {
		fmt.Fprintln(a.Stderr, "himorime comment: exactly one REPORT.json is required")
		return exitcode.Usage
	}
	gh := ghactions.FromLookup(a.LookupEnv)
	run, err := gh.ResolveWorkflowRun(a.ReadFile)
	if err != nil {
		fmt.Fprintf(a.Stderr, "himorime comment: %v\n", err)
		return exitcode.Usage
	}
	rep, err := readJSONReport(operands[0])
	if err != nil {
		fmt.Fprintf(a.Stderr, "himorime comment: invalid report: %v\n", err)
		return exitcode.Config
	}
	body, err := ghactions.RenderComment(rep, run, ghactions.CommentRenderOptions{HideFooter: flags.hideFooter})
	if err != nil {
		fmt.Fprintf(a.Stderr, "himorime comment: %v\n", err)
		return exitcode.Execution
	}
	client := ghactions.CommentClient{HTTP: a.HTTPClient, APIURL: gh.APIURL, Token: gh.Token, Repository: gh.Repository}
	stale, err := client.Upsert(ctx, run, body)
	if err != nil {
		fmt.Fprintf(a.Stderr, "himorime comment: %v\n", err)
		return exitcode.Execution
	}
	if stale {
		fmt.Fprintf(a.Stdout, "himorime: skipped stale run %d; pull request #%d already has a newer result\n", run.RunNumber, run.PullRequest)
	} else {
		fmt.Fprintf(a.Stdout, "himorime: updated pull request #%d from run %d\n", run.PullRequest, run.RunNumber)
	}
	return exitcode.OK
}

func readJSONReport(path string) (*report.Report, error) {
	f, err := os.Open(path) //nolint:gosec // the report path is a command-line argument
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxReportBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > maxReportBytes {
		return nil, fmt.Errorf("%s exceeds the %d-byte limit", path, maxReportBytes)
	}
	var version struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &version); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	if version.SchemaVersion != report.SchemaVersion {
		return nil, fmt.Errorf("schema_version is %q, want %q", version.SchemaVersion, report.SchemaVersion)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema.Report))
	if err != nil {
		return nil, fmt.Errorf("load embedded report schema: %w", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schema.ReportURL, doc); err != nil {
		return nil, fmt.Errorf("load embedded report schema: %w", err)
	}
	s, err := c.Compile(schema.ReportURL)
	if err != nil {
		return nil, fmt.Errorf("compile embedded report schema: %w", err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	if err := s.Validate(instance); err != nil {
		return nil, fmt.Errorf("does not match schema: %w", err)
	}
	var rep report.Report
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&rep); err != nil {
		return nil, fmt.Errorf("decode report: %w", err)
	}
	if err := ensureEOF(dec); err != nil {
		return nil, err
	}
	return &rep, nil
}

func ensureEOF(dec *json.Decoder) error {
	var extra any
	err := dec.Decode(&extra)
	if err == nil {
		return errors.New("contains more than one JSON value")
	}
	if !errors.Is(err, io.EOF) {
		return fmt.Errorf("read trailing JSON: %w", err)
	}
	return nil
}
