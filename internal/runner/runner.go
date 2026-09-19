// Package runner executes benchmark suites and records raw samples.
//
// The runner measures; it does not judge. Budgets, relative speeds and
// regression verdicts are computed afterwards from the samples it returns, so
// the measuring loop stays small and the judgement is testable without
// starting a single process.
package runner

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nao1215/himorime/internal/config"
	"github.com/nao1215/himorime/internal/metric"
	"github.com/nao1215/himorime/internal/proc"
	"github.com/nao1215/himorime/internal/redact"
)

// cleanupGrace bounds cleanup hooks started after the run was interrupted:
// the user asked himorime to stop, so cleanup gets a fair chance but no more.
const cleanupGrace = 30 * time.Second

// stderrTailBytes bounds how much of a failing command's standard error is
// kept for the report.
const stderrTailBytes = 2048

// Runner measures benchmarks.
type Runner struct {
	// TempDir is the private temporary directory of this invocation. Workdirs
	// and captured output live below it; the caller removes it.
	TempDir string
	// ProjectRoot is the top of the working tree (Git top level, or the suite
	// directory outside Git). Relative paths may not resolve outside it.
	ProjectRoot string
	// Seed fixes the execution order.
	Seed uint64
	// RunsOverride and WarmupOverride replace the suite's values when non-nil.
	RunsOverride   *int
	WarmupOverride *int
	// Log receives progress lines. nil discards them.
	Log io.Writer
	// Clock measures durations; time.Now when nil.
	Clock proc.Clock
	// Environ is the base environment of every process; os.Environ when nil.
	Environ func() []string
	// Redactor masks secrets in failure messages.
	Redactor *redact.Redactor
	// Exec starts one process; proc.Run when nil. Tests replace it to
	// simulate what a platform's collector reports.
	Exec func(ctx context.Context, s proc.Spec, now proc.Clock) (proc.Result, error)
	// Capabilities reports what the platform can measure;
	// proc.Capabilities when nil.
	Capabilities func() (cpu, memory error)
}

func (r *Runner) exec(ctx context.Context, s proc.Spec) (proc.Result, error) {
	if r.Exec != nil {
		return r.Exec(ctx, s, r.Clock)
	}
	return proc.Run(ctx, s, r.Clock)
}

// MetricProblem is a requested metric group the platform cannot measure.
type MetricProblem struct {
	Benchmark string
	Group     metric.Group
	Reason    string
	// Skip is true when the benchmark's unsupported policy skips the group.
	Skip bool
}

// UnsupportedMetrics lists the metric groups the benchmarks request that this
// platform cannot measure at all. It runs nothing, so a suite whose policy is
// fail can be stopped before any build or setup.
func (r *Runner) UnsupportedMetrics(benchmarks []config.Benchmark) []MetricProblem {
	capabilities := proc.Capabilities
	if r.Capabilities != nil {
		capabilities = r.Capabilities
	}
	cpuErr, memErr := capabilities()
	var out []MetricProblem
	for _, b := range benchmarks {
		for _, x := range []struct {
			group metric.Group
			err   error
		}{{metric.GroupCPU, cpuErr}, {metric.GroupMemory, memErr}} {
			if x.err != nil && b.Metrics.Collects(x.group) {
				out = append(out, MetricProblem{Benchmark: b.Name, Group: x.group, Reason: x.err.Error(), Skip: b.Metrics.Unsupported == config.UnsupportedSkip})
			}
		}
	}
	return out
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func (r *Runner) logf(format string, args ...any) {
	if r.Log == nil {
		return
	}
	fmt.Fprintf(r.Log, "himorime: "+format+"\n", args...)
}

func (r *Runner) environ() []string {
	if r.Environ != nil {
		return r.Environ()
	}
	return os.Environ()
}

func (r *Runner) lookupEnv(name string) (string, bool) {
	for _, kv := range r.environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if k == name || (runtime.GOOS == "windows" && strings.EqualFold(k, name)) {
			return v, true
		}
	}
	return "", false
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// Build runs the suite's build step for one side. It returns nil when the
// suite has no build or the build succeeded.
func (r *Runner) Build(ctx context.Context, s *config.Suite, side Side) *Failure {
	if s.Build == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(side.Artifact), 0o700); err != nil {
		return &Failure{Kind: FailInternal, Message: fmt.Sprintf("create artifact directory: %v", err)}
	}
	r.logf("building %s: %s", side.Name, r.Redactor.String(s.Build.Display()))
	vars := config.Vars{Artifact: side.Artifact, Root: side.Root, HeadRoot: side.HeadRoot, Exe: exeSuffix(), LookupEnv: r.lookupEnv}
	cwd := side.Root
	if s.Build.Cwd != "" {
		var f *Failure
		cwd, f = r.resolvePath(s.Build.Cwd, vars, side.Root, side.ProjectRoot)
		if f != nil {
			return f
		}
	}
	f := r.runHook(ctx, *s.Build, vars, cwd, FailBuild, "build ("+side.Name+")")
	if f != nil {
		return f
	}
	if _, err := os.Stat(side.Artifact); err != nil {
		return &Failure{
			Kind:    FailBuild,
			Message: fmt.Sprintf("the build for %s succeeded but did not write ${artifact}; pass ${artifact} to the build command as its output path", side.Name),
		}
	}
	return nil
}

