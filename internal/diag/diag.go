// Package diag defines the stable error codes printed by himorime.
package diag

import (
	"fmt"
	"io"
)

// Code identifies a tool-error category corresponding to an exit status.
// Exit status 1 is intentionally absent: budget failures, regressions and
// inconclusive comparisons under --fail-on-inconclusive are measurement
// results, not tool errors.
type Code struct {
	Number  int
	Name    string
	Meaning string
}

var codes = [...]Code{
	{Number: 2001, Name: "invalid input", Meaning: "A suite or saved report is missing, malformed, or invalid."},
	{Number: 3001, Name: "invalid command line", Meaning: "The command name, flag, or command-line argument is invalid."},
	{Number: 4001, Name: "execution failed", Meaning: "A command, hook, build, Git operation, report write, or cleanup step failed."},
	{Number: 5001, Name: "internal error", Meaning: "himorime encountered an unexpected internal error."},
	{Number: 6001, Name: "metric unavailable", Meaning: "A requested metric is unsupported or could not be collected."},
}

func (c Code) String() string { return fmt.Sprintf("HMR%04d", c.Number) }

// All returns the currently assigned codes in numeric order.
func All() []Code { return append([]Code(nil), codes[:]...) }

// Print writes a diagnostic line, prefixed with the code for exit when it
// identifies a tool error. Success and performance results have no prefix.
func Print(w io.Writer, exit int, format string, args ...any) {
	if c, ok := forExit(exit); ok {
		fmt.Fprintf(w, "%s: ", c)
	}
	fmt.Fprintf(w, format+"\n", args...)
}

func forExit(exit int) (Code, bool) {
	for _, c := range codes {
		if c.Number/1000 == exit {
			return c, true
		}
	}
	return Code{}, false
}
