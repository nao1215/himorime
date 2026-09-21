// Command gen writes the input files the third-party benchmark suites measure
// over, so that no sample data from anyone else's project is committed here.
//
// The same seed and the same size always produce the same bytes, on every
// operating system and every Go version that keeps math/rand/v2 stable, so a
// scheduled run compares this week's numbers with last week's over the same
// input.
//
//	gen -kind jsonl -size 5MiB -seed 1 -o out.jsonl
//	gen -kind tree -size 5MiB -seed 1 -o dir
//	gen -kind csv -size 5MiB -seed 1 -o out.csv
//	gen -kind yaml -size 5MiB -seed 1 -o out.yaml
//
// Add a shape here rather than writing a second generator for the next
// category.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "gen: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)
	kind := fs.String("kind", "jsonl", "shape of the generated input: jsonl, csv, yaml, or tree for a directory of log files")
	size := fs.String("size", "1MiB", "size of the output in bytes, with an optional KiB, MiB or GiB suffix")
	seed := fs.Uint64("seed", 1, "seed of the random source; the same seed produces the same bytes")
	out := fs.String("o", "", "path of the file to write, or of the directory to create for tree")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("-o is required")
	}
	n, err := parseSize(*size)
	if err != nil {
		return err
	}
	switch *kind {
	case "jsonl":
		return writeFile(*out, func(w *bufio.Writer) error { return writeJSONL(w, n, *seed) })
	case "csv":
		return writeFile(*out, func(w *bufio.Writer) error { return writeCSV(w, n, *seed) })
	case "yaml":
		return writeFile(*out, func(w *bufio.Writer) error { return writeYAML(w, n, *seed) })
	case "tree":
		return writeTree(*out, n, *seed)
	default:
		return fmt.Errorf("unknown -kind %q; the shapes are: jsonl, csv, yaml, tree", *kind)
	}
}

func writeFile(path string, write func(*bufio.Writer) error) error {
	f, err := os.Create(path) //nolint:gosec // G304: the path is the -o flag, or a file under it, given by the suite that runs this generator
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 1<<20)
	if err := write(w); err != nil {
		_ = f.Close()
		return err
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// parseSize reads a byte count such as 512, 64KiB, 5MiB or 2GiB.
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	mult := int64(1)
	for _, suffix := range []struct {
		name string
		mult int64
	}{
		{"GiB", 1 << 30},
		{"MiB", 1 << 20},
		{"KiB", 1 << 10},
	} {
		if strings.HasSuffix(s, suffix.name) {
			s = strings.TrimSuffix(s, suffix.name)
			mult = suffix.mult
			break
		}
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad -size: %w", err)
	}
	if n <= 0 {
		return 0, errors.New("bad -size: want a positive byte count")
	}
	return n * mult, nil
}

var (
	events = []string{"click", "view", "purchase", "signup", "logout", "search", "error"}
	verbs  = []string{"api", "app", "admin", "static", "docs"}
	nouns  = []string{"orders", "users", "carts", "sessions", "reports", "assets"}
)

// writeJSONL writes exactly total bytes of JSON Lines. Every line but the last
// is a record of the same shape; the last one carries a pad field sized so the
// file ends on the requested byte, because a benchmark that reports throughput
// over a file size must measure the size it asked for. It needs at least the
// size of that last line.
func writeJSONL(w *bufio.Writer, total int64, seed uint64) error {
	if total < int64(minPadLen) {
		return fmt.Errorf("bad -size for jsonl: want at least %d bytes", minPadLen)
	}
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // G404: the input must be the same bytes on every run, which is the opposite of what a cryptographic source gives
	written := int64(0)
	for {
		line := record(r)
		if written+int64(len(line))+int64(minPadLen) > total {
			break
		}
		if _, err := w.WriteString(line); err != nil {
			return err
		}
		written += int64(len(line))
	}
	_, err := w.WriteString(padRecord(int(total - written)))
	return err
}

