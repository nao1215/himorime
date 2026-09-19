package prcomment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	md "github.com/nao1215/markdown"

	"github.com/nao1215/himorime/internal/report"
)

const (
	maxCommentBytes     = 60 * 1024
	maxCommentRows      = 50
	maxCommentPages     = 100
	maxAPIResponseBytes = 8 * 1024 * 1024
)

var commentMarkerV2RE = regexp.MustCompile(`^<!-- himorime:benchmark-report:v2 run_id=([0-9]+) workflow_id=([0-9]+) run_number=([0-9]+) run_attempt=([0-9]+) -->`)
var commentMarkerV1RE = regexp.MustCompile(`^<!-- himorime:benchmark-report:v1 workflow_id=([0-9]+) run_number=([0-9]+) run_attempt=([0-9]+) -->`)
var repositoryRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*/[A-Za-z0-9_.-]+$`)

// Env is the trusted GitHub Actions environment used by the reporter.
type Env struct {
	Actions    bool
	EventName  string
	EventPath  string
	Repository string
	APIURL     string
	Token      string
}

// FromLookup reads the reporter environment.
func FromLookup(lookup func(string) (string, bool)) Env {
	get := func(key string) string {
		value, _ := lookup(key)
		return value
	}
	return Env{
		Actions: get("GITHUB_ACTIONS") == "true", EventName: get("GITHUB_EVENT_NAME"),
		EventPath: get("GITHUB_EVENT_PATH"), Repository: get("GITHUB_REPOSITORY"),
		APIURL: get("GITHUB_API_URL"), Token: get("GITHUB_TOKEN"),
	}
}

// WorkflowRun is the trusted destination and source-run identity extracted
// from a workflow_run payload. None of these values comes from the report.
type WorkflowRun struct {
	Repository   string
	PullRequest  int
	WorkflowID   int64
	WorkflowName string
	RunID        int64
	RunNumber    int64
	RunAttempt   int64
	RunURL       string
	Conclusion   string
}

type workflowRunEvent struct {
	Action     string `json:"action"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	WorkflowRun *struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		WorkflowID int64  `json:"workflow_id"`
		RunNumber  int64  `json:"run_number"`
		RunAttempt int64  `json:"run_attempt"`
		Event      string `json:"event"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HTMLURL    string `json:"html_url"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		PullRequests []struct {
			Number int `json:"number"`
			Base   struct {
				Repo struct {
					FullName string `json:"full_name"`
				} `json:"repo"`
			} `json:"base"`
		} `json:"pull_requests"`
	} `json:"workflow_run"`
}

// ResolveWorkflowRun accepts only a completed source run of a pull_request
// workflow in the repository named by GITHUB_REPOSITORY.
func (e Env) ResolveWorkflowRun(readFile func(string) ([]byte, error)) (WorkflowRun, error) { //nolint:gocyclo // Each rejected field is an independent trust-boundary check.
	if !e.Actions || e.EventName != "workflow_run" {
		return WorkflowRun{}, errors.New("comment is only available in a GitHub Actions workflow_run event")
	}
	if e.EventPath == "" {
		return WorkflowRun{}, errors.New("GITHUB_EVENT_PATH is not set")
	}
	if !validRepository(e.Repository) {
		return WorkflowRun{}, errors.New("GITHUB_REPOSITORY must be an owner/repository name")
	}
	data, err := readFile(e.EventPath)
	if err != nil {
		return WorkflowRun{}, fmt.Errorf("read the workflow_run event payload: %w", err)
	}
	var ev workflowRunEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return WorkflowRun{}, fmt.Errorf("parse the workflow_run event payload: %w", err)
	}
	wr := ev.WorkflowRun
	if wr == nil || ev.Repository.FullName != e.Repository || wr.Repository.FullName != e.Repository {
		return WorkflowRun{}, errors.New("workflow_run repository does not match GITHUB_REPOSITORY")
	}
	if wr.Event != "pull_request" {
		return WorkflowRun{}, fmt.Errorf("source workflow event is %q, want pull_request", wr.Event)
	}
	if ev.Action != "completed" || wr.Status != "completed" || !validConclusion(wr.Conclusion) {
		return WorkflowRun{}, errors.New("workflow_run is not a completed source run")
	}
	if len(wr.PullRequests) != 1 {
		return WorkflowRun{}, fmt.Errorf("workflow_run is associated with %d pull requests, want exactly one", len(wr.PullRequests))
	}
	pr := wr.PullRequests[0]
	if pr.Number < 1 || pr.Base.Repo.FullName != e.Repository {
		return WorkflowRun{}, errors.New("workflow_run pull request does not belong to GITHUB_REPOSITORY")
	}
	if wr.ID < 1 || wr.WorkflowID < 1 || wr.RunNumber < 1 || wr.RunAttempt < 1 || strings.TrimSpace(wr.Name) == "" {
		return WorkflowRun{}, errors.New("workflow_run payload has incomplete run metadata")
	}
	u, err := url.Parse(wr.HTMLURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || strings.ContainsAny(wr.HTMLURL, "<> \t\r\n") {
		return WorkflowRun{}, errors.New("workflow_run payload has an invalid run URL")
	}
	return WorkflowRun{
		Repository: e.Repository, PullRequest: pr.Number, WorkflowID: wr.WorkflowID,
		WorkflowName: wr.Name, RunID: wr.ID, RunNumber: wr.RunNumber,
		RunAttempt: wr.RunAttempt, RunURL: wr.HTMLURL,
		Conclusion: wr.Conclusion,
	}, nil
}

