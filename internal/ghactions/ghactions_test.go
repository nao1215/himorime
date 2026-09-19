package ghactions

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nao1215/himorime/internal/report"
)

func TestWriteReportDestinations(t *testing.T) {
	for _, mode := range []string{"local", "summary", "no summary", "unwritable summary"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "summary.md")
			values := map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_SERVER_URL": "https://github.com", "GITHUB_REPOSITORY": "octo/bench", "GITHUB_RUN_ID": "123"}
			if mode == "local" {
				values["GITHUB_ACTIONS"] = "false"
			}
			if mode == "summary" {
				values["GITHUB_STEP_SUMMARY"] = path
			}
			if mode == "unwritable summary" {
				values["GITHUB_STEP_SUMMARY"] = filepath.Dir(path)
			}
			r := &report.Report{HimorimeVersion: "test", Suites: []report.Suite{{Name: "::error:: hostile", Error: &report.Error{Message: "::warning:: example\nsecond line"}}}}
			var log bytes.Buffer
			err := env(values).WriteReport(&log, r)
			if (err != nil) != (mode == "unwritable summary") {
				t.Fatalf("error = %v", err)
			}
			if mode == "local" {
				if log.Len() != 0 {
					t.Fatalf("local output = %q", log.String())
				}
				return
			}
			if !strings.Contains(log.String(), "https://github.com/octo/bench/actions/runs/123") {
				t.Fatal("missing Actions run URL")
			}
			lines := strings.Split(strings.TrimSpace(log.String()), "\n")
			if !strings.HasPrefix(lines[0], "::stop-commands::") {
				t.Fatal("workflow commands not disabled")
			}
			token := strings.TrimPrefix(lines[0], "::stop-commands::")
			if lines[len(lines)-1] != "::"+token+"::" {
				t.Fatal("workflow commands not restored")
			}
			body := strings.Join(lines[1:len(lines)-1], "\n") + "\n"
			for _, line := range strings.Split(body, "\n") {
				if line != "" && (!strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|")) {
					t.Fatalf("not a table: %s", line)
				}
			}
			if !strings.Contains(body, "Actions run") || !strings.Contains(body, "second line") {
				t.Fatal(body)
			}
			if mode == "summary" {
				data, err := os.ReadFile(path)
				if err != nil || strings.TrimSpace(string(data)) != strings.TrimSpace(body) {
					t.Fatalf("summary = %q, err = %v", data, err)
				}
			}
		})
	}
}

