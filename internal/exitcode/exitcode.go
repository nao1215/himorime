// Package exitcode defines himorime's exit statuses, a stable public contract.
//
// The table returned by All is the single source for the exit-codes
// documentation table; the documentation sync test fails when the page and
// this table disagree.
package exitcode

// Exit statuses.
const (
	// OK: every benchmark completed and every budget and regression check passed.
	OK = 0
	// Failed: a budget was exceeded or a regression was confirmed (or, with
	// --fail-on-inconclusive, a comparison was inconclusive).
	Failed = 1
	// Config: the suite file failed YAML, schema or semantic validation.
	Config = 2
	// Usage: the command line was invalid.
	Usage = 3
	// Execution: a command, hook, build or Git operation failed, timed out,
	// or the run was interrupted.
	Execution = 4
	// Internal: a bug or an unexpected environment failure inside himorime.
	Internal = 5
	// Metric: a requested metric could not be measured, because the platform
	// does not support it or reading it failed. This can be detected before
	// any command is executed.
	Metric = 6
)

// Code documents one exit status.
type Code struct {
	Code    int
	Name    string
	Meaning string
}

// All returns every exit status in ascending order.
func All() []Code {
	return []Code{
		{OK, "ok", "Every selected benchmark completed, and every budget and regression check passed. Inconclusive comparisons also exit 0 unless --fail-on-inconclusive is given."},
		{Failed, "failed", "A performance budget was exceeded, a regression was confirmed on any metric, or --fail-on-inconclusive was given and a comparison was inconclusive. The measurement itself succeeded."},
		{Config, "config", "A suite file is not valid YAML, does not match the schema, or fails semantic validation. Nothing was executed."},
		{Usage, "usage", "The command line is invalid: an unknown flag or command, a missing argument, or no benchmark matched the selection."},
		{Execution, "execution", "A measured command, hook or build failed or timed out, a Git operation failed, the base revision could not be resolved, or the run was interrupted."},
		{Internal, "internal", "himorime hit an unexpected internal error. Please report it."},
		{Metric, "metric", "A requested metric (throughput, cpu or memory) could not be measured: this platform does not support it and metrics.unsupported is fail, or the operating system did not report it. Depending on the cause, the commands may not have run."},
	}
}

// Outcome returns the one-line explanation of a non-zero status printed at
// the end of a run, so a CI log tells a performance failure apart from a
// failure to measure. Set inconclusiveOnly when exit 1 was caused solely by
// --fail-on-inconclusive. It is "" for statuses that need no explanation.
func Outcome(code int, inconclusiveOnly bool) string {
	switch code {
	case Failed:
		if inconclusiveOnly {
			return "performance check failed: a comparison was inconclusive (the measurement itself succeeded)"
		}
		return "performance check failed: a budget or regression threshold was violated (the measurement itself succeeded)"
	case Execution:
		return "the measurement did not complete: a command, hook, build or Git operation failed, or the run was interrupted"
	case Metric:
		return "a requested metric could not be measured; performance was not judged"
	}
	return ""
}
