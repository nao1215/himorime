package metric

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The patterns below are the only spellings a suite accepts for a quantity.
// schema/himorime.schema.json repeats every one of them, and a test keeps the
// two identical, so an editor validating against the schema and himorime
// itself agree on every value.
const (
	// DurationPattern is a duration such as 250ms or 1m30s: no sign, no bare
	// number, every component with a unit.
	DurationPattern = `^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$`
	// BytesPattern is a byte size such as 64MiB or 512KB.
	BytesPattern = `^[0-9]+(\.[0-9]+)?\s*(B|bytes|KB|MB|GB|TB|KiB|MiB|GiB|TiB)$`
	// RatePattern is a rate such as 50MiB/s or 1000 records/s.
	RatePattern = `^[0-9]+(\.[0-9]+)?\s*[A-Za-z][A-Za-z0-9_-]*/s$`

	// DurationBudgetPattern is an upper bound on a duration, such as "< 20ms".
	DurationBudgetPattern = `^\s*(<=|<)\s*(([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+)\s*$`
	// BytesBudgetPattern is an upper bound on a byte size, such as "<= 64MiB".
	BytesBudgetPattern = `^\s*(<=|<)\s*([0-9]+(\.[0-9]+)?)\s*(B|bytes|KB|MB|GB|TB|KiB|MiB|GiB|TiB)\s*$`
	// RateBudgetPattern is a lower bound on a rate, such as ">= 50MiB/s".
	RateBudgetPattern = `^\s*(>=|>)\s*([0-9]+(\.[0-9]+)?)\s*([A-Za-z][A-Za-z0-9_-]*)/s\s*$`
	// PercentBudgetPattern is a bound on a percentage in either direction,
	// such as "<= 150%".
	PercentBudgetPattern = `^\s*(<=|<|>=|>)\s*([0-9]+(\.[0-9]+)?)\s*%\s*$`
)

var (
	durationRE        = regexp.MustCompile(DurationPattern)
	bytesRE           = regexp.MustCompile(`^([0-9]+(\.[0-9]+)?)\s*(B|bytes|KB|MB|GB|TB|KiB|MiB|GiB|TiB)$`)
	rateRE            = regexp.MustCompile(`^([0-9]+(\.[0-9]+)?)\s*([A-Za-z][A-Za-z0-9_-]*)/s$`)
	durationBudgetRE  = regexp.MustCompile(DurationBudgetPattern)
	bytesBudgetRE     = regexp.MustCompile(BytesBudgetPattern)
	rateBudgetRE      = regexp.MustCompile(RateBudgetPattern)
	percentBudgetRE   = regexp.MustCompile(PercentBudgetPattern)
	percentileKeyRE   = regexp.MustCompile(PercentilePattern)
	byteMultiplierMap = map[string]float64{
		"B": 1, "bytes": 1,
		"KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12,
		"KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40,
	}
)

// WorkUnitPattern is a unit of declared work, such as records or bytes.
const WorkUnitPattern = `^[A-Za-z][A-Za-z0-9_-]*$`

// BytesUnit is the work unit whose rates are shown as KiB/s, MiB/s and so on.
const BytesUnit = "bytes"

// MaxDuration bounds every duration a suite may declare. A day is far beyond
// any sensible benchmark and keeps sums of samples clear of overflow.
const MaxDuration = 24 * time.Hour

// ParseDuration parses a suite duration such as "250ms" or "1m30s".
func ParseDuration(s string) (time.Duration, error) {
	if !durationRE.MatchString(s) {
		return 0, fmt.Errorf("invalid duration %q: write a number with a unit, such as 500ms, 2s or 1m30s", s)
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	if d > MaxDuration {
		return 0, fmt.Errorf("duration %q exceeds the maximum of 24h", s)
	}
	return d, nil
}

// ParseBytes parses a byte size such as "64MiB" into bytes. KB, MB, GB and
// TB are powers of 1000; KiB, MiB, GiB and TiB powers of 1024.
func ParseBytes(s string) (float64, error) {
	m := bytesRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, fmt.Errorf("invalid byte size %q: write a number with a unit, such as 64MiB or 512KB (units: B, KB, MB, GB, TB, KiB, MiB, GiB, TiB)", s)
	}
	v, err := finiteFloat(m[1])
	if err != nil {
		return 0, fmt.Errorf("invalid byte size %q: %w", s, err)
	}
	v *= byteMultiplierMap[m[3]]
	if math.IsInf(v, 0) {
		return 0, fmt.Errorf("invalid byte size %q: the value is too large", s)
	}
	return v, nil
}

