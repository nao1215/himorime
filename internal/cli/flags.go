package cli

import (
	"errors"
	"flag"
	"strconv"
	"strings"

	"github.com/nao1215/himorime/internal/config"
)

type selectFlags struct {
	filter   string
	tags     stringList
	skipTags stringList
}

func (s *selectFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&s.filter, "filter", "", "select benchmarks whose name matches this `regexp`")
	fs.Var(&s.tags, "tag", "select benchmarks with this `tag` (repeatable, or comma-separated)")
	fs.Var(&s.skipTags, "skip-tag", "skip benchmarks with this `tag` (repeatable, or comma-separated); wins over --tag")
}

type measureFlags struct {
	selectFlags
	format             string
	output             string
	section            string
	summary            string
	seed               uint64
	seedSet            bool
	runs               int
	warmup             int
	warmupSet          bool
	quiet              bool
	noColor            bool
	against            string
	failOnInconclusive bool
}

func formatNames() string {
	names := make([]string, 0, len(config.Formats()))
	for _, f := range config.Formats() {
		names = append(names, string(f))
	}
	return strings.Join(names, ", ")
}

func (m *measureFlags) registerCommon(fs *flag.FlagSet) {
	m.register(fs)
	fs.StringVar(&m.format, "format", string(config.FormatTable), "report `format` written to stdout or --output: "+formatNames())
	fs.StringVar(&m.output, "output", "", "write the report to this `file` instead of stdout")
	fs.StringVar(&m.section, "section", "", "update only the `name` section of the existing --output file, between its himorime:begin and himorime:end comment lines; needs --format markdown")
	fs.StringVar(&m.summary, "summary", "", "also append a GitHub-flavored Markdown summary to this `file`")
	fs.Func("seed", "`seed` for the execution order and the bootstrap (default: random, printed in the report)", func(v string) error {
		n, err := parseSeed(v)
		if err != nil {
			return err
		}
		m.seed, m.seedSet = n, true
		return nil
	})
	fs.Func("runs", "measure exactly `n` runs per command, overriding the suite", func(v string) error {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return errors.New("must be an integer of at least 1")
		}
		m.runs = n
		return nil
	})
	fs.Func("warmup", "run `n` warmup runs per command, overriding the suite", func(v string) error {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return errors.New("must be an integer of at least 0")
		}
		m.warmup, m.warmupSet = n, true
		return nil
	})
	fs.BoolVar(&m.quiet, "quiet", false, "do not print progress to stderr")
	fs.BoolVar(&m.noColor, "no-color", false, "disable colors in the table (also honored: NO_COLOR)")
}

func runFlags(fs *flag.FlagSet) any {
	m := &measureFlags{}
	m.registerCommon(fs)
	return m
}

func compareFlags(fs *flag.FlagSet) any {
	m := &measureFlags{}
	m.registerCommon(fs)
	fs.StringVar(&m.against, "against", "", "the Git `revision` to compare the working tree with (required)")
	fs.BoolVar(&m.failOnInconclusive, "fail-on-inconclusive", false, "exit 1 when a gated comparison is inconclusive")
	return m
}

func ciFlags(fs *flag.FlagSet) any {
	m := &measureFlags{}
	m.registerCommon(fs)
	fs.StringVar(&m.against, "against", "", "the base `revision`; defaults to $HIMORIME_BASE_REF, then the GitHub Actions event")
	fs.BoolVar(&m.failOnInconclusive, "fail-on-inconclusive", false, "exit 1 when a gated comparison is inconclusive")
	return m
}

type listOptions struct {
	selectFlags
	format string
}

func listFlags(fs *flag.FlagSet) any {
	l := &listOptions{}
	l.register(fs)
	fs.StringVar(&l.format, "format", "text", "output format: text or json")
	return l
}

type initOptions struct {
	force bool
}

func initFlags(fs *flag.FlagSet) any {
	o := &initOptions{}
	fs.BoolVar(&o.force, "force", false, "overwrite an existing file")
	return o
}
