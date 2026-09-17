//go:build !linux && !windows

package proc

// resetMemoryPeak is unavailable: only Linux lets a process lower its
// recorded peak RSS.
func resetMemoryPeak() {}

// memoryPeak is unavailable: the floor comes from getrusage.
func memoryPeak() (int64, bool) { return 0, false }
