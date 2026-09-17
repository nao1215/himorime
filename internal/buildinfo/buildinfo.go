// Package buildinfo reports the version yahiko was built as.
package buildinfo

import (
	"runtime/debug"
	"strings"
)

// Version is injected at link time by the release build:
//
//	-ldflags "-X github.com/nao1215/yahiko/internal/buildinfo.Version=v1.2.3"
//
// A binary built with `go install module@version` has no ldflags, so Get
// falls back to the module version recorded by the Go toolchain.
var Version = ""

// Get returns the version string shown by `yahiko version` and written to
// reports. It never returns an empty string.
func Get() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}

// Commit returns the VCS revision recorded by the Go toolchain, shortened to
// twelve characters, or "" when the binary carries none.
func Commit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var rev string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if rev != "" && dirty {
		rev += "-dirty"
	}
	return strings.TrimSpace(rev)
}
