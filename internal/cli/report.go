package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nao1215/himorime/internal/config"
	"github.com/nao1215/himorime/internal/diag"
	"github.com/nao1215/himorime/internal/exitcode"
	"github.com/nao1215/himorime/internal/report"
)

type reportOptions struct {
	format string
	output string
}

type outputPathError struct{ err error }

func (e outputPathError) Error() string { return e.err.Error() }
func (e outputPathError) Unwrap() error { return e.err }

func reportFlags(fs *flag.FlagSet) any {
	o := &reportOptions{}
	fs.StringVar(&o.format, "format", string(config.FormatTable), "report `format`: table or markdown")
	fs.StringVar(&o.output, "output", "", "write the report to this `file` instead of stdout")
	return o
}

func runReport(_ context.Context, a *App, args []string) int {
	fs, v := newFlagSet("report")
	o, _ := v.(*reportOptions)
	operands, status, ok := parseFlags(fs, args, a.Stdout, a.Stderr)
	if !ok {
		return status
	}
	if len(operands) != 1 {
		diag.Print(a.Stderr, exitcode.Usage, "himorime report: name one INPUT JSON report")
		return exitcode.Usage
	}
	if o.format != string(config.FormatTable) && o.format != string(config.FormatMarkdown) {
		diag.Print(a.Stderr, exitcode.Usage, "himorime report: unknown --format %q; use table or markdown", o.format)
		return exitcode.Usage
	}

	input := operands[0]
	inputPath, err := appAbsolutePath(a, input)
	if err != nil {
		diag.PrintCode(a.Stderr, diag.Input, "himorime report: %s: %v", input, err)
		return exitcode.Config
	}
	if _, err := os.Stat(inputPath); err != nil {
		diag.PrintCode(a.Stderr, diag.Input, "himorime report: read %s: %v", input, err)
		return exitcode.Config
	}
	if o.output != "" {
		same, err := reportPathsSame(a, input, o.output)
		if err != nil {
			var outputErr outputPathError
			if errors.As(err, &outputErr) {
				diag.PrintCode(a.Stderr, diag.Report, "himorime report: check output path %s: %v", o.output, err)
				return exitcode.Execution
			}
			diag.PrintCode(a.Stderr, diag.Input, "himorime report: check input and output paths: %v", err)
			return exitcode.Config
		}
		if same {
			diag.PrintCode(a.Stderr, diag.Input, "himorime report: --output %s resolves to the input report %s", o.output, input)
			return exitcode.Config
		}
	}

	read := a.ReadFile
	if read == nil {
		read = os.ReadFile
	}
	data, err := read(inputPath)
	if err != nil {
		diag.PrintCode(a.Stderr, diag.Input, "himorime report: read %s: %v", input, err)
		return exitcode.Config
	}
	r, err := report.Read(data)
	if err != nil {
		diag.PrintCode(a.Stderr, diag.Input, "himorime report: %s: %v", input, err)
		return exitcode.Config
	}

	var rendered bytes.Buffer
	format := config.Format(o.format)
	if err := report.Write(&rendered, format, r, false); err != nil {
		diag.PrintCode(a.Stderr, diag.Report, "himorime report: render %s: %v", input, err)
		return exitcode.Execution
	}
	if o.output == "" {
		if _, err := io.Copy(a.Stdout, &rendered); err != nil {
			diag.PrintCode(a.Stderr, diag.Report, "himorime report: write stdout: %v", err)
			return exitcode.Execution
		}
		return exitcode.OK
	}
	if err := writeSavedReport(a, o.output, rendered.Bytes()); err != nil {
		diag.PrintCode(a.Stderr, diag.Report, "himorime report: write %s: %v", o.output, err)
		return exitcode.Execution
	}
	return exitcode.OK
}

func appAbsolutePath(a *App, path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	getwd := os.Getwd
	if a != nil && a.Getwd != nil {
		getwd = a.Getwd
	}
	dir, err := getwd()
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(dir, path)), nil
}

// resolvedPath follows existing symlinks and resolves missing final path
// components through existing parents. This catches relative aliases and
// directory symlink aliases before an output file is opened.
func resolvedPath(path string) (string, error) {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path, nil
	}
	resolved, err := resolvedPath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(path)), nil
}

func reportPathsSame(a *App, input, output string) (bool, error) {
	inPath, err := appAbsolutePath(a, input)
	if err != nil {
		return false, err
	}
	outPath, err := appAbsolutePath(a, output)
	if err != nil {
		return false, outputPathError{err}
	}
	inputInfo, err := os.Stat(inPath)
	if err != nil {
		return false, err
	}
	if outputInfo, err := os.Stat(outPath); err == nil && os.SameFile(inputInfo, outputInfo) {
		return true, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, outputPathError{err}
	}
	inputResolved, err := resolvedPath(inPath)
	if err != nil {
		return false, err
	}
	outputResolved, err := resolvedPath(outPath)
	if err != nil {
		return false, outputPathError{err}
	}
	return inputResolved == outputResolved, nil
}

// writeSavedReport renders into memory first and then renames a temporary
// file into place. A failed write therefore cannot truncate the source report.
func writeSavedReport(a *App, path string, data []byte) error {
	abs, err := appAbsolutePath(a, path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create report directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".himorime-report-*")
	if err != nil {
		return fmt.Errorf("create temporary report: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary report: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary report: %w", err)
	}
	if err := os.Rename(tmpName, abs); err != nil {
		return fmt.Errorf("replace report: %w", err)
	}
	return nil
}
