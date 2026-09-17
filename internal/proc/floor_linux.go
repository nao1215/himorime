//go:build linux

package proc

import (
	"bytes"
	"os"
	"strconv"
	"sync"
)

// peakFiles keeps /proc/self/clear_refs and /proc/self/statm open, so reading
// the floor before every run costs two system calls rather than six.
var peakFiles struct {
	mu        sync.Mutex
	opened    bool
	clearRefs *os.File
	statm     *os.File
	buf       [256]byte
}

// resetMemoryPeak lowers this process's recorded peak RSS (VmHWM) to its
// current RSS by writing 5 to /proc/self/clear_refs, and returns the peak
// after the reset. Linux folds that peak into the ru_maxrss of a command
// started from this process, so without the reset every earlier allocation
// spike would raise the peak reported for every later command.
//
// Right after a reset the peak equals the current RSS, which /proc/self/statm
// reports far more cheaply than VmHWM. Where the reset fails (a kernel before
// 4.0, a restricted /proc) the peak is left as it is and read from VmHWM,
// which only makes the floor higher. ok is false when neither can be read.
func resetMemoryPeak() (int64, bool) {
	peakFiles.mu.Lock()
	defer peakFiles.mu.Unlock()
	if !peakFiles.opened {
		peakFiles.opened = true
		peakFiles.clearRefs, _ = os.OpenFile("/proc/self/clear_refs", os.O_WRONLY, 0)
		peakFiles.statm, _ = os.Open("/proc/self/statm")
	}
	if peakFiles.clearRefs != nil && peakFiles.statm != nil {
		if _, err := peakFiles.clearRefs.WriteAt([]byte("5"), 0); err == nil {
			if rss, ok := statmResident(peakFiles.statm, peakFiles.buf[:]); ok {
				return rss, true
			}
		}
	}
	return vmHWM()
}

// statmResident reads the resident set size, in bytes, from /proc/self/statm:
// its second field, in pages.
func statmResident(f *os.File, buf []byte) (int64, bool) {
	n, err := f.ReadAt(buf, 0)
	if n == 0 && err != nil {
		return 0, false
	}
	fields := bytes.Fields(buf[:n])
	if len(fields) < 2 {
		return 0, false
	}
	pages, err := strconv.ParseInt(string(fields[1]), 10, 64)
	if err != nil || pages <= 0 || pages > 1<<40 {
		return 0, false
	}
	return pages * int64(os.Getpagesize()), true
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