// IsByteUnit reports whether a unit is one of the byte size units.
func IsByteUnit(u string) bool {
	_, ok := byteMultiplierMap[u]
	return ok
}

// ParseRate parses a rate such as "50MiB/s" or "1000 records/s". A byte unit
// is converted to bytes per second and reported as the unit "bytes"; any
// other unit is returned as written.
func ParseRate(s string) (value float64, unit string, err error) {
	m := rateRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, "", fmt.Errorf("invalid rate %q: write a number, a unit and /s, such as 50MiB/s or \"1000 records/s\"", s)
	}
	return rateValue(s, m[1], m[3])
}

func rateValue(raw, number, unit string) (float64, string, error) {
	v, err := finiteFloat(number)
	if err != nil {
		return 0, "", fmt.Errorf("invalid rate %q: %w", raw, err)
	}
	if mult, ok := byteMultiplierMap[unit]; ok {
		v *= mult
		if math.IsInf(v, 0) {
			return 0, "", fmt.Errorf("invalid rate %q: the value is too large", raw)
		}
		return v, BytesUnit, nil
	}
	return v, unit, nil
}

func finiteFloat(s string) (float64, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("%q is not a finite number", s)
	}
	return v, nil
}

// FormatDuration renders nanoseconds for humans with two decimals in the
// largest unit that keeps the value at or above one: 850ns, 12.34µs, 1.82ms,
// 3.40s. Machine-readable reports carry the unrounded value instead.
func FormatDuration(ns float64) string {
	abs := math.Abs(ns)
	switch {
	case abs < float64(time.Microsecond):
		return fmt.Sprintf("%.0fns", ns)
	case abs < float64(time.Millisecond):
		return fmt.Sprintf("%.2fµs", ns/1e3)
	case abs < float64(time.Second):
		return fmt.Sprintf("%.2fms", ns/1e6)
	default:
		return fmt.Sprintf("%.2fs", ns/1e9)
	}
}

// FormatBytes renders a byte size with binary units: 512B, 1.50KiB, 64.00MiB.
func FormatBytes(b float64) string {
	abs := math.Abs(b)
	units := []struct {
		name string
		size float64
	}{{"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}}
	for _, u := range units {
		if abs >= u.size {
			return fmt.Sprintf("%.2f%s", b/u.size, u.name)
		}
	}
	return fmt.Sprintf("%.0fB", b)
}

// FormatRate renders a rate of work: bytes as MiB/s and friends, anything
// else as a number with a k, M or G prefix and the unit, such as
// "12.35k records/s".
func FormatRate(v float64, unit string) string {
	if unit == BytesUnit {
		return FormatBytes(v) + "/s"
	}
	if unit == "" {
		unit = "operations"
	}
	abs := math.Abs(v)
	switch {
	case abs >= 1e9:
		return fmt.Sprintf("%.2fG %s/s", v/1e9, unit)
	case abs >= 1e6:
		return fmt.Sprintf("%.2fM %s/s", v/1e6, unit)
	case abs >= 1e4:
		return fmt.Sprintf("%.2fk %s/s", v/1e3, unit)
	default:
		return fmt.Sprintf("%.2f %s/s", v, unit)
	}
}

// FormatPercent renders a percentage with one decimal, such as 187.3%.
func FormatPercent(p float64) string {
	return fmt.Sprintf("%.1f%%", p)
}

// Format renders a value of the given kind for humans. workUnit only matters
// for rates. NaN and infinities render as "-".
func Format(k Kind, v float64, workUnit string) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "-"
	}
	switch k {
	case KindDuration:
		return FormatDuration(v)
	case KindBytes:
		return FormatBytes(v)
	case KindRate:
		return FormatRate(v, workUnit)
	case KindPercent:
		return FormatPercent(v)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// ParseQuantity parses a plain quantity of a kind, without an operator: a
// duration, a byte size or a rate. It is used for regression thresholds such
// as min_difference. unit is the work unit of a rate.
func ParseQuantity(k Kind, s string) (value float64, unit string, err error) {
	switch k {
	case KindDuration:
		d, err := ParseDuration(strings.TrimSpace(s))
		return float64(d), "", err
	case KindBytes:
		v, err := ParseBytes(s)
		return v, "", err
	case KindRate:
		return ParseRate(s)
	case KindPercent:
		return 0, "", fmt.Errorf("a percentage has no absolute quantity form")
	}
	return 0, "", fmt.Errorf("unknown kind %v", k)
}
