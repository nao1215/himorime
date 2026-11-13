package stats

import (
	"math"
	"math/rand/v2"
	"slices"
)

// Verdict is the classification of one base/head comparison.
type Verdict string

// Verdicts. The strings are part of the JSON report contract.
const (
	VerdictPass         Verdict = "pass"
	VerdictImproved     Verdict = "improved"
	VerdictRegression   Verdict = "regression"
	VerdictInconclusive Verdict = "inconclusive"
)

// DefaultResamples is the number of bootstrap resamples. 2,000 keeps the
// Monte Carlo error of a tail probability around one percentage point while a
// comparison of two 100-sample sets still takes only milliseconds.
const DefaultResamples = 2000

// Reasons attached to a verdict. They are part of the JSON report.
const (
	ReasonNoSamples         = "no successful samples"
	ReasonZeroBase          = "the base measurement is zero"
	reasonZeroBootstrapBase = "a bootstrap base measurement is zero"
	ReasonFewSamples        = "fewer samples than min_samples"
	ReasonNoisy             = "measurements are noisier than max_cv"
	ReasonTooClose          = "the change is too close to the tolerance to call"
	ReasonBelowMinDiff      = "the difference is smaller than min_difference"
	ReasonUnpaired          = "base and head do not have one sample per round"
	ReasonZeroBaseSample    = "a base sample is zero, so a per-round ratio is undefined"
)

// CompareOptions configures Compare.
type CompareOptions struct {
	Metric Metric
	// HigherIsBetter flips the direction of a regression: the metric degrades
	// when it shrinks, as throughput does. The default is lower-is-better.
	HigherIsBetter bool
	// MaxPercent is the tolerated degradation in percent, such as 10.
	MaxPercent float64
	// MinDifference is the smallest absolute difference of the metric, in
	// its canonical unit, that can be a regression or an improvement. 0
	// disables the check.
	MinDifference float64
	// Confidence is the probability required to call a change, such as 0.95.
	Confidence float64
	// MinSamples is the smallest sample count on either side that can be judged.
	MinSamples int
	// MaxCV marks overlapping distributions whose dispersion exceeds it as
	// inconclusive. Fully separated samples beyond the tolerance can still
	// be classified, subject to all the other evidence checks. The
	// dispersion follows Metric: Summary.RobustCV for an order statistic,
	// Summary.CV for the mean. 0 disables the check.
	MaxCV float64
	// PairRounds compares the per-round ratios head[i] / base[i] instead of
	// resampling each side on its own. It needs base[i] and head[i] taken in
	// the same round: the runner runs every unit once per round and appends
	// its samples in round order. A slowdown that hits both runs of a round
	// then cancels out of the ratio. MaxCV applies to the dispersion of the
	// ratios, and the observed change is the statistic of the ratios (Min and
	// Max are order statistics of the ratios), which can differ from the ratio
	// of the two side statistics.
	PairRounds bool
	// Resamples is the bootstrap iteration count; DefaultResamples when 0.
	Resamples int
	// Seed makes the bootstrap reproducible.
	Seed uint64
}

// Comparison is the result of Compare.
type Comparison struct {
	Base Summary
	Head Summary
	// BaseValue and HeadValue are the compared statistic of each side.
	BaseValue float64
	HeadValue float64
	// Difference is HeadValue - BaseValue in the metric's unit, or with
	// PairRounds BaseValue * Change / 100.
	Difference float64
	// Change is the observed relative change of the metric in percent:
	// (head - base) / base * 100. Its sign is the direction the value moved,
	// not whether that is better or worse.
	Change float64
	// Low and High bound the central Confidence interval of the change in
	// percent, taken from the bootstrap distribution. Both are zero when
	// inference is unavailable because a base statistic is zero.
	Low  float64
	High float64
	// ProbRegression is the share of bootstrap resamples that degrade beyond
	// MaxPercent in the metric's worse direction; ProbImprovement the share
	// that improve beyond it. Both are zero when inference is unavailable.
	ProbRegression  float64
	ProbImprovement float64
	Verdict         Verdict
	// Reason explains an inconclusive verdict, or a pass decided by
	// MinDifference, in one short sentence.
	Reason string
}

