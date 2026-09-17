// Package envinfo describes the machine a benchmark ran on, for reports.
//
// It records only what helps reproduce or compare a result: operating system,
// architecture, CPU model and logical CPU count, and the Go and himorime
// versions. It never records the host name, the user, or the environment.
package envinfo

import (
	"runtime"
	"strings"

	"github.com/nao1215/himorime/internal/buildinfo"
)

// Info is the machine description written to reports.
type Info struct {
	OS              string
	Arch            string
	CPUModel        string
	LogicalCPUs     int
	GoVersion       string
	HimorimeVersion string
	// CI names the CI provider when one is detected, such as "github-actions".
	CI string
}

// Collect describes the current machine. lookup reads the environment; only
// the presence of well-known CI markers is used.
func Collect(lookup func(string) (string, bool)) Info {
	info := Info{
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		CPUModel:        strings.Join(strings.Fields(cpuModel()), " "),
		LogicalCPUs:     runtime.NumCPU(),
		GoVersion:       runtime.Version(),
		HimorimeVersion: buildinfo.Get(),
	}
	if lookup != nil {
		if v, _ := lookup("GITHUB_ACTIONS"); v == "true" {
			info.CI = "github-actions"
		} else if v, _ := lookup("CI"); v != "" && v != "false" {
			info.CI = "unknown"
		}
	}
	return info
}
