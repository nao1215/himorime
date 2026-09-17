package stats

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"testing"
)

// The calibration test measures how often Compare reaches each verdict on
// synthetic data whose true change is known. Every sample comes from a seeded
// PCG stream and no clock is read, so the tallies, and therefore the
// assertions, are identical on every run.
//
// It also runs pairedCompare, a variant that resamples round indices instead
// of each side independently. The runner interleaves base and head in rounds,
// so sample i of both sides was taken close in time; the paired variant shows
// what that pairing would change when the machine drifts during a run.
//
// HIMORIME_CALIBRATION=full raises the trial count; any non-empty value logs the
// table of verdict rates.

const (
	// calibrationSampleBudget keeps go test ./internal/stats fast: a scenario
	// runs about this many samples per side over all its trials, at least
	// calibrationMinTrials trials. A bootstrap costs roughly linear time in n,
	// so every scenario costs about the same.
	calibrationSampleBudget = 200
	calibrationMinTrials    = 3
	// calibrationFullTrials is the trial count of every scenario with
	// HIMORIME_CALIBRATION=full.
	calibrationFullTrials = 1000
	// calibrationCenter is the median of an unchanged sample, 100ms in ns.
	calibrationCenter = 100e6
)

// bound is an inclusive range the true rate of a verdict must fall in; the
// zero value checks nothing. A tally of a finite number of trials is judged
// against it with binomial slack, so a bound states a property of the method
// rather than of the few trials the default run affords.
type bound struct {
	set      bool
	min, max float64
}

func atMost(v float64) bound  { return bound{set: true, min: 0, max: v} }
func atLeast(v float64) bound { return bound{set: true, min: v, max: 1} }

var never = atMost(0)

// boundSlack is the probability that a tally of a method whose true rate sits
// exactly on the bound still falls outside it.
const boundSlack = 0.01

// holds reports whether count verdicts out of trials are consistent with a
// true rate inside the bound: count may exceed trials*max by what a binomial
// with rate max exceeds with probability boundSlack, and likewise below min.
func (b bound) holds(count, trials int) bool {
	if !b.set {
		return true
	}
	// Upper: the smallest k with P(X > k) <= boundSlack for X ~ Bin(trials, max).
	upper := trials
	for k := 0; k <= trials; k++ {
		if 1-binomialCDF(k, trials, b.max) <= boundSlack {
			upper = k
			break
		}
	}
	// Lower: the largest k with P(X < k) <= boundSlack for X ~ Bin(trials, min).
	lower := 0
	for k := trials; k >= 0; k-- {
		if k == 0 || binomialCDF(k-1, trials, b.min) <= boundSlack {
			lower = k
			break
		}
	}
	return count >= lower && count <= upper
}

// binomialCDF returns P(X <= k) for X ~ Bin(n, p).
func binomialCDF(k, n int, p float64) float64 {
	switch {
	case k < 0:
		return 0
	case k >= n || p <= 0:
		return 1
	case p >= 1:
		return 0
	}
	var sum float64
	for i := 0; i <= k; i++ {
		ln, _ := math.Lgamma(float64(n + 1))
		li, _ := math.Lgamma(float64(i + 1))
		lni, _ := math.Lgamma(float64(n - i + 1))
		sum += math.Exp(ln - li - lni + float64(i)*math.Log(p) + float64(n-i)*math.Log1p(-p))
	}
	return min(sum, 1)
}

func (b bound) String() string {
	return fmt.Sprintf("[%.2f, %.2f]", b.min, b.max)
}

// rates is the share of trials that reached each verdict.
type rates struct {
	trials                                   int
	regression, pass, improved, inconclusive float64
	// noisy is the share of trials that were inconclusive because a side
	// exceeded MaxCV.
	noisy float64
}

// tally counts verdicts over trials.
type tally struct {
	counts map[Verdict]int
	noisy  int
	trials int
}

func (t *tally) add(c Comparison) {
	if t.counts == nil {
		t.counts = map[Verdict]int{}
	}
	t.counts[c.Verdict]++
	if c.Reason == ReasonNoisy {
		t.noisy++
	}
	t.trials++
}

func (t *tally) rates() rates {
	f := func(n int) float64 { return float64(n) / float64(t.trials) }
	return rates{
		trials:       t.trials,
		regression:   f(t.counts[VerdictRegression]),
		pass:         f(t.counts[VerdictPass]),
		improved:     f(t.counts[VerdictImproved]),
		inconclusive: f(t.counts[VerdictInconclusive]),
		noisy:        f(t.noisy),
	}
}

