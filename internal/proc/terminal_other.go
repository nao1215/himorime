//go:build !linux && !darwin

package proc

import "os"

const terminalSupported = false

func openPTY() (master, slave *os.File, err error) {
	return nil, nil, ErrTerminalUnsupported
}

func pendingInput(*os.File) (int, error) { return 0, ErrTerminalUnsupported }

func lineMode(*os.File) (bool, error) { return false, ErrTerminalUnsupported }
