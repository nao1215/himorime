// Package cli implements himorime's command-line interface: subcommand
// dispatch, flag parsing, and the mapping of outcomes to exit codes.
package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nao1215/himorime/internal/diag"
	"github.com/nao1215/himorime/internal/exitcode"
)

// App carries everything a command touches outside its arguments, so tests
// can run the CLI in-process against a fake environment.
type App struct {
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	LookupEnv func(string) (string, bool)
	Environ   func() []string
	Getwd     func() (string, error)
	ReadFile  func(string) ([]byte, error)
	Now       func() time.Time
	// StdoutIsTerminal reports whether stdout is an interactive terminal.
	StdoutIsTerminal bool
	// StderrIsTerminal reports whether stderr is an interactive terminal.
	StderrIsTerminal bool
	// Capabilities reports what this platform can measure; the platform's
	// own when nil. Tests replace it to exercise unsupported metrics.
	Capabilities func() (cpu, memory error)
}

// Main is the process entry point. It installs interrupt handling and returns
// the exit status.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The first interrupt cancels the run, which stops running commands and
	// runs cleanup. Signal handling is then restored, so a second Ctrl+C ends
	// himorime at once for someone who does not want to wait for cleanup.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-signals:
			signal.Stop(signals)
			fmt.Fprintln(stderr, "himorime: interrupted; stopping commands and cleaning up (interrupt again to quit immediately)")
			cancel()
		case <-done:
			signal.Stop(signals)
		}
	}()
	app := &App{
		Stdin:            stdin,
		Stdout:           stdout,
		Stderr:           stderr,
		LookupEnv:        os.LookupEnv,
		Environ:          os.Environ,
		Getwd:            os.Getwd,
		ReadFile:         os.ReadFile,
		Now:              time.Now,
		StdoutIsTerminal: isTerminal(stdout),
		StderrIsTerminal: isTerminal(stderr),
	}
	return app.Run(ctx, args)
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// Run dispatches args to a subcommand and returns the exit status. A panic in
// a command is reported as an internal error rather than a crash.
func (a *App) Run(ctx context.Context, args []string) (code int) {
	defer func() {
		if r := recover(); r != nil {
			diag.Print(a.Stderr, exitcode.Internal, "himorime: internal error: %v\nplease report this at https://github.com/nao1215/himorime/issues", r)
			code = exitcode.Internal
		}
	}()
	if len(args) == 0 {
		diag.Print(a.Stderr, exitcode.Usage, "himorime: no command given")
		fmt.Fprint(a.Stderr, "\n")
		usage(a.Stderr)
		return exitcode.Usage
	}
	name, rest := args[0], args[1:]
	switch name {
	case "-h", "--help", "-help":
		name = "help"
	case "-v", "--version", "-version":
		name = "version"
	}
	for _, c := range Commands() {
		if c.Name == name {
			return c.run(ctx, a, rest)
		}
	}
	diag.Print(a.Stderr, exitcode.Usage, "himorime: unknown command %q", name)
	fmt.Fprint(a.Stderr, "\n")
	usage(a.Stderr)
	return exitcode.Usage
}

func usage(w io.Writer) {
	var sb strings.Builder
	sb.WriteString("himorime - performance budgets and regression checks for command-line programs\n\n")
	sb.WriteString("Usage:\n  himorime <command> [flags] [arguments]\n\nCommands:\n")
	for _, c := range Commands() {
		fmt.Fprintf(&sb, "  %-11s %s\n", c.Name, c.Short)
	}
	sb.WriteString("\nRun \"himorime <command> --help\" for the flags of a command.\n")
	sb.WriteString("Documentation: https://nao1215.github.io/himorime/\n")
	sb.WriteString("GitHub Sponsors: https://github.com/sponsors/nao1215\n")
	fmt.Fprint(w, sb.String())
}

// parseFlags parses args with fs, accepting flags before and after operands.
// The flag package stops at the first operand; re-entering Parse after each
// one lets `himorime run bench.yaml --format json` mean what users expect. A
// `--` ends flag parsing for good.
func parseFlags(fs *flag.FlagSet, args []string, stdout, stderr io.Writer) ([]string, int, bool) {
	var captured bytes.Buffer
	fs.SetOutput(&captured)
	var operands []string
	rest := args
	for {
		// The flag package consumes `--` and returns only the arguments after
		// it. Detect it before parsing so those arguments can never be parsed
		// as flags on a later pass through the loop.
		delimiter := delimiterIndex(fs, rest)
		if err := fs.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				WriteHelp(stdout, fs.Name())
				return nil, exitcode.OK, false
			}
			msg := strings.TrimSpace(strings.SplitN(captured.String(), "\n", 2)[0])
			if msg == "" {
				msg = err.Error()
			}
			diag.Print(stderr, exitcode.Usage, "himorime %s: %s\nrun \"himorime %s --help\" for usage", fs.Name(), msg, fs.Name())
			return nil, exitcode.Usage, false
		}
		parsed := fs.Args()
		if delimiter >= 0 {
			operands = append(operands, parsed...)
			break
		}
		if len(parsed) == 0 {
			break
		}
		operands = append(operands, parsed[0])
		rest = parsed[1:]
	}
	return operands, 0, true
}

// delimiterIndex finds a standalone `--` that flag.Parse would consume. A
// value such as `--filter --` is not a delimiter: the flag package treats the
// second token as the value of --filter.
func delimiterIndex(fs *flag.FlagSet, args []string) int {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return i
		}
		if len(arg) < 2 || arg[0] != '-' || arg == "-" {
			return -1
		}
		name := strings.TrimLeft(arg, "-")
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			name = name[:eq]
		}
		f := fs.Lookup(name)
		if f == nil {
			return -1
		}
		if strings.IndexByte(arg, '=') < 0 {
			boolFlag, isBool := f.Value.(interface{ IsBoolFlag() bool })
			if !isBool || !boolFlag.IsBoolFlag() {
				i++
			}
		}
	}
	return -1
}

// stringList is a repeatable flag that also splits comma-separated values.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return errors.New("empty value")
		}
		*s = append(*s, part)
	}
	return nil
}
