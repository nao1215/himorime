package config

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// The validator applies the rules JSON Schema cannot express, and resolves
// inherited defaults. It runs only after the document matched the schema, so
// types, ranges, enums, required keys, name patterns and path shapes are
// already guaranteed here and are not checked a second time.
type validator struct {
	file     string
	loc      locator
	issues   []Issue
	hasBuild bool
}

func (v *validator) add(p path, hint, format string, args ...any) {
	line, col := v.loc.position(p)
	v.issues = append(v.issues, Issue{
		File:    v.file,
		Line:    line,
		Column:  col,
		Field:   p.String(),
		Message: fmt.Sprintf(format, args...),
		Hint:    hint,
	})
}

func (v *validator) resolve(raw *RawFile) *Suite {
	root := path{}
	s := &Suite{Name: raw.Suite.Name, Description: raw.Suite.Description}
	v.checkText(root.key("suite").key("name"), raw.Suite.Name)
	v.hasBuild = raw.Build != nil

	defaults := raw.Defaults
	if defaults == nil {
		defaults = &RawDefaults{}
	}
	dp := root.key("defaults")
	v.checkSettings(dp, defaults.Cwd, defaults.Env)
	baseRegression := applyRegression(defaultRegression(), defaults.Regression)

	if raw.Build != nil {
		b := v.resolveExec(root.key("build"), *raw.Build, DefaultBuildTimeout, scopeBuild)
		s.Build = &b
	}

	seen := map[string]int{}
	for i, rb := range raw.Benchmarks {
		p := root.key("benchmarks").index(i)
		if prev, dup := seen[rb.Name]; dup {
			v.add(p.key("name"), "give every benchmark a unique name", "benchmark name %q is already used by benchmarks[%d]", rb.Name, prev)
		}
		seen[rb.Name] = i
		s.Benchmarks = append(s.Benchmarks, v.resolveBenchmark(p, rb, defaults, baseRegression))
	}

	if raw.Report != nil {
		for _, o := range raw.Report.Outputs {
			s.Outputs = append(s.Outputs, Output{Format: Format(o.Format), Path: o.Path})
		}
	}
	return s
}

func defaultRegression() Regression {
	return Regression{
		Metric:     DefaultMetric,
		MaxPercent: DefaultMaxPercent,
		Confidence: DefaultConfidence,
		MinSamples: DefaultMinSamples,
		MaxCV:      DefaultMaxCV,
	}
}

func applyRegression(base Regression, raw *RawRegression) Regression {
	if raw == nil {
		return base
	}
	r := base
	if raw.Metric != nil {
		r.Metric = Metric(*raw.Metric)
	}
	if raw.MaxPercent != nil {
		r.MaxPercent = raw.MaxPercent.Value
	}
	if raw.Confidence != nil {
		r.Confidence = *raw.Confidence
	}
	if raw.MinSamples != nil {
		r.MinSamples = *raw.MinSamples
	}
	if raw.MaxCV != nil {
		r.MaxCV = *raw.MaxCV
	}
	if raw.Commands != nil {
		r.Commands = raw.Commands
	}
	return r
}

// checkText rejects control characters in names shown in reports; JSON
// Schema has no practical way to say so.
func (v *validator) checkText(p path, s string) {
	for _, r := range s {
		if r < 0x20 && r != '\t' {
			v.add(p, "remove the control character", "must not contain control characters")
			return
		}
	}
}

func (v *validator) checkSettings(p path, cwd *string, env map[string]string) {
	if cwd != nil {
		v.checkPathTemplate(p.key("cwd"), *cwd, true)
	}
	for _, name := range sortedKeys(env) {
		v.checkTemplate(p.key("env").key(name), env[name], scopeBenchmark)
	}
}

func hasDotDot(s string) bool {
	for _, seg := range strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return true
		}
	}
	return false
}

// checkPathTemplate validates the variables of a path: only ${root} or
// ${workdir} may appear, only at the start, and no ".." may follow them. The
// runner re-checks the final path after symlinks are resolved.
func (v *validator) checkPathTemplate(p path, s string, allowWorkdir bool) {
	refs, err := References(s)
	if err != nil {
		v.add(p, "", "%v", err)
		return
	}
	for i, r := range refs {
		switch r.Name {
		case VarRoot, VarWorkdir:
			if i != 0 || (!strings.HasPrefix(s, "${root}") && !strings.HasPrefix(s, "${workdir}")) {
				v.add(p, "start the path with ${root} or ${workdir}, or write a relative path", "${%s} may only start a path", r.Name)
				return
			}
			if r.Name == VarWorkdir && !allowWorkdir {
				v.add(p, "the build runs before any benchmark workdir exists; use ${root}", "${workdir} is not available in build")
				return
			}
		default:
			v.add(p, "paths may use ${root} or ${workdir} as their first element", "${%s} is not allowed in a path", refName(r))
			return
		}
	}
	if len(refs) > 0 && hasDotDot(s) {
		v.add(p, "remove .. from the path", "path %q must not contain ..", s)
	}
}

