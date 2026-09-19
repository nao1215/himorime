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
	"strconv"
	"strings"
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
		ID       int64  `json:"id"`
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
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		} `json:"repository"`
		PullRequests []struct {
			Number int `json:"number"`
			Base   struct {
				Repo struct {
					ID int64 `json:"id"`
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
	if wr == nil || ev.Repository.ID < 1 || wr.Repository.ID != ev.Repository.ID || ev.Repository.FullName != e.Repository || wr.Repository.FullName != e.Repository {
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
	// Pull request base repositories use GitHub's slim shape, without full_name.
	if pr.Number < 1 || pr.Base.Repo.ID != ev.Repository.ID {
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