// unit is one (side, command) pair measured in the interleaved loop.
type unit struct {
	side    int
	command int
	m       *Measurement
	total   time.Duration
}

// sideState is the per-side state of one benchmark.
type sideState struct {
	side    Side
	workdir string
	vars    config.Vars
	stdin   string
	setupOK bool
}

// Measure runs one benchmark across the given sides. With more than one side
// (a revision comparison), excluded commands with budgets still run on the
// head side. The result is a named return so the deferred
// teardown can record a cleanup failure.
func (r *Runner) Measure(ctx context.Context, b config.Benchmark, sides []Side) (res BenchmarkResult) {
	res.Benchmark = b
	warmup, runs := b.Warmup, b.Runs
	if r.WarmupOverride != nil {
		warmup = *r.WarmupOverride
	}
	if r.RunsOverride != nil {
		runs = *r.RunsOverride
	}

	states := make([]*sideState, len(sides))
	for i, side := range sides {
		states[i] = &sideState{side: side}
	}
	defer r.teardown(ctx, b, states, &res)

	skipped := map[metric.Group]string{}
	for _, p := range r.UnsupportedMetrics([]config.Benchmark{b}) {
		if !p.Skip {
			res.Failure = &Failure{
				Kind:    FailMetricUnsupported,
				Metric:  p.Group,
				Message: fmt.Sprintf("metrics.%s was requested, but %s; set metrics.unsupported: skip to measure the rest without it", p.Group, p.Reason),
			}
			return res
		}
		skipped[p.Group] = p.Reason
		r.logf("benchmark %q: skipping %s metrics: %s", b.Name, p.Group, p.Reason)
	}

	if b.Terminal && !proc.TerminalSupported() {
		res.Failure = &Failure{Kind: FailStart, Message: proc.ErrTerminalUnsupported.Error()}
		return res
	}

	for _, st := range states {
		if f := r.prepareSide(ctx, b, st); f != nil {
			res.Failure = f
			return res
		}
	}

	units := commandUnits(b, sides, &res)
	for _, u := range units {
		for g, reason := range skipped {
			u.m.skip(g, reason)
		}
	}
	minRuns := minRunsFor(b, len(sides))
	mode := plural(runs, "run")
	if runs == 0 {
		mode = fmt.Sprintf("adaptive runs (min %d, max %d, min time %s)", minRuns, b.MaxRuns, b.MinTime)
	}
	r.logf("benchmark %q: %s, %d warmup, %s", b.Name, plural(len(units), "measurement unit"), warmup, mode)

	l := &loop{
		r: r, ctx: ctx, b: b, states: states, units: units,
		rng: rand.New(rand.NewPCG(r.Seed, nameHash(b.Name))), //nolint:gosec // the order must be reproducible from --seed
	}
	for range warmup {
		if _, f := l.round(false); f != nil {
			res.Failure = f
			return res
		}
	}
	for round := 0; !done(units, runs, minRuns, b, round); round++ {
		active, f := l.round(true)
		if f != nil {
			res.Failure = f
			return res
		}
		if active == 0 {
			break
		}
		res.Rounds = round + 1
	}
	r.logf("benchmark %q: finished after %s", b.Name, plural(res.Rounds, "round"))
	return res
}

// commandUnits creates one measurement unit per (side, command) pair and the
// matching command results.
func commandUnits(b config.Benchmark, sides []Side, res *BenchmarkResult) []*unit {
	var units []*unit
	for ci, c := range b.Commands {
		cr := CommandResult{Command: c, Sides: map[string]*Measurement{}}
		budgeted := slices.ContainsFunc(b.Budgets, func(bud config.Budget) bool { return bud.Command == c.Name })
		for si, side := range sides {
			if len(sides) > 1 && !b.Regression.Compares(c.Name) && (side.Name != SideHead || !budgeted) {
				continue
			}
			m := &Measurement{}
			cr.Sides[side.Name] = m
			units = append(units, &unit{side: si, command: ci, m: m})
		}
		res.Commands = append(res.Commands, cr)
	}
	return units
}

