package stats

import (
	"encoding/binary"
	"math"
	"testing"
)

func decodeSamples(data []byte) []float64 {
	var out []float64
	for len(data) >= 4 {
		v := binary.LittleEndian.Uint32(data)
		out = append(out, float64(v))
		data = data[4:]
	}
	return out
}

// FuzzCompare checks invariants of the comparison on arbitrary samples: no
// panic, probabilities within [0, 1], a verdict from the known set, a
// reproducible result for a seed, and no regression without enough samples.
func FuzzCompare(f *testing.F) {
	f.Add([]byte{1, 0, 0, 0, 2, 0, 0, 0}, []byte{3, 0, 0, 0}, uint64(1), 10.0, 0.95, 2)
	f.Add([]byte{}, []byte{1, 0, 0, 0}, uint64(7), 5.0, 0.9, 1)
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 0}, []byte{0, 0, 0, 0}, uint64(0), 0.5, 0.999, 3)
	f.Fuzz(func(t *testing.T, a, b []byte, seed uint64, maxPercent, confidence float64, minSamples int) {
		if math.IsNaN(maxPercent) || math.IsNaN(confidence) || len(a) > 400 || len(b) > 400 {
			return
		}
		o := CompareOptions{Metric: Metric(int(seed % 4)), HigherIsBetter: seed%2 == 1, MaxPercent: math.Abs(maxPercent), Confidence: confidence, MinSamples: minSamples, MaxCV: 0.5, Resamples: 50, Seed: seed}
		base, head := decodeSamples(a), decodeSamples(b)
		c := Compare(base, head, o)
		for _, p := range []float64{c.ProbRegression, c.ProbImprovement} {
			if p < 0 || p > 1 || math.IsNaN(p) {
				t.Fatalf("probability out of range: %+v", c)
			}
		}
		switch c.Verdict {
		case VerdictPass, VerdictImproved, VerdictRegression, VerdictInconclusive:
		default:
			t.Fatalf("unknown verdict %q", c.Verdict)
		}
		if (c.Verdict == VerdictRegression || c.Verdict == VerdictImproved) && (len(base) < minSamples || len(head) < minSamples) {
			t.Fatalf("a verdict without enough samples: %+v", c)
		}
		if again := Compare(base, head, o); again != c {
			t.Fatalf("not reproducible: %+v vs %+v", c, again)
		}
		if gm, ok := GeometricMean([]float64{float64(len(a) + 1), float64(len(b) + 1)}); !ok || gm <= 0 {
			t.Fatalf("geometric mean of positive values = %v, %v", gm, ok)
		}
	})
}
