package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/nao1215/yahiko/internal/config"
	"github.com/nao1215/yahiko/internal/exitcode"
	"github.com/nao1215/yahiko/internal/runner"
)

// loadedSuite pairs a suite with the path the user named it by.
type loadedSuite struct {
	display string
	suite   *config.Suite
}

// suitePaths expands the PATH operands: a directory contributes its
// yahiko.yaml and *.yahiko.yaml files, a file is taken as is, and no operand
// means ./yahiko.yaml.
func suitePaths(args []string) ([]string, error) {
	if len(args) == 0 {
		return []string{config.DefaultFileName}, nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		key := filepath.Clean(p)
		if abs, err := filepath.Abs(p); err == nil {
			key = abs
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, p)
		}
	}
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil || !info.IsDir() {
			add(arg)
			continue
		}
		entries, err := os.ReadDir(arg)
		if err != nil {
			return nil, fmt.Errorf("read directory %s: %w", arg, err)
		}
		var found []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if n := e.Name(); n == config.DefaultFileName || strings.HasSuffix(n, ".yahiko.yaml") {
				found = append(found, filepath.Join(arg, n))
			}
		}
		if len(found) == 0 {
			return nil, fmt.Errorf("%s contains no yahiko.yaml or *.yahiko.yaml file", arg)
		}
		sort.Strings(found)
		for _, f := range found {
			add(f)
		}
	}
	return out, nil
}

// loadSuites loads every suite and prints all validation issues. It returns a
// non-zero exit status when anything is wrong.
func loadSuites(args []string, stderr io.Writer) ([]loadedSuite, int) {
	paths, err := suitePaths(args)
	if err != nil {
		fmt.Fprintf(stderr, "yahiko: %v\n", err)
		return nil, exitcode.Config
	}
	var suites []loadedSuite
	status := 0
	for _, p := range paths {
		s, err := config.Load(p)
		if err != nil {
			var verr *config.ValidationError
			if errors.As(err, &verr) {
				for _, is := range verr.Issues {
					fmt.Fprintf(stderr, "%s\n", is.String())
				}
				status = exitcode.Config
				continue
			}
			fmt.Fprintf(stderr, "yahiko: %v\n", err)
			if status == 0 {
				status = exitcode.Execution
			}
			continue
		}
		suites = append(suites, loadedSuite{display: filepath.ToSlash(p), suite: s})
	}
	if status == exitcode.Config {
		fmt.Fprintln(stderr, "yahiko: the suite is invalid; nothing was run")
	}
	return suites, status
}

func (s *selectFlags) selection(stderr io.Writer, cmd string) (runner.Selection, bool) {
	sel := runner.Selection{Tags: s.tags, SkipTags: s.skipTags}
	if s.filter != "" {
		re, err := regexp.Compile(s.filter)
		if err != nil {
			fmt.Fprintf(stderr, "yahiko %s: invalid --filter: %v\n", cmd, err)
			return sel, false
		}
		sel.Filter = re
	}
	return sel, true
}
