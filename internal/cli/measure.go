package cli

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nao1215/himorime/internal/config"
	"github.com/nao1215/himorime/internal/envinfo"
	"github.com/nao1215/himorime/internal/exitcode"
	"github.com/nao1215/himorime/internal/ghactions"
	"github.com/nao1215/himorime/internal/gitwt"
	"github.com/nao1215/himorime/internal/proc"
	"github.com/nao1215/himorime/internal/redact"
	"github.com/nao1215/himorime/internal/report"
	"github.com/nao1215/himorime/internal/runner"
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
	// tools are the versions of report.versions of every suite measured.
	tools []report.Tool
}

// addTool records a tool version. A tool several suites name is listed once,
// with the version the first of them recorded.
func (m *measurement) addTool(t report.Tool) {
	for _, x := range m.tools {
		if x.Name == t.Name {
			return
		}
	}
	m.tools = append(m.tools, t)
}

func (m *measurement) logf(format string, args ...any) {
	if m.logw != nil {
		fmt.Fprintf(m.logw, "himorime: "+format+"\n", args...)
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
	selected := suites[:0]
	for _, ls := range suites {
		ls.suite.Benchmarks = sel.Select(ls.suite.Benchmarks)
		if len(ls.suite.Benchmarks) > 0 {
			selected = append(selected, ls)
		}
	}
	suites = selected
	if len(suites) == 0 {
		fmt.Fprintf(a.Stderr, "himorime %s: no benchmark matches the selection (--filter, --tag, --skip-tag); run \"himorime list\" to see the benchmarks\n", cmd)
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
		fmt.Fprintf(a.Stderr, "himorime %s: unknown --format %q; use one of %s\n", cmd, f.format, formatNames())
		return exitcode.Usage
	case cmd == "compare" && f.against == "":
		fmt.Fprintf(a.Stderr, "himorime compare: --against is required, for example: himorime compare --against main\n")
		return exitcode.Usage
	case f.section == "":
	case !report.ValidSectionName(f.section):
		fmt.Fprintf(a.Stderr, "himorime %s: invalid --section %q: use lowercase letters, digits and '-', starting with a letter or digit\n", cmd, f.section)
		return exitcode.Usage
	case f.output == "":
		fmt.Fprintf(a.Stderr, "himorime %s: --section needs --output FILE: it replaces a section of an existing Markdown file, for example: himorime %s --format markdown --output README.md --section %s\n", cmd, cmd, f.section)
		return exitcode.Usage
	case f.format != string(config.FormatMarkdown):
		fmt.Fprintf(a.Stderr, "himorime %s: --section needs --format markdown, not %q\n", cmd, f.format)
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
	if err := checkReportDestinations(suites, f, gh, m.cmd == "ci"); err != nil {
		fmt.Fprintf(a.Stderr, "himorime: %v\n", err)
		if errors.Is(err, errReportConflict) {
			return exitcode.Usage
		}
		return exitcode.Execution
	}
	ref, baseSource, code := m.baseRef(gh)
	if code != 0 {
		return code
	}
	if code := m.checkMetrics(suites); code != 0 {
		return code
	}
	if measuresMemory(suites) {
		// Commands whose memory is measured start from a spawner; it comes
		// up while the run is prepared.
		proc.PrepareSpawner()
	}

	tempDir, err := os.MkdirTemp("", "himorime-")
	if err != nil {
		fmt.Fprintf(a.Stderr, "himorime: create temporary directory: %v\n", err)
		return exitcode.Execution
	}
	m.tempDir = tempDir
	defer func() {
		if err := runner.RemoveAll(tempDir); err != nil {
			fmt.Fprintf(a.Stderr, "himorime: remove temporary directory %s: %v\n", tempDir, err)
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
				fmt.Fprintf(a.Stderr, "himorime: remove the temporary worktree: %v\n", err)
				code = cleanupFailed(code)
			}
		}()
		if code := git.checkComparable(suites, f.runs, a.Stderr); code != 0 {
			return code
		}
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
	rep.Environment.Tools = m.tools
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
			fmt.Fprintf(a.Stderr, "himorime: write annotations: %v\n", err)
		}
	}
	if ctx.Err() != nil {
		fmt.Fprintln(a.Stderr, "himorime: interrupted; cleanup has run")
		return exitcode.Execution
	}
	if outcome := exitcode.Outcome(rep.Summary.ExitCode); outcome != "" {
		fmt.Fprintf(a.Stderr, "himorime: exit %d: %s\n", rep.Summary.ExitCode, outcome)
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
		fmt.Fprintf(m.app.Stderr, "himorime: exit %d: %s\n", code, exitcode.Outcome(code))
	}
	return code
}

