package cli

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nao1215/yahiko/internal/config"
	"github.com/nao1215/yahiko/internal/envinfo"
	"github.com/nao1215/yahiko/internal/exitcode"
	"github.com/nao1215/yahiko/internal/ghactions"
	"github.com/nao1215/yahiko/internal/gitwt"
	"github.com/nao1215/yahiko/internal/redact"
	"github.com/nao1215/yahiko/internal/report"
	"github.com/nao1215/yahiko/internal/runner"
)

// maxSeed keeps seeds within the integers a JSON number represents exactly.
const maxSeed = 1<<53 - 1

func parseSeed(v string) (uint64, error) {
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil || n > maxSeed {
		return 0, fmt.Errorf("seed must be an integer between 0 and %d", uint64(maxSeed))
	}
	return n, nil
}

func randomSeed() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 1
	}
	return binary.LittleEndian.Uint64(b[:]) & maxSeed
}

// measurement is the state of one run/compare/ci invocation.
type measurement struct {
	app     *App
	cmd     string
	flags   *measureFlags
	logw    io.Writer
	redact  *redact.Redactor
	tempDir string
}

func (m *measurement) logf(format string, args ...any) {
	if m.logw != nil {
		fmt.Fprintf(m.logw, "yahiko: "+format+"\n", args...)
	}
}

func runMeasure(ctx context.Context, a *App, cmd string, args []string) int {
	fs, v := newFlagSet(cmd)
	f, _ := v.(*measureFlags)
	operands, status, ok := parseFlags(fs, args, a.Stdout, a.Stderr)
	if !ok {
		return status
	}
	if code := f.check(a, cmd); code != 0 {
		return code
	}
	sel, ok := f.selection(a.Stderr, cmd)
	if !ok {
		return exitcode.Usage
	}

	suites, status := loadSuites(operands, a.Stderr)
	if status != 0 {
		return status
	}
	selected := 0
	for i := range suites {
		suites[i].suite.Benchmarks = sel.Select(suites[i].suite.Benchmarks)
		selected += len(suites[i].suite.Benchmarks)
	}
	if selected == 0 {
		fmt.Fprintf(a.Stderr, "yahiko %s: no benchmark matches the selection (--filter, --tag, --skip-tag); run \"yahiko list\" to see the benchmarks\n", cmd)
		return exitcode.Usage
	}

	m := &measurement{app: a, cmd: cmd, flags: f, redact: redact.New(a.Environ())}
	if !f.quiet {
		m.logw = a.Stderr
	}
	return m.execute(ctx, suites)
}

// check validates flag combinations that the flag package cannot express.
func (f *measureFlags) check(a *App, cmd string) int {
	valid := false
	for _, x := range config.Formats() {
		if string(x) == f.format {
			valid = true
		}
	}
	switch {
	case !valid:
		fmt.Fprintf(a.Stderr, "yahiko %s: unknown --format %q; use one of %s\n", cmd, f.format, formatNames())
		return exitcode.Usage
	case cmd == "compare" && f.against == "":
		fmt.Fprintf(a.Stderr, "yahiko compare: --against is required, for example: yahiko compare --against main\n")
		return exitcode.Usage
	}
	return 0
}

func (m *measurement) execute(ctx context.Context, suites []loadedSuite) (code int) {
	a, f := m.app, m.flags
	started := a.Now()
	seed := f.seed
	if !f.seedSet {
		seed = randomSeed()
	}
	compare := m.cmd != "run"
	gh := ghactions.FromLookup(a.LookupEnv)
	ref, baseSource, code := m.baseRef(gh)
	if code != 0 {
		return code
	}
	if compare {
		for _, ls := range suites {
			if code := checkComparable(ls, f.runs, a.Stderr); code != 0 {
				return code
			}
		}
	}
	if code := m.checkMetrics(suites); code != 0 {
		return code
	}

	tempDir, err := os.MkdirTemp("", "yahiko-")
	if err != nil {
		fmt.Fprintf(a.Stderr, "yahiko: create temporary directory: %v\n", err)
		return exitcode.Execution
	}
	m.tempDir = tempDir
	defer func() {
		if err := runner.RemoveAll(tempDir); err != nil {
			fmt.Fprintf(a.Stderr, "yahiko: remove temporary directory %s: %v\n", tempDir, err)
			code = cleanupFailed(code)
		}
	}()

	rep := m.newReport(started)
	git, code := m.openGit(ctx, suites, compare, rep)
	if code != 0 {
		return code
	}
	if compare {
		if code := git.checkout(ctx, m, ref, baseSource, rep); code != 0 {
			return code
		}
		defer func() {
			if err := git.worktree.Remove(ctx); err != nil {
				fmt.Fprintf(a.Stderr, "yahiko: remove the temporary worktree: %v\n", err)
				code = cleanupFailed(code)
			}
		}()
	}

	r := m.newRunner(seed)
	var inputs []report.SuiteInput
	for _, ls := range suites {
		if ctx.Err() != nil {
			break
		}
		inputs = append(inputs, m.measureSuite(ctx, r, ls, git.repo, git.worktree))
	}

	mode := report.ModeRun
	if compare {
		mode = report.ModeCompare
	}
	report.Judge(rep, inputs, report.Options{Mode: mode, Seed: seed, FailOnInconclusive: f.failOnInconclusive})
	rep.FinishedAt = a.Now().UTC()
	if ctx.Err() != nil && rep.Summary.ExitCode == exitcode.OK {
		rep.Summary.ExitCode = exitcode.Execution
	}
	return m.finish(ctx, rep, suites, gh)
}

