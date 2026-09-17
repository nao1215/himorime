package proc

import (
	"os"
	"sync/atomic"
)

// SpawnerEnv selects the spawner mode of the himorime executable. It is
// internal: himorime sets it only in the environment of the spawner it starts
// itself, and main checks it before doing anything else.
const SpawnerEnv = "HIMORIME_INTERNAL_SPAWNER"

// spawnerEnabled is set by EnableSpawner.
var spawnerEnabled atomic.Bool

// EnableSpawner makes Run start measured commands from a spawner: a small,
// long-lived copy of the running executable in spawner mode, started on first
// use. On Unix the kernel folds the peak RSS of the process that starts a
// command into the command's peak RSS, so starting commands from a process
// that stays small keeps that floor low. The spawner mode is selected by this
// package's init, so any executable importing this package can enable it.
// Where the spawner cannot be started, Run starts commands itself.
func EnableSpawner() { spawnerEnabled.Store(true) }

// PrepareSpawner starts a spawner in the background, when EnableSpawner was
// called and none is idle, so that it is up by the time the first command
// whose memory is measured runs. Without it, that command starts it.
func PrepareSpawner() {
	if spawnerEnabled.Load() {
		go prepareSpawner()
	}
}

// IsSpawner reports whether this process was started as a spawner.
func IsSpawner() bool {
	return spawnerSupported && os.Getenv(SpawnerEnv) == "1"
}

// init turns a process started as a spawner into one before the packages
// that import this one initialize: their package variables (the JSON schema
// compiler alone allocates 1.7MiB) would add to the spawner's RSS, which is
// the floor of every command it starts.
func init() { //nolint:gochecknoinits // the spawner mode must be selected before other packages initialize
	if IsSpawner() {
		os.Exit(ServeSpawner())
	}
}
