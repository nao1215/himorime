package report

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// sectionName is the form of a section name in a marker line.
var sectionName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidSectionName reports whether name can name a Markdown section:
// lowercase letters, digits and '-', starting with a letter or digit.
func ValidSectionName(name string) bool {
	return sectionName.MatchString(name)
}

// UpdateMarkdownSection replaces the section name of the Markdown file at
// path with the report. The file must exist and hold the section's markers;
// everything outside them is kept byte for byte. The new content is written
// to a temporary file in the same directory and renamed over the old one, so
// a failure never leaves a truncated document.
func UpdateMarkdownSection(path, name string, r *Report) error {
	wrap := func(err error) error {
		return fmt.Errorf("%s: section %q: %w", path, name, err)
	}
	doc, err := os.ReadFile(path) //nolint:gosec // G304: the document the user named
	if errors.Is(err, fs.ErrNotExist) {
		return wrap(fmt.Errorf("the file does not exist; add the lines %s and %s to an existing Markdown file: %w", beginMarker(name), endMarker(name), fs.ErrNotExist))
	}
	if err != nil {
		return wrap(err)
	}
	out, err := ReplaceSection(doc, name, func(level int) string { return renderMarkdown(r, level) })
	if err != nil {
		return wrap(err)
	}
	if bytes.Equal(out, doc) {
		return nil
	}
	if err := replaceFile(path, out); err != nil {
		return wrap(err)
	}
	return nil
}

func beginMarker(name string) string { return "<!-- himorime:begin " + name + " -->" }
func endMarker(name string) string   { return "<!-- himorime:end " + name + " -->" }

// ReplaceSection returns doc with the lines between the begin and end marker
// lines of the section replaced by render's output, surrounded by one blank
// line on each side. render receives the heading level of the report: one
// below the nearest heading above the begin marker, or 2 without one. Lines
// inside fenced code blocks are neither markers nor headings. The line
// endings of the begin marker line are used for the new content.
func ReplaceSection(doc []byte, name string, render func(level int) string) ([]byte, error) {
	lines := splitLines(doc)
	begin, end := beginMarker(name), endMarker(name)
	beginAt, endAt := -1, -1
	parent := 0
	inFence := fence{}
	for i, l := range lines {
		text := strings.TrimRight(l.text, "\r")
		if l.frontMatter {
			continue
		}
		if inFence.closes(text) {
			inFence = fence{}
			continue
		}
		if inFence.open() {
			continue
		}
		if f, ok := opensFence(text); ok {
			inFence = f
			continue
		}
		switch strings.TrimSpace(text) {
		case begin:
			if beginAt >= 0 {
				return nil, fmt.Errorf("the line %q appears on lines %d and %d; keep exactly one", begin, beginAt+1, i+1)
			}
			beginAt = i
			continue
		case end:
			if endAt >= 0 {
				return nil, fmt.Errorf("the line %q appears on lines %d and %d; keep exactly one", end, endAt+1, i+1)
			}
			endAt = i
			continue
		}
		if beginAt < 0 {
			if level := atxLevel(text); level > 0 {
				parent = level
			}
		}
	}
	switch {
	case beginAt < 0:
		return nil, fmt.Errorf("no line %q", begin)
	case endAt < 0:
		return nil, fmt.Errorf("no line %q after the begin marker on line %d", end, beginAt+1)
	case endAt < beginAt:
		return nil, fmt.Errorf("the end marker on line %d comes before the begin marker on line %d", endAt+1, beginAt+1)
	}

	eol := "\n"
	if strings.HasSuffix(lines[beginAt].text, "\r") {
		eol = "\r\n"
	}
	level := 2
	if parent > 0 {
		level = min(parent+1, 6)
	}
	body := strings.TrimRight(render(level), "\n")
	if eol != "\n" {
		body = strings.ReplaceAll(body, "\n", eol)
	}
	var out bytes.Buffer
	// The begin marker has a line break: the end marker follows it.
	out.Write(doc[:lines[beginAt].next])
	out.WriteString(eol + body + eol + eol)
	out.Write(doc[lines[endAt].start:])
	return out.Bytes(), nil
}

// line is one line of a document: its text without "\n" (a "\r" of a CRLF
// ending stays), where it starts and where the next line starts.
type line struct {
	text        string
	start, next int
	frontMatter bool
}

// splitLines splits doc into lines and marks a YAML or TOML front matter
// block at the start of the file, whose comments are not headings.
func splitLines(doc []byte) []line {
	var lines []line
	for start := 0; start < len(doc); {
		i := bytes.IndexByte(doc[start:], '\n')
		next := len(doc)
		text := string(doc[start:])
		if i >= 0 {
			next = start + i + 1
			text = string(doc[start : start+i])
		}
		lines = append(lines, line{text: text, start: start, next: next})
		start = next
	}
	if len(lines) == 0 {
		return lines
	}
	delim := strings.TrimRight(lines[0].text, "\r")
	if delim != "---" && delim != "+++" {
		return lines
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i].text, "\r") == delim {
			for j := 0; j <= i; j++ {
				lines[j].frontMatter = true
			}
			return lines
		}
	}
	return lines
}

// fence is an open fenced code block: its character and length.
type fence struct {
	char byte
	n    int
}

func (f fence) open() bool { return f.n > 0 }

// closes reports whether text closes the open fence: the same character, at
// least as many of it, and nothing else but spaces.
func (f fence) closes(text string) bool {
	if !f.open() {
		return false
	}
	g, ok := opensFence(text)
	return ok && g.char == f.char && g.n >= f.n && strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(text), string(f.char))) == ""
}

// opensFence reports whether text starts a fenced code block: up to three
// spaces, then three or more backticks or tildes.
func opensFence(text string) (fence, bool) {
	trimmed := strings.TrimLeft(text, " ")
	if len(text)-len(trimmed) > 3 || len(trimmed) < 3 || (trimmed[0] != '`' && trimmed[0] != '~') {
		return fence{}, false
	}
	c := trimmed[0]
	n := len(trimmed) - len(strings.TrimLeft(trimmed, string(c)))
	if n < 3 {
		return fence{}, false
	}
	return fence{char: c, n: n}, true
}

// atxLevel returns the level of an ATX heading starting at the beginning of
// the line, or 0.
func atxLevel(text string) int {
	n := len(text) - len(strings.TrimLeft(text, "#"))
	if n < 1 || n > 6 {
		return 0
	}
	if len(text) > n && text[n] != ' ' && text[n] != '\t' {
		return 0
	}
	return n
}

// replaceFile writes data to a temporary file next to path and renames it
// over path, keeping the file's permissions.
func replaceFile(path string, data []byte) (err error) {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".himorime-*")
	if err != nil {
		return fmt.Errorf("create a temporary file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		return fmt.Errorf("set permissions of %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace the file: %w", err)
	}
	return nil
}
