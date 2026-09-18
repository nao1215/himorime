//go:build linux || darwin

package proc

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// control runs fn on the descriptor of f without switching f to blocking mode.
func control(f *os.File, fn func(fd int) error) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var fnErr error
	if err := rc.Control(func(fd uintptr) { fnErr = fn(int(fd)) }); err != nil {
		return err
	}
	return fnErr
}

func setTerminalSize(f *os.File) error {
	err := control(f, func(fd int) error {
		return unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Row: TerminalRows, Col: TerminalColumns})
	})
	if err != nil {
		return fmt.Errorf("set the terminal size: %w", err)
	}
	return nil
}

// pendingInput returns how many typed bytes the command has not read yet. In
// line mode a partial line does not count: the command cannot read it yet.
//
// Linux moves what is written to a pseudo-terminal into its input queue
// asynchronously, a few microseconds later or more on a busy machine, so a
// key just typed could be counted as read before it arrived. Polling the
// terminal side makes the kernel finish moving it first (n_tty_poll flushes
// the pending buffer), so the count includes every key typed so far. On
// macOS a write reaches the queue before it returns, and the poll is
// harmless.
func pendingInput(f *os.File) (int, error) {
	var n int
	err := control(f, func(fd int) error {
		if _, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 0); err != nil && !errors.Is(err, unix.EINTR) { //nolint:gosec // G115: a descriptor fits in an int32
			return err
		}
		var err error
		n, err = unix.IoctlGetInt(fd, ioctlInputQueue)
		return err
	})
	return n, err
}

// lineMode reports whether the terminal is still in line (canonical) mode.
func lineMode(f *os.File) (bool, error) {
	var canonical bool
	err := control(f, func(fd int) error {
		t, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
		if err != nil {
			return err
		}
		canonical = t.Lflag&unix.ICANON != 0
		return nil
	})
	return canonical, err
}
