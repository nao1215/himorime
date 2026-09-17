package stats

import (
	"math"
	"math/rand/v2"
	"slices"
	"time"
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

// CompareOptions configures Compare.
type CompareOptions struct {
	Metric Metric
	// MaxPercent is the tolerated slowdown in percent, such as 10.
	MaxPercent float64
	// Confidence is the probability required to call a change, such as 0.95.
	Confidence float64
	// MinSamples is the smallest sample count on either side that can be judged.
	MinSamples int
	// MaxCV marks a side noisier than this coefficient of variation as
	// inconclusive. 0 disables the check.
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
	// Change is the observed relative change of the metric in percent:
	// (head - base) / base * 100. Positive means slower.
	Change float64
	// Low and High bound the central Confidence interval of the change in
	// percent, taken from the bootstrap distribution.
	Low  float64
	High float64
	// ProbRegression is the share of bootstrap resamples whose change exceeds
	// +MaxPercent; ProbImprovement the share below -MaxPercent.
	ProbRegression  float64
	ProbImprovement float64
	Verdict         Verdict
	// Reason explains an inconclusive verdict in one short sentence.
	Reason string
}

// Compare classifies the change from base to head.
//
// The method is a percentile bootstrap of the relative change of the chosen
// metric. Each of Resamples iterations draws len(base) values from base and
// len(head) values from head with replacement, computes the metric of each
// resample, and records (head/base - 1) * 100. From that distribution:
//
//   - regression: P(change > +MaxPercent) >= Confidence
//   - improved:   P(change < -MaxPercent) >= Confidence
//   - pass:       P(change <= +MaxPercent) >= Confidence
//   - inconclusive: none of the above, or too few samples, or a side whose
//     coefficient of variation exceeds MaxCV.
//
// A regression therefore needs both an observed slowdown beyond the tolerance
// and enough evidence that it is not noise; a single slow sample cannot fail a
// build.
func Compare(base, head []time.Duration, o CompareOptions) Comparison {
	c := Comparison{Base: Summarize(base), Head: Summarize(head)}
	if c.Base.Count == 0 || c.Head.Count == 0 {
		c.Verdict = VerdictInconclusive
		c.Reason = "no successful samples"
		return c
	}
	bv := float64(c.Base.Value(o.Metric))
	hv := float64(c.Head.Value(o.Metric))
	if bv <= 0 {
		c.Verdict = VerdictInconclusive
		c.Reason = "the base measurement is zero"
		return c
	}
	c.Change = (hv/bv - 1) * 100

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
	var above, below int
	for _, ch := range changes {
		if ch > o.MaxPercent+epsilon {
			above++
		}
		if ch < -o.MaxPercent-epsilon {
			below++
		}
	}
	c.ProbRegression = float64(above) / float64(len(changes))
	c.ProbImprovement = float64(below) / float64(len(changes))

	switch {
	case c.Base.Count < o.MinSamples || c.Head.Count < o.MinSamples:
		c.Verdict = VerdictInconclusive
		c.Reason = "fewer samples than min_samples"
	case o.MaxCV > 0 && (c.Base.CV > o.MaxCV || c.Head.CV > o.MaxCV):
		c.Verdict = VerdictInconclusive
		c.Reason = "measurements are noisier than max_cv"
	case c.Change > o.MaxPercent+epsilon && c.ProbRegression >= o.Confidence:
		c.Verdict = VerdictRegression
	case c.Change < -o.MaxPercent-epsilon && c.ProbImprovement >= o.Confidence:
		c.Verdict = VerdictImproved
	case 1-c.ProbRegression >= o.Confidence:
		c.Verdict = VerdictPass
	default:
		c.Verdict = VerdictInconclusive
		c.Reason = "the change is too close to the tolerance to call"
	}
	return c
}

func bootstrapChanges(base, head []time.Duration, m Metric, resamples int, seed uint64) []float64 {
	// Two PCG streams from one seed: the base and head draws are independent
	// yet fully determined by the seed.
	rng := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15)) //nolint:gosec // a reproducible bootstrap needs a seeded generator, not a secure one
	bbuf := make([]time.Duration, len(base))
	hbuf := make([]time.Duration, len(head))
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
func metricOf(buf []time.Duration, m Metric) float64 {
	switch m {
	case Mean:
		return meanFloat(buf)
	case Min:
		return float64(slices.Min(buf))
	case Max:
		return float64(slices.Max(buf))
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
