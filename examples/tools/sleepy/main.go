// Command sleepy stands in for a program whose speed you control: it sleeps,
// optionally with jitter, then exits with the status you ask for. The Cookbook
// uses it where a real program would make a recipe slow or unpredictable: a
// timeout, a failing command, a deliberately noisy measurement.
//
//	sleepy [-ms N] [-jitter-ms N] [-exit CODE] [-state FILE]
//
// With -state, every other call adds -jitter-ms, which produces the bimodal
// noise shared CI runners are known for.
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	ms := flag.Int("ms", 10, "milliseconds to sleep")
	jitter := flag.Int("jitter-ms", 0, "extra milliseconds added on every other call (needs -state)")
	state := flag.String("state", "", "file that counts calls, for -jitter-ms")
	code := flag.Int("exit", 0, "exit status")
	flag.Parse()

	d := time.Duration(*ms) * time.Millisecond
	if *state != "" {
		n := 0
		if data, err := os.ReadFile(*state); err == nil {
			n, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		if err := os.WriteFile(*state, []byte(strconv.Itoa(n+1)), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "sleepy:", err)
			os.Exit(2)
		}
		if n%2 == 1 {
			d += time.Duration(*jitter) * time.Millisecond
		}
	}
	time.Sleep(d)
	if *code != 0 {
		fmt.Fprintf(os.Stderr, "sleepy: exiting with status %d as asked\n", *code)
	}
	os.Exit(*code)
}
