package cli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/nao1215/himorime/internal/exitcode"
)

func TestCommentForwardsArgumentsStreamsEnvironmentAndExitCode(t *testing.T) {
	var gotArgs, gotEnv []string
	r := runWith(t, t.TempDir(), env{"SENTINEL": "yes"}, func(a *App) {
		a.RunCommentHelper = func(_ context.Context, args []string, _ io.Reader, stdout, stderr io.Writer, environ []string) (int, error) {
			gotArgs, gotEnv = append([]string(nil), args...), append([]string(nil), environ...)
			_, _ = io.WriteString(stdout, "helper stdout\n")
			_, _ = io.WriteString(stderr, "helper stderr\n")
			return exitcode.Config, nil
		}
	}, "comment", "--hide-footer", "report.json")
	if r.code != exitcode.Config || r.stdout != "helper stdout\n" || r.stderr != "helper stderr\n" {
		t.Fatalf("result = %+v", r)
	}
	if strings.Join(gotArgs, " ") != "--hide-footer report.json" || !containsEnv(gotEnv, "SENTINEL=yes") {
		t.Fatalf("args = %v, environment lacks sentinel = %v", gotArgs, gotEnv)
	}
}

func TestCommentReportsMissingHelperAsExecutionFailure(t *testing.T) {
	r := runWith(t, t.TempDir(), nil, func(a *App) {
		a.RunCommentHelper = func(context.Context, []string, io.Reader, io.Writer, io.Writer, []string) (int, error) {
			return 0, errors.New("himorime-comment helper is not installed next to himorime or on PATH")
		}
	}, "comment", "report.json")
	if r.code != exitcode.Execution || !strings.Contains(r.stderr, "helper is not installed") {
		t.Fatalf("result = %+v", r)
	}
}

func TestCommentMetadataIncludesFooterControl(t *testing.T) {
	var out strings.Builder
	WriteHelp(&out, "comment")
	if !strings.Contains(out.String(), "--hide-footer") {
		t.Fatalf("comment help = %s", out.String())
	}
}

func containsEnv(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