func TestReportingURLRejectsInvalidEnvironment(t *testing.T) {
	for _, tt := range []struct{ server, repo, run string }{
		{"", "octo/bench", "1"}, {"javascript:alert(1)", "octo/bench", "1"},
		{"https://user@github.com", "octo/bench", "1"}, {"https://github.com?x=y", "octo/bench", "1"},
		{"https://github.com", "../bench", "1"}, {"https://github.com", "octo/bench/extra", "1"},
		{"https://github.com", "octo/[link]", "1"}, {"https://github.com", "octo/bench", "0"},
		{"https://github.com", "octo/bench", "abc"},
	} {
		e := env(map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_SERVER_URL": tt.server, "GITHUB_REPOSITORY": tt.repo, "GITHUB_RUN_ID": tt.run})
		var log bytes.Buffer
		err := e.WriteReport(&log, &report.Report{})
		if err != nil || strings.Contains(log.String(), "Actions run") {
			t.Fatalf("environment=%+v err=%v", tt, err)
		}
	}
}

const (
	baseSHA   = "1111111111111111111111111111111111111111"
	beforeSHA = "2222222222222222222222222222222222222222"
)

func env(values map[string]string) Env {
	return FromLookup(func(k string) (string, bool) {
		v, ok := values[k]
		return v, ok
	})
}

func files(content string) func(string) ([]byte, error) {
	return func(string) ([]byte, error) { return []byte(content), nil }
}

func TestFromLookup(t *testing.T) {
	t.Parallel()
	e := env(map[string]string{
		"GITHUB_ACTIONS": "true", "GITHUB_EVENT_NAME": "pull_request", "GITHUB_EVENT_PATH": "/e.json",
		"GITHUB_STEP_SUMMARY": "/s.md", "RUNNER_OS": "Linux", "RUNNER_ARCH": "X64",
	})
	if !e.Actions || e.EventName != "pull_request" || e.EventPath != "/e.json" || e.StepSummary != "/s.md" || e.RunnerOS != "Linux" || e.RunnerArch != "X64" {
		t.Fatalf("Env = %+v", e)
	}
	if env(map[string]string{"GITHUB_ACTIONS": "1"}).Actions {
		t.Fatal("only GITHUB_ACTIONS=true means GitHub Actions")
	}
}

func TestResolveBase(t *testing.T) {
	t.Parallel()
	actions := func(event string) Env {
		return env(map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_EVENT_NAME": event, "GITHUB_EVENT_PATH": "/event.json"})
	}
	tests := []struct {
		name    string
		env     Env
		payload string
		sha     string
		source  string
		err     error
		message string
	}{
		{"pull_request", actions("pull_request"), `{"pull_request":{"base":{"sha":"` + baseSHA + `"}}}`, baseSHA, "pull_request.base.sha", nil, ""},
		{"review", actions("pull_request_review"), `{"pull_request":{"base":{"sha":"` + baseSHA + `"}}}`, baseSHA, "pull_request.base.sha", nil, ""},
		{"merge_group", actions("merge_group"), `{"merge_group":{"base_sha":"` + baseSHA + `"}}`, baseSHA, "merge_group.base_sha", nil, ""},
		{"push", actions("push"), `{"before":"` + beforeSHA + `"}`, beforeSHA, "push before", nil, ""},
		{"push creating a branch", actions("push"), `{"before":"0000000000000000000000000000000000000000"}`, "", "", ErrNoBase, "no base commit"},
		{"pull_request_target is refused", actions("pull_request_target"), `{"pull_request":{"base":{"sha":"` + baseSHA + `"}}}`, "", "", ErrUnsafeEvent, "trigger the workflow on pull_request"},
		{"schedule has no base", actions("schedule"), `{}`, "", "", ErrNoBase, "--against"},
		{"not in actions", env(map[string]string{}), ``, "", "", ErrNoBase, "HIMORIME_BASE_REF"},
		{"broken payload", actions("pull_request"), `{`, "", "", ErrNoBase, "parse the event payload"},
		{"payload without pull request", actions("pull_request"), `{}`, "", "", ErrNoBase, "no base commit"},
		{"malformed sha", actions("pull_request"), `{"pull_request":{"base":{"sha":"main; rm -rf /"}}}`, "", "", ErrNoBase, "no base commit"},
		{"no event path", env(map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_EVENT_NAME": "pull_request"}), `{}`, "", "", ErrNoBase, "GITHUB_EVENT_PATH"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base, err := tt.env.ResolveBase(files(tt.payload))
			if tt.err != nil {
				if !errors.Is(err, tt.err) || !strings.Contains(err.Error(), tt.message) {
					t.Fatalf("err = %v, want %v mentioning %q", err, tt.err, tt.message)
				}
				return
			}
			if err != nil || base.SHA != tt.sha || base.Source != tt.source {
				t.Fatalf("base = %+v, err = %v", base, err)
			}
		})
	}
}

func TestResolveBaseUnreadablePayload(t *testing.T) {
	t.Parallel()
	e := env(map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_EVENT_NAME": "pull_request", "GITHUB_EVENT_PATH": "/missing"})
	_, err := e.ResolveBase(func(string) ([]byte, error) { return nil, os.ErrNotExist })
	if !errors.Is(err, ErrNoBase) {
		t.Fatalf("err = %v", err)
	}
}
