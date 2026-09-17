package runner

import (
	"regexp"

	"github.com/nao1215/himorime/internal/config"
)

// Selection chooses which benchmarks run. The zero value selects everything.
type Selection struct {
	// Filter matches benchmark names; nil matches all.
	Filter *regexp.Regexp
	// Tags keeps benchmarks carrying at least one of these tags.
	Tags []string
	// SkipTags drops benchmarks carrying any of these tags. It wins over Tags.
	SkipTags []string
}

// Select returns the benchmarks of s that match the selection, in file order.
func (sel Selection) Select(benchmarks []config.Benchmark) []config.Benchmark {
	var out []config.Benchmark
	for _, b := range benchmarks {
		if sel.Matches(b) {
			out = append(out, b)
		}
	}
	return out
}

// Matches reports whether one benchmark is selected.
func (sel Selection) Matches(b config.Benchmark) bool {
	if sel.Filter != nil && !sel.Filter.MatchString(b.Name) {
		return false
	}
	for _, t := range sel.SkipTags {
		if b.HasTag(t) {
			return false
		}
	}
	if len(sel.Tags) == 0 {
		return true
	}
	for _, t := range sel.Tags {
		if b.HasTag(t) {
			return true
		}
	}
	return false
}
