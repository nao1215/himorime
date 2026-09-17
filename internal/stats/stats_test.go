package stats

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"
)

func ms(values ...float64) []time.Duration {
	out := make([]time.Duration, len(values))
	for i, v := range values {
		out[i] = time.Duration(v * float64(time.Millisecond))
	}
	return out
}

func TestSummarize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		samples []time.Duration
		want    Summary
	}{
		{name: "empty", samples: nil, want: Summary{}},
		{
			name:    "single sample has no spread",
			samples: ms(5),
			want:    Summary{Count: 1, Mean: 5 * time.Millisecond, Median: 5 * time.Millisecond, Min: 5 * time.Millisecond, Max: 5 * time.Millisecond},
		},
		{
			name:    "odd count median is the middle value",
			samples: ms(3, 1, 2),
			want: Summary{
				Count: 3, Mean: 2 * time.Millisecond, Median: 2 * time.Millisecond,
				Stddev: time.Millisecond, Min: time.Millisecond, Max: 3 * time.Millisecond, CV: 0.5,
			},
		},
		{
			name:    "even count median averages the middle pair",
			samples: []time.Duration{4, 1, 3, 2},
			want:    Summary{Count: 4, Mean: 3, Median: 3, Stddev: 1, Min: 1, Max: 4, CV: math.Sqrt(5.0/3.0) / 2.5},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Summarize(tt.samples)
			if got.Count != tt.want.Count || got.Mean != tt.want.Mean || got.Median != tt.want.Median ||
				got.Min != tt.want.Min || got.Max != tt.want.Max || got.Stddev != tt.want.Stddev {
				t.Fatalf("Summarize() = %+v, want %+v", got, tt.want)
			}
			if math.Abs(got.CV-tt.want.CV) > 1e-9 {
				t.Fatalf("CV = %v, want %v", got.CV, tt.want.CV)
			}
		})
	}
}

func TestSummarizeDoesNotReorderInput(t *testing.T) {
	t.Parallel()
	in := ms(3, 1, 2)
	Summarize(in)
	if in[0] != 3*time.Millisecond || in[1] != time.Millisecond {
		t.Fatalf("input was modified: %v", in)
	}
}

func TestSummaryValue(t *testing.T) {
	t.Parallel()
	s := Summary{Mean: 1, Median: 2, Min: 3, Max: 4}
	for m, want := range map[Metric]time.Duration{Mean: 1, Median: 2, Min: 3, Max: 4} {
		if got := s.Value(m); got != want {
			t.Errorf("Value(%v) = %v, want %v", m, got, want)
		}
	}
}

func TestGeometricMean(t *testing.T) {
	t.Parallel()

	got, ok := GeometricMean([]float64{1, 4})
	if !ok || math.Abs(got-2) > 1e-12 {
		t.Fatalf("GeometricMean(1,4) = %v, %v; want 2, true", got, ok)
	}
	got, ok = GeometricMean([]float64{2, 8, 0.5})
	if !ok || math.Abs(got-2) > 1e-12 {
		t.Fatalf("GeometricMean(2,8,0.5) = %v, %v; want 2, true", got, ok)
	}
	for _, bad := range [][]float64{nil, {0}, {-1, 2}, {math.NaN()}, {math.Inf(1)}} {
		if _, ok := GeometricMean(bad); ok {
			t.Errorf("GeometricMean(%v) reported ok", bad)
		}
	}
}

func TestPercentile(t *testing.T) {
	t.Parallel()
	v := []float64{0, 10, 20, 30, 40}
	cases := map[float64]float64{0: 0, 1: 40, 0.5: 20, 0.25: 10, 0.125: 5}
	for p, want := range cases {
		if got := percentile(v, p); math.Abs(got-want) > 1e-9 {
			t.Errorf("percentile(%v) = %v, want %v", p, got, want)
		}
	}
	if !math.IsNaN(percentile(nil, 0.5)) {
		t.Error("percentile of nothing must be NaN")
	}
}

// noisy returns n samples around center with a relative jitter, generated from
// seed so that the test data is fixed.
func noisy(seed uint64, n int, center time.Duration, jitter float64) []time.Duration {
	rng := rand.New(rand.NewPCG(seed, 1))
	out := make([]time.Duration, n)
	for i := range out {
		f := 1 + jitter*(rng.Float64()*2-1)
		out[i] = time.Duration(float64(center) * f)
	}
	return out
}

func defaultOptions() CompareOptions {
	return CompareOptions{Metric: Median, MaxPercent: 10, Confidence: 0.95, MinSamples: 10, MaxCV: 0.5, Seed: 42}
}

