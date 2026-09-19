package config

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nao1215/himorime/internal/metric"
)

// The validator applies the rules JSON Schema cannot express, and resolves
// inherited defaults. It runs only after the document matched the schema, so
// types, ranges, enums, required keys, name patterns and path shapes are
// already guaranteed here and are not checked a second time.
type validator struct {
	file string
	loc  locator
	// dir is the absolute directory holding the suite file, which a relative
	// path is resolved from, and projectRoot is the directory such a path may
	// not leave. Both are empty when the suite has no place on disk.
	dir         string
	projectRoot string
	issues      []Issue
	hasBuild    bool
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
	s := &Suite{Name: raw.Name, Description: raw.Description}
	v.checkText(root.key("name"), raw.Name)
	v.hasBuild = raw.Build != nil

	defaults := raw.Defaults
	if defaults == nil {
		defaults = &RawDefaults{}
	}
	dp := root.key("defaults")
	v.checkSettings(dp, defaults.Cwd, defaults.Env)
	baseRegression := v.applyRegression(defaultRegression(), defaults.Regression, dp.key("regression"))

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
		rp := root.key("report")
		for i, o := range raw.Report.Outputs {
			for j, prev := range s.Outputs {
				if filepath.Clean(filepath.FromSlash(prev.Path)) == filepath.Clean(filepath.FromSlash(o.Path)) && (prev.Section == "" || o.Section == "" || prev.Section == o.Section) {
					v.add(rp.key("outputs").index(i).key("path"), "use different files or distinct Markdown sections", "report output conflicts with report.outputs[%d]", j)
				}
			}
			if o.Section != "" && Format(o.Format) != FormatMarkdown {
				v.add(rp.key("outputs").index(i).key("section"), "a section replaces part of a Markdown file; remove section, or set format: markdown",
					"section is only allowed with format: markdown, not %s", o.Format)
			}
			s.Outputs = append(s.Outputs, Output{Format: Format(o.Format), Path: o.Path, Section: o.Section})
		}
		for _, name := range raw.Report.Versions.Names {
			argv := raw.Report.Versions.ByKey[name]
			for i, arg := range argv {
				v.checkTemplate(rp.key("versions").key(name).index(i), arg, scopeVersions)
			}
			s.Versions = append(s.Versions, ToolVersion{Name: name, Argv: argv})
		}
	}
	return s
}

func defaultRegression() Regression {
	metricDefault := MetricRegression{Metric: DefaultMetric, MaxPercent: DefaultMaxPercent, Gate: true}
	return Regression{
		Confidence: DefaultConfidence,
		MinSamples: DefaultMinSamples,
		MaxCV:      DefaultMaxCV,
		Latency:    metricDefault,
		CPU:        metricDefault,
		Memory:     metricDefault,
	}
}

func (v *validator) applyRegression(base Regression, raw *RawRegression, p path) Regression {
	if raw == nil {
		return base
	}
	r := base
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
	r.Latency = v.applyMetricRegression(r.Latency, raw.Latency, p.key("latency"), metric.KindDuration)
	r.CPU = v.applyMetricRegression(r.CPU, raw.CPUTotal, p.key("cpu_total"), metric.KindDuration)
	r.Memory = v.applyMetricRegression(r.Memory, raw.PeakRSS, p.key("peak_rss"), metric.KindBytes)
	return r
}

func (v *validator) applyMetricRegression(base MetricRegression, raw *RawMetricRegression, p path, k metric.Kind) MetricRegression {
	if raw == nil {
		return base
	}
	r := base
	if raw.Statistic != nil {
		r.Metric = Metric(*raw.Statistic)
	}
	if raw.MaxPercent != nil {
		r.MaxPercent = raw.MaxPercent.Value
	}
	if raw.MinDifference != nil {
		r.MinDifference, _ = v.quantity(p.key("min_difference"), k, *raw.MinDifference)
	}
	if raw.Gate != nil {
		r.Gate = *raw.Gate
	}
	return r
}

