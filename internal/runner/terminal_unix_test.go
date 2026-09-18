//go:build linux || darwin

package runner

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/nao1215/himorime/internal/config"
)

// ttyHelper fails unless its three streams are a terminal, then reads one
// line, prints it on standard output and a marker on standard error, and
// exits with args[0].
func ttyHelper(args []string) int {
	for fd := range 3 {
		if _, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ); err != nil {
			return 10 + fd
		}
	}
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	fmt.Printf("line:%s", line)
	fmt.Fprintln(os.Stderr, "err-stream")
	code, _ := strconv.Atoi(args[0])
	return code
}

func terminalBench(f *fixture, code string) config.Benchmark {
	b := bench("terminal", 3, f.command("tty", "tty", code))
	b.Terminal = true
	b.Stdin = config.Stdin{Kind: config.StdinContent, Content: "hello\n"}
	b.Metrics = config.Metrics{CPU: true, Memory: true}
	return b
}

func TestMeasureOnATerminal(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	res := f.runner.Measure(context.Background(), terminalBench(f, "0"), []Side{f.side})
	if res.Failure != nil {
		t.Fatalf("failure: %+v", res.Failure)
	}
	if m := res.Commands[0].Sides[SideHead]; m.Failure != nil || len(m.Samples) != 3 || len(m.PeakRSS) != 3 {
		t.Fatalf("measurement = %+v", m)
	}
}

func TestMeasureOnATerminalShowsWhatTheCommandWroteWhenItFails(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	res := f.runner.Measure(context.Background(), terminalBench(f, "3"), []Side{f.side})
	m := res.Commands[0].Sides[SideHead]
	if m.Failure == nil || m.Failure.Kind != FailExitCode || m.Failure.ExitCode != 3 {
		t.Fatalf("measurement = %+v, want exit code 3", m)
	}
	for _, want := range []string{"line:hello", "err-stream"} {
		if !strings.Contains(m.Failure.Stderr, want) {
			t.Errorf("failure tail = %q, want it to contain %q", m.Failure.Stderr, want)
		}
	}
}