func record(r *rand.Rand) string {
	var b strings.Builder
	b.Grow(160)
	b.WriteString(`{"ts":"2026-`)
	fmt.Fprintf(&b, "%02d-%02dT%02d:%02d:%02dZ", 1+r.IntN(12), 1+r.IntN(28), r.IntN(24), r.IntN(60), r.IntN(60))
	b.WriteString(`","user":{"id":`)
	b.WriteString(strconv.Itoa(100000 + r.IntN(900000)))
	b.WriteString(`,"name":"user-`)
	b.WriteString(strconv.Itoa(1000 + r.IntN(9000)))
	b.WriteString(`"},"event":"`)
	b.WriteString(events[r.IntN(len(events))])
	b.WriteString(`","path":"/`)
	b.WriteString(verbs[r.IntN(len(verbs))])
	b.WriteString("/")
	b.WriteString(nouns[r.IntN(len(nouns))])
	b.WriteString("/")
	b.WriteString(strconv.Itoa(r.IntN(1000)))
	b.WriteString(`","dur_ms":`)
	b.WriteString(strconv.Itoa(r.IntN(5000)))
	b.WriteString(`,"ok":`)
	b.WriteString(strconv.FormatBool(r.IntN(10) != 0))
	b.WriteString("}\n")
	return b.String()
}

const padPrefix = `{"ts":"2026-01-01T00:00:00Z","user":{"id":999999,"name":"user-9999"},"event":"pad","path":"/app/assets/0","dur_ms":0,"ok":true,"pad":"`
const padSuffix = "\"}\n"

// minPadLen is the length of the shortest last line, the one with an empty pad.
var minPadLen = len(padPrefix) + len(padSuffix)

func padRecord(n int) string {
	return padPrefix + strings.Repeat("x", n-minPadLen) + padSuffix
}

// treeFileSize is the size of every file of a tree, so the number of files is
// the requested size divided by it, and a suite can declare that number as
// the work of each run.
const treeFileSize = 64 << 10

var (
	levels     = []string{"INFO", "INFO", "INFO", "INFO", "INFO", "INFO", "INFO", "INFO", "DEBUG", "WARN"}
	components = []string{"http", "db", "cache", "auth", "queue", "scheduler"}
	messages   = []string{"request served", "connection reused", "retrying after errors", "slow query", "token refreshed", "job finished", "cache miss"}
)

// writeTree writes size/treeFileSize log files of exactly treeFileSize bytes
// under dir, spread over two levels of directories as a source tree or a log
// archive is. About one line in fifty is an ERROR line, and some messages
// contain the lowercase word, so a search that ignores case would find more.
func writeTree(dir string, size int64, seed uint64) error {
	if size%treeFileSize != 0 {
		return fmt.Errorf("bad -size for tree: want a multiple of %d bytes, the size of one file", treeFileSize)
	}
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // G404: the input must be the same bytes on every run, which is the opposite of what a cryptographic source gives
	for i := range size / treeFileSize {
		sub := filepath.Join(dir, fmt.Sprintf("d%02d", i%16), fmt.Sprintf("s%02d", i/16%8))
		if err := os.MkdirAll(sub, 0o750); err != nil {
			return err
		}
		path := filepath.Join(sub, fmt.Sprintf("f%05d.log", i))
		if err := writeFile(path, func(w *bufio.Writer) error { return writeLog(w, r) }); err != nil {
			return err
		}
	}
	return nil
}

// writeLog writes exactly treeFileSize bytes of log lines. The last line is
// padded to the byte, like the last record of writeJSONL.
func writeLog(w *bufio.Writer, r *rand.Rand) error {
	written := 0
	for {
		line := logLine(r)
		if written+len(line)+len(logPadPrefix)+1 > treeFileSize {
			break
		}
		if _, err := w.WriteString(line); err != nil {
			return err
		}
		written += len(line)
	}
	_, err := w.WriteString(logPadPrefix + strings.Repeat("x", treeFileSize-written-len(logPadPrefix)-1) + "\n")
	return err
}

const logPadPrefix = "2026-01-01T00:00:00Z INFO [pad] "

func logLine(r *rand.Rand) string {
	level := levels[r.IntN(len(levels))]
	if r.IntN(50) == 0 {
		level = "ERROR"
	}
	return fmt.Sprintf("2026-%02d-%02dT%02d:%02d:%02dZ %s [%s] %s id=%d dur_ms=%d\n",
		1+r.IntN(12), 1+r.IntN(28), r.IntN(24), r.IntN(60), r.IntN(60),
		level, components[r.IntN(len(components))], messages[r.IntN(len(messages))],
		100000+r.IntN(900000), r.IntN(5000))
}

