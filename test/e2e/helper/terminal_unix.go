//go:build linux || darwin

package main

import (
	"bufio"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// terminal: fail unless standard input, output and error are a terminal, as
// an interactive program would, then read one line and print it.
func terminal([]string) error {
	for fd, name := range []string{"standard input", "standard output", "standard error"} {
		if _, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ); err != nil {
			return fmt.Errorf("%s is not a terminal", name)
		}
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return fmt.Errorf("read a line: %w", err)
	}
	fmt.Print("read " + line)
	return nil
}
