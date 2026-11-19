package stats

import (
	"math"
	"slices"
	"testing"

	"pgregory.net/rapid"
)

// The comparison has no oracle: nobody can write down the right verdict for
// arbitrary samples. These tests state relations between two comparisons
// instead (metamorphic testing), and invariants of the summaries
// (property-based testing). A broken relation is a wrong verdict for at least
// one of the two inputs.

// comparisonInput is one generated base/head pair with its options.
type comparisonInput struct {
	base, head []float64
	o          CompareOptions
}

// genSamples draws n positive samples. Values come from a small pool part of
// the time so that ties, which order statistics and the median handle
// specially, are common.
func genSamples(t *rapid.T, n int, label string) []float64 {
	pool := rapid.SliceOfN(rapid.Float64Range(1e3, 1e9), 1, 4).Draw(t, label+" pool")
	out := make([]float64, n)
	for i := range out {
		if rapid.Bool().Draw(t, label+" from pool") {
			out[i] = rapid.SampledFrom(pool).Draw(t, label+" pooled")
		} else {
			out[i] = rapid.Float64Range(1e3, 1e9).Draw(t, label+" fresh")
		}
	}
	return out
}

func genComparison(t *rapid.T) comparisonInput {
	paired := rapid.Bool().Draw(t, "paired")
	nb := rapid.IntRange(1, 30).Draw(t, "base count")
	nh := nb
	if !paired {
		nh = rapid.IntRange(1, 30).Draw(t, "head count")
	}
	base := genSamples(t, nb, "base")
	// The head is the base moved by a factor, with jitter, so that
	// regressions, improvements, passes and close calls all occur.
	factor := rapid.SampledFrom([]float64{0.5, 0.8, 0.95, 1, 1.05, 1.2, 2}).Draw(t, "factor")
	jitter := rapid.SampledFrom([]float64{0, 0.01, 0.1, 0.5}).Draw(t, "jitter")
	head := make([]float64, nh)
	for i := range head {
		noise := rapid.Float64Range(-jitter, jitter).Draw(t, "noise")
		head[i] = base[i%nb] * factor * (1 + noise)
	}
	o := CompareOptions{
		Metric:         Metric(rapid.IntRange(0, 3).Draw(t, "metric")),
		HigherIsBetter: rapid.Bool().Draw(t, "higher is better"),
		MaxPercent:     rapid.SampledFrom([]float64{0, 1, 5, 10, 30}).Draw(t, "max percent"),
		MinDifference:  rapid.SampledFrom([]float64{0, 0, 1e4, 1e7}).Draw(t, "min difference"),
		Confidence:     rapid.SampledFrom([]float64{0.9, 0.95, 0.99}).Draw(t, "confidence"),
		MinSamples:     rapid.IntRange(0, 5).Draw(t, "min samples"),
		MaxCV:          rapid.SampledFrom([]float64{0, 0.05, 0.5}).Draw(t, "max cv"),
		PairRounds:     paired,
		Resamples:      200,
		Seed:           rapid.Uint64().Draw(t, "seed"),
	}
	return comparisonInput{base: base, head: head, o: o}
}

func scaled(samples []float64, k float64) []float64 {
	out := make([]float64, len(samples))
	for i, v := range samples {
		out[i] = v * k
	}
	return out
}

// sameJudgement reports whether two comparisons reached the same decision
// from the same evidence: the verdict, its reason, the relative change, its
// interval and both probabilities.
func sameJudgement(a, b Comparison) bool {
	return a.Verdict == b.Verdict && a.Reason == b.Reason &&
		a.Change == b.Change && a.Low == b.Low && a.High == b.High &&
		a.ProbRegression == b.ProbRegression && a.ProbImprovement == b.ProbImprovement
}

// Measuring in another unit must not change a verdict: the same run in
// microseconds instead of nanoseconds, or in KiB instead of bytes, is the
// same change. Scaling both sides and min_difference by a power of two keeps
// every floating point operation exact, so the comparison must be identical,
// not merely close.
func TestCompareIsInvariantUnderAChangeOfUnit(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		in := genComparison(t)
		k := math.Ldexp(1, rapid.IntRange(-10, 10).Draw(t, "exponent"))
		o := in.o
		o.MinDifference *= k
		got := Compare(scaled(in.base, k), scaled(in.head, k), o)
		want := Compare(in.base, in.head, in.o)
		if !sameJudgement(got, want) {
			t.Fatalf("scaling by %v changed the judgement:\n got  %+v\n want %+v", k, got, want)
		}
		if got.BaseValue != want.BaseValue*k || got.HeadValue != want.HeadValue*k || got.Difference != want.Difference*k {
			t.Fatalf("scaling by %v did not scale the values:\n got  %+v\n want %+v", k, got, want)
		}
	})
}

