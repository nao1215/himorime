// Command helper is the portable stand-in program the end-to-end suite
// measures. It replaces sleep, cat, touch and friends so the same specs run on
// Linux, macOS and Windows without a POSIX userland.
//
// Every timing it produces is deliberately coarse (tens to hundreds of
// milliseconds) so that the suite asserts on classifications, never on
// millisecond differences.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// subcommand returns the implementation of a helper subcommand.
func subcommand(name string) (func([]string) error, bool) {
	run, ok := map[string]func([]string) error{
		"sleep": sleep, "sleep-from": sleepFrom, "alternate": alternate, "copy": copyFile, "exit": exitWith,
		"gen": gen, "count": count, "spawn": spawn, "counter": counter, "cache": cache, "write": write,
		"remove": remove, "require": require, "replace": replace, "consume": consume, "event": event,
		"interrupt": interrupt, "burn": burn, "alloc": alloc, "records": records, "tree": tree,
		"print": printOut, "getenv": getenv, "pad": pad, "args-from": argsFrom, "json-schema": jsonSchema, "csv-shape": csvShape,
	}[name]
	return run, ok
}

func main() {
	if len(os.Args) < 2 {
		fail("usage: helper <subcommand> ...; see test/e2e/README.md")
	}
	run, ok := subcommand(os.Args[1])
	if !ok {
		fail(fmt.Sprintf("unknown subcommand %q", os.Args[1]))
	}
	if err := run(os.Args[2:]); err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "helper:", msg)
	os.Exit(2)
}

func need(args []string, n int, usage string) error {
	if len(args) < n {
		return errors.New("usage: helper " + usage)
	}
	return nil
}

// sleep DURATION
func sleep(args []string) error {
	if err := need(args, 1, "sleep DURATION"); err != nil {
		return err
	}
	d, err := time.ParseDuration(args[0])
	if err != nil {
		return err
	}
	time.Sleep(d)
	return nil
}

