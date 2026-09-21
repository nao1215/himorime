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
	"unicode/utf8"
)

// The size of every terminal a command runs on. It is fixed so that a program
// that lays out its output for the terminal does the same work in every run.
const (
	TerminalColumns = 80
	TerminalRows    = 24
)

const (
	// terminalDrain bounds how long Close waits for the output of a command
	// that has exited. Only a descendant still holding the terminal makes it
	// wait.
	terminalDrain = 2 * time.Second
	// typingPoll is how often the typist looks at the terminal while it waits
	// for the command to be ready or to read a key.
	typingPoll = 20 * time.Microsecond
)

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

	// wrote is closed once the command has written anything.
	wrote chan struct{}
	// stop is closed by Close, and ends the typing.
	stop chan struct{}
	// ready is closed once the typist found the command ready for keys.
	ready   chan struct{}
	hasKeys bool
	outDone chan error
	inDone  chan error
	once    sync.Once
	err     error
}

// OpenTerminal allocates a pseudo-terminal of TerminalColumns by TerminalRows,
// starts copying what the command writes to output (discarded when output is
// nil) and starts typing input (when it is not nil) the way a person does:
// one key at a time, the first once the command is ready and each next one
// once the command has read the one before. See type for the details. Both
// run outside the command's process tree.
func OpenTerminal(input io.Reader, output io.Writer) (*Terminal, error) {
	var keys []byte
	if input != nil {
		var err error
		if keys, err = io.ReadAll(input); err != nil {
			return nil, fmt.Errorf("read the input to type: %w", err)
		}
	}
	master, slave, err := openPTY()
	if err != nil {
		return nil, err
	}
	if output == nil {
		output = io.Discard
	}
	t := &Terminal{
		master: master, slave: slave,
		wrote: make(chan struct{}), stop: make(chan struct{}), ready: make(chan struct{}),
		hasKeys: len(keys) > 0,
		outDone: make(chan error, 1), inDone: make(chan error, 1),
	}
	go func() { t.outDone <- t.copyOutput(output) }()
	go func() { t.inDone <- t.typeKeys(keys) }()
	return t, nil
}

// File is the terminal side, to be given to the command as its standard
// input, output and error.
func (t *Terminal) File() *os.File { return t.slave }

// NeverReady reports that there was input to type, but the command never
// wrote to the terminal nor left line mode, so none of it was typed. A
// command that reads a line without a prompt waits for its input forever.
func (t *Terminal) NeverReady() bool { return t.hasKeys && !stopped(t.ready) }

// Close is called once the command has exited. It stops typing, closes the
// terminal side, waits for what the command wrote to be copied, and closes
// the rest. It is safe to call more than once.
func (t *Terminal) Close() error {
	t.once.Do(func() {
		close(t.stop)
		_ = t.slave.Close()
		var outErr error
		select {
		case outErr = <-t.outDone:
		case <-time.After(terminalDrain):
		}
		// Closing the controlling side hangs the terminal up, which also stops
		// a descendant that kept it open, and ends a key still being typed.
		_ = t.master.Close()
		t.err = errors.Join(outErr, <-t.inDone)
	})
	return t.err
}

// copyOutput copies until the terminal side is closed. Linux reports that as
// EIO rather than EOF.
func (t *Terminal) copyOutput(w io.Writer) error {
	buf := make([]byte, 32<<10)
	wrote := false
	for {
		n, err := t.master.Read(buf)
		if n > 0 {
			if !wrote {
				wrote = true
				close(t.wrote)
			}
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, syscall.EIO) || errors.Is(err, os.ErrClosed) {
				return nil
			}
			return err
		}
	}
}

// typeKeys types keys one at a time. It starts once the command is ready,
// which is when it has written to the terminal or switched it out of line
// mode, as a person waits for a prompt. It types each next key once the
// command has read the ones before; in line mode that is once it has read the
// line, since a program cannot read part of one. Waiting keeps every key a
// read of its own, so a program that does work per key (highlighting,
// completion, redrawing) does it for every key, as it does for a person.
func (t *Terminal) typeKeys(keys []byte) error {
	if len(keys) == 0 {
		return nil
	}
	if !t.waitReady() {
		return nil
	}
	close(t.ready)
	for len(keys) > 0 {
		n := keyLength(keys)
		if _, err := t.master.Write(keys[:n]); err != nil {
			if stopped(t.stop) || errors.Is(err, syscall.EIO) || errors.Is(err, os.ErrClosed) {
				return nil
			}
			return err
		}
		key := keys[:n]
		keys = keys[n:]
		canonical, err := lineMode(t.slave)
		if err != nil {
			return nil //nolint:nilerr // the terminal side is closed: the run has ended
		}
		if canonical && key[0] != '\n' && key[0] != '\r' && key[0] != 0x04 {
			// In line mode the command reads whole lines, and the terminal
			// edits the line being typed, as it does for a person.
			continue
		}
		if !t.waitRead() {
			return nil
		}
	}
	return nil
}

// waitReady waits until the command has written to the terminal or left line
// mode. It returns false when the run ended first.
func (t *Terminal) waitReady() bool {
	for {
		select {
		case <-t.wrote:
			return true
		case <-t.stop:
			return false
		default:
		}
		canonical, err := lineMode(t.slave)
		if err != nil {
			return false
		}
		if !canonical {
			return true
		}
		if !t.pause() {
			return false
		}
	}
}

// waitRead waits until the command has read everything typed. It returns
// false when the run ended first.
func (t *Terminal) waitRead() bool {
	for {
		if stopped(t.stop) {
			return false
		}
		n, err := pendingInput(t.slave)
		if err != nil {
			return false
		}
		if n == 0 {
			return true
		}
		if !t.pause() {
			return false
		}
	}
}

// pause waits typingPoll. It returns false when the run ended meanwhile. The
// poll is short enough for the timer to be ready by the time the select runs,
// and select picks among ready cases at random, so the stop is checked again
// after the timer fires.
func (t *Terminal) pause() bool {
	timer := time.NewTimer(typingPoll)
	defer timer.Stop()
	select {
	case <-t.stop:
		return false
	case <-timer.C:
		return !stopped(t.stop)
	}
}

func stopped(c chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// keyLength returns the length of the key at the start of b: an escape
// sequence as a terminal sends for an arrow or function key (ESC [ ... final,
// ESC O x), Alt with a character (ESC x), or one UTF-8 character.
func keyLength(b []byte) int {
	if b[0] == 0x1b && len(b) > 1 {
		switch b[1] {
		case '[':
			for i := 2; i < len(b); i++ {
				if b[i] >= 0x40 && b[i] <= 0x7e {
					return i + 1
				}
			}
			return len(b)
		case 'O':
			return min(3, len(b))
		case 0x1b:
			return 1
		default:
			_, size := utf8.DecodeRune(b[1:])
			return 1 + size
		}
	}
	_, size := utf8.DecodeRune(b)
	return size
}