var (
	cities = []string{"Tokyo", "Osaka", "Sapporo", "Fukuoka", "Nagoya", "Sendai"}
	notes  = []string{"none", "gift", "express", "returned, refunded", `said "thanks"`, "left at door"}
)

const csvHeader = "id,user,city,amount,note\n"

// writeCSV writes exactly total bytes of CSV with a header line. A field is
// quoted only when it holds a comma or a double quote; about a third of the
// rows have such a note. The last row carries a padded note so the file ends
// on the requested byte, which needs at least a header and that row.
func writeCSV(w *bufio.Writer, total int64, seed uint64) error {
	if least := int64(len(csvHeader) + len(csvPadPrefix) + 1); total < least {
		return fmt.Errorf("bad -size for csv: want at least %d bytes", least)
	}
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // G404: the input must be the same bytes on every run, which is the opposite of what a cryptographic source gives
	if _, err := w.WriteString(csvHeader); err != nil {
		return err
	}
	written := int64(len(csvHeader))
	for {
		row := csvRow(r)
		if written+int64(len(row))+int64(len(csvPadPrefix))+1 > total {
			break
		}
		if _, err := w.WriteString(row); err != nil {
			return err
		}
		written += int64(len(row))
	}
	_, err := w.WriteString(csvPadPrefix + strings.Repeat("x", int(total-written)-len(csvPadPrefix)-1) + "\n")
	return err
}

const csvPadPrefix = "0,user-0,Tokyo,0.00,"

func csvRow(r *rand.Rand) string {
	return fmt.Sprintf("%d,user-%d,%s,%d.%02d,%s\n",
		100000+r.IntN(900000), 1000+r.IntN(9000), cities[r.IntN(len(cities))],
		r.IntN(100000), r.IntN(100), csvField(notes[r.IntN(len(notes))]))
}

// csvField quotes a field that holds a comma or a double quote, doubling the
// quotes inside, and leaves every other field bare.
func csvField(s string) string {
	if !strings.ContainsAny(s, ",\"") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// writeYAML writes exactly total bytes of one YAML document: a sequence of
// records of the shape writeJSONL writes, in block style. The timestamps are
// double-quoted, because a YAML 1.1 reader can resolve them to timestamps,
// and no number has a leading zero, which readers disagree on, so every
// reader sees the same values. The last record carries only ts, event and a
// pad field of at least one byte so the file ends on the requested byte.
func writeYAML(w *bufio.Writer, total int64, seed uint64) error {
	if least := len(yamlPadPrefix) + 2; total < int64(least) {
		return fmt.Errorf("bad -size for yaml: want at least %d bytes", least)
	}
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // G404: the input must be the same bytes on every run, which is the opposite of what a cryptographic source gives
	written := int64(0)
	for {
		rec := yamlRecord(r)
		if written+int64(len(rec))+int64(len(yamlPadPrefix))+2 > total {
			break
		}
		if _, err := w.WriteString(rec); err != nil {
			return err
		}
		written += int64(len(rec))
	}
	_, err := w.WriteString(yamlPadPrefix + strings.Repeat("x", int(total-written)-len(yamlPadPrefix)-1) + "\n")
	return err
}

const yamlPadPrefix = "- ts: \"2026-01-01T00:00:00Z\"\n  event: pad\n  pad: "

func yamlRecord(r *rand.Rand) string {
	return fmt.Sprintf("- ts: \"2026-%02d-%02dT%02d:%02d:%02dZ\"\n  user:\n    id: %d\n    name: user-%d\n  event: %s\n  path: /%s/%s/%d\n  dur_ms: %d\n  ok: %t\n",
		1+r.IntN(12), 1+r.IntN(28), r.IntN(24), r.IntN(60), r.IntN(60),
		100000+r.IntN(900000), 1000+r.IntN(9000), events[r.IntN(len(events))],
		verbs[r.IntN(len(verbs))], nouns[r.IntN(len(nouns))], r.IntN(1000),
		r.IntN(5000), r.IntN(10) != 0)
}
