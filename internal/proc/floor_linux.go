//go:build linux

package proc

import (
	"bytes"
	"os"
	"strconv"
	"sync"
)

// peakFiles keeps /proc/self/clear_refs open, so resetting the peak before
// every run is one system call.
var peakFiles struct {
	mu        sync.Mutex
	opened    bool
	clearRefs *os.File
}

// resetMemoryPeak lowers this process's recorded peak RSS (VmHWM) to its
// current RSS by writing 5 to /proc/self/clear_refs. Linux folds that peak
// into the ru_maxrss of a command started from this process, so without the
// reset every earlier allocation spike would raise the peak reported for
// every later command. Where the reset fails (a kernel before 4.0, a
// restricted /proc) the peak is left as it is, which only makes the floor
// higher.
func resetMemoryPeak() {
	peakFiles.mu.Lock()
	defer peakFiles.mu.Unlock()
	if !peakFiles.opened {
		peakFiles.opened = true
		peakFiles.clearRefs, _ = os.OpenFile("/proc/self/clear_refs", os.O_WRONLY, 0)
	}
	if peakFiles.clearRefs != nil {
		_, _ = peakFiles.clearRefs.WriteAt([]byte("5"), 0)
	}
}

// memoryPeak returns VmHWM, the peak RSS of this process's address space
// since the last reset. Read after a command was started and reaped, it
// covers the moment of exec, when Linux folds it into the command's
// ru_maxrss.
func memoryPeak() (int64, bool) {
	return vmHWM()
}

// vmHWM reads VmHWM, the peak RSS of this process's address space, which is
// what Linux folds into a started command's ru_maxrss. getrusage would not
// do: its ru_maxrss also keeps the peak of the process that started this one,
// which a command does not inherit and clear_refs does not reset.
func vmHWM() (int64, bool) {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, false
	}
	for line := range bytes.Lines(data) {
		rest, ok := bytes.CutPrefix(line, []byte("VmHWM:"))
		if !ok {
			continue
		}
		kib, ok := bytes.CutSuffix(bytes.TrimSpace(rest), []byte("kB"))
		if !ok {
			return 0, false
		}
		v, err := strconv.ParseInt(string(bytes.TrimSpace(kib)), 10, 64)
		if err != nil || v <= 0 || v > 1<<53 {
			return 0, false
		}
		return v * 1024, true
	}
	return 0, false
}