// quantity parses a typed quantity and records an issue when it is invalid.
func (v *validator) quantity(p path, k metric.Kind, s string) (float64, string) {
	val, unit, err := metric.ParseQuantity(k, s)
	if err != nil {
		v.add(p, "", "%v", err)
		return 0, ""
	}
	return val, unit
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

// checkPathTemplate validates the variables of a path: only ${root},
// ${head_root} or ${workdir} may appear, only at the start. A ".." after
// ${root} or ${head_root} must stay inside the project, as in a relative
// path, since both are the suite's directory in their tree; none may follow
// ${workdir}, which is himorime's own temporary directory. The runner
// re-checks the final path after symlinks are resolved.
func (v *validator) checkPathTemplate(p path, s string, allowWorkdir bool) {
	refs, err := References(s)
	if err != nil {
		v.add(p, "", "%v", err)
		return
	}
	for i, r := range refs {
		switch r.Name {
		case VarRoot, VarHeadRoot, VarWorkdir:
			if i != 0 || !strings.HasPrefix(s, "${"+r.Name+"}") {
				v.add(p, "start the path with ${root}, ${head_root} or ${workdir}, or write a relative path", "${%s} may only start a path", r.Name)
				return
			}
			if r.Name == VarWorkdir && !allowWorkdir {
				v.add(p, "the build runs before any benchmark workdir exists; use ${root}", "${workdir} is not available in build")
				return
			}
		default:
			v.add(p, "paths may use ${root}, ${head_root} or ${workdir} as their first element", "${%s} is not allowed in a path", refName(r))
			return
		}
	}
	if !hasDotDot(s) {
		return
	}
	rel := s
	if len(refs) > 0 {
		if refs[0].Name == VarWorkdir {
			v.add(p, "${workdir} is a fresh directory for the benchmark; write the path inside it", "path %q must not contain .. after ${workdir}", s)
			return
		}
		rel = strings.TrimLeft(strings.TrimPrefix(s, "${"+refs[0].Name+"}"), `/\`)
	}
	if v.leavesProject(rel) {
		v.add(p, "paths must stay inside the repository holding the suite, or inside the suite's directory when it is not in a repository",
			"path %q leaves the project", s)
	}
}

// leavesProject reports whether a relative path without variables resolves
// outside the project. The run resolves .. the same way, before it follows
// symbolic links, so a path rejected here is one the run would reject too,
// after it had already built and started measuring.
func (v *validator) leavesProject(s string) bool {
	if v.dir == "" || v.projectRoot == "" {
		return false
	}
	target := filepath.Join(v.dir, filepath.FromSlash(s))
	rel, err := filepath.Rel(v.projectRoot, target)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
	// scopeVersions is report.versions, run in the suite directory before
	// the build and outside any benchmark.
	scopeVersions
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
		case (r.Name == VarWorkdir || r.Name == VarArtifact) && sc == scopeVersions:
			v.add(p, "version commands run once before the build and any benchmark; use a program on PATH or a path under ${root}",
				"${%s} is not available in report.versions", r.Name)
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
			v.add(p.key("command"), `write the variable without quotes, such as cat ${workdir}/in.txt; himorime quotes substituted values for the shell itself`,
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
	b.Terminal = rb.Terminal
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
	if b.Terminal {
		v.checkTerminalStderr(p, rb, d)
	}

	if rb.Baseline != "" {
		if _, ok := b.Command(rb.Baseline); !ok {
			v.add(p.key("baseline"), "set baseline to one of: "+names, "baseline %q is not a command of this benchmark", rb.Baseline)
		}
	}
	b.Metrics = v.resolveMetrics(p.key("metrics"), d.Metrics, rb.Metrics)
	for _, name := range rb.Commands.Names {
		if rc := rb.Commands.ByKey[name]; rc.Budget != nil {
			for _, e := range v.budgetEntries(p.key("commands").key(name).key("budget"), rc.Budget) {
				if bud, ok := v.resolveBudget(name, e, b.Metrics); ok {
					b.Budgets = append(b.Budgets, bud)
				}
			}
		}
	}

	b.Regression = v.applyRegression(baseRegression, rb.Regression, p.key("regression"))
	if rb.Regression != nil {
		for i, name := range rb.Regression.Commands {
			if _, ok := b.Command(name); !ok {
				v.add(p.key("regression").key("commands").index(i), "list command names of this benchmark: "+names, "%q is not a command of this benchmark", name)
			}
		}
		v.checkRegressionMetrics(p.key("regression"), rb.Regression, b.Metrics)
	}
	return b
}

// checkRegressionMetrics rejects a benchmark's tolerance for a metric the
// benchmark does not measure: the setting would silently do nothing.
func (v *validator) checkRegressionMetrics(p path, raw *RawRegression, m Metrics) {
	for _, x := range []struct {
		key   string
		group metric.Group
		set   bool
	}{{"cpu_total", metric.GroupCPU, raw.CPUTotal != nil}, {"peak_rss", metric.GroupMemory, raw.PeakRSS != nil}} {
		if x.set && !m.Collects(x.group) {
			v.add(p.key(x.key), metricHint(x.group), "regression.%s is set but this benchmark does not measure %s", x.key, x.group)
		}
	}
}

func metricHint(g metric.Group) string {
	if g == metric.GroupThroughput {
		return "declare the work of one run under metrics.throughput.work, such as {value: 1000, unit: records}"
	}
	return fmt.Sprintf("enable it with metrics: {%s: true}", g)
}

// resolveMetrics merges the metrics of defaults and a benchmark. Only a
// benchmark can declare throughput, because its work belongs to its input.
func (v *validator) resolveMetrics(p path, d, rb *RawMetrics) Metrics {
	m := Metrics{Unsupported: UnsupportedFail}
	for _, raw := range []*RawMetrics{d, rb} {
		if raw == nil {
			continue
		}
		if raw.CPU != nil {
			m.CPU = *raw.CPU
		}
		if raw.Memory != nil {
			m.Memory = *raw.Memory
		}
		if raw.Unsupported != nil {
			m.Unsupported = *raw.Unsupported
		}
	}
	if rb != nil && rb.Throughput != nil {
		m.Throughput = v.resolveWork(p.key("throughput").key("work"), rb.Throughput.Work)
	}
	return m
}

func (v *validator) resolveWork(p path, raw RawWork) *Work {
	w := &Work{}
	switch {
	case raw.Value != nil && raw.FileSize != nil:
		v.add(p, "keep either value or file_size", "work declares both value and file_size; declare exactly one")
		return w
	case raw.Value == nil && raw.FileSize == nil:
		v.add(p, "write value: 1000 for a fixed amount, or file_size: path/to/input for the size of a file", "work needs a value or a file_size")
		return w
	}
	unit := ""
	if raw.Unit != nil {
		unit = *raw.Unit
	}
	if raw.FileSize != nil {
		v.checkPathTemplate(p.key("file_size"), *raw.FileSize, true)
		w.FileSize = *raw.FileSize
		if unit == "" {
			unit = metric.BytesUnit
		}
		if unit != metric.BytesUnit {
			v.add(p.key("unit"), "remove unit or write unit: bytes", "file_size measures bytes, so its unit must be bytes, not %q", unit)
			// The unit the writer has to put here is bytes. Carrying the
			// rejected one further would make the budget and tolerance
			// checks below repeat it back as the unit to write.
			unit = metric.BytesUnit
		}
	} else {
		w.Value = *raw.Value
		if unit == "" {
			unit = "operations"
		}
		if metric.IsByteUnit(unit) && unit != metric.BytesUnit {
			v.add(p.key("unit"), "convert the value to bytes and write unit: bytes; throughput is then shown as KiB/s, MiB/s and so on", "work unit %q is a byte size multiple", unit)
			unit = metric.BytesUnit
		}
	}
	w.Unit = unit
	return w
}

// budgetEntry is one budget expression as written, before it is typed.
type budgetEntry struct {
	metric metric.Name
	agg    string
	expr   string
	path   path
}

// budgetEntries flattens one command's budgets in report order: metric order,
// then aggregation order.
func (v *validator) budgetEntries(p path, rbud map[string]map[string]string) []budgetEntry {
	var entries []budgetEntry
	add := func(n metric.Name, m map[string]string, base path) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return metric.Aggregation(keys[i]).Less(metric.Aggregation(keys[j])) })
		for _, k := range keys {
			ep := base.key(k)
			entries = append(entries, budgetEntry{metric: n, agg: k, expr: m[k], path: ep})
		}
	}
	for _, d := range metric.Defs() {
		if values, ok := rbud[string(d.Name)]; ok {
			add(d.Name, values, p.key(string(d.Name)))
		}
	}
	return entries
}

func (v *validator) resolveBudget(command string, e budgetEntry, m Metrics) (Budget, bool) {
	def := metric.MustLookup(e.metric)
	agg, err := metric.ParseAggregation(e.agg)
	if err != nil {
		v.add(e.path, "", "%v", err)
		return Budget{}, false
	}
	th, err := metric.ParseThreshold(def.Kind, def.Better, e.expr)
	if err != nil {
		v.add(e.path, "", "%v", err)
		return Budget{}, false
	}
	if !m.Collects(def.Group) {
		v.add(e.path, metricHint(def.Group), "a budget on %s needs the benchmark to measure %s", def.Label, def.Group)
		return Budget{}, false
	}
	if e.metric == metric.Throughput && th.Unit != m.WorkUnit() {
		v.add(e.path, "write the budget in the declared work unit, such as \">= 1000 "+m.WorkUnit()+"/s\"",
			"the budget is in %s/s but the declared work unit is %s", th.Unit, m.WorkUnit())
		return Budget{}, false
	}
	return Budget{Command: command, Metric: e.metric, Aggregation: agg, Threshold: th}, true
}

// checkTerminalStderr rejects a stderr setting on a benchmark that runs on a
// terminal, where standard error is the same terminal as standard output.
func (v *validator) checkTerminalStderr(p path, rb RawBenchmark, d *RawDefaults) {
	const hint = "remove stderr, or set stderr: discard; on a terminal the output of both streams goes to stdout"
	const msg = "stderr cannot be set with terminal: true, because standard output and standard error are the same terminal"
	set := func(s *string) bool { return s != nil && *s != OutputDiscard }
	for _, name := range rb.Commands.Names {
		if rc := rb.Commands.ByKey[name]; rc.Stderr != nil {
			if set(rc.Stderr) {
				v.add(p.key("commands").key(name).key("stderr"), hint, msg)
			}
			continue
		}
		switch {
		case rb.Stderr != nil:
			if set(rb.Stderr) {
				v.add(p.key("stderr"), hint, msg)
			}
			return
		case set(d.Stderr):
			v.add(p.key("terminal"), "set stderr: discard on this benchmark; defaults.stderr applies to it", msg)
			return
		}
	}
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
