package proc

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
)

// The size of every terminal a command runs on. It is fixed so that a program
// that lays out its output for the terminal does the same work in every run.
const (
	TerminalColumns = 80
	TerminalRows    = 24
)

// terminalDrain bounds how long Close waits for the output of a command that
// has exited. Only a descendant still holding the terminal makes it wait.
const terminalDrain = 2 * time.Second

// ErrTerminalUnsupported is returned by OpenTerminal where himorime cannot
// allocate a pseudo-terminal.
var ErrTerminalUnsupported = fmt.Errorf("terminal: true needs a pseudo-terminal, which himorime provides on Linux and macOS, not on %s", runtime.GOOS)

// TerminalSupported reports whether OpenTerminal can work on this system.
func TerminalSupported() bool { return terminalSupported }

// Terminal is a pseudo-terminal for one run of a command. The command gets
// the terminal side as its standard input, output and error; himorime keeps
// the other side, types the input into it and reads what the command writes.
type Terminal struct {
	master *os.File
	slave  *os.File

	outDone chan error
	inDone  chan error
	once    sync.Once
	err     error
}

// OpenTerminal allocates a pseudo-terminal of TerminalColumns by TerminalRows
// and starts typing input into it (when input is not nil) and copying what the
// command writes to output (discarded when output is nil). Both copies run
// outside the command's process tree.
func OpenTerminal(input io.Reader, output io.Writer) (*Terminal, error) {
	master, slave, err := openPTY()
	if err != nil {
		return nil, err
	}
	if output == nil {
		output = io.Discard
	}
	t := &Terminal{master: master, slave: slave, outDone: make(chan error, 1), inDone: make(chan error, 1)}
	go func() { t.outDone <- copyTerminalOutput(output, master) }()
	if input == nil {
		t.inDone <- nil
	} else {
		go func() {
			_, err := io.Copy(master, input)
			if errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EIO) {
				err = nil
			}
			t.inDone <- err
		}()
	}
	return t, nil
}

// File is the terminal side, to be given to the command as its standard
// input, output and error.
func (t *Terminal) File() *os.File { return t.slave }

// Close is called once the command has exited. It closes the terminal side,
// waits for what the command wrote to be copied, and closes the rest. It is
// safe to call more than once.
func (t *Terminal) Close() error {
	t.once.Do(func() {
		_ = t.slave.Close()
		var outErr error
		select {
		case outErr = <-t.outDone:
		case <-time.After(terminalDrain):
		}
		// Closing the controlling side hangs the terminal up, which also stops
		// a descendant that kept it open.
		_ = t.master.Close()
		inErr := <-t.inDone
		t.err = errors.Join(outErr, inErr)
	})
	return t.err
}

// copyTerminalOutput copies until the terminal side is closed. Linux reports
// that as EIO rather than EOF.
func copyTerminalOutput(w io.Writer, master *os.File) error {
	_, err := io.Copy(w, master)
	if errors.Is(err, syscall.EIO) || errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}
