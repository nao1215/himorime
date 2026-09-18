package proc

import (
	"fmt"
	"os"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

const terminalSupported = true

// openPTY opens a new pseudo-terminal pair through /dev/ptmx. The ioctls go
// through SyscallConn so that the controlling side stays non-blocking and a
// read waiting on it returns when it is closed.
func openPTY() (master, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open a pseudo-terminal: %w", err)
	}
	var n uint32
	if err := control(master, func(fd int) error {
		if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
			return err
		}
		n, err = unix.IoctlGetUint32(fd, unix.TIOCGPTN)
		return err
	}); err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("open a pseudo-terminal: %w", err)
	}
	slave, err = os.OpenFile("/dev/pts/"+strconv.FormatUint(uint64(n), 10), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("open a pseudo-terminal: %w", err)
	}
	if err := setTerminalSize(slave); err != nil {
		_ = master.Close()
		_ = slave.Close()
		return nil, nil, err
	}
	return master, slave, nil
}