// finish writes the reports and the GitHub Actions annotations, and ends
// with a line that says why a non-zero status was returned.
func (m *measurement) finish(ctx context.Context, rep *report.Report, suites []loadedSuite, gh ghactions.Env) int {
	a := m.app
	if code := m.writeReports(rep, suites, gh); code != 0 {
		return code
	}
	if gh.Actions {
		if err := report.WriteAnnotations(a.Stderr, rep); err != nil {
			fmt.Fprintf(a.Stderr, "yahiko: write annotations: %v\n", err)
		}
	}
	if ctx.Err() != nil {
		fmt.Fprintln(a.Stderr, "yahiko: interrupted; cleanup has run")
		return exitcode.Execution
	}
	if outcome := exitcode.Outcome(rep.Summary.ExitCode); outcome != "" {
		fmt.Fprintf(a.Stderr, "yahiko: exit %d: %s\n", rep.Summary.ExitCode, outcome)
	}
	return rep.Summary.ExitCode
}

// checkMetrics stops before anything is built or run when a benchmark
// requests a metric this platform cannot measure and its policy is fail. A
// benchmark whose policy is skip is only reported.
func (m *measurement) checkMetrics(suites []loadedSuite) int {
	r := &runner.Runner{Capabilities: m.app.Capabilities}
	code := 0
	for _, ls := range suites {
		for _, p := range r.UnsupportedMetrics(ls.suite.Benchmarks) {
			if p.Skip {
				m.logf("benchmark %q: metrics.%s will be reported as unsupported: %s", p.Benchmark, p.Group, p.Reason)
				continue
			}
			fmt.Fprintf(m.app.Stderr, "%s: benchmark %q: metrics.%s cannot be measured on this platform: %s\n    hint: remove it, or set metrics.unsupported: skip to measure everything else and report it as unsupported\n",
				ls.display, p.Benchmark, p.Group, p.Reason)
			code = exitcode.Metric
		}
	}
	if code != 0 {
		fmt.Fprintf(m.app.Stderr, "yahiko: exit %d: %s\n", code, exitcode.Outcome(code))
	}
	return code
}

// cleanupFailed returns the exit status after a cleanup problem: an execution
// error, unless the run had already failed, whose status is kept.
func cleanupFailed(code int) int {
	if code == exitcode.OK {
		return exitcode.Execution
	}
	return code
}

// baseRef decides the base revision: --against, then (for ci) YAHIKO_BASE_REF,
// then the GitHub Actions event.
func (m *measurement) baseRef(gh ghactions.Env) (string, string, int) {
	a, f := m.app, m.flags
	if m.cmd != "ci" || f.against != "" {
		return f.against, "--against", 0
	}
	if env, ok := a.LookupEnv("YAHIKO_BASE_REF"); ok && strings.TrimSpace(env) != "" {
		return strings.TrimSpace(env), "YAHIKO_BASE_REF", 0
	}
	base, err := gh.ResolveBase(a.ReadFile)
	if err != nil {
		fmt.Fprintf(a.Stderr, "yahiko ci: %v\n", err)
		return "", "", exitcode.Usage
	}
	return base.SHA, base.Source, 0
}

func (m *measurement) newReport(started time.Time) *report.Report {
	info := envinfo.Collect(m.app.LookupEnv)
	return &report.Report{
		YahikoVersion: info.YahikoVersion,
		StartedAt:     started.UTC(),
		Environment: report.Environment{
			OS: info.OS, Arch: info.Arch, CPUModel: info.CPUModel,
			LogicalCPUs: info.LogicalCPUs, GoVersion: info.GoVersion, CI: info.CI,
		},
	}
}

func (m *measurement) newRunner(seed uint64) *runner.Runner {
	r := &runner.Runner{
		TempDir:  m.tempDir,
		Seed:     seed,
		Log:      m.logw,
		Environ:  m.app.Environ,
		Redactor: m.redact,

		Capabilities: m.app.Capabilities,
	}
	if m.flags.runs > 0 {
		r.RunsOverride = &m.flags.runs
	}
	if m.flags.warmupSet {
		r.WarmupOverride = &m.flags.warmup
	}
	return r
}

