// Package stats computes the summary statistics himorime reports and the
// bootstrap comparison it uses to classify a change between two revisions.
//
// Every function is pure: samples in, numbers out. Samples are float64 values
// in the canonical unit of their metric (nanoseconds, bytes, work per second,
// percent), so one implementation serves every metric. Randomness comes from
// a caller-supplied seed, so a comparison is reproducible bit for bit.
package stats

import (
	"math"
	"slices"
	"time"

	"github.com/nao1215/himorime/internal/metric"
)

// Summary describes a set of samples.
type Summary struct {
	Count  int
	Mean   float64
	Median float64
	// Stddev is the sample standard deviation (n-1 denominator); 0 with fewer
	// than two samples.
	Stddev float64
	Min    float64
	Max    float64
	// CV is the coefficient of variation: the sample standard deviation divided
	// by the mean. It is 0 when there are fewer than two samples or the mean
	// is not positive.
	CV float64
}

// Summarize computes the summary of samples. It does not modify samples.
func Summarize(samples []float64) Summary {
	n := len(samples)
	if n == 0 {
		return Summary{}
	}
	sorted := slices.Clone(samples)
	slices.Sort(sorted)

	mean := meanFloat(samples)
	s := Summary{
		Count:  n,
		Mean:   mean,
		Median: medianSorted(sorted),
		Min:    sorted[0],
		Max:    sorted[n-1],
	}
	if n > 1 {
		var ss float64
		for _, v := range samples {
			d := v - mean
			ss += d * d
		}
		sd := math.Sqrt(ss / float64(n-1))
		s.Stddev = sd
		if mean > 0 {
			s.CV = sd / mean
		}
	}
	return s
}

// Durations converts duration samples to float64 nanoseconds.
func Durations(ds []time.Duration) []float64 {
	out := make([]float64, len(ds))
	for i, d := range ds {
		out[i] = float64(d)
	}
	return out
}

func meanFloat(samples []float64) float64 {
	var sum float64
	for _, v := range samples {
		sum += v
	}
	return sum / float64(len(samples))
}

// medianSorted returns the median of an already sorted slice.
func medianSorted(sorted []float64) float64 {
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// Quantile returns the p-quantile (0..1) of samples using linear
// interpolation between the closest ranks (the method numpy and R call
// type 7). It does not modify samples; it returns NaN for no samples.
func Quantile(samples []float64, p float64) float64 {
	sorted := slices.Clone(samples)
	slices.Sort(sorted)
	return percentile(sorted, p)
}

// Aggregate reduces samples with an aggregation. ok is false when there are
// no samples or the aggregation is unknown.
func Aggregate(samples []float64, agg metric.Aggregation) (float64, bool) {
	if len(samples) == 0 {
		return math.NaN(), false
	}
	switch agg {
	case metric.AggMin:
		return slices.Min(samples), true
	case metric.AggMax:
		return slices.Max(samples), true
	case metric.AggMean:
		return meanFloat(samples), true
	case metric.AggMedian:
		sorted := slices.Clone(samples)
		slices.Sort(sorted)
		return medianSorted(sorted), true
	}
	if p, ok := agg.Percentile(); ok {
		return Quantile(samples, p), true
	}
	return math.NaN(), false
}

// Metric selects the statistic a comparison is made on.
type Metric int

// Metrics understood by Value and Compare.
const (
	Median Metric = iota
	Mean
	Min
	Max
)

// Value returns the chosen statistic of the summary.
func (s Summary) Value(m Metric) float64 {
	switch m {
	case Mean:
		return s.Mean
	case Min:
		return s.Min
	case Max:
		return s.Max
	case Median:
		return s.Median
	}
	return s.Median
}

// GeometricMean returns the geometric mean of positive ratios. ok is false when
// ratios is empty or holds a value that is not positive and finite, because the
// geometric mean is undefined there and a substitute would be a made-up number.
func GeometricMean(ratios []float64) (float64, bool) {
	if len(ratios) == 0 {
		return 0, false
	}
	var logSum float64
	for _, r := range ratios {
		if r <= 0 || math.IsNaN(r) || math.IsInf(r, 0) {
			return 0, false
		}
		logSum += math.Log(r)
	}
	return math.Exp(logSum / float64(len(ratios))), true
}