func refName(r Ref) string {
	if r.Name == "env" {
		return "env:" + r.Env
	}
	return r.Name
}

type scope int

const (
	scopeBuild scope = iota
	scopeBenchmark
)

// checkTemplate validates the variables of a command argument or value.
func (v *validator) checkTemplate(p path, s string, sc scope) {
	refs, err := References(s)
	if err != nil {
		v.add(p, "", "%v", err)
		return
	}
	for _, r := range refs {
		switch {
		case r.Name == VarArtifact && sc == scopeBenchmark && !v.hasBuild:
			v.add(p, "add a build section that writes ${artifact}, or remove the reference", "${artifact} is used but the suite has no build section")
		case r.Name == VarWorkdir && sc == scopeBuild:
			v.add(p, "the build runs before any benchmark workdir exists; use ${root} or ${artifact}", "${workdir} is not available in build")
		}
	}
}

func (v *validator) resolveExec(p path, raw RawExec, defaultTimeout time.Duration, sc scope) Exec {
	e := Exec{Shell: raw.Shell, Argv: raw.Command.List, Script: raw.Command.Script, Timeout: defaultTimeout}
	if raw.Command.IsList {
		for i, arg := range raw.Command.List {
			v.checkTemplate(p.key("command").index(i), arg, sc)
		}
	} else {
		v.checkTemplate(p.key("command"), raw.Command.Script, sc)
		if QuotedReference(raw.Command.Script) {
			v.add(p.key("command"), `write the variable without quotes, such as cat ${workdir}/in.txt; yahiko quotes substituted values for the shell itself`,
				"a variable inside quotes in a shell command would be quoted twice")
		}
	}
	if raw.Cwd != nil {
		v.checkPathTemplate(p.key("cwd"), *raw.Cwd, sc != scopeBuild)
		e.Cwd = *raw.Cwd
	}
	for _, name := range sortedKeys(raw.Env) {
		v.checkTemplate(p.key("env").key(name), raw.Env[name], sc)
	}
	e.Env = mergeEnv(nil, raw.Env)
	if raw.Timeout != nil {
		e.Timeout = raw.Timeout.D
	}
	return e
}

func (v *validator) resolveBenchmark(p path, rb RawBenchmark, d *RawDefaults, baseRegression Regression) Benchmark {
	b := Benchmark{Name: rb.Name, Description: rb.Description, Tags: rb.Tags, Baseline: rb.Baseline}
	v.checkText(p.key("name"), rb.Name)
	v.checkSettings(p, rb.Cwd, rb.Env)

	b.Warmup = pickInt(rb.Warmup, d.Warmup, DefaultWarmup)
	b.Runs = pickInt(rb.Runs, d.Runs, 0)
	b.MinRuns = pickInt(rb.MinRuns, d.MinRuns, DefaultMinRuns)
	b.MaxRuns = pickInt(rb.MaxRuns, d.MaxRuns, DefaultMaxRuns)
	b.MinTime = pickDuration(rb.MinTime, d.MinTime, DefaultMinTime)
	if b.Runs == 0 && b.MaxRuns < b.MinRuns {
		v.add(p.key("max_runs"), "raise max_runs or lower min_runs", "max_runs (%d) is lower than min_runs (%d)", b.MaxRuns, b.MinRuns)
	}

	b.Stdin = v.resolveStdin(p.key("stdin"), rb.Stdin)
	for i, h := range rb.Setup {
		b.Setup = append(b.Setup, v.resolveHook(p.key("setup").index(i), h, rb, d))
	}
	for i, h := range rb.PrepareEach {
		b.PrepareEach = append(b.PrepareEach, v.resolveHook(p.key("prepare_each").index(i), h, rb, d))
	}
	for i, h := range rb.Cleanup {
		b.Cleanup = append(b.Cleanup, v.resolveHook(p.key("cleanup").index(i), h, rb, d))
	}
	for _, name := range rb.Commands.Names {
		b.Commands = append(b.Commands, v.resolveCommand(p.key("commands").key(name), name, rb.Commands.ByKey[name], rb, d))
	}
	names := strings.Join(rb.Commands.Names, ", ")

	if rb.Baseline != "" {
		if _, ok := b.Command(rb.Baseline); !ok {
			v.add(p.key("baseline"), "set baseline to one of: "+names, "baseline %q is not a command of this benchmark", rb.Baseline)
		}
	}
	b.Budgets = v.resolveBudgets(p.key("budget"), rb, b, names)

	b.Regression = applyRegression(baseRegression, rb.Regression)
	if rb.Regression != nil {
		for i, name := range rb.Regression.Commands {
			if _, ok := b.Command(name); !ok {
				v.add(p.key("regression").key("commands").index(i), "list command names of this benchmark: "+names, "%q is not a command of this benchmark", name)
			}
		}
	}
	return b
}