// Compare classifies the change from base to head.
//
// The method is a percentile bootstrap of the relative change of the chosen
// statistic. Each of Resamples iterations draws len(base) values from base and
// len(head) values from head with replacement, computes the statistic of each
// resample, and records (head/base - 1) * 100. The degradation of a change is
// the change itself for a lower-is-better metric and its negation for a
// higher-is-better metric. From that distribution:
//
//   - regression: P(degradation > +MaxPercent) >= Confidence
//   - improved:   P(degradation < -MaxPercent) >= Confidence
//   - pass:       P(degradation <= +MaxPercent) >= Confidence
//   - inconclusive: none of the above, or too few samples, or overlapping
//     ranges with a side whose dispersion exceeds MaxCV. The dispersion follows the compared statistic:
//     the classic coefficient of variation for the mean, the robust one for
//     the median and the other order statistics.
//
// With PairRounds the same rules apply to the per-round ratios head[i] /
// base[i], resampled by round; see CompareOptions.PairRounds.
//
// An observed absolute difference below MinDifference is a pass whatever the
// relative change. A regression therefore needs an observed degradation
// beyond the tolerance and enough evidence that it is not noise; a single
// slow sample cannot fail a build.
func Compare(base, head []float64, o CompareOptions) Comparison {
	c := Comparison{Base: Summarize(base), Head: Summarize(head)}
	if c.Base.Count == 0 || c.Head.Count == 0 {
		c.Verdict = VerdictInconclusive
		c.Reason = ReasonNoSamples
		return c
	}
	if o.PairRounds {
		return compareRounds(base, head, o, c)
	}
	bv := c.Base.Value(o.Metric)
	hv := c.Head.Value(o.Metric)
	c.BaseValue, c.HeadValue, c.Difference = bv, hv, hv-bv
	if bv <= 0 {
		c.Verdict = VerdictInconclusive
		c.Reason = ReasonZeroBase
		return c
	}
	c.Change = (hv/bv - 1) * 100
	changes := bootstrapChanges(base, head, o.Metric, resamplesOf(o), o.Seed)
	noisy := o.MaxCV > 0 && (c.Base.Dispersion(o.Metric) > o.MaxCV || c.Head.Dispersion(o.Metric) > o.MaxCV) && !separated(c.Base, c.Head)
	return classify(c, changes, noisy, o)
}

// compareRounds is Compare with PairRounds: it resamples rounds, not samples,
// and judges the statistic of the per-round ratios.
func compareRounds(base, head []float64, o CompareOptions, c Comparison) Comparison {
	c.BaseValue, c.HeadValue = c.Base.Value(o.Metric), c.Head.Value(o.Metric)
	c.Difference = c.HeadValue - c.BaseValue
	if len(base) != len(head) {
		c.Verdict, c.Reason = VerdictInconclusive, ReasonUnpaired
		return c
	}
	ratios := make([]float64, len(base))
	for i := range base {
		if base[i] <= 0 {
			c.Verdict, c.Reason = VerdictInconclusive, ReasonZeroBaseSample
			return c
		}
		ratios[i] = head[i] / base[i]
	}
	r := Summarize(ratios)
	c.Change = (r.Value(o.Metric) - 1) * 100
	// min_difference is an absolute threshold, so the paired change is
	// expressed in the metric's unit through the base statistic.
	c.Difference = c.BaseValue * c.Change / 100
	changes := roundChanges(ratios, o.Metric, resamplesOf(o), o.Seed)
	noisy := o.MaxCV > 0 && r.Dispersion(o.Metric) > o.MaxCV
	return classify(c, changes, noisy, o)
}

func resamplesOf(o CompareOptions) int {
	if o.Resamples <= 0 {
		return DefaultResamples
	}
	return o.Resamples
}