func validConclusion(s string) bool {
	switch s {
	case "success", "failure", "cancelled", "timed_out", "action_required", "neutral", "skipped", "stale", "startup_failure":
		return true
	default:
		return false
	}
}

func validRepository(s string) bool {
	return repositoryRE.MatchString(s) && !strings.Contains(s, "..")
}

// CommentRenderOptions controls presentation of a pull request comment. The
// options are supplied by the trusted reporter workflow, never by the report.
type CommentRenderOptions struct {
	HideFooter bool
}

// RenderComment renders a bounded, escaped summary suitable for a pull
// request comment. Large reports are represented by at most maxCommentRows.
func RenderComment(r *report.Report, run WorkflowRun, opts CommentRenderOptions) (string, error) {
	marker := fmt.Sprintf("<!-- himorime:benchmark-report:v2 run_id=%d workflow_id=%d run_number=%d run_attempt=%d -->", run.RunID, run.WorkflowID, run.RunNumber, run.RunAttempt)
	var details bytes.Buffer
	if err := report.WriteMarkdown(&details, r); err != nil {
		return "", fmt.Errorf("render benchmark details: %w", err)
	}
	if body, err := buildComment(r, run, marker, details.String(), nil, 0, opts); err != nil {
		return "", err
	} else if len(body) <= maxCommentBytes {
		return body, nil
	}

	var noteworthy, passing [][]string
	total := 0
	for _, s := range r.Suites {
		for _, b := range s.Benchmarks {
			for _, c := range b.Commands {
				total++
				if c.Result == report.ResultPass {
					if len(passing) >= maxCommentRows {
						continue
					}
				} else if len(noteworthy) >= maxCommentRows {
					continue
				}
				row := []string{safeCell(s.Name), safeCell(b.Name), safeCell(c.Name), string(c.Result), safeCell(commandDetails(c))}
				if c.Result == report.ResultPass {
					passing = append(passing, row)
				} else {
					noteworthy = append(noteworthy, row)
				}
			}
		}
	}
	rows := append([][]string(nil), noteworthy...)
	if len(rows) < maxCommentRows {
		rows = append(rows, passing[:min(len(passing), maxCommentRows-len(rows))]...)
	}
	if len(rows) > maxCommentRows {
		rows = rows[:maxCommentRows]
	}
	body, err := buildComment(r, run, marker, "", rows, total, opts)
	if err != nil {
		return "", err
	}
	if len(body) > maxCommentBytes {
		return "", fmt.Errorf("rendered pull request comment is %d bytes, limit is %d", len(body), maxCommentBytes)
	}
	return body, nil
}

func buildComment(r *report.Report, run WorkflowRun, marker, details string, rows [][]string, total int, opts CommentRenderOptions) (string, error) {
	var out bytes.Buffer
	s := r.Summary
	verdict := commentVerdict(s)
	b := md.NewMarkdown(&out, md.WithBlockSpacing()).
		PlainText(marker).
		H2("himorime: " + verdict).
		PlainText(fmt.Sprintf("%d passed · %d improved · %d inconclusive · %d over budget · %d regressed · %d metric errors · %d errored",
			s.Pass, s.Improved, s.Inconclusive, s.OverBudget, s.Regression, s.MetricError, s.Error))
	if details != "" {
		b.Details("Benchmark details", details)
	} else if len(rows) > 0 {
		b.Table(md.TableSet{Header: []string{"Suite", "Benchmark", "Command", "Result", "Details"}, Rows: rows, EscapeCells: true})
	}
	if details == "" && total > len(rows) {
		b.PlainText(fmt.Sprintf("Showing %d of %d commands. The workflow artifact contains the complete JSON report.", len(rows), total))
	}
	b.PlainText(fmt.Sprintf("Source: %s, run %d (attempt %d): <%s> — %s", safeCell(run.WorkflowName), run.RunNumber, run.RunAttempt, run.RunURL, run.Conclusion))
	if !opts.HideFooter {
		b.PlainText("<sub>Generated by [himorime](https://github.com/nao1215/himorime)</sub>")
	}
	if err := b.Build(); err != nil {
		return "", fmt.Errorf("render pull request comment: %w", err)
	}
	return strings.ReplaceAll(out.String(), "@", "&#64;"), nil
}

