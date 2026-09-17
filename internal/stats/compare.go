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
	ReasonNoSamples    = "no successful samples"
	ReasonZeroBase     = "the base measurement is zero"
	ReasonFewSamples   = "fewer samples than min_samples"
	ReasonNoisy        = "measurements are noisier than max_cv"
	ReasonTooClose     = "the change is too close to the tolerance to call"
	ReasonBelowMinDiff = "the difference is smaller than min_difference"
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
	// MaxCV marks a side whose dispersion exceeds it as inconclusive. The
	// dispersion follows Metric: Summary.RobustCV for an order statistic,
	// Summary.CV for the mean. 0 disables the check.
	MaxCV float64
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
	// Difference is HeadValue - BaseValue in the metric's unit.
	Difference float64
	// Change is the observed relative change of the metric in percent:
	// (head - base) / base * 100. Its sign is the direction the value moved,
	// not whether that is better or worse.
	Change float64
	// Low and High bound the central Confidence interval of the change in
	// percent, taken from the bootstrap distribution.
	Low  float64
	High float64
	// ProbRegression is the share of bootstrap resamples that degrade beyond
	// MaxPercent in the metric's worse direction; ProbImprovement the share
	// that improve beyond it.
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
//   - inconclusive: none of the above, or too few samples, or a side whose
//     dispersion exceeds MaxCV. The dispersion follows the compared statistic:
//     the classic coefficient of variation for the mean, the robust one for
//     the median and the other order statistics.
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
	bv := c.Base.Value(o.Metric)
	hv := c.Head.Value(o.Metric)
	c.BaseValue, c.HeadValue, c.Difference = bv, hv, hv-bv
	if bv <= 0 {
		c.Verdict = VerdictInconclusive
		c.Reason = ReasonZeroBase
		return c
	}
	c.Change = (hv/bv - 1) * 100
	sign := 1.0
	if o.HigherIsBetter {
		sign = -1
	}

	resamples := o.Resamples
	if resamples <= 0 {
		resamples = DefaultResamples
	}
	changes := bootstrapChanges(base, head, o.Metric, resamples, o.Seed)
	alpha := (1 - o.Confidence) / 2
	c.Low = percentile(changes, alpha)
	c.High = percentile(changes, 1-alpha)
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
	c.ProbRegression = float64(worse) / float64(len(changes))
	c.ProbImprovement = float64(better) / float64(len(changes))
	degradation := sign * c.Change

	switch {
	case c.Base.Count < o.MinSamples || c.Head.Count < o.MinSamples:
		c.Verdict = VerdictInconclusive
		c.Reason = ReasonFewSamples
	case o.MaxCV > 0 && (c.Base.Dispersion(o.Metric) > o.MaxCV || c.Head.Dispersion(o.Metric) > o.MaxCV):
		c.Verdict = VerdictInconclusive
		c.Reason = ReasonNoisy
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
			continue
		}
		out = append(out, (h/b-1)*100)
	}
	if len(out) == 0 {
		out = append(out, 0)
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
		slices.Sort(buf)
		return medianSorted(buf)
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
