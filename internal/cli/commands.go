package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/nao1215/himorime/internal/diag"
	"github.com/nao1215/himorime/internal/exitcode"
)

// Command describes one subcommand. The table returned by Commands is the
// single source for dispatch, `--help`, shell completion and the Commands
// documentation page.
type Command struct {
	Name  string
	Usage string
	Short string
	Long  string
	// Flags registers the command's flags on fs and returns a value the
	// command's run function reads. Documentation generators call it with a
	// throw-away FlagSet.
	Flags func(fs *flag.FlagSet) any
	run   func(ctx context.Context, a *App, args []string) int
}

// Commands returns every subcommand in display order.
func Commands() []Command {
	return []Command{
		{
			Name:  "init",
			Usage: "himorime init [flags] [PATH]",
			Short: "Write a minimal, runnable himorime.yaml",
			Long: "Writes a small suite that measures `git --version`, with a JSON Schema comment so editors " +
				"can complete and validate it. PATH defaults to himorime.yaml. An existing file is never " +
				"overwritten unless --force is given.",
			Flags: initFlags,
			run:   runInit,
		},
		{
			Name:  "validate",
			Usage: "himorime validate [PATH...]",
			Short: "Check suite files without running anything",
			Long: "Checks YAML syntax, the schema (unknown keys, types, durations, byte sizes, rates, " +
				"percentages, paths) and semantic rules (baselines, budgets and regression settings that refer " +
				"to real commands and measured metrics, budget directions, throughput units, variables that " +
				"exist). No command is executed. Each PATH is a suite file or a directory holding himorime.yaml " +
				"or *.himorime.yaml files; the default is himorime.yaml.",
			Flags: func(*flag.FlagSet) any { return nil },
			run:   runValidate,
		},
		{
			Name:  "list",
			Usage: "himorime list [flags] [PATH...]",
			Short: "List suites, benchmarks, commands, tags and fixtures",
			Long: "Prints one line per command of every selected benchmark: suite file, benchmark, tags, " +
				"stdin fixture, command name and the command as written. Nothing is executed, and variables " +
				"are shown unexpanded. --format json prints the same data for tools.",
			Flags: listFlags,
			run:   runList,
		},
		{
			Name:  "run",
			Usage: "himorime run [flags] [PATH...]",
			Short: "Measure the suite in the current environment",
			Long: "Builds the suite's artifact when a build section exists, then measures every selected " +
				"benchmark: latency, and the throughput, CPU time and peak RSS the suite asks for. Commands of " +
				"one benchmark run interleaved in a seeded random order. Exits 1 when a budget is exceeded, 4 " +
				"when a command fails, and 6 when a requested metric cannot be measured or a required budget " +
				"cannot be assessed.",
			Flags: runFlags,
			run:   func(ctx context.Context, a *App, args []string) int { return runMeasure(ctx, a, "run", args) },
		},
		{
			Name:  "compare",
			Usage: "himorime compare --against REF [flags] [PATH...]",
			Short: "Compare a Git revision with the working tree",
			Long: "Checks REF out into a temporary Git worktree, builds both REF and the current working " +
				"tree (uncommitted changes included), and measures them interleaved on this machine. Commands " +
				"run in ${root} of each revision, so each runs its own code; ${head_root} names the working " +
				"tree's copy for shared fixtures. Every measured metric is compared in the direction that is " +
				"worse for it; a metric with `regression.<metric>.gate: false` is reported but never fails. The " +
				"working tree, index and branches are never modified. Exits 1 on a gated regression or an " +
				"exceeded budget, and 6 when a requested metric cannot be measured or a required budget " +
				"cannot be assessed.",
			Flags: compareFlags,
			run:   func(ctx context.Context, a *App, args []string) int { return runMeasure(ctx, a, "compare", args) },
		},
		{
			Name:  "ci",
			Usage: "himorime ci [flags] [PATH...]",
			Short: "Compare against the pull request base in CI",
			Long: "Like compare, with CI defaults: no colors, and a Markdown summary appended to " +
				"$GITHUB_STEP_SUMMARY when it is set. The base revision is --against, else $HIMORIME_BASE_REF, " +
				"else the base commit of the GitHub Actions pull_request, merge_group or push event. " +
				"pull_request_target is refused. In GitHub Actions, every missed budget, regression and " +
				"failure is also printed as an annotation. Exits 6 when a requested metric cannot be measured " +
				"or a required budget cannot be assessed.",
			Flags: ciFlags,
			run:   func(ctx context.Context, a *App, args []string) int { return runMeasure(ctx, a, "ci", args) },
		},
		{
			Name:  "version",
			Usage: "himorime version",
			Short: "Print the himorime version",
			Long:  "Prints the version, the Go version and the platform.",
			Flags: func(*flag.FlagSet) any { return nil },
			run:   runVersion,
		},
		{
			Name:  "completion",
			Usage: "himorime completion <bash|zsh|fish|powershell>",
			Short: "Print a shell completion script",
			Long:  "Prints a completion script for the named shell to standard output.",
			Flags: func(*flag.FlagSet) any { return nil },
			run:   runCompletion,
		},
		{
			Name:  "help",
			Usage: "himorime help [COMMAND]",
			Short: "Show help for himorime or a command",
			Long:  "Shows the command list, or the usage and flags of one command.",
			Flags: func(*flag.FlagSet) any { return nil },
			run:   runHelp,
		},
	}
}

