// Package stats computes the summary statistics yahiko reports and the
// bootstrap comparison it uses to classify a change between two revisions.
//
// Every function is pure: samples in, numbers out. Randomness comes from a
// caller-supplied seed, so a comparison is reproducible bit for bit.
package stats

import (
	"math"
	"slices"
	"time"
)

// Summary describes a set of duration samples.
type Summary struct {
	Count  int
	Mean   time.Duration
	Median time.Duration
	Stddev time.Duration
	Min    time.Duration
	Max    time.Duration
	// CV is the coefficient of variation: the sample standard deviation divided
	// by the mean. It is 0 when there are fewer than two samples.
	CV float64
}

// Summarize computes the summary of samples. It does not modify samples.
func Summarize(samples []time.Duration) Summary {
	n := len(samples)
	if n == 0 {
		return Summary{}
	}
	sorted := slices.Clone(samples)
	slices.Sort(sorted)

	mean := meanFloat(samples)
	s := Summary{
		Count:  n,
		Mean:   roundDuration(mean),
		Median: roundDuration(medianSorted(sorted)),
		Min:    sorted[0],
		Max:    sorted[n-1],
	}
	if n > 1 {
		var ss float64
		for _, v := range samples {
			d := float64(v) - mean
			ss += d * d
		}
		sd := math.Sqrt(ss / float64(n-1))
		s.Stddev = roundDuration(sd)
		if mean > 0 {
			s.CV = sd / mean
		}
	}
	return s
}

func meanFloat(samples []time.Duration) float64 {
	// Accumulating in float64 cannot overflow the way summing int64
	// nanoseconds can for long runs, and the precision lost is far below a
	// nanosecond for any realistic sample count.
	var sum float64
	for _, v := range samples {
		sum += float64(v)
	}
	return sum / float64(len(samples))
}

// medianSorted returns the median of an already sorted slice.
func medianSorted(sorted []time.Duration) float64 {
	n := len(sorted)
	if n%2 == 1 {
		return float64(sorted[n/2])
	}
	return (float64(sorted[n/2-1]) + float64(sorted[n/2])) / 2
}

func roundDuration(ns float64) time.Duration {
	return time.Duration(math.Round(ns))
}

// Metric selects a statistic.
type Metric int

// Metrics understood by Value and Compare.
const (
	Median Metric = iota
	Mean
	Min
	Max
)

// Value returns the chosen statistic of the summary.
func (s Summary) Value(m Metric) time.Duration {
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