// classify sets the interval, the probabilities and the verdict of c from the
// bootstrap distribution of the change. noisy marks a comparison too
// dispersed to judge.
func classify(c Comparison, changes []float64, noisy bool, o CompareOptions) Comparison {
	sign := 1.0
	if o.HigherIsBetter {
		sign = -1
	}
	alpha := (1 - o.Confidence) / 2
	if len(changes) > 0 {
		c.Low = percentile(changes, alpha)
		c.High = percentile(changes, 1-alpha)
	}
	// epsilon absorbs floating point noise: 110/100 is not exactly 1.1, and a
	// change of exactly the tolerance must not count as beyond it.
	const epsilon = 1e-9
	var worse, better int
	for _, ch := range changes {
		d := sign * ch
		if d > o.MaxPercent+epsilon {
			worse++
		}
		if d < -o.MaxPercent-epsilon {
			better++
		}
	}
	if len(changes) > 0 {
		c.ProbRegression = float64(worse) / float64(len(changes))
		c.ProbImprovement = float64(better) / float64(len(changes))
	}
	degradation := sign * c.Change

	switch {
	case c.Base.Count < o.MinSamples || c.Head.Count < o.MinSamples:
		c.Verdict = VerdictInconclusive
		c.Reason = ReasonFewSamples
	case noisy:
		c.Verdict = VerdictInconclusive
		c.Reason = ReasonNoisy
	case len(changes) == 0:
		c.Verdict = VerdictInconclusive
		c.Reason = reasonZeroBootstrapBase
	case o.MinDifference > 0 && math.Abs(c.Difference) < o.MinDifference:
		c.Verdict = VerdictPass
		c.Reason = ReasonBelowMinDiff
	case degradation > o.MaxPercent+epsilon && c.ProbRegression >= o.Confidence:
		c.Verdict = VerdictRegression
	case degradation < -o.MaxPercent-epsilon && c.ProbImprovement >= o.Confidence:
		c.Verdict = VerdictImproved
	case 1-c.ProbRegression >= o.Confidence:
		c.Verdict = VerdictPass
	default:
		c.Verdict = VerdictInconclusive
		c.Reason = ReasonTooClose
	}
	return c
}

// separated bounds every possible resampled statistic by the observed
// extrema. Complete separation establishes an ordering despite within-side
// dispersion. It never bypasses min_samples, min_difference, the effect-size
// tolerance or bootstrap confidence; those still decide the verdict.
func separated(base, head Summary) bool {
	if base.Min <= 0 || head.Min <= 0 {
		return false
	}
	return head.Min > base.Max || head.Max < base.Min
}

// bootstrapChanges returns nil if a draw has an undefined relative change.
func bootstrapChanges(base, head []float64, m Metric, resamples int, seed uint64) []float64 {
	// Two PCG streams from one seed: the base and head draws are independent
	// yet fully determined by the seed.
	rng := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15)) //nolint:gosec // a reproducible bootstrap needs a seeded generator, not a secure one
	bbuf := make([]float64, len(base))
	hbuf := make([]float64, len(head))
	out := make([]float64, 0, resamples)
	for range resamples {
		for i := range bbuf {
			bbuf[i] = base[rng.IntN(len(base))]
		}
		for i := range hbuf {
			hbuf[i] = head[rng.IntN(len(head))]
		}
		b := metricOf(bbuf, m)
		h := metricOf(hbuf, m)
		if b <= 0 {
			// Dropping undefined relative changes would condition the
			// probabilities on a positive base and overstate the evidence.
			return nil
		}
		out = append(out, (h/b-1)*100)
	}
	slices.Sort(out)
	return out
}

// roundChanges resamples round indices with replacement and returns the
// sorted changes (statistic of the drawn ratios - 1) * 100. Every ratio is
// positive, so every change is defined.
func roundChanges(ratios []float64, m Metric, resamples int, seed uint64) []float64 {
	rng := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15)) //nolint:gosec // a reproducible bootstrap needs a seeded generator, not a secure one
	buf := make([]float64, len(ratios))
	out := make([]float64, 0, resamples)
	for range resamples {
		for i := range buf {
			buf[i] = ratios[rng.IntN(len(ratios))]
		}
		out = append(out, (metricOf(buf, m)-1)*100)
	}
	slices.Sort(out)
	return out
}

// metricOf computes the metric of buf, sorting it in place when needed.
func metricOf(buf []float64, m Metric) float64 {
	switch m {
	case Mean:
		return meanFloat(buf)
	case Min:
		return slices.Min(buf)
	case Max:
		return slices.Max(buf)
	case Median:
	}
	slices.Sort(buf)
	return medianSorted(buf)
}

// percentile returns the p-quantile (0..1) of sorted values using linear
// interpolation between closest ranks.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return math.NaN()
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	pos := p * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	frac := pos - float64(lo)
	return sorted[lo] + (sorted[hi]-sorted[lo])*frac
}
