//go:build windows

package proc

import (
	"errors"
	"fmt"
	"os/exec"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// usageProbe holds a handle to the started process, opened right after it
// started. The handle keeps the process object, and with it the counters
// Windows keeps for it, readable after the process has exited.
type usageProbe struct {
	process windows.Handle
}

func newUsageProbe(cmd *exec.Cmd, _ *tree) *usageProbe {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(cmd.Process.Pid)) //nolint:gosec // a PID always fits in uint32
	if err != nil {
		return &usageProbe{}
	}
	return &usageProbe{process: h}
}

func (p *usageProbe) close() {
	if p.process != 0 {
		_ = windows.CloseHandle(p.process)
		p.process = 0
	}
}

func platformCapabilities() (cpu, memory error) { return nil, nil }

// collect reads the usage of the tree. CPU time comes from the Job Object's
// accounting, which covers every process that was ever part of the job, so it
// includes descendants whether or not anyone waited for them. Windows keeps
// no peak working set for a job, only per process, and a descendant's handle
// is gone by the time the command exits; peak RSS is therefore the peak
// working set of the started process and is reported only when the job ran
// that single process. A larger tree reports peak RSS as unsupported instead
// of under-reporting it.
func (p *usageProbe) collect(_ *exec.Cmd, t *tree) Usage {
	var u Usage
	if t != nil && t.job != 0 {
		acct, err := jobAccountingInfo(t.job)
		if err != nil {
			u.CPUErr = fmt.Errorf("query job accounting: %w", err)
			u.MemoryErr = u.CPUErr
			return u
		}
		u.UserCPU = filetimeUnits(acct.TotalUserTime)
		u.SystemCPU = filetimeUnits(acct.TotalKernelTime)
		u.Processes = int(acct.TotalProcesses)
	} else {
		// The process exited before it could be assigned to a job, within
		// microseconds of starting; only its own counters exist.
		user, kernel, err := p.processTimes()
		if err != nil {
			u.CPUErr = err
			u.MemoryErr = err
			return u
		}
		u.UserCPU, u.SystemCPU, u.Processes = user, kernel, 1
	}
	if u.Processes > 1 {
		u.MemoryErr = &UnsupportedError{
			What:   "peak rss",
			Reason: fmt.Sprintf("the command ran %d processes, and Windows records a peak working set only per process", u.Processes),
		}
		return u
	}
	u.PeakRSS, u.MemoryErr = p.peakWorkingSet()
	return u
}

func (p *usageProbe) processTimes() (user, kernel time.Duration, err error) {
	if p.process == 0 {
		return 0, 0, errors.New("no handle to the process to read its CPU time")
	}
	var creation, exit, k, us windows.Filetime
	if err := windows.GetProcessTimes(p.process, &creation, &exit, &k, &us); err != nil {
		return 0, 0, fmt.Errorf("GetProcessTimes: %w", err)
	}
	return filetimeDuration(us), filetimeDuration(k), nil
}

// processMemoryCounters mirrors PROCESS_MEMORY_COUNTERS.
type processMemoryCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

var procGetProcessMemoryInfo = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

func (p *usageProbe) peakWorkingSet() (int64, error) {
	if p.process == 0 {
		return 0, errors.New("no handle to the process to read its peak working set")
	}
	var c processMemoryCounters
	c.cb = uint32(unsafe.Sizeof(c))
	r, _, callErr := procGetProcessMemoryInfo.Call(uintptr(p.process), uintptr(unsafe.Pointer(&c)), uintptr(c.cb)) //nolint:gosec // G103: the structure outlives the call
	if r == 0 {
		return 0, fmt.Errorf("GetProcessMemoryInfo: %w", callErr)
	}
	if c.PeakWorkingSetSize == 0 {
		return 0, errors.New("the operating system reported a peak working set of 0")
	}
	return int64(c.PeakWorkingSetSize), nil //nolint:gosec // a working set fits in int64
}

// filetimeUnits converts a count of 100-nanosecond intervals.
func filetimeUnits(v int64) time.Duration { return time.Duration(v) * 100 }

// filetimeDuration converts a FILETIME that holds a duration, not a date.
func filetimeDuration(ft windows.Filetime) time.Duration {
	return filetimeUnits(int64(ft.HighDateTime)<<32 | int64(ft.LowDateTime))
}