// loop runs rounds of one benchmark.
type loop struct {
	r      *Runner
	ctx    context.Context
	b      config.Benchmark
	states []*sideState
	units  []*unit
	rng    *rand.Rand
}

// round runs every healthy unit once in a seeded random order. It returns how
// many units ran, and a failure only when the run was interrupted; command
// failures are recorded on their unit.
func (l *loop) round(record bool) (int, *Failure) {
	active := 0
	for _, i := range l.rng.Perm(len(l.units)) {
		u := l.units[i]
		if u.m.Failure != nil {
			continue
		}
		active++
		f := l.r.runOnce(l.ctx, l.b, l.states[u.side], u, record)
		if f != nil && f.Kind == FailInterrupted {
			return active, f
		}
		if f != nil {
			u.m.Failure = f
		}
		if !record {
			u.m.Warmups++
		}
	}
	return active, nil
}

// minRunsFor is the fewest measured runs of adaptive measuring. A revision
// comparison (more than one side) also needs regression.min_samples on each
// side, or its verdict could only be inconclusive; a plain run judges no
// comparison and keeps min_runs. Callers reject a max_runs, runs or --runs
// below min_samples before measuring, so the loop can always reach it.
func minRunsFor(b config.Benchmark, sides int) int {
	if sides > 1 && b.Regression.MinSamples > b.MinRuns {
		return b.Regression.MinSamples
	}
	return b.MinRuns
}

// done decides whether the measuring loop stops before starting round. Every
// unit runs once per round, so all healthy units hold the same sample count.
func done(units []*unit, runs, minRuns int, b config.Benchmark, round int) bool {
	if runs > 0 {
		return round >= runs
	}
	if round >= b.MaxRuns {
		return true
	}
	for _, u := range units {
		if u.m.Failure != nil {
			continue
		}
		if len(u.m.Samples) < minRuns || u.total < b.MinTime {
			return false
		}
	}
	return true
}

func nameHash(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

func (r *Runner) prepareSide(ctx context.Context, b config.Benchmark, st *sideState) *Failure {
	base := filepath.Join(r.TempDir, st.side.Name)
	if err := os.MkdirAll(base, 0o700); err != nil {
		return &Failure{Kind: FailInternal, Message: fmt.Sprintf("create temporary directory: %v", err)}
	}
	wd, err := os.MkdirTemp(base, "workdir-")
	if err != nil {
		return &Failure{Kind: FailInternal, Message: fmt.Sprintf("create workdir: %v", err)}
	}
	st.workdir = wd
	st.vars = config.Vars{Artifact: st.side.Artifact, Root: st.side.Root, HeadRoot: st.side.HeadRoot, Workdir: wd, Exe: exeSuffix(), LookupEnv: r.lookupEnv}

	if b.Stdin.Kind == config.StdinContent {
		content, err := config.Expand(b.Stdin.Content, st.vars, nil)
		if err != nil {
			return &Failure{Kind: FailSetup, Message: fmt.Sprintf("stdin content: %v", err)}
		}
		f, err := os.CreateTemp(base, "stdin-")
		if err != nil {
			return &Failure{Kind: FailInternal, Message: fmt.Sprintf("write stdin content: %v", err)}
		}
		_, werr := f.WriteString(content)
		cerr := f.Close()
		if err := errors.Join(werr, cerr); err != nil {
			return &Failure{Kind: FailInternal, Message: fmt.Sprintf("write stdin content: %v", err)}
		}
		st.stdin = f.Name()
	}

	st.setupOK = true
	for i, h := range b.Setup {
		if f := r.runBenchmarkHook(ctx, h, st, FailSetup, fmt.Sprintf("setup[%d]", i)); f != nil {
			return f
		}
	}

	if b.Stdin.Kind == config.StdinFile {
		p, f := r.resolvePath(b.Stdin.File, st.vars, st.side.Root, st.side.ProjectRoot, wd)
		if f != nil {
			f.Message = "stdin: " + f.Message
			return f
		}
		info, err := os.Stat(p)
		if err != nil {
			return &Failure{Kind: FailSetup, Message: fmt.Sprintf("stdin fixture %s: %v%s", b.Stdin.File, redactErr(r.Redactor, err), sharedHint(st.side, b.Stdin.File, err))}
		}
		if info.IsDir() {
			return &Failure{Kind: FailSetup, Message: fmt.Sprintf("stdin fixture %s is a directory", b.Stdin.File)}
		}
		st.stdin = p
	}
	return nil
}

func (r *Runner) teardown(ctx context.Context, b config.Benchmark, states []*sideState, res *BenchmarkResult) {
	cctx := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), cleanupGrace)
		defer cancel()
	}
	for _, st := range states {
		if st.setupOK {
			for i, h := range b.Cleanup {
				if f := r.runBenchmarkHook(cctx, h, st, FailCleanup, fmt.Sprintf("cleanup[%d]", i)); f != nil && res.Failure == nil {
					res.Failure = f
				}
			}
		}
		if st.workdir != "" {
			if err := removeTemp(r.TempDir, st.workdir); err != nil && res.Failure == nil {
				res.Failure = &Failure{Kind: FailCleanup, Message: fmt.Sprintf("remove workdir: %v", err)}
			}
		}
	}
	if ctx.Err() != nil && res.Failure == nil {
		res.Failure = &Failure{Kind: FailInterrupted, Message: "stopped before the benchmark completed"}
	}
}