// expect holds the bounds of each verdict rate for one variant.
type expect struct {
	regression, pass, improved, inconclusive bound
}

func (e expect) check(r rates) []string {
	var bad []string
	for _, c := range []struct {
		name string
		b    bound
		rate float64
	}{
		{"regression", e.regression, r.regression},
		{"pass", e.pass, r.pass},
		{"improved", e.improved, r.improved},
		{"inconclusive", e.inconclusive, r.inconclusive},
	} {
		if !c.b.holds(int(math.Round(c.rate*float64(r.trials))), r.trials) {
			bad = append(bad, fmt.Sprintf("%s rate %.3f is not consistent with a true rate in %s", c.name, c.rate, c.b))
		}
	}
	return bad
}

// noise returns a multiplicative noise factor with median 1.
type noise func(r *rand.Rand, cv float64) float64

// lognormal draws exp(sigma*Z) with sigma chosen so the coefficient of
// variation is cv. Its median is exactly 1, so a scaled side has a known
// true median.
func lognormal(r *rand.Rand, cv float64) float64 {
	sigma := math.Sqrt(math.Log1p(cv * cv))
	return math.Exp(sigma * r.NormFloat64())
}

// bimodal is lognormal noise where 30% of the samples land in a slower mode
// 25% above the fast one, like a cache that is sometimes cold.
func bimodal(r *rand.Rand, cv float64) float64 {
	f := lognormal(r, cv)
	if r.Float64() < 0.3 {
		f *= 1.25
	}
	return f
}

// linearDrift slows both sides by up to frac from the first round to the
// last, like a machine heating up.
func linearDrift(frac float64) func(i, n int) float64 {
	return func(i, n int) float64 { return 1 + frac*float64(i)/float64(n-1) }
}

// stepDrift slows both sides by frac from the middle round on, like another
// job starting on a shared runner.
func stepDrift(frac float64) func(i, n int) float64 {
	return func(i, n int) float64 {
		if i >= n/2 {
			return 1 + frac
		}
		return 1
	}
}

// calibrationScenario is one synthetic population and the verdict rates it
// must produce.
type calibrationScenario struct {
	name string
	n    int
	cv   float64
	// change is the true relative change of head's median in percent.
	change         float64
	higherIsBetter bool
	noise          noise
	// drift is a slowdown factor shared by base and head at the same round
	// index; nil means none.
	drift func(i, n int) float64
	// baseOutliers and headOutliers are counts of samples made 3-10x slower.
	baseOutliers, headOutliers int
	// noCVGate disables MaxCV to show the bootstrap alone on outliers.
	noCVGate bool
	// independent bounds Compare; paired bounds pairedCompare and defaults to
	// independent.
	independent, paired expect
	// pairedNoLessDecisive requires pairedCompare to be inconclusive no more
	// often than Compare.
	pairedNoLessDecisive bool
}

// generate draws base and head for one trial.
func (s calibrationScenario) generate(r *rand.Rand) (base, head []float64) {
	center := calibrationCenter
	if s.higherIsBetter {
		center = 1000 // work per second
	}
	side := func(scale float64, outliers int) []float64 {
		out := make([]float64, s.n)
		for i := range out {
			f := s.noise(r, s.cv)
			if s.drift != nil {
				d := s.drift(i, s.n)
				if s.higherIsBetter {
					d = 1 / d // a slower machine does less work per second
				}
				f *= d
			}
			out[i] = center * scale * f
		}
		for _, i := range r.Perm(s.n)[:outliers] {
			k := 3 + 7*r.Float64()
			if s.higherIsBetter {
				k = 1 / k
			}
			out[i] *= k
		}
		return out
	}
	base = side(1, s.baseOutliers)
	head = side(1+s.change/100, s.headOutliers)
	return base, head
}

// calibrationOptions are the regression defaults of a himorime config.
func calibrationOptions(s calibrationScenario, seed uint64) CompareOptions {
	return CompareOptions{
		Metric: Median, HigherIsBetter: s.higherIsBetter,
		MaxPercent: 10, Confidence: 0.95, MinSamples: 10, MaxCV: maxCV(s.noCVGate),
		Seed: seed,
	}
}

func maxCV(off bool) float64 {
	if off {
		return 0
	}
	return 0.5
}

func nameSeed(name string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	return h.Sum64()
}

