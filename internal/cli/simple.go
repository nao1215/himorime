package cli

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"runtime"
	"strings"

	"github.com/nao1215/himorime/internal/buildinfo"
	"github.com/nao1215/himorime/internal/config"
	"github.com/nao1215/himorime/internal/diag"
	"github.com/nao1215/himorime/internal/exitcode"
)

// InitTemplate is the suite `himorime init` writes. TestInitTemplateIsValid
// loads it, and the E2E suite runs it.
//
//go:embed init.yaml
var InitTemplate string

func runInit(_ context.Context, a *App, args []string) int {
	fs, v := newFlagSet("init")
	o, _ := v.(*initOptions)
	operands, status, ok := parseFlags(fs, args, a.Stdout, a.Stderr)
	if !ok {
		return status
	}
	if len(operands) > 1 {
		diag.Print(a.Stderr, exitcode.Usage, "himorime init: at most one PATH may be given")
		return exitcode.Usage
	}
	path := config.DefaultFileName
	if len(operands) == 1 {
		path = operands[0]
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if o.force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o644) //nolint:gosec // the user names the file to create
	if err != nil {
		if errors.Is(err, iofs.ErrExist) {
			diag.Print(a.Stderr, exitcode.Usage, "himorime init: %s already exists; it was not changed (use --force to overwrite it)", path)
			return exitcode.Usage
		}
		diag.Print(a.Stderr, exitcode.Execution, "himorime init: %v", err)
		return exitcode.Execution
	}
	_, werr := f.WriteString(InitTemplate)
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		diag.Print(a.Stderr, exitcode.Execution, "himorime init: write %s: %v", path, err)
		return exitcode.Execution
	}
	fmt.Fprintf(a.Stdout, "wrote %s\nnext: himorime validate %s && himorime run %s\n", path, path, path)
	return exitcode.OK
}

func runValidate(_ context.Context, a *App, args []string) int {
	fs, _ := newFlagSet("validate")
	operands, status, ok := parseFlags(fs, args, a.Stdout, a.Stderr)
	if !ok {
		return status
	}
	suites, status := loadSuites(operands, a.Stderr)
	for _, ls := range suites {
		commands := 0
		for _, b := range ls.suite.Benchmarks {
			commands += len(b.Commands)
		}
		fmt.Fprintf(a.Stdout, "%s: ok (%s, %s)\n", ls.display, plural(len(ls.suite.Benchmarks), "benchmark"), plural(commands, "command"))
	}
	return status
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// ListEntry is one line of `himorime list`, and one element of its JSON output.
type ListEntry struct {
	Suite     string   `json:"suite"`
	SuiteName string   `json:"suite_name"`
	Benchmark string   `json:"benchmark"`
	Tags      []string `json:"tags"`
	Stdin     string   `json:"stdin"`
	Command   string   `json:"command"`
	Argv      string   `json:"argv"`
	Shell     bool     `json:"shell"`
	Baseline  bool     `json:"baseline"`
}

func runList(_ context.Context, a *App, args []string) int {
	fs, v := newFlagSet("list")
	o, _ := v.(*listOptions)
	operands, status, ok := parseFlags(fs, args, a.Stdout, a.Stderr)
	if !ok {
		return status
	}
	if o.format != "text" && o.format != "json" {
		diag.Print(a.Stderr, exitcode.Usage, "himorime list: unknown --format %q; use text or json", o.format)
		return exitcode.Usage
	}
	sel, ok := o.selection(a.Stderr, "list")
	if !ok {
		return exitcode.Usage
	}
	suites, status := loadSuites(operands, a.Stderr)
	if status != 0 {
		return status
	}
	entries := []ListEntry{}
	for _, ls := range suites {
		for _, b := range sel.Select(ls.suite.Benchmarks) {
			stdin := ""
			switch b.Stdin.Kind {
			case config.StdinFile:
				stdin = b.Stdin.File
			case config.StdinContent:
				stdin = "(inline)"
			}
			for _, c := range b.Commands {
				entries = append(entries, ListEntry{
					Suite: ls.display, SuiteName: ls.suite.Name, Benchmark: b.Name, Tags: nonNilStrings(b.Tags),
					Stdin: stdin, Command: c.Name, Argv: c.Display(), Shell: c.Shell, Baseline: b.Baseline == c.Name,
				})
			}
		}
	}
	if o.format == "json" {
		enc := json.NewEncoder(a.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(entries); err != nil {
			diag.Print(a.Stderr, exitcode.Execution, "himorime list: %v", err)
			return exitcode.Execution
		}
		return exitcode.OK
	}
	t := [][]string{{"SUITE", "BENCHMARK", "TAGS", "STDIN", "COMMAND", "RUN"}}
	for _, e := range entries {
		tags, stdin, name := strings.Join(e.Tags, ","), e.Stdin, e.Command
		if tags == "" {
			tags = "-"
		}
		if stdin == "" {
			stdin = "-"
		}
		if e.Baseline {
			name += "*"
		}
		argv := e.Argv
		if e.Shell {
			argv = "shell: " + argv
		}
		t = append(t, []string{e.Suite, e.Benchmark, tags, stdin, name, argv})
	}
	writeColumns(a, t)
	if len(entries) == 0 {
		fmt.Fprintln(a.Stderr, "himorime list: no benchmark matches the selection")
	}
	return exitcode.OK
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func writeColumns(a *App, rows [][]string) {
	widths := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, c := range r {
			if n := len([]rune(c)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	var sb strings.Builder
	for _, r := range rows {
		for i, c := range r {
			if i == len(r)-1 {
				sb.WriteString(c)
				continue
			}
			sb.WriteString(c + strings.Repeat(" ", widths[i]-len([]rune(c))+2))
		}
		sb.WriteString("\n")
	}
	fmt.Fprint(a.Stdout, sb.String())
}

func runVersion(_ context.Context, a *App, args []string) int {
	fs, _ := newFlagSet("version")
	if _, status, ok := parseFlags(fs, args, a.Stdout, a.Stderr); !ok {
		return status
	}
	commit := ""
	if c := buildinfo.Commit(); c != "" {
		commit = ", commit " + c
	}
	fmt.Fprintf(a.Stdout, "himorime %s (%s, %s/%s%s)\n", buildinfo.Get(), runtime.Version(), runtime.GOOS, runtime.GOARCH, commit)
	return exitcode.OK
}
