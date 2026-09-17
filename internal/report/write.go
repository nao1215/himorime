package report

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/nao1215/himorime/internal/config"
)

// WriteJSON renders the report as indented JSON.
func WriteJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(r); err != nil {
		return fmt.Errorf("encode json report: %w", err)
	}
	return nil
}

// Write renders r in the given format.
func Write(w io.Writer, format config.Format, r *Report, color bool) error {
	switch format {
	case config.FormatTable:
		return WriteTerminal(w, r, TerminalOptions{Color: color})
	case config.FormatJSON:
		return WriteJSON(w, r)
	case config.FormatCSV:
		return WriteCSV(w, r)
	case config.FormatMarkdown:
		return WriteMarkdown(w, r)
	case config.FormatGitHub:
		return WriteGitHubSummary(w, r)
	case config.FormatSamplesCSV:
		return WriteSamplesCSV(w, r)
	}
	return fmt.Errorf("unknown report format %q", format)
}
