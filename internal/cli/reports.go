package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nao1215/himorime/internal/config"
	"github.com/nao1215/himorime/internal/ghactions"
	"github.com/nao1215/himorime/internal/report"
)

var errReportConflict = errors.New("conflicting report destinations")

// reportPath resolves existing ancestors too, so two outputs through a
// directory symlink are recognized even before their files exist.
func reportPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if info, lerr := os.Lstat(abs); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("report path %s is a symbolic link to a missing target", path)
	}
	parent := filepath.Dir(abs)
	if parent == abs {
		return "", err
	}
	resolved, err = reportPath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(abs)), nil
}

func checkReportDestinations(suites []loadedSuite, f *measureFlags, gh ghactions.Env, ci bool) error {
	type destination struct {
		path, section, source string
		info                  os.FileInfo
	}
	var seen []destination
	add := func(path, section, source string) error {
		resolved, err := reportPath(path)
		if err != nil {
			return fmt.Errorf("%s: %w", source, err)
		}
		info, _ := os.Stat(resolved)
		for _, prev := range seen {
			same := prev.path == resolved || (info != nil && prev.info != nil && os.SameFile(info, prev.info))
			if same && (section == "" || prev.section == "" || section == prev.section) {
				return fmt.Errorf("%w: %s conflicts with %s at %s; use different files or distinct Markdown sections", errReportConflict, source, prev.source, path)
			}
		}
		seen = append(seen, destination{resolved, section, source, info})
		return nil
	}
	for _, ls := range suites {
		for i, out := range ls.suite.Outputs {
			if err := add(filepath.Join(ls.suite.Dir, filepath.FromSlash(out.Path)), out.Section, fmt.Sprintf("%s report.outputs[%d]", ls.display, i)); err != nil {
				return err
			}
		}
	}
	for _, out := range []struct{ path, section, source string }{
		{f.output, f.section, "--output"}, {f.summary, "", "--summary"},
	} {
		if out.path != "" {
			if err := add(out.path, out.section, out.source); err != nil {
				return err
			}
		}
	}
	if ci && gh.StepSummary != "" && gh.StepSummary != f.summary {
		return add(gh.StepSummary, "", "GITHUB_STEP_SUMMARY")
	}
	return nil
}

func writeSuiteReport(dir string, out config.Output, rep *report.Report) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	path, err := reportPath(filepath.Join(dir, filepath.FromSlash(out.Path)))
	if err != nil {
		return err
	}
	path, err = filepath.Rel(realDir(dir), path)
	if err != nil {
		return err
	}
	if out.Section != "" {
		return report.UpdateMarkdownSection(root, path, out.Section, rep)
	}
	return writeFile(root, path, false, func(w io.Writer) error {
		return report.Write(w, out.Format, rep, false)
	})
}

// A CLI output explicitly names its destination, unlike a suite-relative
// output. Open its chosen directory as the root for an atomic section update.
func writeSection(path, section string, rep *report.Report) error {
	resolved, err := reportPath(path)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(resolved))
	if err != nil {
		return err
	}
	defer root.Close()
	if err := report.UpdateMarkdownSection(root, filepath.Base(resolved), section, rep); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
