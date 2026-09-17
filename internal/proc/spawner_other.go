//go:build !unix

package proc

import "context"

// spawnerSupported is false: Windows reads the peak working set of the
// started process itself, which the process that starts it does not raise,
// so commands are started by himorime directly.
const spawnerSupported = false

// ServeSpawner is never called where spawnerSupported is false.
func ServeSpawner() int { return 2 }

func prepareSpawner() {}

func runSpawned(context.Context, Spec) (Result, bool, error) {
	return Result{}, false, nil
}