// run evaluates both variants over trials independent trials.
func (s calibrationScenario) run(trials int) (independent, paired rates) {
	var ind, pair tally
	key := nameSeed(s.name)
	for trial := range uint64(trials) {
		r := rand.New(rand.NewPCG(key, trial))
		base, head := s.generate(r)
		o := calibrationOptions(s, key^(trial*0x9e3779b97f4a7c15))
		ind.add(Compare(base, head, o))
		pair.add(pairedCompare(base, head, o))
	}
	return ind.rates(), pair.rates()
}

// pairedCompare mirrors Compare with one difference: each resample draws
// round indices and takes base[i] and head[i] together, so drift shared by
// both sides at the same round cancels out of the change instead of widening
// it. base and head must have the same length. It exists to evaluate the
// independence assumption of Compare and is not used by himorime.
func pairedCompare(base, head []float64, o CompareOptions) Comparison {
	c := Comparison{Base: Summarize(base), Head: Summarize(head)}
	if c.Base.Count == 0 || c.Head.Count == 0 {
		c.Verdict, c.Reason = VerdictInconclusive, ReasonNoSamples
		return c
	}
	bv, hv := c.Base.Value(o.Metric), c.Head.Value(o.Metric)
	c.BaseValue, c.HeadValue, c.Difference = bv, hv, hv-bv
	if bv <= 0 {
		c.Verdict, c.Reason = VerdictInconclusive, ReasonZeroBase
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
	changes := pairedChanges(base, head, o.Metric, resamples, o.Seed)
	alpha := (1 - o.Confidence) / 2
	c.Low, c.High = percentile(changes, alpha), percentile(changes, 1-alpha)
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
		c.Verdict, c.Reason = VerdictInconclusive, ReasonFewSamples
	case o.MaxCV > 0 && (c.Base.CV > o.MaxCV || c.Head.CV > o.MaxCV):
		c.Verdict, c.Reason = VerdictInconclusive, ReasonNoisy
	case o.MinDifference > 0 && math.Abs(c.Difference) < o.MinDifference:
		c.Verdict, c.Reason = VerdictPass, ReasonBelowMinDiff
	case degradation > o.MaxPercent+epsilon && c.ProbRegression >= o.Confidence:
		c.Verdict = VerdictRegression
	case degradation < -o.MaxPercent-epsilon && c.ProbImprovement >= o.Confidence:
		c.Verdict = VerdictImproved
	case 1-c.ProbRegression >= o.Confidence:
		c.Verdict = VerdictPass
	default:
		c.Verdict, c.Reason = VerdictInconclusive, ReasonTooClose
	}
	return c
}