// gitState is the repository of the suites and, in a comparison, the base
// worktree.
type gitState struct {
	repo     *gitwt.Repo
	worktree *gitwt.Worktree
}

// openGit finds the repository. Outside Git a plain run proceeds without Git
// information; a comparison cannot.
func (m *measurement) openGit(ctx context.Context, suites []loadedSuite, compare bool, rep *report.Report) (*gitState, int) {
	a := m.app
	repo, err := gitwt.Open(ctx, suites[0].suite.Dir)
	if err != nil {
		if compare {
			fmt.Fprintf(a.Stderr, "yahiko %s: %v; compare needs the suite to live in a Git repository\n", m.cmd, err)
			return nil, exitcode.Execution
		}
		return &gitState{}, 0
	}
	if compare {
		for _, ls := range suites[1:] {
			other, err := gitwt.Open(ctx, ls.suite.Dir)
			if err != nil || other.Top != repo.Top {
				fmt.Fprintf(a.Stderr, "yahiko %s: %s is not in the same Git repository as %s\n", m.cmd, ls.display, suites[0].display)
				return nil, exitcode.Usage
			}
		}
	}
	rep.Git = &report.Git{HeadSHA: repo.HeadSHA(ctx)}
	if dirty, err := repo.Dirty(ctx); err == nil {
		rep.Git.Dirty = dirty
	}
	return &gitState{repo: repo}, 0
}

// checkout resolves the base revision and checks it out into a worktree.
func (g *gitState) checkout(ctx context.Context, m *measurement, ref, source string, rep *report.Report) int {
	a := m.app
	sha, err := g.repo.ResolveCommit(ctx, ref)
	if err != nil {
		fmt.Fprintf(a.Stderr, "yahiko %s: %v\n", m.cmd, m.redact.String(err.Error()))
		return exitcode.Execution
	}
	rep.Git.BaseRef, rep.Git.BaseSHA, rep.Git.BaseSource = ref, sha, source
	m.logf("comparing base %s (%s) with the working tree%s", shortRef(sha), ref, dirtyNote(rep.Git.Dirty))
	wt, err := g.repo.AddWorktree(ctx, filepath.Join(m.tempDir, "git"), sha)
	if err != nil {
		fmt.Fprintf(a.Stderr, "yahiko %s: %v\n", m.cmd, m.redact.String(err.Error()))
		return exitcode.Execution
	}
	g.worktree = wt
	return 0
}

// checkComparable rejects settings that could only ever be inconclusive in a
// revision comparison.
func checkComparable(ls loadedSuite, runsOverride int, stderr io.Writer) int {
	for _, b := range ls.suite.Benchmarks {
		limit, what := b.MaxRuns, "max_runs"
		if b.Runs > 0 {
			limit, what = b.Runs, "runs"
		}
		if runsOverride > 0 {
			if runsOverride < b.Regression.MinSamples {
				fmt.Fprintf(stderr, "%s: benchmark %q: --runs %d is lower than regression.min_samples (%d), so the comparison could never be conclusive\n    hint: raise --runs or lower regression.min_samples\n",
					ls.display, b.Name, runsOverride, b.Regression.MinSamples)
				return exitcode.Usage
			}
			continue
		}
		if limit < b.Regression.MinSamples {
			fmt.Fprintf(stderr, "%s: benchmark %q: %s (%d) is lower than regression.min_samples (%d), so the comparison could never be conclusive\n    hint: raise %s or lower regression.min_samples\n",
				ls.display, b.Name, what, limit, b.Regression.MinSamples, what)
			return exitcode.Config
		}
	}
	return 0
}

