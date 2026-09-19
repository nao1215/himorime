// Package metric defines the performance metrics himorime collects: their
// names, units, the direction that counts as better, how raw samples are
// aggregated, and the typed quantities (durations, byte sizes, rates and
// percentages) that budgets and regression thresholds are written in.
//
// The definitions here are the single source for every layer that handles a
// metric. The runner decides what to collect from Group, the report judges a
// budget or a comparison through Better, and every renderer formats a value
// through Kind. No layer compares metrics by their name.
package metric

import "fmt"

// Name identifies one metric. The strings are part of the JSON report and CSV
// contracts.
type Name string

// Metrics.
const (
	// Latency is the wall-clock duration of one run, from just before the
	// process is started until it is reaped.
	Latency Name = "latency"
	// Throughput is the declared work divided by the latency of the run.
	Throughput Name = "throughput"
	// CPUUser is the user-mode CPU time of the process tree.
	CPUUser Name = "cpu_user"
	// CPUSystem is the kernel-mode CPU time of the process tree.
	CPUSystem Name = "cpu_system"
	// CPUTotal is CPUUser plus CPUSystem.
	CPUTotal Name = "cpu_total"
	// CPUUtilization is CPUTotal divided by Latency, in percent. It exceeds
	// 100 when the tree ran on more than one CPU at a time.
	CPUUtilization Name = "cpu_utilization"
	// PeakRSS is the largest resident set size of the process tree, in bytes.
	PeakRSS Name = "peak_rss"
)

// Group is a set of metrics collected together and enabled by one setting.
type Group string

// Groups, in report order.
const (
	GroupLatency    Group = "latency"
	GroupThroughput Group = "throughput"
	GroupCPU        Group = "cpu"
	GroupMemory     Group = "memory"
)

// Groups returns every group in report order.
func Groups() []Group {
	return []Group{GroupLatency, GroupThroughput, GroupCPU, GroupMemory}
}

// Kind is the physical kind of a metric's values, which fixes the canonical
// unit values are stored in.
type Kind int

// Kinds.
const (
	// KindDuration values are nanoseconds.
	KindDuration Kind = iota + 1
	// KindBytes values are bytes.
	KindBytes
	// KindRate values are units of declared work per second.
	KindRate
	// KindPercent values are percentages, where 100 means one full CPU.
	KindPercent
)

// String names the kind for messages.
func (k Kind) String() string {
	switch k {
	case KindDuration:
		return "duration"
	case KindBytes:
		return "byte size"
	case KindRate:
		return "rate"
	case KindPercent:
		return "percentage"
	}
	return fmt.Sprintf("kind(%d)", int(k))
}

// Direction says which way a change is an improvement.
type Direction string

// Directions. The strings are part of the JSON report contract.
const (
	LowerIsBetter  Direction = "lower"
	HigherIsBetter Direction = "higher"
	// Neutral metrics have no better direction. CPU utilization is one: a
	// higher value can mean better parallelism or wasted work. It can carry
	// a budget in either direction but is never judged as a regression.
	Neutral Direction = "neutral"
)

// Scope says which processes a metric covers.
type Scope string

// Scopes. The strings are part of the suite format and the JSON report.
const (
	// ScopeWall is a wall-clock measurement around the process, which
	// covers everything the command did until it was reaped.
	ScopeWall Scope = "wall_clock"
	// ScopeProcessTree covers the started process and its descendants; the
	// platform page documents exactly which descendants each OS includes.
	ScopeProcessTree Scope = "process_tree"
)

// Def describes one metric.
type Def struct {
	Name  Name
	Group Group
	Kind  Kind
	// Better is the direction of an improvement.
	Better Direction
	Scope  Scope
	// Label is the short human name used in tables and messages.
	Label string
	// Comparable reports whether a revision comparison judges this metric.
	Comparable bool
}

var defs = []Def{
	{Name: Latency, Group: GroupLatency, Kind: KindDuration, Better: LowerIsBetter, Scope: ScopeWall, Label: "latency", Comparable: true},
	{Name: Throughput, Group: GroupThroughput, Kind: KindRate, Better: HigherIsBetter, Scope: ScopeWall, Label: "throughput", Comparable: true},
	{Name: CPUUser, Group: GroupCPU, Kind: KindDuration, Better: LowerIsBetter, Scope: ScopeProcessTree, Label: "cpu user"},
	{Name: CPUSystem, Group: GroupCPU, Kind: KindDuration, Better: LowerIsBetter, Scope: ScopeProcessTree, Label: "cpu system"},
	{Name: CPUTotal, Group: GroupCPU, Kind: KindDuration, Better: LowerIsBetter, Scope: ScopeProcessTree, Label: "cpu total", Comparable: true},
	{Name: CPUUtilization, Group: GroupCPU, Kind: KindPercent, Better: Neutral, Scope: ScopeProcessTree, Label: "cpu utilization"},
	{Name: PeakRSS, Group: GroupMemory, Kind: KindBytes, Better: LowerIsBetter, Scope: ScopeProcessTree, Label: "peak rss", Comparable: true},
}

// Defs returns every metric in report order.
func Defs() []Def {
	out := make([]Def, len(defs))
	copy(out, defs)
	return out
}

// Names returns every metric name in report order.
func Names() []Name {
	out := make([]Name, len(defs))
	for i, d := range defs {
		out[i] = d.Name
	}
	return out
}

// Lookup returns the definition of a metric.
func Lookup(n Name) (Def, bool) {
	for _, d := range defs {
		if d.Name == n {
			return d, true
		}
	}
	return Def{}, false
}

// MustLookup returns the definition of a metric defined in this package. It
// panics on an unknown name, which is a programming error.
func MustLookup(n Name) Def {
	d, ok := Lookup(n)
	if !ok {
		panic(fmt.Sprintf("metric: unknown metric %q", n))
	}
	return d
}

// InGroup returns the metrics of a group in report order.
func InGroup(g Group) []Def {
	var out []Def
	for _, d := range defs {
		if d.Group == g {
			out = append(out, d)
		}
	}
	return out
}

// Unit returns the canonical unit of the metric's values. workUnit is the
// declared unit of work and only matters for throughput.
func (d Def) Unit(workUnit string) string {
	switch d.Kind {
	case KindDuration:
		return "ns"
	case KindBytes:
		return "bytes"
	case KindRate:
		if workUnit == "" {
			workUnit = "operations"
		}
		return workUnit + "/s"
	case KindPercent:
		return "percent"
	}
	return ""
}