// sleep-from FILE: sleep for the duration written in FILE. A build step copies
// a revision's delay file to ${artifact}, so the "program" differs per revision.
func sleepFrom(args []string) error {
	if err := need(args, 1, "sleep-from FILE"); err != nil {
		return err
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	d, err := time.ParseDuration(strings.TrimSpace(string(data)))
	if err != nil {
		return err
	}
	time.Sleep(d)
	return nil
}

// alternate STATE FAST SLOW: sleep FAST and SLOW on alternate calls, tracking
// the call count in STATE. It produces a bimodal, very noisy distribution.
func alternate(args []string) error {
	if err := need(args, 3, "alternate STATE FAST SLOW"); err != nil {
		return err
	}
	n := 0
	if data, err := os.ReadFile(args[0]); err == nil {
		n, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	if err := os.WriteFile(args[0], []byte(strconv.Itoa(n+1)), 0o600); err != nil {
		return err
	}
	pick := args[1]
	if n%2 == 1 {
		pick = args[2]
	}
	return sleep([]string{pick})
}

// copy SRC DST
func copyFile(args []string) error {
	if err := need(args, 2, "copy SRC DST"); err != nil {
		return err
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	return os.WriteFile(args[1], data, 0o600)
}

// exit CODE [MESSAGE]
func exitWith(args []string) error {
	if err := need(args, 1, "exit CODE [MESSAGE]"); err != nil {
		return err
	}
	code, err := strconv.Atoi(args[0])
	if err != nil {
		return err
	}
	if len(args) > 1 {
		fmt.Fprintln(os.Stderr, strings.Join(args[1:], " "))
	}
	os.Exit(code)
	return nil
}

// gen LINES FILE: write LINES lines of text.
func gen(args []string) error {
	if err := need(args, 2, "gen LINES FILE"); err != nil {
		return err
	}
	n, err := strconv.Atoi(args[0])
	if err != nil {
		return err
	}
	f, err := os.Create(args[1])
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for i := range n {
		fmt.Fprintf(w, "line %d the quick brown fox jumps over the lazy dog\n", i)
	}
	return errors.Join(w.Flush(), f.Close())
}

// count [EXPECT]: count stdin lines; fail when EXPECT is given and differs.
func count(args []string) error {
	sc := bufio.NewScanner(os.Stdin)
	n := 0
	for sc.Scan() {
		n++
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if len(args) > 0 {
		want, err := strconv.Atoi(args[0])
		if err != nil {
			return err
		}
		if n != want {
			return fmt.Errorf("read %d lines, want %d", n, want)
		}
	}
	fmt.Println(n)
	return nil
}

// spawn MARKER: start a grandchild that writes MARKER after two seconds, then
// hang. Used to prove a timeout stops the whole process tree.
func spawn(args []string) error {
	if err := need(args, 1, "spawn MARKER"); err != nil {
		return err
	}
	if len(args) > 1 && args[1] == "child" {
		time.Sleep(2 * time.Second)
		return os.WriteFile(args[0], []byte("survived"), 0o600)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "spawn", args[0], "child")
	if err := cmd.Start(); err != nil {
		return err
	}
	time.Sleep(time.Hour)
	return nil
}

// counter FILE: increment the integer in FILE.
func counter(args []string) error {
	if err := need(args, 1, "counter FILE"); err != nil {
		return err
	}
	n := 0
	if data, err := os.ReadFile(args[0]); err == nil {
		n, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	return os.WriteFile(args[0], []byte(strconv.Itoa(n+1)), 0o600)
}

// cache DIR: slow (200ms) when DIR/cache is missing, then create it; fast
// (5ms) when it exists.
func cache(args []string) error {
	if err := need(args, 1, "cache DIR"); err != nil {
		return err
	}
	p := args[0] + string(os.PathSeparator) + "cache"
	if _, err := os.Stat(p); err == nil {
		time.Sleep(5 * time.Millisecond)
		return nil
	}
	time.Sleep(200 * time.Millisecond)
	if err := os.MkdirAll(args[0], 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte("warm"), 0o600)
}

// write FILE TEXT...
func write(args []string) error {
	if err := need(args, 1, "write FILE [TEXT...]"); err != nil {
		return err
	}
	return os.WriteFile(args[0], []byte(strings.Join(args[1:], " ")+"\n"), 0o600)
}

// remove PATH: remove a file or directory tree if it exists.
func remove(args []string) error {
	if err := need(args, 1, "remove PATH"); err != nil {
		return err
	}
	return os.RemoveAll(args[0])
}

// require FILE: fail unless FILE exists; with "absent", fail if it exists.
func require(args []string) error {
	if err := need(args, 1, "require FILE [absent]"); err != nil {
		return err
	}
	_, err := os.Stat(args[0])
	absent := len(args) > 1 && args[1] == "absent"
	switch {
	case absent && err == nil:
		return fmt.Errorf("%s exists", args[0])
	case !absent && err != nil:
		return err
	}
	_, _ = io.WriteString(os.Stdout, "")
	return nil
}

// event NAME REPO OUT: write a GitHub Actions event payload for NAME whose
// base commit is REPO's HEAD, the way the runner would provide it.
func event(args []string) error {
	if err := need(args, 3, "event NAME REPO OUT"); err != nil {
		return err
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = args[1]
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git rev-parse HEAD in %s: %w", args[1], err)
	}
	sha := strings.TrimSpace(string(out))
	var payload string
	switch args[0] {
	case "push":
		payload = fmt.Sprintf(`{"before":%q}`, sha)
	case "merge_group":
		payload = fmt.Sprintf(`{"merge_group":{"base_sha":%q}}`, sha)
	default:
		payload = fmt.Sprintf(`{"pull_request":{"base":{"sha":%q}}}`, sha)
	}
	return os.WriteFile(args[2], []byte(payload), 0o600)
}

// interrupt AFTER MARKER -- COMMAND...: start COMMAND, wait until MARKER
// exists (the benchmark is running) or AFTER passes, send it an interrupt,
// and print "exit=N" with its exit status. The child's own output is passed
// through. Unix only: Windows has no way to deliver Ctrl+C to one child.
func interrupt(args []string) error {
	if len(args) < 4 || args[2] != "--" {
		return errors.New("usage: helper interrupt AFTER MARKER -- COMMAND")
	}
	after, err := time.ParseDuration(args[0])
	if err != nil {
		return err
	}
	cmd := exec.Command(args[3], args[4:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	deadline := time.Now().Add(after)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(args[1]); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		return err
	}
	err = cmd.Wait()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		return err
	}
	fmt.Printf("exit=%d\n", code)
	return nil
}

// consume FILE: fail unless FILE exists, then delete it. A command that
// consumes state proves prepare_each recreated that state before every run.
func consume(args []string) error {
	if err := need(args, 1, "consume FILE"); err != nil {
		return err
	}
	if _, err := os.Stat(args[0]); err != nil {
		return fmt.Errorf("state was not prepared: %w", err)
	}
	return os.Remove(args[0])
}

// replace FILE OLD NEW: replace the only occurrence of OLD in FILE. It fails
// when OLD does not occur exactly once, so a scenario that edits source code
// cannot silently stop editing it.
func replace(args []string) error {
	if err := need(args, 3, "replace FILE OLD NEW"); err != nil {
		return err
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	if n := strings.Count(string(data), args[1]); n != 1 {
		return fmt.Errorf("%q occurs %d times in %s, want exactly once", args[1], n, args[0])
	}
	return os.WriteFile(args[0], []byte(strings.Replace(string(data), args[1], args[2], 1)), 0o600)
}

// burn DURATION [THREADS]: keep THREADS goroutines (default 1) busy for
// DURATION of wall-clock time, so the process uses about DURATION x THREADS
// of CPU time.
func burn(args []string) error {
	if err := need(args, 1, "burn DURATION [THREADS]"); err != nil {
		return err
	}
	d, err := time.ParseDuration(args[0])
	if err != nil {
		return err
	}
	threads := 1
	if len(args) > 1 {
		if threads, err = strconv.Atoi(args[1]); err != nil || threads < 1 {
			return fmt.Errorf("invalid thread count %q", args[1])
		}
	}
	runtime.GOMAXPROCS(threads + 1)
	deadline := time.Now().Add(d)
	var wg sync.WaitGroup
	for range threads {
		wg.Add(1)
		go func() {
			defer wg.Done()
			x := uint64(0)
			for time.Now().Before(deadline) {
				for range 1000 {
					x = x*6364136223846793005 + 1442695040888963407
				}
			}
			_ = x
		}()
	}
	wg.Wait()
	return nil
}

// alloc MIB [HOLD]: allocate MIB mebibytes, write to every page so they are
// resident, and keep them for HOLD (default 50ms).
func alloc(args []string) error {
	if err := need(args, 1, "alloc MIB [HOLD]"); err != nil {
		return err
	}
	mib, err := strconv.Atoi(args[0])
	if err != nil || mib < 0 {
		return fmt.Errorf("invalid size %q", args[0])
	}
	hold := 50 * time.Millisecond
	if len(args) > 1 {
		if hold, err = time.ParseDuration(args[1]); err != nil {
			return err
		}
	}
	buf := make([]byte, mib<<20)
	for i := 0; i < len(buf); i += 4096 {
		buf[i] = byte(i)
	}
	time.Sleep(hold)
	runtime.KeepAlive(buf)
	return nil
}

// records N [DELAY]: process N records, hashing each one, and sleep DELAY
// (default 0) in total; print how many were processed.
func records(args []string) error {
	if err := need(args, 1, "records N [DELAY]"); err != nil {
		return err
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 0 {
		return fmt.Errorf("invalid record count %q", args[0])
	}
	if len(args) > 1 {
		d, err := time.ParseDuration(args[1])
		if err != nil {
			return err
		}
		time.Sleep(d)
	}
	var sum [32]byte
	for i := range n {
		sum = sha256.Sum256(append(sum[:], strconv.Itoa(i)...))
	}
	fmt.Printf("processed %d records %x\n", n, sum[:4])
	return nil
}

// tree CHILDREN -- SUBCOMMAND ARGS...: run CHILDREN copies of this helper
// with SUBCOMMAND at the same time and wait for all of them, doing no work
// itself. It proves that CPU time and memory are measured for the process
// tree rather than only the process himorime started.
func tree(args []string) error {
	if len(args) < 3 || args[1] != "--" {
		return errors.New("usage: helper tree CHILDREN -- SUBCOMMAND ARGS")
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 {
		return fmt.Errorf("invalid child count %q", args[0])
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmds := make([]*exec.Cmd, n)
	for i := range cmds {
		cmds[i] = exec.Command(exe, args[2:]...)
		cmds[i].Stdout, cmds[i].Stderr = os.Stdout, os.Stderr
		if err := cmds[i].Start(); err != nil {
			return err
		}
	}
	var errs []error
	for _, c := range cmds {
		errs = append(errs, c.Wait())
	}
	return errors.Join(errs...)
}

// print STDOUT [STDERR]: write a line to standard output and, when given, one
// to standard error.
func printOut(args []string) error {
	if err := need(args, 1, "print STDOUT [STDERR]"); err != nil {
		return err
	}
	fmt.Println(args[0])
	if len(args) > 1 {
		fmt.Fprintln(os.Stderr, args[1])
	}
	return nil
}

// getenv NAME: print the value of an environment variable, or nothing.
func getenv(args []string) error {
	if err := need(args, 1, "getenv NAME"); err != nil {
		return err
	}
	fmt.Println(os.Getenv(args[0]))
	return nil
}

// pad BYTES FILE: write a file of exactly BYTES bytes, as a throughput input.
func pad(args []string) error {
	if err := need(args, 2, "pad BYTES FILE"); err != nil {
		return err
	}
	n, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || n < 0 {
		return fmt.Errorf("invalid size %q", args[0])
	}
	if err := os.MkdirAll(filepath.Dir(args[1]), 0o700); err != nil {
		return err
	}
	f, err := os.Create(args[1])
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for i := int64(0); i < n; i++ {
		_ = w.WriteByte('x')
	}
	return errors.Join(w.Flush(), f.Close())
}

// args-from FILE: run the subcommand and arguments written in FILE. A build
// copies a revision's file to ${artifact}, so each revision of a comparison
// can burn a different amount of CPU or allocate a different amount of memory.
func argsFrom(args []string) error {
	if err := need(args, 1, "args-from FILE"); err != nil {
		return err
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return fmt.Errorf("%s holds no subcommand", args[0])
	}
	run, ok := subcommand(fields[0])
	if !ok || fields[0] == "args-from" {
		return fmt.Errorf("unknown subcommand %q in %s", fields[0], args[0])
	}
	return run(fields[1:])
}

// json-schema SCHEMA DOCUMENT: validate a JSON document against a JSON Schema
// file and print "valid", or fail with the validation errors.
func jsonSchema(args []string) error {
	if err := need(args, 2, "json-schema SCHEMA DOCUMENT"); err != nil {
		return err
	}
	c := jsonschema.NewCompiler()
	sch, err := c.Compile(args[0])
	if err != nil {
		return err
	}
	f, err := os.Open(args[1])
	if err != nil {
		return err
	}
	defer f.Close()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		return err
	}
	if err := sch.Validate(doc); err != nil {
		return err
	}
	fmt.Println("valid")
	return nil
}

// csv-shape FILE: parse a CSV file strictly and print "rows=N columns=M", or
// fail when a row has a different number of fields or a cell holds JSON.
func csvShape(args []string) error {
	if err := need(args, 1, "csv-shape FILE"); err != nil {
		return err
	}
	f, err := os.Open(args[0])
	if err != nil {
		return err
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return errors.New("empty CSV")
	}
	for i, row := range rows {
		for _, cell := range row {
			if strings.HasPrefix(cell, "{") || strings.HasPrefix(cell, "[") {
				return fmt.Errorf("row %d holds structured data: %q", i+1, cell)
			}
		}
	}
	fmt.Printf("rows=%d columns=%d\n", len(rows), len(rows[0]))
	return nil
}
