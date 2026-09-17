//go:build !unix

package proc

// ownerHelper is only used by the spawner tests, which need Unix.
func ownerHelper() int { return 99 }
