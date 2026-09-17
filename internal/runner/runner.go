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
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nao1215/yahiko/internal/config"
	"github.com/nao1215/yahiko/internal/proc"
	"github.com/nao1215/yahiko/internal/redact"
)

// cleanupGrace bounds cleanup hooks started after the run was interrupted:
// the user asked yahiko to stop, so cleanup gets a fair chance but no more.
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
}

func (r *Runner) logf(format string, args ...any) {
	if r.Log == nil {
		return
	}
	fmt.Fprintf(r.Log, "yahiko: "+format+"\n", args...)
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
	vars := config.Vars{Artifact: side.Artifact, Root: side.Root, Exe: exeSuffix(), LookupEnv: r.lookupEnv}
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
// (a revision comparison) only the commands the benchmark's regression
// settings compare are measured. The result is a named return so the deferred
// teardown can record a cleanup failure.
func (r *Runner) Measure(ctx context.Context, s *config.Suite, b config.Benchmark, sides []Side) (res BenchmarkResult) {
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
	defer r.teardown(ctx, s, b, states, &res)

	for _, st := range states {
		if f := r.prepareSide(ctx, s, b, st); f != nil {
			res.Failure = f
			return res
		}
	}

	units := commandUnits(b, sides, &res)
	mode := fmt.Sprintf("%d runs", runs)
	if runs == 0 {
		mode = fmt.Sprintf("adaptive runs (min %d, max %d, min time %s)", b.MinRuns, b.MaxRuns, b.MinTime)
	}
	r.logf("benchmark %q: %d measurement units, %d warmup, %s", b.Name, len(units), warmup, mode)

	l := &loop{
		r: r, ctx: ctx, s: s, b: b, states: states, units: units,
		rng: rand.New(rand.NewPCG(r.Seed, nameHash(b.Name))), //nolint:gosec // the order must be reproducible from --seed
	}
	for range warmup {
		if _, f := l.round(false); f != nil {
			res.Failure = f
			return res
		}
	}
	for round := 0; !done(units, runs, b, round); round++ {
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
	r.logf("benchmark %q: finished after %d rounds", b.Name, res.Rounds)
	return res
}

// commandUnits creates one measurement unit per (side, command) pair and the
// matching command results.
func commandUnits(b config.Benchmark, sides []Side, res *BenchmarkResult) []*unit {
	var units []*unit
	for ci, c := range b.Commands {
		cr := CommandResult{Command: c, Sides: map[string]*Measurement{}}
		for si, side := range sides {
			if len(sides) > 1 && !b.Regression.Compares(c.Name) {
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
	s      *config.Suite
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
		f := l.r.runOnce(l.ctx, l.s, l.b, l.states[u.side], u, record)
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

// done decides whether the measuring loop stops before starting round. Every
// unit runs once per round, so all healthy units hold the same sample count.
func done(units []*unit, runs int, b config.Benchmark, round int) bool {
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
		if len(u.m.Samples) < b.MinRuns || u.total < b.MinTime {
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

func (r *Runner) prepareSide(ctx context.Context, s *config.Suite, b config.Benchmark, st *sideState) *Failure {
	base := filepath.Join(r.TempDir, st.side.Name)
	if err := os.MkdirAll(base, 0o700); err != nil {
		return &Failure{Kind: FailInternal, Message: fmt.Sprintf("create temporary directory: %v", err)}
	}
	wd, err := os.MkdirTemp(base, "workdir-")
	if err != nil {
		return &Failure{Kind: FailInternal, Message: fmt.Sprintf("create workdir: %v", err)}
	}
	st.workdir = wd
	st.vars = config.Vars{Artifact: st.side.Artifact, Root: st.side.Root, Workdir: wd, Exe: exeSuffix(), LookupEnv: r.lookupEnv}

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
		if f := r.runBenchmarkHook(ctx, s, h, st, FailSetup, fmt.Sprintf("setup[%d]", i)); f != nil {
			return f
		}
	}

	if b.Stdin.Kind == config.StdinFile {
		p, f := r.resolvePath(b.Stdin.File, st.vars, s.Dir, st.side.ProjectRoot, wd)
		if f != nil {
			f.Message = "stdin: " + f.Message
			return f
		}
		info, err := os.Stat(p)
		if err != nil {
			return &Failure{Kind: FailSetup, Message: fmt.Sprintf("stdin fixture %s: %v", b.Stdin.File, redactErr(r.Redactor, err))}
		}
		if info.IsDir() {
			return &Failure{Kind: FailSetup, Message: fmt.Sprintf("stdin fixture %s is a directory", b.Stdin.File)}
		}
		st.stdin = p
	}
	return nil
}

func (r *Runner) teardown(ctx context.Context, s *config.Suite, b config.Benchmark, states []*sideState, res *BenchmarkResult) {
	cctx := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), cleanupGrace)
		defer cancel()
	}
	for _, st := range states {
		if st.setupOK {
			for i, h := range b.Cleanup {
				if f := r.runBenchmarkHook(cctx, s, h, st, FailCleanup, fmt.Sprintf("cleanup[%d]", i)); f != nil && res.Failure == nil {
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

func (r *Runner) runBenchmarkHook(ctx context.Context, s *config.Suite, h config.Exec, st *sideState, kind FailureKind, label string) *Failure {
	dir := s.Dir
	if h.Cwd != "" {
		var f *Failure
		dir, f = r.resolvePath(h.Cwd, st.vars, s.Dir, st.side.ProjectRoot, st.workdir)
		if f != nil {
			f.Kind = kind
			return f
		}
	}
	return r.runHook(ctx, h, st.vars, dir, kind, label)
}

// runHook runs a build step or a hook to completion and reports a failure.
func (r *Runner) runHook(ctx context.Context, e config.Exec, vars config.Vars, dir string, kind FailureKind, label string) *Failure {
	spec, f := r.spec(e, vars, dir)
	if f != nil {
		f.Kind = kind
		f.Message = label + ": " + f.Message
		return f
	}
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
	spec.Stdout = nil
	spec.Stderr = errFile
	spec.Timeout = e.Timeout
	res, err := proc.Run(ctx, spec, r.Clock)
	if res.Canceled {
		return &Failure{Kind: FailInterrupted, Message: label + ": interrupted"}
	}
	if err != nil {
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
func (r *Runner) runOnce(ctx context.Context, s *config.Suite, b config.Benchmark, st *sideState, u *unit, record bool) *Failure {
	for i, h := range b.PrepareEach {
		if f := r.runBenchmarkHook(ctx, s, h, st, FailPrepareEach, fmt.Sprintf("prepare_each[%d]", i)); f != nil {
			return f
		}
	}
	c := b.Commands[u.command]
	dir := s.Dir
	if c.Cwd != "" {
		var f *Failure
		dir, f = r.resolvePath(c.Cwd, st.vars, s.Dir, st.side.ProjectRoot, st.workdir)
		if f != nil {
			return f
		}
	}
	spec, f := r.spec(c.Exec, st.vars, dir)
	if f != nil {
		return f
	}

	var closers []io.Closer
	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}()
	if st.stdin != "" {
		in, err := os.Open(st.stdin)
		if err != nil {
			return &Failure{Kind: FailSetup, Message: fmt.Sprintf("open stdin fixture: %v", redactErr(r.Redactor, err))}
		}
		closers = append(closers, in)
		spec.Stdin = in
	}
	outPath, err := r.outputPath(c.Stdout, st, c.Name+".stdout", false)
	if err != nil {
		return &Failure{Kind: FailPath, Message: fmt.Sprintf("stdout: %v", err)}
	}
	if outPath != "" {
		stdout, err := openOutput(outPath)
		if err != nil {
			return &Failure{Kind: FailPath, Message: fmt.Sprintf("stdout: %v", err)}
		}
		closers = append(closers, stdout)
		spec.Stdout = stdout
	}
	errPath, err := r.outputPath(c.Stderr, st, c.Name+".stderr", true)
	if err != nil {
		return &Failure{Kind: FailPath, Message: fmt.Sprintf("stderr: %v", err)}
	}
	stderr, err := openOutput(errPath)
	if err != nil {
		return &Failure{Kind: FailPath, Message: fmt.Sprintf("stderr: %v", err)}
	}
	closers = append(closers, stderr)
	spec.Stderr = stderr
	spec.Timeout = c.Timeout

	res, err := proc.Run(ctx, spec, r.Clock)
	if res.Canceled {
		return &Failure{Kind: FailInterrupted, Message: "stopped before the benchmark completed"}
	}
	if err != nil {
		kind := FailInternal
		if errors.Is(err, proc.ErrStart) {
			kind = FailStart
		}
		return &Failure{Kind: kind, Message: fmt.Sprintf("%s: %v", r.Redactor.String(c.Display()), redactErr(r.Redactor, err))}
	}
	if res.TimedOut {
		return &Failure{Kind: FailTimeout, Message: fmt.Sprintf("timed out after %s and was stopped", c.Timeout), Stderr: r.tail(stderr.Name())}
	}
	if !allowed(c.ExitCodes, res.ExitCode) {
		return &Failure{
			Kind:     FailExitCode,
			ExitCode: res.ExitCode,
			Message:  fmt.Sprintf("exited with status %d (allowed: %s)", res.ExitCode, joinInts(c.ExitCodes)),
			Stderr:   r.tail(stderr.Name()),
		}
	}
	if record {
		u.m.Samples = append(u.m.Samples, res.Elapsed)
		u.total += res.Elapsed
	}
	return nil
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
	return os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // G304: confined to ${workdir} or yahiko's private temp dir by outputPath
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
	f, err := os.Open(path) //nolint:gosec // G304: a capture file yahiko created in its temp dir
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
