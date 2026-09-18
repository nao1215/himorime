//go:build linux || darwin

package proc

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

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
	time.Sleep(200 * time.Millisecond)
	t, err := unix.IoctlGetTermios(0, getTermios)
	if err != nil {
		return 23
	}
	t.Lflag &^= unix.ICANON | unix.ECHO
	t.Cc[unix.VMIN], t.Cc[unix.VTIME] = 1, 0
	if err := unix.IoctlSetTermios(0, setTermios, t); err != nil {
		return 24
	}
	var got []byte
	b := make([]byte, 1)
	for {
		if _, err := os.Stdin.Read(b); err != nil {
			return 25
		}
		if b[0] == 'q' {
			break
		}
		got = append(got, b[0])
	}
	fmt.Printf("got:%q\n", got)
	return 0
}

func TestRunOnATerminalGivesTheCommandAControllingTerminal(t *testing.T) {
	t.Parallel()
	for _, path := range startPaths() {
		t.Run(path.name, func(t *testing.T) {
			t.Parallel()
			runWith := path.run(t)
			var out bytes.Buffer
			term, err := OpenTerminal(strings.NewReader("abc\nq"), &out)
			if err != nil {
				t.Fatal(err)
			}
			s := helper(t, "terminal")
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
			// Typed while the terminal was in line mode, read in raw mode: the
			// complete line and the key after it both arrive.
			if !strings.Contains(out.String(), `got:"abc\n"`) {
				t.Fatalf("output = %q, want the helper to have read %q", out.String(), "abc\n")
			}
		})
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
