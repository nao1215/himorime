package proc

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const terminalSupported = true

// openPTY opens a new pseudo-terminal pair through /dev/ptmx, granting and
// unlocking it as posix_openpt, grantpt and unlockpt do.
func openPTY() (master, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open a pseudo-terminal: %w", err)
	}
	var name [128]byte
	if err := control(master, func(fd int) error {
		for _, req := range []uint{unix.TIOCPTYGRANT, unix.TIOCPTYUNLK} {
			if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(req), 0); errno != 0 {
				return errno
			}
		}
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&name[0]))); errno != 0 { //nolint:gosec // G103: TIOCPTYGNAME writes the name into this buffer
			return errno
		}
		return nil
	}); err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("open a pseudo-terminal: %w", err)
	}
	path := string(name[:bytes.IndexByte(name[:], 0)])
	slave, err = os.OpenFile(path, os.O_RDWR|syscall.O_NOCTTY, 0) //nolint:gosec // G304: the terminal the kernel just allocated
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
