// Command sleepy stands in for a program whose behavior you control: it
// sleeps or spins, optionally with jitter, optionally in child processes, then
// exits with the status you ask for. The Cookbook uses it where a real program
// would make a recipe slow or unpredictable: a timeout, a failing command, a
// deliberately noisy measurement, a command that starts other processes.
//
//	sleepy [-ms N] [-jitter-ms N] [-state FILE] [-busy] [-children N] [-exit CODE]
//
// With -state, every other call adds -jitter-ms, which produces the bimodal
// noise shared CI runners are known for. -busy spins on a CPU instead of
// sleeping. -children starts N copies of sleepy with the same -ms and -busy,
// waits for all of them, and does no work itself.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func main() {
	ms := flag.Int("ms", 10, "milliseconds to sleep, or to spin with -busy")
	jitter := flag.Int("jitter-ms", 0, "extra milliseconds added on every other call (needs -state)")
	state := flag.String("state", "", "file that counts calls, for -jitter-ms")
	busy := flag.Bool("busy", false, "spin on a CPU instead of sleeping")
	children := flag.Int("children", 0, "run this many copies as child processes and wait for them")
	code := flag.Int("exit", 0, "exit status")
	flag.Parse()

	if *children > 0 {
		if err := spawn(*children, *ms, *busy); err != nil {
			fmt.Fprintln(os.Stderr, "sleepy:", err)
			os.Exit(2)
		}
		os.Exit(*code)
	}

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
	if *busy {
		x := 0
		for deadline := time.Now().Add(d); time.Now().Before(deadline); {
			x++
		}
	} else {
		time.Sleep(d)
	}
	if *code != 0 {
		fmt.Fprintf(os.Stderr, "sleepy: exiting with status %d as asked\n", *code)
	}
	os.Exit(*code)
}

// spawn runs n copies of this program at the same time and waits for them.
func spawn(n, ms int, busy bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"-ms", strconv.Itoa(ms)}
	if busy {
		args = append(args, "-busy")
	}
	cmds := make([]*exec.Cmd, n)
	for i := range cmds {
		cmds[i] = exec.Command(exe, args...)
		cmds[i].Stdout, cmds[i].Stderr = os.Stdout, os.Stderr
		if err := cmds[i].Start(); err != nil {
			return err
		}
	}
	for _, c := range cmds {
		if err := c.Wait(); err != nil {
			return err
		}
	}
	return nil
}
