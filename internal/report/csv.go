package report

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
)

// CSVHeader is the stable column list of the CSV report.
var CSVHeader = []string{
	"suite", "benchmark", "command", "side", "result",
	"count", "mean_ns", "median_ns", "stddev_ns", "min_ns", "max_ns", "cv",
	"vs_baseline", "vs_fastest", "change_percent", "ci_low_percent", "ci_high_percent",
	"probability_regression", "error_kind", "error_message",
}

// WriteCSV renders one row per command per side. encoding/csv quotes fields
// holding commas, quotes or line breaks.
func WriteCSV(w io.Writer, r *Report) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(CSVHeader); err != nil {
		return err
	}
	for _, s := range r.Suites {
		for _, row := range suiteRows(r.Mode, s) {
			if err := cw.Write(row); err != nil {
				return err
			}
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("write csv: %w", err)
	}
	return nil
}

func suiteRows(mode Mode, s Suite) [][]string {
	if s.Error != nil {
		return [][]string{csvRow(s.Name, "", "", "", ResultError, nil, s.Error)}
	}
	var rows [][]string
	for _, b := range s.Benchmarks {
		if len(b.Commands) == 0 {
			rows = append(rows, csvRow(s.Name, b.Name, "", "", ResultError, nil, b.Error))
			continue
		}
		for _, c := range b.Commands {
			rows = append(rows, commandRows(mode, s, b, c)...)
		}
	}
	return rows
}

type sideMeasurement struct {
	name string
	m    *Measurement
}

func commandRows(mode Mode, s Suite, b Benchmark, c Command) [][]string {
	sides := []sideMeasurement{{"head", c.Head}}
	if mode == ModeCompare {
		sides = []sideMeasurement{{"base", c.Base}, {"head", c.Head}}
	}
	rows := make([][]string, 0, len(sides))
	for _, side := range sides {
		row := csvRow(s.Name, b.Name, c.Name, side.name, c.Result, side.m, nil)
		switch {
		case side.m != nil && side.m.Error != nil:
			row[18], row[19] = side.m.Error.Kind, side.m.Error.Message
		case b.Error != nil:
			row[18], row[19] = b.Error.Kind, b.Error.Message
		}
		if c.Relative != nil {
			row[12] = ratioCell(c.Relative.VsBaseline)
			row[13] = ratioCell(c.Relative.VsFastest)
		}
		if c.Comparison != nil && side.name == "head" {
			row[14] = floatCell(c.Comparison.ChangePercent)
			row[15] = floatCell(c.Comparison.CILowPercent)
			row[16] = floatCell(c.Comparison.CIHighPercent)
			row[17] = floatCell(c.Comparison.ProbRegression)
		}
		rows = append(rows, row)
	}
	return rows
}

func csvRow(suite, bench, command, side string, result Result, m *Measurement, e *Error) []string {
	row := make([]string, len(CSVHeader))
	row[0], row[1], row[2], row[3], row[4] = suite, bench, command, side, string(result)
	if m != nil {
		row[5] = strconv.Itoa(m.Count)
		row[6] = strconv.FormatInt(m.MeanNS, 10)
		row[7] = strconv.FormatInt(m.MedianNS, 10)
		row[8] = strconv.FormatInt(m.StddevNS, 10)
		row[9] = strconv.FormatInt(m.MinNS, 10)
		row[10] = strconv.FormatInt(m.MaxNS, 10)
		row[11] = floatCell(m.CV)
	}
	if e != nil {
		row[18], row[19] = e.Kind, e.Message
	}
	return row
}

func ratioCell(v *float64) string {
	if v == nil {
		return ""
	}
	return floatCell(*v)
}

func floatCell(v float64) string {
	return strconv.FormatFloat(v, 'f', 6, 64)
}
