// Command wordcount counts lines, words and bytes, like a tiny wc. It is the
// program the Cookbook examples measure: small enough to read in a minute,
// real enough that its implementations and its cache behave differently.
//
//	wordcount [-impl scanner|readall|parallel [-workers N]] [-cache DIR [-purge]] [FILE]
//	wordcount -gen LINES [-gen-format text|csv|jsonl] -o FILE
//
// -impl chooses between a streaming implementation, one that reads the whole
// input into memory, and one that reads it into memory and counts chunks on
// -workers goroutines at once. -cache stores the result keyed by the input's
// content hash, so a second run over the same input skips the counting.
// -purge deletes the cache directory and exits, like `go clean -cache`.
// -gen writes a fixture of the given number of lines or records instead of
// counting: plain text, CSV with a header, or JSON Lines.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// defaultImpl is the implementation used without -impl. The Cookbook's
// regression recipes change it in a scratch repository to show what a slower
// or hungrier implementation looks like against the base revision.
const defaultImpl = "scanner"

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "wordcount:", err)
		os.Exit(1)
	}
}

type counts struct {
	lines, words, bytes int
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("wordcount", flag.ContinueOnError)
	impl := fs.String("impl", defaultImpl, "counting implementation: scanner, readall or parallel")
	workers := fs.Int("workers", runtime.NumCPU(), "goroutines for -impl parallel")
	genFormat := fs.String("gen-format", "text", "fixture format for -gen: text, csv or jsonl")
	cacheDir := fs.String("cache", "", "cache results in this directory")
	gen := fs.Int("gen", 0, "write a fixture with this many lines instead of counting")
	out := fs.String("o", "", "fixture path for -gen")
	purge := fs.Bool("purge", false, "delete the -cache directory and exit")
	version := fs.Bool("version", false, "print the version")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *version {
		_, err := fmt.Fprintln(stdout, "wordcount 1.0.0")
		return err
	}
	if *gen > 0 {
		return generate(*gen, *genFormat, *out)
	}
	if *purge {
		if *cacheDir == "" {
			return errors.New("-purge needs -cache DIR")
		}
		return os.RemoveAll(*cacheDir)
	}

	input := stdin
	if fs.NArg() > 0 {
		f, err := os.Open(fs.Arg(0))
		if err != nil {
			return err
		}
		defer f.Close()
		input = f
	}

	var c counts
	var err error
	switch {
	case *cacheDir != "":
		c, err = cached(input, *cacheDir, *impl, *workers)
	default:
		c, err = count(input, *impl, *workers)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%d %d %d\n", c.lines, c.words, c.bytes)
	return err
}

func count(r io.Reader, impl string, workers int) (counts, error) {
	switch impl {
	case "scanner":
		return countScanner(r)
	case "readall":
		data, err := io.ReadAll(r)
		if err != nil {
			return counts{}, err
		}
		return countBytes(data), nil
	case "parallel":
		data, err := io.ReadAll(r)
		if err != nil {
			return counts{}, err
		}
		return countParallel(data, workers), nil
	default:
		return counts{}, fmt.Errorf("unknown -impl %q", impl)
	}
}

func countScanner(r io.Reader) (counts, error) {
	var c counts
	br := bufio.NewReaderSize(r, 64*1024)
	inWord := false
	for {
		b, err := br.ReadByte()
		if errors.Is(err, io.EOF) {
			return c, nil
		}
		if err != nil {
			return c, err
		}
		c.bytes++
		if b == '\n' {
			c.lines++
		}
		space := unicode.IsSpace(rune(b))
		if !space && !inWord {
			c.words++
		}
		inWord = !space
	}
}

func countBytes(data []byte) counts {
	return counts{
		lines: bytes.Count(data, []byte{'\n'}),
		words: len(strings.Fields(string(data))),
		bytes: len(data),
	}
}

// countParallel splits data into one chunk per worker and counts the chunks
// at the same time. A word that starts in one chunk and ends in the next is
// counted once: a chunk counts a word at its start only when the byte before
// the chunk is a space.
func countParallel(data []byte, workers int) counts {
	if workers < 1 {
		workers = 1
	}
	size := (len(data) + workers - 1) / workers
	if size == 0 {
		return counts{}
	}
	results := make([]counts, workers)
	var wg sync.WaitGroup
	for i := range workers {
		lo := i * size
		if lo >= len(data) {
			break
		}
		hi := min(lo+size, len(data))
		wg.Add(1)
		go func() {
			defer wg.Done()
			inWord := lo > 0 && !unicode.IsSpace(rune(data[lo-1]))
			var c counts
			for _, b := range data[lo:hi] {
				c.bytes++
				if b == '\n' {
					c.lines++
				}
				space := unicode.IsSpace(rune(b))
				if !space && !inWord {
					c.words++
				}
				inWord = !space
			}
			results[i] = c
		}()
	}
	wg.Wait()
	var total counts
	for _, c := range results {
		total.lines += c.lines
		total.words += c.words
		total.bytes += c.bytes
	}
	return total
}

// cached counts through a content-addressed cache: hashing is cheaper than
// counting words, so a warm cache is visibly faster than a cold one.
func cached(r io.Reader, dir, impl string, workers int) (counts, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return counts{}, err
	}
	sum := sha256.Sum256(data)
	path := filepath.Join(dir, hex.EncodeToString(sum[:]))
	if saved, err := os.ReadFile(path); err == nil {
		var c counts
		if _, err := fmt.Sscanf(string(saved), "%d %d %d", &c.lines, &c.words, &c.bytes); err == nil {
			return c, nil
		}
	}
	c, err := count(bytes.NewReader(data), impl, workers)
	if err != nil {
		return c, err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return c, err
	}
	return c, os.WriteFile(path, []byte(fmt.Sprintf("%d %d %d", c.lines, c.words, c.bytes)), 0o600)
}

func generate(lines int, format, path string) error {
	if path == "" {
		return errors.New("-gen needs -o FILE")
	}
	line, header, err := fixtureLine(format)
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	if _, err := w.WriteString(header); err != nil {
		_ = f.Close()
		return err
	}
	for i := range lines {
		if _, err := w.WriteString(line(i)); err != nil {
			_ = f.Close()
			return err
		}
	}
	return errors.Join(w.Flush(), f.Close())
}

// fixtureLine returns the writer of one line or record of a fixture format,
// and the format's header.
func fixtureLine(format string) (func(int) string, string, error) {
	switch format {
	case "text":
		return func(i int) string {
			return "entry " + strconv.Itoa(i) + " the quick brown fox jumps over the lazy dog\n"
		}, "", nil
	case "csv":
		return func(i int) string {
			return strconv.Itoa(i) + ",fox " + strconv.Itoa(i%97) + ",jumps over the lazy dog," + strconv.Itoa(i*7%1000) + "\n"
		}, "id,name,note,score\n", nil
	case "jsonl":
		return func(i int) string {
			return `{"id": ` + strconv.Itoa(i) + `, "name": "fox ` + strconv.Itoa(i%97) + `", "note": "jumps over the lazy dog", "score": ` + strconv.Itoa(i*7%1000) + "}\n"
		}, "", nil
	}
	return nil, "", fmt.Errorf("unknown -gen-format %q: use text, csv or jsonl", format)
}
