package metric

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestDefinitionsAreComplete(t *testing.T) {
	t.Parallel()
	seen := map[Name]bool{}
	for _, d := range Defs() {
		if seen[d.Name] {
			t.Fatalf("%s defined twice", d.Name)
		}
		seen[d.Name] = true
		if d.Label == "" || d.Kind == 0 || d.Better == "" || d.Scope == "" || d.Group == "" {
			t.Errorf("%s is incomplete: %+v", d.Name, d)
		}
		if d.Better == Neutral && d.Comparable {
			t.Errorf("%s is neutral but comparable; a neutral metric has no regression direction", d.Name)
		}
		if got := MustLookup(d.Name); got != d {
			t.Errorf("Lookup(%s) = %+v", d.Name, got)
		}
	}
	if len(Names()) != len(Defs()) {
		t.Fatal("Names and Defs disagree")
	}
	if _, ok := Lookup("heap"); ok {
		t.Fatal("Lookup found an unknown metric")
	}
	var grouped int
	for _, g := range Groups() {
		grouped += len(InGroup(g))
	}
	if grouped != len(Defs()) {
		t.Fatalf("groups cover %d of %d metrics", grouped, len(Defs()))
	}
}

func TestDirections(t *testing.T) {
	t.Parallel()
	want := map[Name]Direction{
		Latency: LowerIsBetter, Throughput: HigherIsBetter, CPUUser: LowerIsBetter, CPUSystem: LowerIsBetter,
		CPUTotal: LowerIsBetter, CPUUtilization: Neutral, PeakRSS: LowerIsBetter,
	}
	for n, dir := range want {
		if got := MustLookup(n).Better; got != dir {
			t.Errorf("%s better = %s, want %s", n, got, dir)
		}
	}
}

func TestUnits(t *testing.T) {
	t.Parallel()
	for n, want := range map[Name]string{Latency: "ns", CPUTotal: "ns", PeakRSS: "bytes", CPUUtilization: "percent", Throughput: "records/s"} {
		if got := MustLookup(n).Unit("records"); got != want {
			t.Errorf("%s unit = %s, want %s", n, got, want)
		}
	}
	if got := MustLookup(Throughput).Unit(""); got != "operations/s" {
		t.Errorf("throughput default unit = %s", got)
	}
}

func TestParseBytes(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]float64{
		"1B": 1, "512bytes": 512, "1KB": 1000, "1KiB": 1024, "64MiB": 64 << 20, "1.5GiB": 1.5 * (1 << 30),
		"2 MB": 2e6, "1TiB": 1 << 40, "0B": 0,
	} {
		got, err := ParseBytes(in)
		if err != nil || got != want {
			t.Errorf("ParseBytes(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "64", "64mib", "-1MiB", "MiB", "1e3B", "1.MiB", "64 MiB/s", "1 PiB"} {
		if _, err := ParseBytes(in); err == nil {
			t.Errorf("ParseBytes(%q) accepted an invalid size", in)
		}
	}
}

func TestParseRate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		v    float64
		unit string
	}{
		{"50MiB/s", 50 << 20, BytesUnit},
		{"1000 records/s", 1000, "records"},
		{"12.5 ops/s", 12.5, "ops"},
		{"10bytes/s", 10, BytesUnit},
		{"3 lines_out/s", 3, "lines_out"},
	}
	for _, tt := range tests {
		v, unit, err := ParseRate(tt.in)
		if err != nil || v != tt.v || unit != tt.unit {
			t.Errorf("ParseRate(%q) = %v %q %v; want %v %q", tt.in, v, unit, err, tt.v, tt.unit)
		}
	}
	for _, in := range []string{"50MiB", "records/s", "1000 records", "-1 records/s", "1 2records/s", "1 records/min"} {
		if _, _, err := ParseRate(in); err == nil {
			t.Errorf("ParseRate(%q) accepted an invalid rate", in)
		}
	}
}

func TestParseDuration(t *testing.T) {
	t.Parallel()
	if d, err := ParseDuration("1m30s"); err != nil || d != 90*time.Second {
		t.Fatalf("ParseDuration = %v, %v", d, err)
	}
	for _, in := range []string{"10", "-1s", "25h", "1 s"} {
		if _, err := ParseDuration(in); err == nil {
			t.Errorf("ParseDuration(%q) accepted", in)
		}
	}
}

