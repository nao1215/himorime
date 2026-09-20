package stats

import "testing"

func TestSeparatedRejectsZeroLowerBounds(t *testing.T) {
	t.Parallel()
	for _, bounds := range [][2]Summary{
		{{Min: 0, Max: 1}, {Min: 2, Max: 3}},
		{{Min: 2, Max: 3}, {Min: 0, Max: 1}},
	} {
		if separated(bounds[0], bounds[1]) {
			t.Fatalf("accepted a zero lower bound: %+v", bounds)
		}
	}
}

func TestSeparatedNoisySamples(t *testing.T) {
	t.Parallel()
	base := []float64{8.6, 8.6, 8.8, 9.2, 21.6, 24.3, 30.1, 30.1, 30.6, 35.6}
	head := []float64{38.9, 39, 40, 43, 50, 55, 60, 65, 68, 68.8}
	for _, statistic := range []Metric{Median, Mean} {
		for _, reverse := range []bool{false, true} {
			for _, higher := range []bool{false, true} {
				b, h := base, head
				if reverse {
					b, h = h, b
				}
				o := defaultOptions()
				o.Metric, o.HigherIsBetter = statistic, higher
				want := VerdictRegression
				if reverse != higher {
					want = VerdictImproved
				}
				for _, seed := range []uint64{1, 42, 999} {
					o.Seed = seed
					c := Compare(b, h, o)
					if c.Verdict != want {
						t.Fatalf("%v reverse=%t higher=%t: %+v", statistic, reverse, higher, c)
					}
				}
			}
		}
	}
	o := defaultOptions()
	o.MinSamples = 11
	if c := Compare(base, head, o); c.Reason != ReasonFewSamples {
		t.Fatalf("few samples: %+v", c)
	}
	o.MinSamples = 10
	o.MinDifference = 100
	if c := Compare(base, head, o); c.Reason != ReasonBelowMinDiff {
		t.Fatalf("absolute floor: %+v", c)
	}
	o.MinDifference = 0
	o.MaxPercent = 1000
	if c := Compare(base, head, o); c.Verdict != VerdictPass {
		t.Fatalf("within tolerance: %+v", c)
	}
	o.MaxPercent = 10
	overlap := append([]float64(nil), head...)
	overlap[0] = 20
	if c := Compare(base, overlap, o); c.Reason != ReasonNoisy {
		t.Fatalf("overlap: %+v", c)
	}
	// Touching ranges do not establish strict ordering.
	overlap[0] = 35.6
	if c := Compare(base, overlap, o); c.Reason != ReasonNoisy {
		t.Fatalf("touching: %+v", c)
	}
}
