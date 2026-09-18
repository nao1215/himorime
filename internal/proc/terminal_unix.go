//go:build linux || darwin

package proc

import (
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
