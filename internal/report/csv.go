package report

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/nao1215/himorime/internal/metric"
)

// CSVHeader is the stable column list of the CSV report. The report is long
// rather than wide: one row per statistic, budget or comparison of one metric,
// so a spreadsheet can filter and pivot on record, metric and statistic
// without parsing a cell. Columns that do not apply to a row are empty.
var CSVHeader = []string{
	"suite", "benchmark", "command", "result", "record", "side",
	"metric", "unit", "scope", "source", "process_aggregation", "status", "statistic", "value",
	"operator", "limit", "base", "head", "difference",
	"change_percent", "ci_low_percent", "ci_high_percent", "probability_regression", "max_percent",
	"verdict", "gate", "reason", "error_kind", "error_message",
}

// CSV record types.
const (
	recordStat       = "stat"
	recordBudget     = "budget"
	recordComparison = "comparison"
	recordError      = "error"
	// recordNewInHead is the first row of a suite the base revision does
	// not have; reason says so. The rows after it are those of a plain run.
	recordNewInHead = "new_in_head"
)

// csvCol returns the index of a CSVHeader column.
func csvCol(name string) int {
	for i, h := range CSVHeader {
		if h == name {
			return i
		}
	}
	panic("report: unknown CSV column " + name)
}

// statistics lists the statistic rows written for every measured metric.
var statistics = []string{"count", "min", "median", "mean", "max", "stddev", "cv", "robust_cv"}

// WriteCSV renders the long-format CSV report. encoding/csv quotes fields
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

type csvRowBuilder struct {
	row []string
}

func newRow(suite, bench, command string, result Result, record string) *csvRowBuilder {
	b := &csvRowBuilder{row: make([]string, len(CSVHeader))}
	b.set("suite", suite).set("benchmark", bench).set("command", command).set("result", string(result)).set("record", record)
	return b
}

func (b *csvRowBuilder) set(col, value string) *csvRowBuilder {
	b.row[csvCol(col)] = value
	return b
}

func (b *csvRowBuilder) setError(e *Error) *csvRowBuilder {
	if e != nil {
		b.set("error_kind", e.Kind).set("error_message", e.Message)
	}
	return b
}