func commentVerdict(s report.Summary) string {
	verdict := "No regression"
	if s.Error > 0 || s.MetricError > 0 {
		return "Measurement failed"
	} else if s.Regression > 0 && s.OverBudget > 0 {
		return "Regression and budget violation"
	} else if s.Regression > 0 {
		return "Performance regression"
	} else if s.OverBudget > 0 {
		return "Budget exceeded"
	} else if s.Inconclusive > 0 {
		return "Inconclusive comparison"
	}
	return verdict
}

func shortCell(s string) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	r := []rune(s)
	if len(r) > 120 {
		s = string(r[:119]) + "…"
	}
	return s
}

func safeCell(s string) string { return report.EscapeMarkdown(shortCell(s)) }

func commandDetails(c report.Command) string {
	var details []string
	keys := make([]string, 0, len(c.Comparisons))
	for name := range c.Comparisons {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		comparison := c.Comparisons[name]
		if comparison == nil || comparison.DerivedFrom != "" || comparison.Verdict == report.VerdictSkipped {
			continue
		}
		details = append(details, fmt.Sprintf("%s %+.1f%% (%s)", name, comparison.ChangePercent, comparison.Verdict))
	}
	for _, budget := range c.Budgets {
		if budget.Status == report.BudgetFail {
			details = append(details, fmt.Sprintf("%s %s budget failed", budget.Metric, budget.Aggregation))
		}
	}
	for _, measurement := range []*report.Measurement{c.Head, c.Base} {
		if measurement != nil && measurement.Error != nil {
			details = append(details, measurement.Error.Message)
			break
		}
	}
	if len(details) > 3 {
		details = append(details[:3], "more in artifact")
	}
	return strings.Join(details, "; ")
}

// CommentClient keeps one current himorime comment on a pull request.
type CommentClient struct {
	HTTP       *http.Client
	APIURL     string
	Token      string
	Repository string
}

// Upsert posts this run's result, then removes every owned himorime comment
// except the globally newest result. Posting before the final list makes
// concurrent workflow_run jobs converge without overwriting each other.
func (c CommentClient) Upsert(ctx context.Context, run WorkflowRun, body string) (stale bool, err error) {
	c, base, p, err := c.prepare(run)
	if err != nil {
		return false, err
	}
	incoming := marker{Version: 2, RunID: run.RunID, WorkflowID: run.WorkflowID, RunNumber: run.RunNumber, RunAttempt: run.RunAttempt}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", url.PathEscape(p[0]), url.PathEscape(p[1]), run.PullRequest)
	var created issueComment
	if err := c.send(ctx, apiEndpoint(base, path), http.MethodPost, map[string]string{"body": body}, &created); err != nil {
		return false, err
	}
	if created.ID < 1 {
		return false, errors.New("GitHub API returned a comment without an id")
	}
	comments, err := c.list(ctx, base, run.PullRequest)
	if err != nil {
		return false, err
	}
	owned := ownedComments(comments)
	if !hasCommentID(owned, created.ID) {
		created.Body = body
		created.User.Login, created.User.Type = "github-actions[bot]", "Bot"
		owned = append(owned, ownedComment{Comment: &created, Marker: incoming})
	}
	winner := newestOwned(owned)
	if winner == nil {
		return false, errors.New("posted himorime comment was not found")
	}
	if err := c.deleteOld(ctx, base, p, winner, owned); err != nil {
		return false, err
	}
	return winner.ID != created.ID, nil
}