func (m *measurement) measureSuite(ctx context.Context, r *runner.Runner, ls loadedSuite, repo *gitwt.Repo, wt *gitwt.Worktree) report.SuiteInput {
	s := ls.suite
	in := report.SuiteInput{Suite: s, File: ls.display}

	head := runner.Side{Name: runner.SideHead, Root: s.Dir, HeadRoot: s.Dir, ProjectRoot: s.Dir}
	switch {
	case wt != nil:
		head.ProjectRoot = repo.Top
	default:
		// A plain run may name suites from different repositories; each one's
		// paths are confined to its own repository.
		if own, err := gitwt.Open(ctx, s.Dir); err == nil {
			head.ProjectRoot = own.Top
		}
	}
	r.ProjectRoot = head.ProjectRoot
	sides := []runner.Side{head}
	if wt != nil {
		rel, err := filepath.Rel(repo.Top, realDir(s.Dir))
		if err != nil || strings.HasPrefix(rel, "..") {
			in.BuildFailure = &runner.Failure{Kind: runner.FailPath, Message: fmt.Sprintf("%s is outside the Git repository %s", s.Dir, repo.Top)}
			return in
		}
		base := runner.Side{Name: runner.SideBase, Root: filepath.Join(wt.Dir, rel), HeadRoot: s.Dir, ProjectRoot: wt.Dir}
		sides = []runner.Side{base, head}
	}
	for i := range sides {
		if s.Build != nil {
			sides[i].Artifact = filepath.Join(m.tempDir, "artifacts", ls.suiteKey(), sides[i].Name, "artifact"+exeSuffix())
		}
	}

	m.logf("suite %q (%s): %s", s.Name, ls.display, plural(len(s.Benchmarks), "benchmark"))
	for _, side := range sides {
		// Commands, hooks and relative paths run inside ${root} of each
		// revision, so a revision without the suite directory cannot be
		// measured, with or without a build.
		if _, err := os.Stat(side.Root); err != nil {
			kind := runner.FailBuild
			if s.Build == nil {
				kind = runner.FailSetup
			}
			in.BuildFailure = &runner.Failure{Kind: kind, Message: fmt.Sprintf("the suite directory does not exist in the %s revision (%s)", side.Name, side.Root)}
			return in
		}
		if s.Build == nil {
			continue
		}
		if f := r.Build(ctx, s, side); f != nil {
			in.BuildFailure = f
			return in
		}
	}
	for _, b := range s.Benchmarks {
		if ctx.Err() != nil {
			break
		}
		in.Benchmarks = append(in.Benchmarks, r.Measure(ctx, b, sides))
	}
	return in
}

func (ls loadedSuite) suiteKey() string {
	sum := 0
	for _, c := range ls.suite.Path {
		sum = sum*31 + int(c)
	}
	return strconv.FormatUint(uint64(sum)&0xffffffff, 16)
}

func realDir(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func exeSuffix() string {
	if filepath.Separator == '\\' {
		return ".exe"
	}
	return ""
}

func shortRef(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func dirtyNote(dirty bool) string {
	if dirty {
		return " (including uncommitted changes)"
	}
	return ""
}

// writeReports writes the requested report to stdout or --output, the
// suite's configured outputs, and the job summary. A report that cannot be
// written is an error: a CI job must not pass with its evidence missing.
func (m *measurement) writeReports(rep *report.Report, suites []loadedSuite, gh ghactions.Env) int {
	a, f := m.app, m.flags
	color := f.format == string(config.FormatTable) && f.output == "" && a.StdoutIsTerminal && !f.noColor && m.cmd != "ci"
	if v, ok := a.LookupEnv("NO_COLOR"); ok && v != "" {
		color = false
	}
	if term, _ := a.LookupEnv("TERM"); term == "dumb" {
		color = false
	}

	var errs []error
	if f.output != "" {
		errs = append(errs, writeFile(f.output, false, func(w io.Writer) error {
			return report.Write(w, config.Format(f.format), rep, false)
		}))
	} else if err := report.Write(a.Stdout, config.Format(f.format), rep, color); err != nil {
		errs = append(errs, fmt.Errorf("write report to stdout: %w", err))
	}

	for _, ls := range suites {
		for _, o := range ls.suite.Outputs {
			p := filepath.Join(ls.suite.Dir, filepath.FromSlash(o.Path))
			format := o.Format
			errs = append(errs, writeFile(p, false, func(w io.Writer) error {
				return report.Write(w, format, rep, false)
			}))
			m.logf("wrote %s report to %s", format, p)
		}
	}

	summaries := []string{}
	if f.summary != "" {
		summaries = append(summaries, f.summary)
	}
	if m.cmd == "ci" && gh.StepSummary != "" && gh.StepSummary != f.summary {
		summaries = append(summaries, gh.StepSummary)
	}
	for _, p := range summaries {
		errs = append(errs, writeFile(p, true, func(w io.Writer) error {
			return report.WriteGitHubSummary(w, rep)
		}))
	}

	if err := errors.Join(errs...); err != nil {
		fmt.Fprintf(a.Stderr, "yahiko: %v\n", err)
		return exitcode.Execution
	}
	return 0
}

func writeFile(path string, appendMode bool, write func(io.Writer) error) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create report directory %s: %w", dir, err)
		}
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if appendMode {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	file, err := os.OpenFile(path, flags, 0o644) //nolint:gosec // report paths are chosen by the user
	if err != nil {
		return fmt.Errorf("open report file: %w", err)
	}
	werr := write(file)
	cerr := file.Close()
	if err := errors.Join(werr, cerr); err != nil {
		return fmt.Errorf("write report file %s: %w", path, err)
	}
	return nil
}