func (r *Runner) runBenchmarkHook(ctx context.Context, h config.Exec, st *sideState, kind FailureKind, label string) *Failure {
	dir := st.side.Root
	if h.Cwd != "" {
		var f *Failure
		dir, f = r.resolvePath(h.Cwd, st.vars, st.side.Root, st.side.ProjectRoot, st.workdir)
		if f != nil {
			f.Kind = kind
			return f
		}
	}
	return r.runHook(ctx, h, st.vars, dir, kind, label)
}

// runHook runs a build step or a hook to completion and reports a failure.
func (r *Runner) runHook(ctx context.Context, e config.Exec, vars config.Vars, dir string, kind FailureKind, label string) *Failure {
	errFile, err := os.CreateTemp(r.TempDir, "hook-stderr-")
	if err != nil {
		return &Failure{Kind: FailInternal, Message: fmt.Sprintf("capture stderr: %v", err)}
	}
	defer func() {
		_ = errFile.Close()
		_ = os.Remove(errFile.Name())
	}()
	// nil is the null device. A Go writer such as io.Discard would make Wait
	// also wait for background processes that inherited the output pipe.
	return r.runProcess(ctx, e, vars, dir, kind, label, nil, errFile)
}

// versionOutputBytes bounds how much of a version command's output is read.
const versionOutputBytes = 64 << 10

