package ghactions

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/nao1215/himorime/internal/report"
)

// WriteReport writes the complete tables to the Actions log and, if configured,
// appends identical tables to the job summary. It returns the reporting run URL.
// Outside Actions it does nothing. Summary failures leave the log available.
func (e Env) WriteReport(w io.Writer, r *report.Report, sourceURL string) (string, error) {
	if !e.Actions {
		return "", nil
	}
	var body bytes.Buffer
	if err := report.WriteGitHubSummary(&body, r); err != nil {
		return e.runURL, err
	}
	if sourceURL != "" || e.runURL != "" {
		body.WriteString("\n| Execution | URL |\n|---|---|\n")
		if sourceURL != "" {
			fmt.Fprintf(&body, "| Source run | %s |\n", report.EscapeMarkdown(sourceURL))
		}
		if e.runURL != "" {
			fmt.Fprintf(&body, "| Reporting run | %s |\n", report.EscapeMarkdown(e.runURL))
		}
		body.WriteByte('\n')
	}
	// Reports contain untrusted command output. Disable runner command parsing
	// with an unpredictable token while writing it, including on writer failure.
	token := rand.Text()
	_, startErr := fmt.Fprintf(w, "::stop-commands::%s\n", token)
	_, writeErr := w.Write(body.Bytes())
	_, endErr := fmt.Fprintf(w, "::%s::\n", token)
	if err := errors.Join(startErr, writeErr, endErr); err != nil {
		return e.runURL, fmt.Errorf("write Actions log: %w", err)
	}
	if e.StepSummary == "" {
		return e.runURL, nil
	}
	f, err := os.OpenFile(e.StepSummary, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return e.runURL, fmt.Errorf("open job summary: %w", err)
	}
	_, err = f.Write(body.Bytes())
	return e.runURL, errors.Join(err, f.Close())
}

func actionsRunURL(server, repository, run string) string {
	u, err := url.Parse(server)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	parts := strings.Split(repository, "/")
	if len(parts) != 2 {
		return ""
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return ""
		}
		for _, c := range p {
			valid := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.'
			if !valid {
				return ""
			}
		}
	}
	id, err := strconv.ParseUint(run, 10, 64)
	if err != nil || id == 0 {
		return ""
	}
	return strings.TrimRight(u.String(), "/") + "/" + repository + "/actions/runs/" + strconv.FormatUint(id, 10)
}
