//go:build !linux && !darwin

package proc

import (
	"errors"
	"testing"
)

func terminalHelper() int { return 99 }

func TestOpenTerminalNamesTheSystemWithoutOne(t *testing.T) {
	t.Parallel()
	if TerminalSupported() {
		t.Fatal("TerminalSupported() = true")
	}
	if _, err := OpenTerminal(nil, nil); !errors.Is(err, ErrTerminalUnsupported) {
		t.Fatalf("OpenTerminal() error = %v, want ErrTerminalUnsupported", err)
	}
}