// Version runs the version command of a tool once in the suite directory of
// side and returns the first non-empty line it printed on standard output,
// or else on standard error, trimmed. A command that fails, times out after
// config.DefaultVersionTimeout or prints nothing is a setup failure.
func (r *Runner) Version(ctx context.Context, tool config.ToolVersion, side Side) (string, *Failure) {
	label := "report.versions." + tool.Name
	// LC_ALL=C keeps the version line in English, so a page generated on a
	// machine with another locale does not change language.
	e := config.Exec{Argv: tool.Argv, Env: []config.EnvVar{{Name: "LC_ALL", Value: "C"}}, Timeout: config.DefaultVersionTimeout}
	vars := config.Vars{Root: side.Root, HeadRoot: side.HeadRoot, Exe: exeSuffix(), LookupEnv: r.lookupEnv}
	var files []*os.File
	defer func() {
		for _, f := range files {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}()
	for _, name := range []string{"version-stdout-", "version-stderr-"} {
		f, err := os.CreateTemp(r.TempDir, name)
		if err != nil {
			return "", &Failure{Kind: FailInternal, Message: fmt.Sprintf("capture output: %v", err)}
		}
		files = append(files, f)
	}
	if f := r.runProcess(ctx, e, vars, side.Root, FailSetup, label, files[0], files[1]); f != nil {
		return "", f
	}
	for _, f := range files {
		if v := firstLine(f.Name()); v != "" {
			return r.Redactor.String(v), nil
		}
	}
	return "", &Failure{Kind: FailSetup, Message: fmt.Sprintf("%s printed nothing on standard output or standard error: %s", label, r.Redactor.String(e.Display()))}
}

// firstLine returns the first non-empty line of a capture file, trimmed.
func firstLine(path string) string {
	f, err := os.Open(path) //nolint:gosec // G304: a capture file himorime created in its temp dir
	if err != nil {
		return ""
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, versionOutputBytes))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.ToValidUTF8(string(data), "?"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// runProcess runs a build step, a hook or a version command to completion
// with the given output files, and reports a failure with the tail of
// errFile.
func (r *Runner) runProcess(ctx context.Context, e config.Exec, vars config.Vars, dir string, kind FailureKind, label string, outFile, errFile *os.File) *Failure {
	spec, f := r.spec(e, vars, dir)
	if f != nil {
		f.Kind = kind
		f.Message = label + ": " + f.Message
		return f
	}
	if outFile != nil {
		spec.Stdout = outFile
	}
	spec.Stderr = errFile
	spec.Timeout = e.Timeout
	res, err := r.exec(ctx, spec)
	if res.Canceled {
		return &Failure{Kind: FailInterrupted, Message: label + ": interrupted"}
	}
	if err != nil {
		if msg := missingWorkingDirectory(dir, e.Cwd, isBenchmarkHook(label)); msg != "" {
			return &Failure{Kind: kind, Message: fmt.Sprintf("%s: %s: %s", label, r.Redactor.String(e.Display()), msg)}
		}
		return &Failure{Kind: kind, Message: fmt.Sprintf("%s: %s: %v", label, r.Redactor.String(e.Display()), redactErr(r.Redactor, err))}
	}
	switch {
	case res.TimedOut:
		return &Failure{Kind: kind, Message: fmt.Sprintf("%s timed out after %s: %s", label, e.Timeout, r.Redactor.String(e.Display())), Stderr: r.tail(errFile.Name())}
	case res.ExitCode != 0:
		return &Failure{Kind: kind, ExitCode: res.ExitCode, Message: fmt.Sprintf("%s exited with status %d: %s", label, res.ExitCode, r.Redactor.String(e.Display())), Stderr: r.tail(errFile.Name())}
	}
	return nil
}

// runOnce runs prepare_each hooks and then the command once.
func (r *Runner) runOnce(ctx context.Context, b config.Benchmark, st *sideState, u *unit, record bool) *Failure {
	for i, h := range b.PrepareEach {
		if f := r.runBenchmarkHook(ctx, h, st, FailPrepareEach, fmt.Sprintf("prepare_each[%d]", i)); f != nil {
			return f
		}
	}
	c := b.Commands[u.command]
	dir := st.side.Root
	if c.Cwd != "" {
		var f *Failure
		dir, f = r.resolvePath(c.Cwd, st.vars, st.side.Root, st.side.ProjectRoot, st.workdir)
		if f != nil {
			return f
		}
	}
	spec, f := r.spec(c.Exec, st.vars, dir)
	if f != nil {
		return f
	}
	work, f := r.work(b, st)
	if f != nil {
		return f
	}
	_, cpuSkipped := u.m.Skipped(metric.GroupCPU)
	_, memSkipped := u.m.Skipped(metric.GroupMemory)
	wantCPU := b.Metrics.CPU && !cpuSkipped
	wantMemory := b.Metrics.Memory && !memSkipped
	spec.CollectUsage = wantCPU || wantMemory
	spec.MeasureMemory = wantMemory

	stderr, term, closeIO, f := r.openIO(&spec, c, st, b.Terminal)
	defer closeIO()
	if f != nil {
		return f
	}
	spec.Timeout = c.Timeout

	res, err := r.exec(ctx, spec)
	// Everything the command wrote is in its files once they are closed, which
	// on a terminal means once its output has been read to the end.
	closeIO()
	if res.Canceled {
		return &Failure{Kind: FailInterrupted, Message: "stopped before the benchmark completed"}
	}
	if err != nil {
		kind := FailInternal
		if errors.Is(err, proc.ErrStart) {
			kind = FailStart
		}
		_, statErr := os.Stat(dir)
		if msg := missingWorkingDirectory(dir, c.Cwd, false); msg != "" {
			return &Failure{Kind: kind, Message: fmt.Sprintf("%s: %s%s", r.Redactor.String(c.Display()), msg, sharedHint(st.side, "the working directory", statErr))}
		}
		return &Failure{Kind: kind, Message: fmt.Sprintf("%s: %v%s", r.Redactor.String(c.Display()), redactErr(r.Redactor, err), sharedHint(st.side, "the working directory", statErr))}
	}
	if res.TimedOut {
		msg := fmt.Sprintf("timed out after %s and was stopped", c.Timeout)
		if term != nil && term.NeverReady() {
			msg += "; none of stdin was typed, because the command never wrote to the terminal nor switched it out of line mode (a program that reads a line without a prompt waits for it forever)"
		}
		return &Failure{Kind: FailTimeout, Message: msg, Stderr: r.tail(stderr.Name())}
	}
	if !allowed(c.ExitCodes, res.ExitCode) {
		return &Failure{
			Kind:     FailExitCode,
			ExitCode: res.ExitCode,
			Message:  fmt.Sprintf("exited with status %d (allowed: %s)", res.ExitCode, joinInts(c.ExitCodes)),
			Stderr:   r.tail(stderr.Name()),
		}
	}
	return recordRun(u, b, res, work, wantCPU, wantMemory, record)
}

// recordRun checks the usage of a successful run and, for a measured run,
// appends its samples. Every collected slice grows together with Samples.
func recordRun(u *unit, b config.Benchmark, res proc.Result, work float64, wantCPU, wantMemory, record bool) *Failure {
	if wantCPU {
		if f := usageFailure(u.m, metric.GroupCPU, res.Usage.CPUErr, b.Metrics.Unsupported); f != nil {
			return f
		}
	}
	if wantMemory {
		if f := usageFailure(u.m, metric.GroupMemory, res.Usage.MemoryErr, b.Metrics.Unsupported); f != nil {
			return f
		}
	}
	if !record {
		return nil
	}
	u.m.Samples = append(u.m.Samples, res.Elapsed)
	u.total += res.Elapsed
	if _, skipped := u.m.Skipped(metric.GroupCPU); wantCPU && !skipped {
		u.m.CPUUser = append(u.m.CPUUser, res.Usage.UserCPU)
		u.m.CPUSystem = append(u.m.CPUSystem, res.Usage.SystemCPU)
	}
	if _, skipped := u.m.Skipped(metric.GroupMemory); wantMemory && !skipped {
		u.m.PeakRSS = append(u.m.PeakRSS, res.Usage.PeakRSS)
		u.m.PeakRSSFloor = append(u.m.PeakRSSFloor, res.Usage.Floor)
	}
	if b.Metrics.Throughput != nil {
		u.m.Work = append(u.m.Work, work)
	}
	return nil
}

// usageFailure classifies a missing usage value. An unsupported value under
// the skip policy marks the group skipped for this measurement and is not a
// failure; under the fail policy it is. Any other error is a failed
// collection, which no policy skips: the platform claimed to support it.
func usageFailure(m *Measurement, g metric.Group, err error, policy string) *Failure {
	if err == nil {
		return nil
	}
	if proc.IsUnsupported(err) {
		if policy == config.UnsupportedSkip {
			m.skip(g, err.Error())
			return nil
		}
		return &Failure{Kind: FailMetricUnsupported, Metric: g, Message: fmt.Sprintf("metrics.%s: %v; set metrics.unsupported: skip to measure the rest without it", g, err)}
	}
	return &Failure{Kind: FailMetricCollection, Metric: g, Message: fmt.Sprintf("metrics.%s: %v", g, err)}
}

// work returns the declared work of the next run. A file_size is read now,
// after prepare_each and before the process starts, so producing or reading
// the file is never part of the measured time.
func (r *Runner) work(b config.Benchmark, st *sideState) (float64, *Failure) {
	w := b.Metrics.Throughput
	if w == nil {
		return 0, nil
	}
	if w.FileSize == "" {
		return w.Value, nil
	}
	p, f := r.resolvePath(w.FileSize, st.vars, st.side.Root, st.side.ProjectRoot, st.workdir)
	if f != nil {
		return 0, &Failure{Kind: FailMetricCollection, Metric: metric.GroupThroughput, Message: "throughput work file_size: " + f.Message}
	}
	info, err := os.Stat(p)
	switch {
	case err != nil:
		return 0, &Failure{Kind: FailMetricCollection, Metric: metric.GroupThroughput, Message: fmt.Sprintf("throughput work file_size %s: %v%s", w.FileSize, redactErr(r.Redactor, err), sharedHint(st.side, w.FileSize, err))}
	case info.IsDir():
		return 0, &Failure{Kind: FailMetricCollection, Metric: metric.GroupThroughput, Message: fmt.Sprintf("throughput work file_size %s is a directory", w.FileSize)}
	case info.Size() == 0:
		return 0, &Failure{Kind: FailMetricCollection, Metric: metric.GroupThroughput, Message: fmt.Sprintf("throughput work file_size %s is empty; throughput needs work greater than zero", w.FileSize)}
	}
	return float64(info.Size()), nil
}

// openIO connects the stdin fixture and the stdout and stderr files of one
// run to spec. It returns the stderr file, whose tail a failure shows, and a
// function that closes everything opened, to be called in every case and
// safe to call again.
func (r *Runner) openIO(spec *proc.Spec, c config.Command, st *sideState, terminal bool) (*os.File, *proc.Terminal, func(), *Failure) {
	var closers []io.Closer
	var once sync.Once
	closeAll := func() {
		once.Do(func() {
			for _, c := range closers {
				_ = c.Close()
			}
		})
	}
	if terminal {
		out, term, f := r.openTerminal(spec, c, st, &closers)
		return out, term, closeAll, f
	}
	if st.stdin != "" {
		in, err := os.Open(st.stdin)
		if err != nil {
			return nil, nil, closeAll, &Failure{Kind: FailSetup, Message: fmt.Sprintf("open stdin fixture: %v", redactErr(r.Redactor, err))}
		}
		closers = append(closers, in)
		spec.Stdin = in
	}
	outPath, err := r.outputPath(c.Stdout, st, c.Name+".stdout", false)
	if err != nil {
		return nil, nil, closeAll, &Failure{Kind: FailPath, Message: fmt.Sprintf("stdout: %v", err)}
	}
	if outPath != "" {
		stdout, err := openOutput(outPath)
		if err != nil {
			return nil, nil, closeAll, &Failure{Kind: FailPath, Message: fmt.Sprintf("stdout: %v", err)}
		}
		closers = append(closers, stdout)
		spec.Stdout = stdout
	}
	errPath, err := r.outputPath(c.Stderr, st, c.Name+".stderr", true)
	if err != nil {
		return nil, nil, closeAll, &Failure{Kind: FailPath, Message: fmt.Sprintf("stderr: %v", err)}
	}
	stderr, err := openOutput(errPath)
	if err != nil {
		return nil, nil, closeAll, &Failure{Kind: FailPath, Message: fmt.Sprintf("stderr: %v", err)}
	}
	closers = append(closers, stderr)
	spec.Stderr = stderr
	return stderr, nil, closeAll, nil
}

// openTerminal gives the command a new pseudo-terminal as its standard input,
// output and error. The stdin fixture is typed into it, and what the command
// writes goes to its stdout setting, or to a private file whose tail a failure
// shows. The terminal is closed first, so its output is complete before the
// files it copies from and to are closed.
func (r *Runner) openTerminal(spec *proc.Spec, c config.Command, st *sideState, closers *[]io.Closer) (*os.File, *proc.Terminal, *Failure) {
	var in io.Reader
	if st.stdin != "" {
		f, err := os.Open(st.stdin)
		if err != nil {
			return nil, nil, &Failure{Kind: FailSetup, Message: fmt.Sprintf("open stdin fixture: %v", redactErr(r.Redactor, err))}
		}
		*closers = append(*closers, f)
		in = f
	}
	outPath, err := r.outputPath(c.Stdout, st, c.Name+".terminal", true)
	if err != nil {
		return nil, nil, &Failure{Kind: FailPath, Message: fmt.Sprintf("stdout: %v", err)}
	}
	out, err := openOutput(outPath)
	if err != nil {
		return nil, nil, &Failure{Kind: FailPath, Message: fmt.Sprintf("stdout: %v", err)}
	}
	*closers = append(*closers, out)
	term, err := proc.OpenTerminal(in, out)
	if err != nil {
		return out, nil, &Failure{Kind: FailStart, Message: err.Error()}
	}
	*closers = append([]io.Closer{term}, *closers...)
	spec.Stdin, spec.Stdout, spec.Stderr = term.File(), term.File(), term.File()
	spec.Terminal = true
	return out, term, nil
}

// outputPath decides where a command's stdout or stderr goes for one run.
// It returns "" for the null device. stderr is always written to a private
// file so a failure can show its tail.
func (r *Runner) outputPath(setting string, st *sideState, name string, capture bool) (string, error) {
	switch {
	case setting != config.OutputDiscard:
		p, err := confine(filepath.Join(st.workdir, filepath.FromSlash(setting)), st.workdir)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return "", err
		}
		return p, nil
	case capture:
		return filepath.Join(filepath.Dir(st.workdir), "captured-"+filepath.Base(st.workdir)+"-"+sanitize(name)), nil
	default:
		return "", nil
	}
}

func openOutput(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // G304: confined to ${workdir} or himorime's private temp dir by outputPath
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' {
			return '_'
		}
		return r
	}, s)
}

