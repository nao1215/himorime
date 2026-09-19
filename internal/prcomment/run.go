// Package prcomment validates and publishes benchmark reports from a trusted
// GitHub Actions workflow_run job.
package prcomment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/nao1215/himorime/internal/diag"
	"github.com/nao1215/himorime/internal/exitcode"
	"github.com/nao1215/himorime/internal/report"
	"github.com/nao1215/himorime/schema"
)

const maxReportBytes = 16 * 1024 * 1024

// Dependencies are the external operations used by Main.
type Dependencies struct {
	LookupEnv func(string) (string, bool)
	ReadFile  func(string) ([]byte, error)
	HTTP      *http.Client
}

// Main runs the helper command and returns its process exit status.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	fs := flag.NewFlagSet("comment", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	hideFooter := fs.Bool("hide-footer", false, "omit the himorime attribution footer from the pull request comment")
	fs.Usage = func() {}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			writeHelp(stdout)
			return exitcode.OK
		}
		diag.Print(stderr, exitcode.Usage, "himorime comment: %v", err)
		return exitcode.Usage
	}
	if fs.NArg() != 1 {
		diag.Print(stderr, exitcode.Usage, "himorime comment: exactly one REPORT.json is required")
		return exitcode.Usage
	}
	if deps.LookupEnv == nil {
		deps.LookupEnv = os.LookupEnv
	}
	if deps.ReadFile == nil {
		deps.ReadFile = os.ReadFile
	}
	if deps.HTTP == nil {
		deps.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	env := FromLookup(deps.LookupEnv)
	run, err := env.ResolveWorkflowRun(deps.ReadFile)
	if err != nil {
		diag.Print(stderr, exitcode.Usage, "himorime comment: %v", err)
		return exitcode.Usage
	}
	rep, err := readJSONReport(fs.Arg(0))
	if err != nil {
		diag.Print(stderr, exitcode.Config, "himorime comment: invalid report: %v", err)
		return exitcode.Config
	}
	body, err := RenderComment(rep, run, CommentRenderOptions{HideFooter: *hideFooter})
	if err != nil {
		diag.Print(stderr, exitcode.Execution, "himorime comment: %v", err)
		return exitcode.Execution
	}
	client := CommentClient{HTTP: deps.HTTP, APIURL: env.APIURL, Token: env.Token, Repository: env.Repository}
	stale, err := client.Upsert(ctx, run, body)
	if err != nil {
		diag.Print(stderr, exitcode.Execution, "himorime comment: %v", err)
		return exitcode.Execution
	}
	if stale {
		fmt.Fprintf(stdout, "himorime: skipped stale run %d; pull request #%d already has a newer result\n", run.RunNumber, run.PullRequest)
	} else {
		fmt.Fprintf(stdout, "himorime: posted pull request #%d from run %d\n", run.PullRequest, run.RunNumber)
	}
	return exitcode.OK
}

func writeHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage: himorime comment [--hide-footer] REPORT.json")
	fmt.Fprintln(w, "\nReads a JSON report produced by himorime, posts the latest pull request comment, and removes older himorime comments.")
	fmt.Fprintln(w, "\nFlags:\n  --hide-footer\n      omit the himorime attribution footer from the pull request comment")
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
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return nil, errors.New("contains more than one JSON value")
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read trailing JSON: %w", err)
	}
	return &rep, nil
}