func (v *validator) resolveBudgets(p path, rb RawBenchmark, b Benchmark, names string) []Budget {
	var out []Budget
	for _, name := range sortedBudgetKeys(rb.Budget, rb.Commands.Names) {
		if _, ok := b.Command(name); !ok {
			v.add(p.key(name), "budgets are keyed by command name: "+names, "budget refers to unknown command %q", name)
			continue
		}
		rbud := rb.Budget[name]
		for _, m := range []struct {
			metric Metric
			expr   *BudgetExpr
		}{{MetricMean, rbud.Mean}, {MetricMedian, rbud.Median}, {MetricMin, rbud.Min}, {MetricMax, rbud.Max}} {
			if m.expr != nil {
				out = append(out, Budget{Command: name, Metric: m.metric, Expr: *m.expr})
			}
		}
	}
	return out
}

func (v *validator) resolveStdin(p path, s *StdinSpec) Stdin {
	switch {
	case s == nil:
		return Stdin{}
	case s.Content != nil:
		v.checkTemplate(p.key("content"), *s.Content, scopeBenchmark)
		return Stdin{Kind: StdinContent, Content: *s.Content}
	default:
		fp := p
		if !s.scalar {
			fp = p.key("file")
		}
		v.checkPathTemplate(fp, s.File, true)
		return Stdin{Kind: StdinFile, File: s.File}
	}
}

func (v *validator) resolveHook(p path, h RawExec, rb RawBenchmark, d *RawDefaults) Exec {
	e := v.resolveExec(p, h, DefaultHookTimeout, scopeBenchmark)
	if h.Cwd == nil {
		e.Cwd = firstString(rb.Cwd, d.Cwd)
	}
	e.Env = mergeEnv(mergeEnv(mergeEnv(nil, d.Env), rb.Env), h.Env)
	return e
}

func (v *validator) resolveCommand(p path, name string, rc RawCommand, rb RawBenchmark, d *RawDefaults) Command {
	c := Command{Name: name}
	c.Exec = v.resolveExec(p, RawExec{Command: rc.Command, Shell: rc.Shell, Cwd: rc.Cwd, Env: rc.Env, Timeout: rc.Timeout}, DefaultTimeout, scopeBenchmark)
	if rc.Cwd == nil {
		c.Cwd = firstString(rb.Cwd, d.Cwd)
	}
	if rc.Timeout == nil {
		c.Timeout = pickDuration(rb.Timeout, d.Timeout, DefaultTimeout)
	}
	c.Env = mergeEnv(mergeEnv(mergeEnv(nil, d.Env), rb.Env), rc.Env)
	c.Stdout = firstString(rc.Stdout, rb.Stdout, d.Stdout)
	if c.Stdout == "" {
		c.Stdout = OutputDiscard
	}
	c.Stderr = firstString(rc.Stderr, rb.Stderr, d.Stderr)
	if c.Stderr == "" {
		c.Stderr = OutputDiscard
	}
	switch {
	case rc.ExitCodes != nil:
		c.ExitCodes = rc.ExitCodes
	case rb.ExitCodes != nil:
		c.ExitCodes = rb.ExitCodes
	case d.ExitCodes != nil:
		c.ExitCodes = d.ExitCodes
	default:
		c.ExitCodes = []int{0}
	}
	return c
}

func pickInt(a, b *int, def int) int {
	if a != nil {
		return *a
	}
	if b != nil {
		return *b
	}
	return def
}

func pickDuration(a, b *Duration, def time.Duration) time.Duration {
	if a != nil {
		return a.D
	}
	if b != nil {
		return b.D
	}
	return def
}

func firstString(values ...*string) string {
	for _, s := range values {
		if s != nil {
			return *s
		}
	}
	return ""
}

func mergeEnv(base []EnvVar, overlay map[string]string) []EnvVar {
	m := map[string]string{}
	for _, e := range base {
		m[e.Name] = e.Value
	}
	for k, val := range overlay {
		m[k] = val
	}
	out := make([]EnvVar, 0, len(m))
	for _, k := range sortedKeys(m) {
		out = append(out, EnvVar{Name: k, Value: m[k]})
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedBudgetKeys orders budget entries by command declaration order, then
// any unknown names alphabetically, so issues and reports are deterministic.
func sortedBudgetKeys(m map[string]RawBudget, order []string) []string {
	var keys []string
	seen := map[string]bool{}
	for _, name := range order {
		if _, ok := m[name]; ok {
			keys = append(keys, name)
			seen[name] = true
		}
	}
	var rest []string
	for k := range m {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}