// pairedChanges is bootstrapChanges with one index drawn per round for both
// sides.
func pairedChanges(base, head []float64, m Metric, resamples int, seed uint64) []float64 {
	rng := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
	n := len(base)
	bbuf := make([]float64, n)
	hbuf := make([]float64, n)
	out := make([]float64, 0, resamples)
	for range resamples {
		for i := range n {
			k := rng.IntN(n)
			bbuf[i], hbuf[i] = base[k], head[k]
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

func TestPairedCompareMatchesCompareWithoutNoise(t *testing.T) {
	t.Parallel()
	// Without noise every resample of either variant equals the observed
	// change, so both must agree exactly, including at the tolerance.
	base := ms(100, 100, 100, 100, 100, 100, 100, 100, 100, 100)
	for _, head := range [][]float64{
		ms(100, 100, 100, 100, 100, 100, 100, 100, 100, 100),
		ms(110, 110, 110, 110, 110, 110, 110, 110, 110, 110),
		ms(111, 111, 111, 111, 111, 111, 111, 111, 111, 111),
		ms(89, 89, 89, 89, 89, 89, 89, 89, 89, 89),
	} {
		o := defaultOptions()
		want, got := Compare(base, head, o), pairedCompare(base, head, o)
		if got != want {
			t.Fatalf("pairedCompare = %+v, Compare = %+v", got, want)
		}
	}
}

// calibrationScenarios lists the populations. Bounds are verdict rates over
// the trials and hold for both the default and the full trial count.
//
// The bounds state what the method promises rather than what it happens to
// reach: an unchanged benchmark rarely fails (at most 5%, the complement of
// Confidence, although the tolerance margin keeps the observed rate far
// lower), a change well beyond the tolerance is found in nearly all trials,
// and no trial is confidently wrong in the opposite direction.
func calibrationScenarios() []calibrationScenario {
	var (
		out     []calibrationScenario
		rarely  = atMost(0.05)
		mostly  = atLeast(0.95)
		quiet   = expect{regression: rarely, improved: rarely}
		slower  = expect{regression: mostly, improved: never}
		faster  = expect{regression: never, improved: mostly}
		cvGated = expect{regression: never, improved: never, pass: never, inconclusive: atLeast(1)}
	)
	add := func(s calibrationScenario) {
		if s.noise == nil {
			s.noise = lognormal
		}
		if s.paired == (expect{}) {
			s.paired = s.independent
		}
		out = append(out, s)
	}

	// Identical distributions: no false alarm in either direction. Low noise
	// with enough samples must also pass, not merely avoid failing.
	for _, n := range []int{10, 30, 100} {
		for _, cv := range []float64{0.02, 0.10, 0.25} {
			e := quiet
			if cv == 0.02 || (cv == 0.10 && n == 100) {
				e.pass = mostly
			}
			add(calibrationScenario{name: fmt.Sprintf("identical n=%d cv=%.0f%%", n, cv*100), n: n, cv: cv, independent: e})
		}
	}

	// Degradations far beyond the 10% tolerance.
	for _, n := range []int{10, 30} {
		for _, change := range []float64{25, 50} {
			add(calibrationScenario{name: fmt.Sprintf("slower %+.0f%% n=%d cv=2%%", change, n), n: n, cv: 0.02, change: change, independent: slower})
		}
	}
	add(calibrationScenario{name: "slower +25% n=30 cv=10%", n: 30, cv: 0.10, change: 25, independent: slower})
	add(calibrationScenario{name: "slower +50% n=30 cv=10%", n: 30, cv: 0.10, change: 50, independent: slower})

	// Near the tolerance the verdict may split between pass, inconclusive and
	// regression, but a change below it rarely fails, a change above it is
	// rarely passed, and none is called an improvement. At exactly the
	// tolerance a regression is a one-sided error bounded by 1-Confidence.
	add(calibrationScenario{name: "near +8% n=30 cv=5%", n: 30, cv: 0.05, change: 8,
		independent: expect{regression: rarely, improved: never}})
	add(calibrationScenario{name: "near +10% n=30 cv=5%", n: 30, cv: 0.05, change: 10,
		independent: expect{regression: atMost(0.10), improved: never}})
	add(calibrationScenario{name: "near +12% n=30 cv=5%", n: 30, cv: 0.05, change: 12,
		independent: expect{pass: rarely, improved: never}})
	add(calibrationScenario{name: "near +12% n=100 cv=5%", n: 100, cv: 0.05, change: 12,
		independent: expect{regression: atLeast(0.3), pass: rarely, improved: never}})

	// Improvements are never regressions.
	add(calibrationScenario{name: "faster -25% n=30 cv=10%", n: 30, cv: 0.10, change: -25, independent: faster})
	add(calibrationScenario{name: "faster -12% n=30 cv=5%", n: 30, cv: 0.05, change: -12,
		independent: expect{regression: never}})

	// A higher-is-better metric degrades when it drops.
	add(calibrationScenario{name: "throughput -20% n=30 cv=5%", n: 30, cv: 0.05, change: -20, higherIsBetter: true, independent: slower})
	add(calibrationScenario{name: "throughput +20% n=30 cv=5%", n: 30, cv: 0.05, change: 20, higherIsBetter: true, independent: faster})
	add(calibrationScenario{name: "throughput identical n=30 cv=10%", n: 30, cv: 0.10, higherIsBetter: true, independent: quiet})

	// Outliers 3-10x slower push the coefficient of variation past MaxCV,
	// so the comparison is inconclusive, even with a real change underneath.
	// Without the gate the median ignores them.
	add(calibrationScenario{name: "outliers head 1/30 cv=5%", n: 30, cv: 0.05, headOutliers: 1,
		independent: expect{regression: never, improved: never}})
	add(calibrationScenario{name: "outliers head 3/30 cv=5%", n: 30, cv: 0.05, headOutliers: 3, independent: cvGated})
	add(calibrationScenario{name: "outliers both 3/30 cv=5%", n: 30, cv: 0.05, baseOutliers: 3, headOutliers: 3, independent: cvGated})
	add(calibrationScenario{name: "outliers head 2/10 cv=5%", n: 10, cv: 0.05, headOutliers: 2, independent: cvGated})
	add(calibrationScenario{name: "outliers base 3/30 +20% cv=5%", n: 30, cv: 0.05, change: 20, baseOutliers: 3, independent: cvGated})
	add(calibrationScenario{name: "outliers head 3/30 no max_cv", n: 30, cv: 0.05, headOutliers: 3, noCVGate: true,
		independent: expect{regression: rarely, improved: never, pass: mostly}})
	add(calibrationScenario{name: "outliers base 3/30 +20% no max_cv", n: 30, cv: 0.05, change: 20, baseOutliers: 3, noCVGate: true,
		independent: expect{regression: atLeast(0.5), pass: never, improved: never}})

	// Bimodal noise: 30% of samples in a mode 25% slower.
	add(calibrationScenario{name: "bimodal identical n=30 cv=5%", n: 30, cv: 0.05, noise: bimodal, independent: quiet})
	add(calibrationScenario{name: "bimodal +25% n=30 cv=5%", n: 30, cv: 0.05, change: 25, noise: bimodal,
		independent: expect{regression: atLeast(0.5), pass: rarely, improved: never}})

	// Drift shared by both sides at the same round. Compare treats the sides
	// as independent, so shared drift widens its interval: it may become
	// inconclusive but must not raise a false alarm or pass a real +20%.
	// pairedCompare cancels the drift and must stay at least as decisive.
	for _, d := range []struct {
		name  string
		drift func(i, n int) float64
		sizes []int
	}{
		{"linear 30%", linearDrift(0.3), []int{10, 30}},
		{"step 30%", stepDrift(0.3), []int{10, 30, 100}},
		{"step 10%", stepDrift(0.1), []int{10, 30}},
	} {
		for _, n := range d.sizes {
			add(calibrationScenario{name: fmt.Sprintf("%s drift identical n=%d", d.name, n), n: n, cv: 0.05, drift: d.drift,
				independent: expect{regression: rarely, improved: rarely}, pairedNoLessDecisive: true})
		}
		for _, n := range []int{10, 30} {
			add(calibrationScenario{name: fmt.Sprintf("%s drift +20%% n=%d", d.name, n), n: n, cv: 0.05, change: 20, drift: d.drift,
				independent:          expect{pass: never, improved: never},
				paired:               expect{regression: atLeast(0.6), pass: never, improved: never},
				pairedNoLessDecisive: true})
		}
		add(calibrationScenario{name: fmt.Sprintf("%s drift +8%% n=30", d.name), n: 30, cv: 0.05, change: 8, drift: d.drift,
			independent: expect{regression: rarely, improved: never}, pairedNoLessDecisive: true})
	}
	return out
}

func TestCalibration(t *testing.T) {
	t.Parallel()
	mode := os.Getenv("HIMORIME_CALIBRATION")
	scenarios := calibrationScenarios()
	type result struct{ ind, pair rates }
	results := make([]result, len(scenarios))
	if mode != "" {
		t.Cleanup(func() {
			t.Log("\n" + calibrationTable(scenarios, func(i int) (rates, rates) { return results[i].ind, results[i].pair }))
		})
	}
	for i, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			trials := max(calibrationSampleBudget/s.n, calibrationMinTrials)
			if mode == "full" {
				trials = calibrationFullTrials
			}
			ind, pair := s.run(trials)
			results[i] = result{ind, pair}
			for _, msg := range s.independent.check(ind) {
				t.Errorf("Compare: %s over %d trials", msg, trials)
			}
			for _, msg := range s.paired.check(pair) {
				t.Errorf("pairedCompare: %s over %d trials", msg, trials)
			}
			if s.pairedNoLessDecisive && pair.inconclusive > ind.inconclusive {
				t.Errorf("pairedCompare is inconclusive in %.3f of %d trials, Compare in %.3f", pair.inconclusive, trials, ind.inconclusive)
			}
		})
	}
}

// calibrationTable renders the verdict rates of both variants.
func calibrationTable(scenarios []calibrationScenario, get func(int) (rates, rates)) string {
	var b strings.Builder
	b.WriteString("rates in percent of trials; change is the true change of the metric; (cv N) is the share inconclusive by max_cv\n")
	fmt.Fprintf(&b, "%-36s %4s %4s %7s %6s | %-29s | %-29s\n", "scenario", "n", "cv", "change", "trials",
		"independent reg/pass/imp/inc", "paired reg/pass/imp/inc")
	for i, s := range scenarios {
		ind, pair := get(i)
		fmt.Fprintf(&b, "%-36s %4d %3.0f%% %+6.0f%% %6d | %s | %s\n", s.name, s.n, s.cv*100, s.change, ind.trials,
			formatRates(ind), formatRates(pair))
	}
	return b.String()
}

func formatRates(r rates) string {
	return fmt.Sprintf("%5.1f %5.1f %5.1f %5.1f (cv%3.0f)", r.regression*100, r.pass*100, r.improved*100, r.inconclusive*100, r.noisy*100)
}
