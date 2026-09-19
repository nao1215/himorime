// Package diag defines the stable error codes printed by himorime.
package diag

import "fmt"

// Code identifies a failure to execute a command. The first digit is the
// process exit status. Exit status 1 is intentionally absent: it is a valid
// measurement result (a budget or regression failure), not a tool error.
type Code struct {
	Number  int
	Name    string
	Meaning string
}

var codes = [...]Code{
	{Number: 2001, Name: "invalid suite", Meaning: "The suite file is missing, malformed, or violates the schema."},
	{Number: 3001, Name: "invalid command line", Meaning: "The command name, flag, or command-line argument is invalid."},
	{Number: 4001, Name: "execution failed", Meaning: "A command, hook, build, Git operation, or cleanup step could not run."},
	{Number: 5001, Name: "internal error", Meaning: "himorime encountered an unexpected internal error."},
	{Number: 6001, Name: "metric unavailable", Meaning: "A requested metric cannot be measured on this platform."},
}

func (c Code) String() string { return fmt.Sprintf("HMR%04d", c.Number) }

// All returns the currently assigned codes in numeric order.
func All() []Code { return append([]Code(nil), codes[:]...) }

// ForExit returns the code used for a non-result failure exit status.
func ForExit(exit int) (Code, bool) {
	for _, c := range codes {
		if c.Number/1000 == exit {
			return c, true
		}
	}
	return Code{}, false
}

// Lookup resolves a printed HMR code.
func Lookup(value string) (Code, bool) {
	for _, c := range codes {
		if c.String() == value {
			return c, true
		}
	}
	return Code{}, false
}