func (r *Runner) spec(e config.Exec, vars config.Vars, dir string) (proc.Spec, *Failure) {
	env, err := r.childEnv(e.Env, vars)
	if err != nil {
		return proc.Spec{}, &Failure{Kind: FailSetup, Message: err.Error()}
	}
	spec := proc.Spec{Dir: dir, Env: env}
	if e.Shell {
		script, err := config.Expand(e.Script, vars, proc.ShellQuote)
		if err != nil {
			return proc.Spec{}, &Failure{Kind: FailSetup, Message: err.Error()}
		}
		spec.Script = script
		return spec, nil
	}
	argv := make([]string, len(e.Argv))
	for i, a := range e.Argv {
		v, err := config.Expand(a, vars, nil)
		if err != nil {
			return proc.Spec{}, &Failure{Kind: FailSetup, Message: err.Error()}
		}
		argv[i] = v
	}
	spec.Path, spec.Args = argv[0], argv[1:]
	return spec, nil
}

func (r *Runner) childEnv(vars []config.EnvVar, v config.Vars) ([]string, error) {
	env := r.environ()
	for _, e := range vars {
		val, err := config.Expand(e.Value, v, nil)
		if err != nil {
			return nil, fmt.Errorf("env %s: %w", e.Name, err)
		}
		env = append(env, e.Name+"="+val)
		if redact.IsSecretName(e.Name) {
			r.Redactor.Add(val)
		}
	}
	return env, nil
}