func (c CommentClient) prepare(run WorkflowRun) (CommentClient, *url.URL, []string, error) {
	if c.HTTP == nil {
		c.HTTP = http.DefaultClient
	}
	if c.Token == "" {
		return c, nil, nil, errors.New("GITHUB_TOKEN is not set")
	}
	base, err := url.Parse(c.APIURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil {
		return c, nil, nil, errors.New("GITHUB_API_URL must be an https URL")
	}
	if c.Repository != run.Repository || !validRepository(c.Repository) {
		return c, nil, nil, errors.New("comment repository does not match the workflow_run target")
	}
	return c, base, strings.Split(c.Repository, "/"), nil
}

func ownedComments(comments []issueComment) []ownedComment {
	var owned []ownedComment
	for i := range comments {
		m, ok := parseMarker(comments[i].Body)
		if !ok || comments[i].User.Login != "github-actions[bot]" || comments[i].User.Type != "Bot" {
			continue
		}
		owned = append(owned, ownedComment{Comment: &comments[i], Marker: m})
	}
	return owned
}

func newestOwned(owned []ownedComment) *issueComment {
	var winner *ownedComment
	for _, old := range owned {
		if old.Marker.Version < 2 {
			continue
		}
		if winner == nil || newer(old.Marker, winner.Marker) || sameRun(old.Marker, winner.Marker) && old.Comment.ID > winner.Comment.ID {
			candidate := old
			winner = &candidate
		}
	}
	if winner == nil {
		return nil
	}
	return winner.Comment
}

func hasCommentID(owned []ownedComment, id int64) bool {
	for _, old := range owned {
		if old.Comment.ID == id {
			return true
		}
	}
	return false
}

func (c CommentClient) deleteOld(ctx context.Context, base *url.URL, repository []string, existing *issueComment, owned []ownedComment) error {
	for _, old := range owned {
		if existing != nil && old.Comment.ID == existing.ID {
			continue
		}
		deletePath := fmt.Sprintf("/repos/%s/%s/issues/comments/%d", url.PathEscape(repository[0]), url.PathEscape(repository[1]), old.Comment.ID)
		if err := c.send(ctx, apiEndpoint(base, deletePath), http.MethodDelete, nil, nil); err != nil {
			return fmt.Errorf("delete old himorime comment %d: %w", old.Comment.ID, err)
		}
	}
	return nil
}

type issueComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	User struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"user"`
}

func (c CommentClient) list(ctx context.Context, base *url.URL, pr int) ([]issueComment, error) {
	p := strings.Split(c.Repository, "/")
	var all []issueComment
	for page := 1; page <= maxCommentPages; page++ {
		u := apiEndpoint(base, fmt.Sprintf("/repos/%s/%s/issues/%d/comments", url.PathEscape(p[0]), url.PathEscape(p[1]), pr))
		q := u.Query()
		q.Set("per_page", "100")
		q.Set("page", strconv.Itoa(page))
		u.RawQuery = q.Encode()
		var got []issueComment
		if err := c.send(ctx, u, http.MethodGet, nil, &got); err != nil {
			return nil, err
		}
		all = append(all, got...)
		if len(got) < 100 {
			return all, nil
		}
	}
	return nil, errors.New("GitHub comment list exceeded 100 pages")
}

func apiEndpoint(base *url.URL, path string) *url.URL {
	u := *base
	u.Path = strings.TrimRight(base.Path, "/") + path
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return &u
}

func (c CommentClient) send(ctx context.Context, u *url.URL, method string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(payload); err != nil {
			return fmt.Errorf("encode GitHub API request: %w", err)
		}
		body = &buf
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return fmt.Errorf("build GitHub API request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub API request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
		return fmt.Errorf("GitHub API returned %s", resp.Status)
	}
	if out != nil {
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxAPIResponseBytes)).Decode(out); err != nil {
			return fmt.Errorf("decode GitHub API response: %w", err)
		}
	}
	return nil
}

type marker struct {
	Version    int
	RunID      int64
	WorkflowID int64
	RunNumber  int64
	RunAttempt int64
}

func parseMarker(body string) (marker, bool) {
	if match := commentMarkerV2RE.FindStringSubmatch(body); match != nil {
		values, ok := markerValues(match[1:])
		if !ok {
			return marker{}, false
		}
		return marker{Version: 2, RunID: values[0], WorkflowID: values[1], RunNumber: values[2], RunAttempt: values[3]}, true
	}
	match := commentMarkerV1RE.FindStringSubmatch(body)
	if match == nil {
		return marker{}, false
	}
	values, ok := markerValues(match[1:])
	if !ok {
		return marker{}, false
	}
	return marker{Version: 1, WorkflowID: values[0], RunNumber: values[1], RunAttempt: values[2]}, true
}

func markerValues(parts []string) ([]int64, bool) {
	values := make([]int64, len(parts))
	for i := range values {
		v, err := strconv.ParseInt(parts[i], 10, 64)
		if err != nil || v < 1 {
			return nil, false
		}
		values[i] = v
	}
	return values, true
}

func newer(a, b marker) bool {
	if a.Version < 2 {
		return false
	}
	return a.RunID > b.RunID || a.RunID == b.RunID && a.RunAttempt > b.RunAttempt
}

func sameRun(a, b marker) bool {
	return a.Version == 2 && b.Version == 2 && a.RunID == b.RunID && a.RunAttempt == b.RunAttempt
}

type ownedComment struct {
	Comment *issueComment
	Marker  marker
}
