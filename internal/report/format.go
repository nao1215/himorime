package report

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/nao1215/himorime/internal/metric"
)

// FormatDuration renders nanoseconds for humans with two decimals in the
// largest unit that keeps the value at or above one: 850ns, 12.34µs, 1.82ms,
// 3.40s. JSON reports carry the unrounded integer instead.
func FormatDuration(ns int64) string {
	return metric.FormatDuration(float64(ns))
}

// FormatRatio renders a speed ratio such as 15.61x.
func FormatRatio(r float64) string {
	if math.IsNaN(r) || math.IsInf(r, 0) {
		return "-"
	}
	return fmt.Sprintf("%.2fx", r)
}

// FormatChange renders a signed percentage such as +2.7% or -12.0%.
func FormatChange(p float64) string {
	if math.IsNaN(p) || math.IsInf(p, 0) {
		return "-"
	}
	if math.Abs(p) < 0.05 {
		return "+0.0%"
	}
	return fmt.Sprintf("%+.1f%%", p)
}

func trimFloat(f float64) string {
	s := fmt.Sprintf("%.2f", f)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// table renders aligned columns. Text columns are left-aligned and numeric
// columns right-aligned; widths count runes so "µs" aligns.
type table struct {
	header []string
	right  []bool
	rows   [][]string
}

// add appends a row. Control characters are replaced so that a name can
// never break the table into extra lines or move the cursor.
func (t *table) add(cells ...string) {
	for i, c := range cells {
		cells[i] = cellText(c)
	}
	t.rows = append(t.rows, cells)
}

func cellText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

// lines renders the header and every row as aligned lines without newlines.
func (t *table) lines() []string {
	var sb strings.Builder
	t.render(&sb)
	return strings.Split(strings.TrimSuffix(sb.String(), "\n"), "\n")
}

func (t *table) render(sb *strings.Builder) {
	widths := make([]int, len(t.header))
	for i, h := range t.header {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, r := range t.rows {
		for i, c := range r {
			if n := utf8.RuneCountInString(c); n > widths[i] {
				widths[i] = n
			}
		}
	}
	line := func(cells []string) {
		for i, c := range cells {
			pad := strings.Repeat(" ", widths[i]-utf8.RuneCountInString(c))
			if i > 0 {
				sb.WriteString("  ")
			}
			if t.right[i] {
				sb.WriteString(pad + c)
			} else if i == len(cells)-1 {
				sb.WriteString(c)
			} else {
				sb.WriteString(c + pad)
			}
		}
		sb.WriteString("\n")
	}
	line(t.header)
	for _, r := range t.rows {
		line(r)
	}
}

// oneLine collapses whitespace so a message cannot break a table row.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