func findCommand(name string) (Command, bool) {
	for _, c := range Commands() {
		if c.Name == name {
			return c, true
		}
	}
	return Command{}, false
}

// newFlagSet builds the FlagSet for a command and returns its settings value.
func newFlagSet(name string) (*flag.FlagSet, any) {
	c, _ := findCommand(name)
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	v := c.Flags(fs)
	return fs, v
}

// WriteHelp prints a command's usage, description and flags.
func WriteHelp(w io.Writer, name string) {
	c, ok := findCommand(name)
	if !ok {
		return
	}
	fs, _ := newFlagSet(name)
	var sb strings.Builder
	fmt.Fprintf(&sb, "Usage: %s\n\n%s\n", c.Usage, wrap(c.Long, 78))
	if flags := FlagDocs(fs); len(flags) > 0 {
		sb.WriteString("\nFlags:\n")
		for _, f := range flags {
			fmt.Fprintf(&sb, "  %s\n      %s\n", f.Synopsis, f.Usage)
		}
	}
	fmt.Fprint(w, sb.String())
}

// FlagDoc documents one flag.
type FlagDoc struct {
	Name     string
	Synopsis string
	Usage    string
	Default  string
}

// FlagDocs lists the flags of fs in name order.
func FlagDocs(fs *flag.FlagSet) []FlagDoc {
	var out []FlagDoc
	fs.VisitAll(func(f *flag.Flag) {
		arg, usage := flag.UnquoteUsage(f)
		syn := "--" + f.Name
		if arg != "" {
			syn += " " + strings.ToUpper(arg)
		}
		d := FlagDoc{Name: f.Name, Synopsis: syn, Usage: usage, Default: f.DefValue}
		if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" {
			d.Usage += fmt.Sprintf(" (default %s)", f.DefValue)
		}
		out = append(out, d)
	})
	return out
}

func wrap(s string, width int) string {
	var lines []string
	var cur strings.Builder
	for _, word := range strings.Fields(s) {
		if cur.Len() > 0 && cur.Len()+1+len(word) > width {
			lines = append(lines, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(word)
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return strings.Join(lines, "\n")
}

func runHelp(_ context.Context, a *App, args []string) int {
	if len(args) == 0 {
		usage(a.Stdout)
		return 0
	}
	if _, ok := findCommand(args[0]); !ok {
		diag.Print(a.Stderr, exitcode.Usage, "himorime help: unknown command %q", args[0])
		return 3
	}
	WriteHelp(a.Stdout, args[0])
	return 0
}
