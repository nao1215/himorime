//go:build !linux && !darwin

package proc

import "os"

const terminalSupported = false

func openPTY() (master, slave *os.File, err error) {
	return nil, nil, ErrTerminalUnsupported
}
