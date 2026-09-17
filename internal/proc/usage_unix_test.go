//go:build !windows

package proc

import (
	"runtime"
	"syscall"
	"testing"
	"time"
)

// TestNormalizeMaxRSS pins the unit of ru_maxrss per platform: bytes on
// macOS, kilobytes elsewhere. Treating one as the other would misreport
// memory by a factor of 1024.
func TestNormalizeMaxRSS(t *testing.T) {
	t.Parallel()
	want := int64(1024)
	if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		want = 1
	}
	if maxRSSUnit != want {
		t.Fatalf("maxRSSUnit on %s = %d, want %d", runtime.GOOS, maxRSSUnit, want)
	}
	got, err := normalizeMaxRSS(2048)
	if err != nil || got != 2048*want {
		t.Fatalf("normalizeMaxRSS(2048) = %d, %v", got, err)
	}
	for _, raw := range []int64{0, -1} {
		if _, err := normalizeMaxRSS(raw); err == nil {
			t.Errorf("normalizeMaxRSS(%d) accepted a peak that no process can have", raw)
		}
	}
	if maxRSSUnit != 1 {
		if _, err := normalizeMaxRSS(1 << 60); err == nil {
			t.Error("an overflowing ru_maxrss was accepted")
		}
	}
}

func TestUsageFromRusage(t *testing.T) {
	t.Parallel()
	ru := &syscall.Rusage{}
	ru.Utime.Sec, ru.Utime.Usec = 1, 500000
	ru.Stime.Usec = 250000
	ru.Maxrss = 10
	u := usageFromRusage(ru)
	if u.CPUErr != nil || u.MemoryErr != nil {
		t.Fatalf("errors: %v %v", u.CPUErr, u.MemoryErr)
	}
	if u.UserCPU != 1500*time.Millisecond || u.SystemCPU != 250*time.Millisecond {
		t.Fatalf("cpu = %v / %v", u.UserCPU, u.SystemCPU)
	}
	if u.PeakRSS != 10*maxRSSUnit {
		t.Fatalf("peak rss = %d", u.PeakRSS)
	}
	ru.Maxrss = 0
	if u := usageFromRusage(ru); u.MemoryErr == nil || IsUnsupported(u.MemoryErr) || u.CPUErr != nil {
		t.Fatalf("a zero ru_maxrss must be a collection failure, not unsupported: %+v", u)
	}
}

// selfCPU is the user and system CPU time this process has used.
func selfCPU() time.Duration {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
