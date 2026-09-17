//go:build !linux && !windows

package proc

// resetMemoryPeak is unavailable: only Linux lets a process lower its
// recorded peak RSS, and the floor comes from getrusage.
func resetMemoryPeak() (int64, bool) { return 0, false }
