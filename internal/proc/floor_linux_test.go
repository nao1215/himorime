//go:build linux

package proc

import (
	"context"
	"runtime"
	"testing"
)

// TestRunPeakRSSIsNotRaisedByHimorimesOwnMemory: Linux folds the peak RSS of
// the process that starts a command into the command's ru_maxrss at exec, so
// a command could never be reported below himorime's own peak. With 200MiB
// resident in this process, a helper that exits at once must still report a
// small peak and a small floor.
func TestRunPeakRSSIsNotRaisedByHimorimesOwnMemory(t *testing.T) {
	const held = 200 << 20
	buf := make([]byte, held)
	for i := 0; i < len(buf); i += 4096 {
		buf[i] = 1
	}
	s := helper(t, "exit")
	s.CollectUsage, s.MeasureMemory = true, true
	res, err := Run(context.Background(), s, nil)
	runtime.KeepAlive(buf)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("run = %+v, %v", res, err)
	}
	u := res.Usage
	if u.MemoryErr != nil {
		t.Fatalf("memory: %v", u.MemoryErr)
	}
	const limit = 50 << 20
	if u.PeakRSS >= limit {
		t.Errorf("peak rss of a helper that exits at once = %d bytes while this process holds %d; the spawner's memory leaked into it", u.PeakRSS, held)
	}
	if u.Floor <= 0 || u.Floor >= limit {
		t.Errorf("floor = %d bytes, want a positive value below %d", u.Floor, limit)
	}
}