// resolvePath expands a configured path, makes it absolute against base, and
// confines it to the given roots after resolving symlinks.
func (r *Runner) resolvePath(p string, vars config.Vars, base string, roots ...string) (string, *Failure) {
	expanded, err := config.Expand(p, vars, nil)
	if err != nil {
		return "", &Failure{Kind: FailPath, Message: err.Error()}
	}
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(base, filepath.FromSlash(expanded))
	}
	all := append([]string{r.ProjectRoot}, roots...)
	resolved, err := confine(expanded, all...)
	if err != nil {
		return "", &Failure{Kind: FailPath, Message: fmt.Sprintf("%s: %v", p, err)}
	}
	return resolved, nil
}

func (r *Runner) tail(path string) string {
	f, err := os.Open(path) //nolint:gosec // G304: a capture file himorime created in its temp dir
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	// Read enough extra bytes that a secret straddling the cut is still seen
	// whole by the redactor, then cut after masking.
	off := info.Size() - stderrTailBytes - int64(r.Redactor.MaxLen())
	if off < 0 {
		off = 0
	}
	buf := make([]byte, info.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
		return ""
	}
	text := r.Redactor.String(strings.ToValidUTF8(string(buf), "?"))
	if off > 0 {
		cut := len(text) - stderrTailBytes
		if cut > 0 {
			text = strings.ToValidUTF8(text[cut:], "")
		}
	}
	return strings.TrimSpace(text)
}

