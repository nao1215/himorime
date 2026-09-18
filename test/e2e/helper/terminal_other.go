//go:build !linux && !darwin

package main

import "errors"

// terminal: there is no terminal to check here; scenarios that use it skip
// these systems.
func terminal([]string) error {
	return errors.New("terminal is only implemented on Linux and macOS")
}
