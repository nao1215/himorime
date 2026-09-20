//go:build linux || darwin

package proc

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestTerminalClosedAndNonTerminalDescriptors(t *testing.T) {
	t.Parallel()
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := setTerminalSize(f); err == nil {
		t.Fatal("resized a non-terminal")
	}
	if _, err := lineMode(f); err == nil {
		t.Fatal("read line mode on a non-terminal")
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	term := &Terminal{master: f, slave: f, stop: make(chan struct{}), wrote: make(chan struct{}), ready: make(chan struct{})}
	if term.waitReady() || term.waitRead() {
		t.Fatal("closed terminal was ready for input")
	}
	close(term.wrote)
	if err := term.typeKeys([]byte("x")); err != nil {
		t.Fatalf("typing after close: %v", err)
	}
	if err := term.copyOutput(io.Discard); err != nil {
		t.Fatalf("copying after close: %v", err)
	}
}

// terminalHelper checks that the helper runs the way a program started from a
// shell on a terminal does, then reads keys in raw mode up to a q and prints
// them. It switches to raw mode only after a pause, so the input was typed
// while the terminal was still in line mode.
func terminalHelper() int {
	for fd := range 3 {
		if _, err := unix.IoctlGetTermios(fd, getTermios); err != nil {
			fmt.Printf("descriptor %d is not a terminal: %v\n", fd, err)
			return 10 + fd
		}
	}
	if sid, err := unix.Getsid(0); err != nil || sid != os.Getpid() {
		fmt.Printf("session %d (%v), want %d\n", sid, err, os.Getpid())
		return 20
	}
	if pgrp, err := unix.IoctlGetInt(0, unix.TIOCGPGRP); err != nil || pgrp != os.Getpid() {
		fmt.Printf("foreground process group %d (%v), want %d: not the controlling terminal\n", pgrp, err, os.Getpid())
		return 21
	}
	ws, err := unix.IoctlGetWinsize(0, unix.TIOCGWINSZ)
	if err != nil || ws.Row != TerminalRows || ws.Col != TerminalColumns {
		fmt.Printf("size %+v (%v), want %dx%d\n", ws, err, TerminalColumns, TerminalRows)
		return 22
	}
	if os.Getenv("HELPER_PROMPT") != "" {
		// A line-mode program: prompt, then read a line.
		fmt.Print("> ")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return 23
		}
		fmt.Printf("got:%q\n", line)
		return 0
	}
	// A raw-mode program that writes nothing first: typing must wait for the
	// switch, and then every read must return exactly one key.
	time.Sleep(100 * time.Millisecond)
	t, err := unix.IoctlGetTermios(0, getTermios)
	if err != nil {
		return 24
	}
	t.Lflag &^= unix.ICANON | unix.ECHO
	t.Cc[unix.VMIN], t.Cc[unix.VTIME] = 1, 0
	if err := unix.IoctlSetTermios(0, setTermios, t); err != nil {
		return 25
	}
	var reads []string
	buf := make([]byte, 64)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return 26
		}
		if string(buf[:n]) == "q" {
			break
		}
		reads = append(reads, string(buf[:n]))
		// Be slower than the typist, so keys would pile up without pacing.
		time.Sleep(2 * time.Millisecond)
	}
	fmt.Printf("reads:%q\n", reads)
	return 0
}

func TestRunOnATerminalGivesTheCommandAControllingTerminal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		env   []string
		want  string
	}{
		{"raw mode reads one key at a time", "ab\u00e9\x1b[Ac\x1bxq", nil, `reads:["a" "b" "é" "\x1b[A" "c" "\x1bx"]`},
		{"line mode reads the line after the prompt", "abc\n", []string{"HELPER_PROMPT=1"}, `got:"abc\n"`},
	}
	for _, path := range startPaths() {
		for _, tt := range tests {
			t.Run(path.name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				runWith := path.run(t)
				var out bytes.Buffer
				term, err := OpenTerminal(strings.NewReader(tt.input), &out)
				if err != nil {
					t.Fatal(err)
				}
				s := helper(t, "terminal", tt.env...)
				s.Stdin, s.Stdout, s.Stderr = term.File(), term.File(), term.File()
				s.Terminal = true
				s.Timeout = 30 * time.Second
				res, err := runWith(context.Background(), s)
				if cerr := term.Close(); cerr != nil {
					t.Errorf("close: %v", cerr)
				}
				if err != nil {
					t.Fatal(err)
				}
				if res.ExitCode != 0 || res.TimedOut {
					t.Fatalf("result = %+v, output %q", res, out.String())
				}
				if !strings.Contains(out.String(), tt.want) {
					t.Fatalf("output = %q, want it to contain %s", out.String(), tt.want)
				}
			})
		}
	}
}