func TestCompareVerdicts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		base   []time.Duration
		head   []time.Duration
		opts   func(*CompareOptions)
		want   Verdict
		reason string
	}{
		{
			name: "identical distributions pass",
			base: noisy(1, 30, 100*time.Millisecond, 0.02),
			head: noisy(2, 30, 100*time.Millisecond, 0.02),
			want: VerdictPass,
		},
		{
			name: "small slowdown within tolerance passes",
			base: noisy(1, 30, 100*time.Millisecond, 0.02),
			head: noisy(2, 30, 103*time.Millisecond, 0.02),
			want: VerdictPass,
		},
		{
			name: "clear slowdown beyond tolerance is a regression",
			base: noisy(1, 30, 100*time.Millisecond, 0.02),
			head: noisy(2, 30, 150*time.Millisecond, 0.02),
			want: VerdictRegression,
		},
		{
			name: "clear speedup is an improvement",
			base: noisy(1, 30, 150*time.Millisecond, 0.02),
			head: noisy(2, 30, 100*time.Millisecond, 0.02),
			want: VerdictImproved,
		},
		{
			name:   "too few samples is inconclusive even for a large change",
			base:   noisy(1, 5, 100*time.Millisecond, 0.01),
			head:   noisy(2, 5, 300*time.Millisecond, 0.01),
			want:   VerdictInconclusive,
			reason: "fewer samples than min_samples",
		},
		{
			name: "a noisy side is inconclusive",
			base: ms(10, 400, 10, 400, 10, 400, 10, 400, 10, 400, 10, 400),
			head: ms(12, 420, 12, 420, 12, 420, 12, 420, 12, 420, 12, 420),
			want: VerdictInconclusive, reason: "measurements are noisier than max_cv",
		},
		{
			name: "change straddling the tolerance is inconclusive",
			base: noisy(1, 12, 100*time.Millisecond, 0.15),
			head: noisy(2, 12, 112*time.Millisecond, 0.15),
			want: VerdictInconclusive, reason: "the change is too close to the tolerance to call",
		},
		{
			name: "empty side is inconclusive",
			base: nil,
			head: ms(1, 2, 3),
			want: VerdictInconclusive, reason: "no successful samples",
		},
		{
			name: "mean metric sees an outlier the median ignores",
			base: ms(10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10),
			head: ms(10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 100),
			opts: func(o *CompareOptions) { o.MaxCV = 0 },
			want: VerdictPass,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o := defaultOptions()
			if tt.opts != nil {
				tt.opts(&o)
			}
			got := Compare(tt.base, tt.head, o)
			if got.Verdict != tt.want {
				t.Fatalf("verdict = %s (change %.2f%%, P(reg)=%.3f, P(imp)=%.3f, CI [%.2f, %.2f]), want %s",
					got.Verdict, got.Change, got.ProbRegression, got.ProbImprovement, got.Low, got.High, tt.want)
			}
			if tt.reason != "" && got.Reason != tt.reason {
				t.Fatalf("reason = %q, want %q", got.Reason, tt.reason)
			}
		})
	}
}

func TestCompareIsDeterministicForASeed(t *testing.T) {
	t.Parallel()
	base := noisy(7, 25, 50*time.Millisecond, 0.1)
	head := noisy(8, 25, 55*time.Millisecond, 0.1)
	o := defaultOptions()
	first := Compare(base, head, o)
	for range 5 {
		again := Compare(base, head, o)
		if again != first {
			t.Fatalf("Compare is not deterministic: %+v != %+v", again, first)
		}
	}
	o.Seed = 43
	other := Compare(base, head, o)
	if other.Low == first.Low && other.High == first.High && other.ProbRegression == first.ProbRegression {
		t.Fatalf("a different seed produced an identical bootstrap: %+v", other)
	}
}

func TestCompareRegressionBoundary(t *testing.T) {
	t.Parallel()
	// With no noise at all every resample equals the observed change, so the
	// verdict flips exactly at the tolerance.
	base := ms(100, 100, 100, 100, 100, 100, 100, 100, 100, 100)
	at := ms(110, 110, 110, 110, 110, 110, 110, 110, 110, 110)
	over := ms(111, 111, 111, 111, 111, 111, 111, 111, 111, 111)
	o := defaultOptions()
	if v := Compare(base, at, o).Verdict; v != VerdictPass {
		t.Fatalf("exactly +10%% with max_percent 10 = %s, want pass", v)
	}
	if v := Compare(base, over, o).Verdict; v != VerdictRegression {
		t.Fatalf("+11%% with max_percent 10 = %s, want regression", v)
	}
	under := ms(89, 89, 89, 89, 89, 89, 89, 89, 89, 89)
	if v := Compare(base, under, o).Verdict; v != VerdictImproved {
		t.Fatalf("-11%% = %s, want improved", v)
	}
	o.MinSamples = 11
	if v := Compare(base, over, o).Verdict; v != VerdictInconclusive {
		t.Fatalf("10 samples with min_samples 11 = %s, want inconclusive", v)
	}
}

func TestCompareConfidenceBounds(t *testing.T) {
	t.Parallel()
	base := noisy(3, 40, 100*time.Millisecond, 0.05)
	head := noisy(4, 40, 120*time.Millisecond, 0.05)
	c := Compare(base, head, defaultOptions())
	if c.Low > c.Change || c.High < c.Change {
		t.Fatalf("observed change %.2f outside interval [%.2f, %.2f]", c.Change, c.Low, c.High)
	}
	if c.ProbRegression < 0 || c.ProbRegression > 1 || c.ProbImprovement < 0 || c.ProbImprovement > 1 {
		t.Fatalf("probabilities out of range: %+v", c)
	}
}
