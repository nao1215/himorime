//go:build linux

package proc

import (
	"context"
	"os/exec"
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

// TestRunFloorCoversWhatExecFoldsIn: a command smaller than the process that
// starts it reports that process's peak, so its peak RSS must never be above
// the recorded floor. A floor read before the start missed what the start
// itself added (the child runs in the starter's address space until exec),
// and a stripped release binary on a CI runner reported true above its floor.
func TestRunFloorCoversWhatExecFoldsIn(t *testing.T) {
	path, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true is not installed")
	}
	for _, p := range startPaths() {
		t.Run(p.name, func(t *testing.T) {
			runWith := p.run(t)
			for range 30 {
				res, err := runWith(context.Background(), Spec{Path: path, CollectUsage: true, MeasureMemory: true})
				if err != nil || res.ExitCode != 0 {
					t.Fatalf("run = %+v, %v", res, err)
				}
				if u := res.Usage; u.Floor <= 0 || u.PeakRSS > u.Floor {
					t.Fatalf("peak rss %d bytes above its floor %d: true is smaller than its starter", u.PeakRSS, u.Floor)
				}
			}
		})
	}
}
