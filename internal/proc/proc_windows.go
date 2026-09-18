//go:build windows

package proc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// configure starts the child in its own process group, so that a console
// Ctrl+C aimed at himorime is handled by himorime, which then stops the job, and
// suspended, so that attach can put it in its Job Object before it runs.
// There is no terminal on Windows: OpenTerminal fails before a run starts.
func configure(cmd *exec.Cmd, _ bool) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED
}

// suspendedUntilAttached is true: the process is created suspended and
// resumed by attach.
const suspendedUntilAttached = true

type tree struct {
	job windows.Handle
}

// attach assigns the suspended process to a new Job Object and then resumes
// it. The process runs nothing before it belongs to the job, so every process
// it starts inherits the job, and terminating the job stops the whole tree.
// Every error is a real failure (access denied, a job that forbids nesting):
// the process cannot have exited on its own while suspended. On failure the
// job is released and the caller kills the still suspended process.
func attach(cmd *exec.Cmd) (*tree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	// SetInformationJobObject takes the structure by address; the pointer is
	// only valid for this call and info outlives it.
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil { //nolint:gosec // G103: audited, see above
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("configure job object: %w", err)
	}
	pid := uint32(cmd.Process.Pid) //nolint:gosec // a PID always fits in uint32
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("open process: %w", err)
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("assign process to job object: %w", err)
	}
	if err := resumeProcess(pid); err != nil {
		// The job holds the process now; closing it kills the process.
		_ = windows.CloseHandle(job)
		return nil, err
	}
	return &tree{job: job}, nil
}

// resumeProcess resumes the threads of a process created suspended. os/exec
// closes the thread handle CreateProcess returned, so the thread is found
// through a Toolhelp snapshot; a process created suspended has exactly one
// thread, and it runs no code before it is resumed.
func resumeProcess(pid uint32) error {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("resume process: snapshot threads: %w", err)
	}
	defer windows.CloseHandle(snap)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	resumed := 0
	for err = windows.Thread32First(snap, &entry); err == nil; err = windows.Thread32Next(snap, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		th, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return fmt.Errorf("resume process: open thread %d: %w", entry.ThreadID, err)
		}
		_, rerr := windows.ResumeThread(th)
		_ = windows.CloseHandle(th)
		if rerr != nil {
			return fmt.Errorf("resume process: resume thread %d: %w", entry.ThreadID, rerr)
		}
		resumed++
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return fmt.Errorf("resume process: list threads: %w", err)
	}
	if resumed == 0 {
		return fmt.Errorf("resume process: process %d has no thread to resume", pid)
	}
	return nil
}

func (t *tree) kill() error {
	if t.job == 0 {
		return nil
	}
	err := windows.TerminateJobObject(t.job, 1)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return nil
	}
	return err
}

// close stops any process of the tree that outlived the command, waits until
// the job is empty so no file in a temporary directory is still open, and
// releases the job.
func (t *tree) close() {
	if t.job == 0 {
		return
	}
	_ = t.kill()
	deadline := time.Now().Add(jobDrainTimeout)
	for time.Now().Before(deadline) && activeProcesses(t.job) > 0 {
		time.Sleep(10 * time.Millisecond) //nolint:forbidigo // polling the job; Windows offers no wait for an empty job without a completion port
	}
	_ = windows.CloseHandle(t.job)
	t.job = 0
}

const jobDrainTimeout = 5 * time.Second

// jobAccounting mirrors JOBOBJECT_BASIC_ACCOUNTING_INFORMATION.
type jobAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

func jobAccountingInfo(job windows.Handle) (jobAccounting, error) {
	var info jobAccounting
	err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil) //nolint:gosec // G103: the structure outlives the call
	return info, err
}

func activeProcesses(job windows.Handle) uint32 {
	info, err := jobAccountingInfo(job)
	if err != nil {
		return 0
	}
	return info.ActiveProcesses
}

func shellCommand(script string) (*exec.Cmd, error) {
	comspec := os.Getenv("ComSpec")
	if comspec == "" {
		comspec = filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	}
	cmd := exec.Command(comspec) //nolint:noctx,gosec // %ComSpec% is the system shell; the tree is stopped by Run's watcher
	// cmd.exe does not parse its command line with the C runtime rules, so the
	// line is passed verbatim: /s strips exactly the outer quotes.
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `/d /s /c "` + script + `"`}
	return cmd, nil
}

// ShellQuote quotes s for cmd.exe. cmd.exe expands %VAR% even inside quotes
// and has no escape for a double quote within quotes, so a value holding
// either is refused instead of being passed on with a changed meaning.
func ShellQuote(s string) (string, error) {
	if strings.ContainsAny(s, "\"%\r\n") {
		return "", errors.New("a value substituted into a cmd.exe command must not contain \", %, or a line break")
	}
	// A trailing backslash would escape the closing quote for a program that
	// parses its command line with the C runtime rules.
	if strings.HasSuffix(s, `\`) {
		return "", errors.New("a value substituted into a cmd.exe command must not end with a backslash")
	}
	return `"` + s + `"`, nil
}

// ShellName names the shell `shell: true` commands run through.
func ShellName() string { return "cmd.exe /d /s /c" }

func hasSeparator(name string) bool { return strings.ContainsAny(name, `/\:`) }

func isPathVar(k string) bool { return strings.EqualFold(k, "PATH") }

func lookPathIn(name, pathList, workdir string) (string, error) {
	exts := []string{""}
	if filepath.Ext(name) == "" {
		exts = []string{".com", ".exe", ".bat", ".cmd"}
		if pe := os.Getenv("PATHEXT"); pe != "" {
			exts = strings.Split(strings.ToLower(pe), ";")
		}
	}
	for _, dir := range filepath.SplitList(pathList) {
		if dir == "" {
			continue
		}
		for _, ext := range exts {
			p := filepath.Join(pathEntry(dir, workdir), name+ext)
			if info, err := os.Stat(p); err == nil && !info.IsDir() { //nolint:gosec // G703: probing PATH entries is what a PATH lookup is
				return p, nil
			}
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}