func suiteRows(mode Mode, s Suite) [][]string {
	var rows [][]string
	if s.NewInHead {
		rows = append(rows, newRow(s.Name, "", "", s.Result, recordNewInHead).set("reason", newInHeadLine(s)).row)
	}
	if s.Error != nil {
		return append(rows, newRow(s.Name, "", "", s.Result, recordError).setError(s.Error).row)
	}
	mode = suiteMode(mode, s)
	for _, b := range s.Benchmarks {
		if len(b.Commands) == 0 {
			rows = append(rows, newRow(s.Name, b.Name, "", b.Result, recordError).setError(b.Error).row)
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
	var rows [][]string
	if b.Error != nil {
		rows = append(rows, newRow(s.Name, b.Name, c.Name, c.Result, recordError).setError(b.Error).row)
	}
	sides := []sideMeasurement{{"head", c.Head}}
	if mode == ModeCompare {
		sides = []sideMeasurement{{"base", c.Base}, {"head", c.Head}}
	}
	for _, side := range sides {
		if side.m == nil {
			continue
		}
		if side.m.Error != nil {
			rows = append(rows, newRow(s.Name, b.Name, c.Name, c.Result, recordError).set("side", side.name).setError(side.m.Error).row)
		}
		for _, def := range metric.Defs() {
			rows = append(rows, statRows(s, b, c, side, def)...)
		}
	}
	if c.Relative != nil {
		for _, rel := range []struct {
			statistic string
			v         *float64
		}{{"relative_to_baseline", c.Relative.VsBaseline}, {"relative_to_fastest", c.Relative.VsFastest}} {
			if rel.v != nil {
				rows = append(rows, newRow(s.Name, b.Name, c.Name, c.Result, recordStat).set("side", "head").set("metric", string(metric.Latency)).
					set("unit", "ratio").set("status", StatusMeasured).set("statistic", rel.statistic).set("value", floatCell(*rel.v)).row)
			}
		}
	}
	for _, bc := range c.Budgets {
		row := newRow(s.Name, b.Name, c.Name, c.Result, recordBudget).
			set("side", "head").set("metric", bc.Metric).set("unit", bc.Unit).set("statistic", bc.Aggregation).
			set("operator", bc.Operator).set("limit", floatCell(bc.Limit)).set("verdict", bc.Status).set("reason", bc.Reason)
		if bc.Actual != nil {
			row.set("value", floatCell(*bc.Actual))
		}
		rows = append(rows, row.row)
	}
	for _, def := range metric.Defs() {
		mc := c.Comparisons[string(def.Name)]
		if mc == nil {
			continue
		}
		row := newRow(s.Name, b.Name, c.Name, c.Result, recordComparison).
			set("metric", mc.Metric).set("unit", mc.Unit).set("statistic", mc.Statistic).
			set("max_percent", floatCell(mc.MaxPercent)).set("verdict", mc.Verdict).set("gate", strconv.FormatBool(mc.Gate)).set("reason", mc.Reason)
		if mc.Verdict != VerdictSkipped {
			row.set("change_percent", floatCell(mc.ChangePercent)).set("ci_low_percent", floatCell(mc.CILowPercent)).
				set("ci_high_percent", floatCell(mc.CIHighPercent)).set("probability_regression", floatCell(mc.ProbRegression))
		}
		for col, v := range map[string]*float64{"base": mc.Base, "head": mc.Head, "difference": mc.Difference} {
			if v != nil {
				row.set(col, floatCell(*v))
			}
		}
		rows = append(rows, row.row)
	}
	return rows
}

// statRows writes one row per statistic of a measured metric, or a single
// row with the status of a metric that was requested but not measured.
func statRows(s Suite, b Benchmark, c Command, side sideMeasurement, def metric.Def) [][]string {
	ms := metricSummary(side.m, def.Name)
	if ms == nil || ms.Status == StatusNotRequested {
		return nil
	}
	base := func() *csvRowBuilder {
		return newRow(s.Name, b.Name, c.Name, c.Result, recordStat).
			set("side", side.name).set("metric", ms.Name).set("unit", ms.Unit).set("status", ms.Status).
			set("scope", ms.Scope).set("source", ms.Source).set("process_aggregation", ms.ProcessAggregation)
	}
	if ms.Stats == nil {
		return [][]string{base().set("reason", ms.Reason).row}
	}
	values := map[string]float64{
		"count": float64(ms.Stats.Count), "min": ms.Stats.Min, "median": ms.Stats.Median, "mean": ms.Stats.Mean,
		"max": ms.Stats.Max, "stddev": ms.Stats.Stddev, "cv": ms.Stats.CV, "robust_cv": ms.Stats.RobustCV,
	}
	var rows [][]string
	for _, st := range statistics {
		rows = append(rows, base().set("statistic", st).set("value", floatCell(values[st])).row)
	}
	percentiles := make([]string, 0, len(ms.Stats.Percentiles))
	for p := range ms.Stats.Percentiles {
		percentiles = append(percentiles, p)
	}
	sort.Slice(percentiles, func(i, j int) bool {
		return metric.Aggregation(percentiles[i]).Less(metric.Aggregation(percentiles[j]))
	})
	for _, p := range percentiles {
		rows = append(rows, base().set("statistic", p).set("value", floatCell(ms.Stats.Percentiles[p])).row)
	}
	if def.Name == metric.PeakRSS {
		rows = append(rows,
			base().set("statistic", "floor").set("value", strconv.FormatInt(ms.Floor, 10)).row,
			base().set("statistic", "samples_at_floor").set("value", strconv.Itoa(ms.SamplesAtFloor)).row)
	}
	if def.Name == metric.Throughput && ms.Work != nil && ms.Work.MeasuredMin != nil {
		rows = append(rows,
			base().set("statistic", "measured_work_min").set("value", floatCell(*ms.Work.MeasuredMin)).row,
			base().set("statistic", "measured_work_max").set("value", floatCell(*ms.Work.MeasuredMax)).row)
	}
	return rows
}

// SamplesCSVHeader is the stable column list of the samples CSV: one row per
// measured value of every run.
var SamplesCSVHeader = []string{"suite", "benchmark", "command", "side", "run", "metric", "unit", "value"}

// WriteSamplesCSV renders every raw sample of every measured metric. run is
// the 1-based index of the measured run, so the metrics of one run can be
// joined on it.
func WriteSamplesCSV(w io.Writer, r *Report) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(SamplesCSVHeader); err != nil {
		return err
	}
	for _, s := range r.Suites {
		for _, b := range s.Benchmarks {
			for _, c := range b.Commands {
				for _, side := range []sideMeasurement{{"base", c.Base}, {"head", c.Head}} {
					if err := writeSideSamples(cw, s, b, c, side); err != nil {
						return err
					}
				}
			}
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("write samples csv: %w", err)
	}
	return nil
}

func writeSideSamples(cw *csv.Writer, s Suite, b Benchmark, c Command, side sideMeasurement) error {
	if side.m == nil {
		return nil
	}
	for _, def := range metric.Defs() {
		ms := metricSummary(side.m, def.Name)
		if ms == nil || ms.Status != StatusMeasured {
			continue
		}
		for i, v := range ms.Samples {
			if err := cw.Write([]string{s.Name, b.Name, c.Name, side.name, strconv.Itoa(i + 1), ms.Name, ms.Unit, floatCell(v)}); err != nil {
				return err
			}
		}
	}
	return nil
}

// floatCell renders a number without losing precision or using an exponent
// a spreadsheet might misread.
func floatCell(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