func measuresMemory(suites []loadedSuite) bool {
	for _, ls := range suites {
		for _, b := range ls.suite.Benchmarks {
			if b.Metrics.Memory {
				return true
			}
		}
	}
	return false
}

// cleanupFailed returns the exit status after a cleanup problem: an execution
// error, unless the run had already failed, whose status is kept.
func cleanupFailed(code int) int {
	if code == exitcode.OK {
		return exitcode.Execution
	}
	return code
}

// baseRef decides the base revision: --against, then (for ci) HIMORIME_BASE_REF,
// then the GitHub Actions event.
func (m *measurement) baseRef(gh ghactions.Env) (string, string, int) {
	a, f := m.app, m.flags
	if m.cmd != "ci" || f.against != "" {
		return f.against, "--against", 0
	}
	if env, ok := a.LookupEnv("HIMORIME_BASE_REF"); ok && strings.TrimSpace(env) != "" {
		return strings.TrimSpace(env), "HIMORIME_BASE_REF", 0
	}
	base, err := gh.ResolveBase(a.ReadFile)
	if err != nil {
		fmt.Fprintf(a.Stderr, "himorime ci: %v\n", err)
		return "", "", exitcode.Usage
	}
	return base.SHA, base.Source, 0
}

func (m *measurement) newReport(started time.Time) *report.Report {
	info := envinfo.Collect(m.app.LookupEnv)
	return &report.Report{
		HimorimeVersion: info.HimorimeVersion,
		StartedAt:       started.UTC(),
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
			fmt.Fprintf(a.Stderr, "himorime %s: %v; compare needs the suite to live in a Git repository\n", m.cmd, err)
			return nil, exitcode.Execution
		}
		return &gitState{}, 0
	}
	if compare {
		for _, ls := range suites[1:] {
			other, err := gitwt.Open(ctx, ls.suite.Dir)
			if err != nil || other.Top != repo.Top {
				fmt.Fprintf(a.Stderr, "himorime %s: %s is not in the same Git repository as %s\n", m.cmd, ls.display, suites[0].display)
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
		fmt.Fprintf(a.Stderr, "himorime %s: %v\n", m.cmd, m.redact.String(err.Error()))
		return exitcode.Execution
	}
	rep.Git.BaseRef, rep.Git.BaseSHA, rep.Git.BaseSource = ref, sha, source
	m.logf("comparing base %s (%s) with the working tree%s", shortRef(sha), ref, dirtyNote(rep.Git.Dirty))
	wt, err := g.repo.AddWorktree(ctx, filepath.Join(m.tempDir, "git"), sha)
	if err != nil {
		fmt.Fprintf(a.Stderr, "himorime %s: %v\n", m.cmd, m.redact.String(err.Error()))
		return exitcode.Execution
	}
	g.worktree = wt
	return 0
}

func (g *gitState) checkComparable(suites []loadedSuite, runs int, stderr io.Writer) int {
	for _, ls := range suites {
		root, err := ls.baseRoot(g.repo, g.worktree)
		if err != nil {
			fmt.Fprintf(stderr, "himorime: %v\n", err)
			return exitcode.Execution
		}
		if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if code := checkComparable(ls, runs, stderr); code != 0 {
			return code
		}
	}
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
		root, err := ls.baseRoot(repo, wt)
		if err != nil {
			in.BuildFailure = &runner.Failure{Kind: runner.FailPath, Message: err.Error()}
			return in
		}
		base := runner.Side{Name: runner.SideBase, Root: root, HeadRoot: s.Dir, ProjectRoot: wt.Dir}
		sides = []runner.Side{base, head}
	}
	for i := range sides {
		if s.Build != nil {
			sides[i].Artifact = filepath.Join(m.tempDir, "artifacts", ls.suiteKey(), sides[i].Name, "artifact"+exeSuffix())
		}
	}

	if len(sides) == 2 {
		// The change adds this suite: the base revision has nothing to
		// compare against, so the working tree is measured alone and judged
		// as a plain run would judge it. A broken build or an exceeded
		// budget fails the pull request that adds it, not the next one.
		if _, err := os.Stat(sides[0].Root); errors.Is(err, fs.ErrNotExist) {
			m.logf("suite %q (%s): new in this revision; %s does not exist in the base revision, so only this revision is measured", s.Name, ls.display, filepath.Dir(ls.display))
			in.NewInHead = true
			sides = sides[1:]
		}
	}
	m.logf("suite %q (%s): %s", s.Name, ls.display, plural(len(s.Benchmarks), "benchmark"))
	// Versions are recorded from the working tree, once, before anything is
	// built or measured.
	for _, tool := range s.Versions {
		version, f := r.Version(ctx, tool, head)
		if f != nil {
			in.BuildFailure = f
			return in
		}
		m.logf("%s: %s", tool.Name, version)
		m.addTool(report.Tool{Name: tool.Name, Version: version})
	}
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

func (ls loadedSuite) baseRoot(repo *gitwt.Repo, wt *gitwt.Worktree) (string, error) {
	rel, err := filepath.Rel(repo.Top, realDir(ls.suite.Dir))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s is outside the Git repository %s", ls.suite.Dir, repo.Top)
	}
	return filepath.Join(wt.Dir, rel), nil
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
	if f.section != "" {
		errs = append(errs, writeSection(f.output, f.section, rep))
	} else if f.output != "" {
		errs = append(errs, writeFile(nil, f.output, false, func(w io.Writer) error {
			return report.Write(w, config.Format(f.format), rep, false)
		}))
	} else if err := report.Write(a.Stdout, config.Format(f.format), rep, color); err != nil {
		errs = append(errs, fmt.Errorf("write report to stdout: %w", err))
	}

	for _, ls := range suites {
		for _, o := range ls.suite.Outputs {
			p := filepath.Join(ls.suite.Dir, filepath.FromSlash(o.Path))
			if err := writeSuiteReport(ls.suite.Dir, o, rep); err != nil {
				errs = append(errs, fmt.Errorf("write report %s: %w", p, err))
			} else if o.Section != "" {
				m.logf("updated section %s of %s", o.Section, p)
			} else {
				m.logf("wrote %s report to %s", o.Format, p)
			}
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
		errs = append(errs, writeFile(nil, p, true, func(w io.Writer) error {
			return report.WriteGitHubSummary(w, rep)
		}))
	}

	if err := errors.Join(errs...); err != nil {
		fmt.Fprintf(a.Stderr, "himorime: %v\n", err)
		return exitcode.Execution
	}
	return 0
}

func writeFile(root *os.Root, path string, appendMode bool, write func(io.Writer) error) error {
	mkdir, open := os.MkdirAll, os.OpenFile
	if root != nil {
		mkdir, open = root.MkdirAll, root.OpenFile
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := mkdir(dir, 0o750); err != nil {
			return fmt.Errorf("create report directory %s: %w", dir, err)
		}
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if appendMode {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	file, err := open(path, flags, 0o644)
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