func TestKeyLengthSplitsInputIntoKeys(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]int{
		"a":         1,
		"\u00e9x":   2,
		"\u65e5":    3,
		"\x1b[A":    3,
		"\x1b[15~x": 5,
		"\x1bOP":    3,
		"\x1bx":     2,
		"\x1b\x1b":  1,
		"\x1b":      1,
		"\x1b[":     2,
		"\xff":      1,
	} {
		if got := keyLength([]byte(in)); got != want {
			t.Errorf("keyLength(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestTerminalCloseStopsCopyingInputTheCommandNeverRead(t *testing.T) {
	t.Parallel()
	// More than a terminal buffers: the copy blocks until Close.
	term, err := OpenTerminal(strings.NewReader(strings.Repeat("x", 1<<20)), nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- term.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("close: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return")
	}
	if err := term.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestTerminalReportsSupportAndReadinessBeforeCommandStarts(t *testing.T) {
	t.Parallel()
	if !TerminalSupported() {
		t.Fatal("TerminalSupported() = false on Unix")
	}
	term, err := OpenTerminal(strings.NewReader("x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !term.NeverReady() {
		t.Fatal("NeverReady() = false before a command writes or switches mode")
	}
	if err := term.Close(); err != nil {
		t.Fatal(err)
	}
}

type terminalFailReader struct{}

func (terminalFailReader) Read([]byte) (int, error) { return 0, errors.New("input failed") }

type terminalFailWriter struct{}

func (terminalFailWriter) Write([]byte) (int, error) { return 0, errors.New("output failed") }

func TestTerminalInternalStateAndIOFailures(t *testing.T) {
	if _, err := OpenTerminal(terminalFailReader{}, nil); err == nil || !strings.Contains(err.Error(), "input failed") {
		t.Fatalf("OpenTerminal failing input = %v", err)
	}
	master, slave, err := openPTY()
	if err != nil {
		t.Fatal(err)
	}
	term := &Terminal{master: master, slave: slave, wrote: make(chan struct{}), stop: make(chan struct{}), ready: make(chan struct{})}
	if _, err := slave.Write([]byte("output")); err != nil {
		t.Fatal(err)
	}
	if err := term.copyOutput(terminalFailWriter{}); err == nil || err.Error() != "output failed" {
		t.Fatalf("copyOutput failing writer = %v", err)
	}
	_ = master.Close()
	_ = slave.Close()

	stop := make(chan struct{})
	close(stop)
	state := &Terminal{wrote: make(chan struct{}), stop: stop, ready: make(chan struct{})}
	if state.typeKeys([]byte("x")) != nil || state.waitReady() {
		t.Fatal("stopped terminal did not stop key typing/readiness")
	}
	if state.waitRead() || state.pause() {
		t.Fatal("stopped terminal wait helpers returned true")
	}
	if stopped(stop) == false {
		t.Fatal("stopped did not recognize a closed channel")
	}
	close(state.ready)
	state.hasKeys = true
	if state.NeverReady() {
		t.Fatal("NeverReady remained true after readiness")
	}
	if state.typeKeys(nil) != nil {
		t.Fatal("empty key input returned an error")
	}

	wrote := make(chan struct{})
	close(wrote)
	ready := &Terminal{wrote: wrote, stop: make(chan struct{}), ready: make(chan struct{})}
	if !ready.waitReady() {
		t.Fatal("waitReady did not observe terminal output")
	}
}
