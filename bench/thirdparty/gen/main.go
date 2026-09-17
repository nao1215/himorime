// Command gen writes the input files the third-party benchmark suites measure
// over, so that no sample data from anyone else's project is committed here.
//
// The same seed and the same size always produce the same bytes, on every
// operating system and every Go version that keeps math/rand/v2 stable, so a
// scheduled run compares this week's numbers with last week's over the same
// input.
//
//	gen -kind jsonl -size 5MiB -seed 1 -o out.jsonl
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
	kind := fs.String("kind", "jsonl", "shape of the generated input: jsonl")
	size := fs.String("size", "1MiB", "size of the output in bytes, with an optional KiB, MiB or GiB suffix")
	seed := fs.Uint64("seed", 1, "seed of the random source; the same seed produces the same bytes")
	out := fs.String("o", "", "path of the file to write")
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
	if *kind != "jsonl" {
		return fmt.Errorf("unknown -kind %q; the shapes are: jsonl", *kind)
	}

	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 1<<20)
	if err := writeJSONL(w, n, *seed); err != nil {
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
// over a file size must measure the size it asked for.
func writeJSONL(w *bufio.Writer, total int64, seed uint64) error {
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
