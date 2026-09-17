package envinfo

import (
	"runtime"
	"testing"
)

func TestCollect(t *testing.T) {
	t.Parallel()
	env := map[string]string{"GITHUB_ACTIONS": "true", "HOSTNAME": "secret-host"}
	info := Collect(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if info.OS != runtime.GOOS || info.Arch != runtime.GOARCH {
		t.Fatalf("OS/Arch = %s/%s", info.OS, info.Arch)
	}
	if info.LogicalCPUs < 1 {
		t.Fatalf("LogicalCPUs = %d", info.LogicalCPUs)
	}
	if info.CI != "github-actions" {
		t.Fatalf("CI = %q", info.CI)
	}
	if info.HimorimeVersion == "" || info.GoVersion == "" {
		t.Fatalf("versions missing: %+v", info)
	}
}

func TestCollectOutsideCI(t *testing.T) {
	t.Parallel()
	info := Collect(func(string) (string, bool) { return "", false })
	if info.CI != "" {
		t.Fatalf("CI = %q outside CI", info.CI)
	}
	generic := Collect(func(k string) (string, bool) {
		if k == "CI" {
			return "1", true
		}
		return "", false
	})
	if generic.CI != "unknown" {
		t.Fatalf("CI = %q with CI=1", generic.CI)
	}
}
