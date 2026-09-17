// Command wordcount counts lines, words and bytes, like a tiny wc. It is the
// program the Cookbook examples measure: small enough to read in a minute,
// real enough that its implementations and its cache behave differently.
//
//	wordcount [-impl scanner|readall] [-cache DIR [-purge]] [-gen LINES -o FILE] [FILE]
//
// -impl chooses between a streaming implementation and one that reads the
// whole input into memory. -cache stores the result keyed by the input's
// content hash, so a second run over the same input skips the counting.
// -purge deletes the cache directory and exits, like `go clean -cache`.
// -gen writes a text fixture of the given number of lines instead of counting.
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
	"strconv"
	"strings"
	"unicode"
)

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
	impl := fs.String("impl", "scanner", "counting implementation: scanner or readall")
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
		return generate(*gen, *out)
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
		c, err = cached(input, *cacheDir, *impl)
	default:
		c, err = count(input, *impl)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%d %d %d\n", c.lines, c.words, c.bytes)
	return err
}

func count(r io.Reader, impl string) (counts, error) {
	switch impl {
	case "scanner":
		return countScanner(r)
	case "readall":
		data, err := io.ReadAll(r)
		if err != nil {
			return counts{}, err
		}
		return countBytes(data), nil
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

// cached counts through a content-addressed cache: hashing is cheaper than
// counting words, so a warm cache is visibly faster than a cold one.
func cached(r io.Reader, dir, impl string) (counts, error) {
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
	c, err := count(bytes.NewReader(data), impl)
	if err != nil {
		return c, err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return c, err
	}
	return c, os.WriteFile(path, []byte(fmt.Sprintf("%d %d %d", c.lines, c.words, c.bytes)), 0o600)
}

func generate(lines int, path string) error {
	if path == "" {
		return errors.New("-gen needs -o FILE")
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for i := range lines {
		if _, err := w.WriteString("entry " + strconv.Itoa(i) + " the quick brown fox jumps over the lazy dog\n"); err != nil {
			_ = f.Close()
			return err
		}
	}
	return errors.Join(w.Flush(), f.Close())
}