// sharedHint explains a path missing from the base revision: relative paths
// are relative to ${root} of the revision being measured, so a file added in
// the working tree does not exist there. It returns "" in every other case.
// missingWorkingDirectory explains a process that could not start because
// its working directory does not exist, which the operating system reports as
// the program not being found. cwd is the setting as written; an empty one
// means ${root}. It returns "" when the directory exists.
func missingWorkingDirectory(dir, cwd string, hook bool) string {
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	if cwd == "" {
		cwd = "${root}"
	}
	msg := fmt.Sprintf("the working directory %s (cwd %s) does not exist, so the program was not started", dir, cwd)
	if hook {
		msg += "; a hook runs in the benchmark's cwd unless it sets its own, so a setup step that creates that directory needs cwd: ${workdir}"
	}
	return msg
}

// isBenchmarkHook reports whether label names a setup, prepare_each or
// cleanup step, which run in the benchmark's cwd, rather than the build.
func isBenchmarkHook(label string) bool {
	for _, p := range []string{"setup[", "prepare_each[", "cleanup["} {
		if strings.HasPrefix(label, p) {
			return true
		}
	}
	return false
}

func sharedHint(side Side, what string, err error) string {
	if side.Name != SideBase || !errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	return fmt.Sprintf("; %s does not exist in the base revision, where relative paths and ${root} point; if both revisions should use the working tree's copy, start the path with ${head_root}", what)
}

func redactErr(r *redact.Redactor, err error) string {
	return r.String(err.Error())
}

func allowed(codes []int, code int) bool {
	for _, c := range codes {
		if c == code {
			return true
		}
	}
	return false
}

func joinInts(v []int) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = fmt.Sprint(n)
	}
	return strings.Join(parts, ", ")
}
