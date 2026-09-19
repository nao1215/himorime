// Package ghactions is the thin adapter between himorime and GitHub Actions.
//
// It reads the environment GitHub sets for a job, finds the base commit of a
// pull request from the event payload, and locates the job summary file.
// Nothing else in himorime knows about GitHub.
package ghactions

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Env is the part of the GitHub Actions environment himorime reads.
type Env struct {
	Actions     bool
	EventName   string
	EventPath   string
	StepSummary string
	RunnerOS    string
	RunnerArch  string
	Repository  string
	APIURL      string
	Token       string
}

// FromLookup reads the environment through lookup (os.LookupEnv in production).
func FromLookup(lookup func(string) (string, bool)) Env {
	get := func(k string) string {
		v, _ := lookup(k)
		return v
	}
	return Env{
		Actions:     get("GITHUB_ACTIONS") == "true",
		EventName:   get("GITHUB_EVENT_NAME"),
		EventPath:   get("GITHUB_EVENT_PATH"),
		StepSummary: get("GITHUB_STEP_SUMMARY"),
		RunnerOS:    get("RUNNER_OS"),
		RunnerArch:  get("RUNNER_ARCH"),
		Repository:  get("GITHUB_REPOSITORY"),
		APIURL:      get("GITHUB_API_URL"),
		Token:       get("GITHUB_TOKEN"),
	}
}

// Base is the resolved comparison base.
type Base struct {
	SHA string
	// Source says where the SHA came from, for the log.
	Source string
}

// ErrNoBase is returned when the event carries no usable base commit.
var ErrNoBase = errors.New("cannot determine the base revision")

// ErrUnsafeEvent is returned for pull_request_target, whose jobs run with
// secrets and write access while checking out untrusted code.
var ErrUnsafeEvent = errors.New("refusing to run for pull_request_target")

// Guidance explains how to name the base explicitly.
const Guidance = "pass --against <ref> or set HIMORIME_BASE_REF, for example --against origin/main"

const zeroSHA = "0000000000000000000000000000000000000000"

type event struct {
	PullRequest *struct {
		Base struct {
			SHA string `json:"sha"`
		} `json:"base"`
	} `json:"pull_request"`
	MergeGroup *struct {
		BaseSHA string `json:"base_sha"`
	} `json:"merge_group"`
	Before string `json:"before"`
}

// ResolveBase finds the base commit for the current event. readFile reads the
// event payload (os.ReadFile in production).
func (e Env) ResolveBase(readFile func(string) ([]byte, error)) (Base, error) {
	if !e.Actions {
		return Base{}, fmt.Errorf("%w: not running in GitHub Actions; %s", ErrNoBase, Guidance)
	}
	switch e.EventName {
	case "pull_request_target":
		return Base{}, fmt.Errorf("%w: it runs untrusted pull request code with the base repository's secrets and write token; trigger the workflow on pull_request instead", ErrUnsafeEvent)
	case "pull_request", "pull_request_review", "pull_request_review_comment",
		"merge_group", "push":
	default:
		return Base{}, fmt.Errorf("%w: event %q has no base commit; %s", ErrNoBase, e.EventName, Guidance)
	}
	if e.EventPath == "" {
		return Base{}, fmt.Errorf("%w: GITHUB_EVENT_PATH is not set; %s", ErrNoBase, Guidance)
	}
	data, err := readFile(e.EventPath)
	if err != nil {
		return Base{}, fmt.Errorf("%w: read the event payload: %w; %s", ErrNoBase, err, Guidance)
	}
	var ev event
	if err := json.Unmarshal(data, &ev); err != nil {
		return Base{}, fmt.Errorf("%w: parse the event payload: %w; %s", ErrNoBase, err, Guidance)
	}
	var sha, source string
	switch e.EventName {
	case "merge_group":
		if ev.MergeGroup != nil {
			sha, source = ev.MergeGroup.BaseSHA, "merge_group.base_sha"
		}
	case "push":
		sha, source = ev.Before, "push before"
	default:
		if ev.PullRequest != nil {
			sha, source = ev.PullRequest.Base.SHA, "pull_request.base.sha"
		}
	}
	sha = strings.TrimSpace(sha)
	if sha == "" || sha == zeroSHA || !isHexSHA(sha) {
		return Base{}, fmt.Errorf("%w: the %s event payload has no base commit; %s", ErrNoBase, e.EventName, Guidance)
	}
	return Base{SHA: sha, Source: source}, nil
}

func isHexSHA(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