// Flipping the direction of a metric (latency, where lower is better, against
// throughput, where higher is better) swaps what counts as worse and better.
// The same samples must then swap the two probabilities, turn a regression
// into an improvement and back, and leave the evidence gates alone.
func TestCompareMirrorsWhenTheBetterDirectionFlips(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		in := genComparison(t)
		flipped := in.o
		flipped.HigherIsBetter = !in.o.HigherIsBetter
		a := Compare(in.base, in.head, in.o)
		b := Compare(in.base, in.head, flipped)
		if a.ProbRegression != b.ProbImprovement || a.ProbImprovement != b.ProbRegression {
			t.Fatalf("probabilities did not swap:\n %+v\n %+v", a, b)
		}
		if a.Change != b.Change || a.Low != b.Low || a.High != b.High {
			t.Fatalf("the observed change depends on the direction:\n %+v\n %+v", a, b)
		}
		mirror := map[Verdict]Verdict{VerdictRegression: VerdictImproved, VerdictImproved: VerdictRegression}
		if want, ok := mirror[a.Verdict]; ok && b.Verdict != want {
			t.Fatalf("%s did not become %s when the direction flipped:\n %+v\n %+v", a.Verdict, want, a, b)
		}
		switch a.Reason {
		case ReasonNoSamples, ReasonZeroBase, ReasonFewSamples, ReasonNoisy, ReasonBelowMinDiff, ReasonUnpaired, ReasonZeroBaseSample:
			if b.Verdict != a.Verdict || b.Reason != a.Reason {
				t.Fatalf("the evidence gate %q depends on the direction:\n %+v\n %+v", a.Reason, a, b)
			}
		}
	})
}

// Comparing a revision with itself is the A/A test: nothing changed, so no
// confidence level a user can configure may report a regression or an
// improvement. Paired rounds of identical samples have every ratio at
// exactly 1, so once the sample count is enough the verdict is a pass.
func TestCompareNeverCallsAChangeBetweenIdenticalSamples(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		in := genComparison(t)
		c := Compare(in.base, slices.Clone(in.base), in.o)
		if c.Verdict == VerdictRegression || c.Verdict == VerdictImproved {
			t.Fatalf("identical samples were judged %s: %+v", c.Verdict, c)
		}
		if in.o.PairRounds && len(in.base) >= in.o.MinSamples && c.Verdict != VerdictPass {
			t.Fatalf("identical paired rounds were not a pass: %+v", c)
		}
	})
}

// Making the head slower can only add evidence of a regression: with the
// same seed every bootstrap draw picks the same samples, and each drawn
// change grows. A regression stays a regression, and a lower-is-better
// comparison that was a pass cannot become a regression when the head gets
// faster.
func TestCompareIsMonotoneInTheHead(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		in := genComparison(t)
		in.o.HigherIsBetter = false
		k := math.Ldexp(1, rapid.IntRange(1, 4).Draw(t, "exponent"))
		before := Compare(in.base, in.head, in.o)
		slower := Compare(in.base, scaled(in.head, k), in.o)
		if slower.ProbRegression < before.ProbRegression {
			t.Fatalf("a %vx slower head lowered the regression probability:\n before %+v\n slower %+v", k, before, slower)
		}
		if before.Verdict == VerdictRegression && slower.Verdict != VerdictRegression {
			t.Fatalf("a %vx slower head is no longer a regression:\n before %+v\n slower %+v", k, before, slower)
		}
		faster := Compare(in.base, scaled(in.head, 1/k), in.o)
		if before.Verdict == VerdictPass && faster.Verdict == VerdictRegression {
			t.Fatalf("a %vx faster head turned a pass into a regression:\n before %+v\n faster %+v", k, before, faster)
		}
	})
}

// The summary statistics are order statistics and averages of the same
// samples, so they are ordered, independent of the order the samples arrived
// in, and the quantile function runs from the minimum to the maximum without
// going back.
func TestSummaryProperties(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		samples := genSamples(t, rapid.IntRange(1, 40).Draw(t, "count"), "samples")
		s := Summarize(samples)
		if s.Count != len(samples) {
			t.Fatalf("count %d, want %d", s.Count, len(samples))
		}
		// The mean is a rounded sum, so allow it a few ulps outside the range.
		slack := 4 * (math.Nextafter(s.Max, math.Inf(1)) - s.Max)
		if s.Min > s.Median || s.Median > s.Max || s.Mean < s.Min-slack || s.Mean > s.Max+slack {
			t.Fatalf("summary out of order: %+v", s)
		}
		if s.Stddev < 0 || s.CV < 0 || s.RobustCV < 0 {
			t.Fatalf("negative dispersion: %+v", s)
		}
		if Quantile(samples, 0) != s.Min || Quantile(samples, 1) != s.Max {
			t.Fatalf("quantile ends disagree with the summary: %+v", s)
		}
		// The median averages the middle pair while the quantile
		// interpolates from its lower end, so the two may differ in the last
		// bit, never more.
		if mid := Quantile(samples, 0.5); math.Abs(mid-s.Median) > slack {
			t.Fatalf("Quantile(0.5) = %v, median %v", mid, s.Median)
		}
		p := rapid.Float64Range(0, 1).Draw(t, "p")
		q := rapid.Float64Range(p, 1).Draw(t, "q")
		if Quantile(samples, p) > Quantile(samples, q) {
			t.Fatalf("Quantile(%v) = %v > Quantile(%v) = %v", p, Quantile(samples, p), q, Quantile(samples, q))
		}
		shuffled := slices.Clone(samples)
		perm := rapid.Permutation(shuffled).Draw(t, "permutation")
		r := Summarize(perm)
		if r.Min != s.Min || r.Max != s.Max || r.Median != s.Median || r.RobustCV != s.RobustCV || Quantile(perm, p) != Quantile(samples, p) {
			t.Fatalf("order statistics depend on the sample order:\n %+v\n %+v", s, r)
		}
	})
}