func TestParseThreshold(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind   Kind
		better Direction
		in     string
		op     Op
		limit  float64
		unit   string
	}{
		{KindDuration, LowerIsBetter, "<= 80ms", OpLessEqual, 80e6, ""},
		{KindDuration, LowerIsBetter, "<1s", OpLess, 1e9, ""},
		{KindBytes, LowerIsBetter, "<= 64MiB", OpLessEqual, 64 << 20, ""},
		{KindBytes, LowerIsBetter, " < 1.5 GB ", OpLess, 1.5e9, ""},
		{KindRate, HigherIsBetter, ">= 50MiB/s", OpGreaterEqual, 50 << 20, BytesUnit},
		{KindRate, HigherIsBetter, "> 1000 records/s", OpGreater, 1000, "records"},
		{KindPercent, Neutral, "<= 150%", OpLessEqual, 150, ""},
		{KindPercent, Neutral, ">= 180 %", OpGreaterEqual, 180, ""},
	}
	for _, tt := range tests {
		th, err := ParseThreshold(tt.kind, tt.better, tt.in)
		if err != nil {
			t.Errorf("ParseThreshold(%q): %v", tt.in, err)
			continue
		}
		if th.Op != tt.op || th.Limit != tt.limit || th.Unit != tt.unit || th.Kind != tt.kind {
			t.Errorf("ParseThreshold(%q) = %+v", tt.in, th)
		}
	}
	bad := []struct {
		kind   Kind
		better Direction
		in     string
		want   string
	}{
		{KindDuration, LowerIsBetter, "80ms", "invalid budget"},
		{KindDuration, LowerIsBetter, ">= 80ms", `write "<" or "<="`},
		{KindDuration, LowerIsBetter, "< 0s", "greater than zero"},
		{KindBytes, LowerIsBetter, "<= 64", "byte size"},
		{KindBytes, LowerIsBetter, "<= 0MiB", "greater than zero"},
		{KindBytes, LowerIsBetter, ">= 64MiB", `write "<" or "<="`},
		{KindRate, HigherIsBetter, "<= 50MiB/s", `write ">" or ">="`},
		{KindRate, HigherIsBetter, ">= 0 records/s", "greater than zero"},
		{KindRate, HigherIsBetter, ">= 50ms", "rate"},
		{KindPercent, Neutral, "<= 150", "percentage"},
		{KindDuration, HigherIsBetter, "< 1s", "better when higher"},
		{KindRate, LowerIsBetter, "> 1 ops/s", "better when lower"},
	}
	for _, tt := range bad {
		_, err := ParseThreshold(tt.kind, tt.better, tt.in)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("ParseThreshold(%v, %q) error = %v, want %q", tt.kind, tt.in, err, tt.want)
		}
	}
}

func TestThresholdAllows(t *testing.T) {
	t.Parallel()
	le, _ := ParseThreshold(KindBytes, LowerIsBetter, "<= 1KiB")
	lt, _ := ParseThreshold(KindBytes, LowerIsBetter, "< 1KiB")
	ge, _ := ParseThreshold(KindRate, HigherIsBetter, ">= 10 ops/s")
	gt, _ := ParseThreshold(KindRate, HigherIsBetter, "> 10 ops/s")
	if !le.Allows(1024) || lt.Allows(1024) || !lt.Allows(1023) || le.Allows(1025) {
		t.Error("upper bound boundary")
	}
	if !ge.Allows(10) || gt.Allows(10) || !gt.Allows(10.0001) || ge.Allows(9.9) {
		t.Error("lower bound boundary")
	}
	if le.Allows(math.NaN()) || ge.Allows(math.NaN()) {
		t.Error("a missing value satisfied a budget")
	}
	if got := le.String(); got != "<= 1.00KiB" {
		t.Errorf("String = %q", got)
	}
	if got := ge.String(); got != ">= 10.00 ops/s" {
		t.Errorf("String = %q", got)
	}
}

func TestFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind Kind
		v    float64
		unit string
		want string
	}{
		{KindDuration, 850, "", "850ns"},
		{KindDuration, 12340, "", "12.34µs"},
		{KindDuration, 1.82e6, "", "1.82ms"},
		{KindDuration, 3.4e9, "", "3.40s"},
		{KindBytes, 512, "", "512B"},
		{KindBytes, 1536, "", "1.50KiB"},
		{KindBytes, 64 << 20, "", "64.00MiB"},
		{KindBytes, 3 << 30, "", "3.00GiB"},
		{KindRate, 50 << 20, BytesUnit, "50.00MiB/s"},
		{KindRate, 999, "records", "999.00 records/s"},
		{KindRate, 12345, "records", "12.35k records/s"},
		{KindRate, 2.5e6, "records", "2.50M records/s"},
		{KindRate, 7e9, "", "7.00G operations/s"},
		{KindPercent, 187.34, "", "187.3%"},
		{KindDuration, math.NaN(), "", "-"},
	}
	for _, tt := range tests {
		if got := Format(tt.kind, tt.v, tt.unit); got != tt.want {
			t.Errorf("Format(%v, %v, %q) = %q, want %q", tt.kind, tt.v, tt.unit, got, tt.want)
		}
	}
}

func TestParseQuantity(t *testing.T) {
	t.Parallel()
	if v, _, err := ParseQuantity(KindDuration, "2ms"); err != nil || v != 2e6 {
		t.Errorf("duration = %v %v", v, err)
	}
	if v, _, err := ParseQuantity(KindBytes, "1MiB"); err != nil || v != 1<<20 {
		t.Errorf("bytes = %v %v", v, err)
	}
	if v, u, err := ParseQuantity(KindRate, "5 records/s"); err != nil || v != 5 || u != "records" {
		t.Errorf("rate = %v %v %v", v, u, err)
	}
	if _, _, err := ParseQuantity(KindPercent, "5%"); err == nil {
		t.Error("a percent quantity was accepted")
	}
}

func TestAggregations(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"min", "max", "mean", "median", "p1", "p50", "p90", "p95", "p99", "p99.9", "p9.5"} {
		if _, err := ParseAggregation(in); err != nil {
			t.Errorf("ParseAggregation(%q): %v", in, err)
		}
	}
	for _, in := range []string{"", "p0", "p100", "p01", "p", "p99.", "stddev", "avg", "P95", "p-1"} {
		if _, err := ParseAggregation(in); err == nil {
			t.Errorf("ParseAggregation(%q) accepted", in)
		}
	}
	if p, ok := Aggregation("p99.9").Percentile(); !ok || math.Abs(p-0.999) > 1e-12 {
		t.Errorf("p99.9 = %v %v", p, ok)
	}
	if _, ok := AggMedian.Percentile(); ok {
		t.Error("median is not a percentile")
	}
	order := []Aggregation{AggMin, AggMedian, AggMean, AggMax, "p9.5", "p90", "p99", "p99.9"}
	for i := 1; i < len(order); i++ {
		if !order[i-1].Less(order[i]) || order[i].Less(order[i-1]) {
			t.Errorf("%s should sort before %s", order[i-1], order[i])
		}
	}
	for _, u := range []string{"records", "bytes", "ops_1", "x-y"} {
		if _, _, err := ParseRate("1 " + u + "/s"); err != nil {
			t.Errorf("ParseRate unit %q: %v", u, err)
		}
	}
	for _, u := range []string{"", "1x", "records/s", "a b"} {
		if _, _, err := ParseRate("1 " + u + "/s"); err == nil {
			t.Errorf("ParseRate accepted invalid unit %q", u)
		}
	}
	if !IsByteUnit("MiB") || IsByteUnit("records") {
		t.Error("unit helpers")
	}
}

// FuzzParseThreshold checks that a parsed threshold always has a positive,
// finite limit and an operator that matches the metric's direction.
func FuzzParseThreshold(f *testing.F) {
	for _, seed := range []string{"<= 80ms", ">= 50MiB/s", "<= 64MiB", "< 150%", "> 0 ops/s", ">= 1e9 records/s", "<= 99999999999999999999GiB"} {
		f.Add(seed, uint8(0), uint8(0))
	}
	f.Fuzz(func(t *testing.T, s string, k, dir uint8) {
		kind := Kind(int(k%4) + 1)
		better := []Direction{LowerIsBetter, HigherIsBetter, Neutral}[int(dir)%3]
		th, err := ParseThreshold(kind, better, s)
		if err != nil {
			return
		}
		if th.Limit <= 0 || math.IsNaN(th.Limit) || math.IsInf(th.Limit, 0) {
			t.Fatalf("ParseThreshold(%q) limit = %v", s, th.Limit)
		}
		if (better == LowerIsBetter && !th.Op.Upper()) || (better == HigherIsBetter && th.Op.Upper()) {
			t.Fatalf("ParseThreshold(%q) op %s against direction %s", s, th.Op, better)
		}
	})
}
