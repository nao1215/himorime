//go:build unix

package proc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"syscall"
	"time"
)

// spawner is himorime's handle on one spawner process.
type spawner struct {
	conn    *spawnConn
	process *os.Process
	// env is the environment of the last request, which the spawner keeps.
	env []string
}

// spawners holds the idle spawners. Run takes one per command and returns it
// afterwards, so commands run concurrently each get their own; himorime runs
// one command at a time and starts a single spawner.
var spawners struct {
	mu     sync.Mutex
	idle   []*spawner
	failed bool
}

func takeSpawner() *spawner {
	spawners.mu.Lock()
	defer spawners.mu.Unlock()
	if n := len(spawners.idle); n > 0 {
		sp := spawners.idle[n-1]
		spawners.idle = spawners.idle[:n-1]
		return sp
	}
	if spawners.failed {
		return nil
	}
	sp, err := startSpawner()
	if err != nil {
		// Starting it again for every command would fail the same way and
		// cost each run the attempt.
		spawners.failed = true
		return nil
	}
	return sp
}

// prepareSpawner starts a spawner and leaves it idle. It holds the lock while
// the spawner starts, so a command arriving meanwhile waits for this one
// instead of starting a second.
func prepareSpawner() {
	spawners.mu.Lock()
	defer spawners.mu.Unlock()
	if len(spawners.idle) > 0 || spawners.failed {
		return
	}
	sp, err := startSpawner()
	if err != nil {
		spawners.failed = true
		return
	}
	spawners.idle = append(spawners.idle, sp)
}

func returnSpawner(sp *spawner) {
	spawners.mu.Lock()
	defer spawners.mu.Unlock()
	spawners.idle = append(spawners.idle, sp)
}

// discard stops a spawner that can no longer be trusted to answer.
func (sp *spawner) discard() {
	_ = sp.process.Kill()
	sp.conn.close()
}

func spawnerExecutable() (string, error) {
	if runtime.GOOS == "linux" {
		// The running executable itself, even if its file was replaced.
		if _, err := os.Stat("/proc/self/exe"); err == nil {
			return "/proc/self/exe", nil
		}
	}
	return os.Executable()
}

func startSpawner() (*spawner, error) {
	exe, err := spawnerExecutable()
	if err != nil {
		return nil, err
	}
	syscall.ForkLock.RLock()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err == nil {
		syscall.CloseOnExec(fds[0])
		syscall.CloseOnExec(fds[1])
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("socketpair: %w", err)
	}
	local := os.NewFile(uintptr(fds[0]), "spawner")
	remote := os.NewFile(uintptr(fds[1]), "spawner")
	cmd := exec.Command(exe) //nolint:noctx,gosec // the spawner lives as long as himorime; G204: himorime's own executable
	cmd.Args = []string{os.Args[0]}
	cmd.Env = append(os.Environ(), SpawnerEnv+"=1")
	cmd.ExtraFiles = []*os.File{remote}
	// Its own process group, so a Ctrl+C aimed at himorime does not stop the
	// spawner before himorime has stopped the running command.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err = cmd.Start()
	_ = remote.Close()
	if err != nil {
		_ = local.Close()
		return nil, err
	}
	go func() { _ = cmd.Wait() }()
	sp := &spawner{conn: newSpawnConn(local), process: cmd.Process}
	hello := make(chan error, 1)
	go func() {
		var r spawnReply
		_, err := sp.conn.recv(&r)
		if err == nil && r.Version != spawnProtocol {
			err = fmt.Errorf("spawner speaks protocol %d, want %d", r.Version, spawnProtocol)
		}
		hello <- err
	}()
	timer := time.NewTimer(spawnerHandshake)
	defer timer.Stop()
	select {
	case err = <-hello:
	case <-timer.C:
		_ = sp.process.Kill()
		<-hello
		err = errors.New("the spawner did not answer")
	}
	if err != nil {
		sp.discard()
		return nil, err
	}
	return sp, nil
}

// spawnIO connects a Spec's standard streams to descriptors a spawner can
// receive. A file is passed as it is; any other reader or writer gets a pipe
// that himorime copies, as os/exec does.
type spawnIO struct {
	send        []*os.File
	afterSend   []*os.File
	afterWait   []*os.File
	copies      []func() error
	stdin       int
	stdout      int
	stderr      int
	copyResults chan error
}

func (sio *spawnIO) closeAll() {
	closeFiles(sio.afterSend)
	closeFiles(sio.afterWait)
}

func newSpawnIO(s Spec) (*spawnIO, error) {
	sio := &spawnIO{stdin: -1, stdout: -1, stderr: -1}
	add := func(f *os.File) int {
		sio.send = append(sio.send, f)
		return len(sio.send) - 1
	}
	if s.Stdin != nil {
		if f, ok := s.Stdin.(*os.File); ok {
			sio.stdin = add(f)
		} else {
			pr, pw, err := os.Pipe()
			if err != nil {
				return nil, err
			}
			sio.stdin = add(pr)
			sio.afterSend = append(sio.afterSend, pr)
			sio.afterWait = append(sio.afterWait, pw)
			r := s.Stdin
			sio.copies = append(sio.copies, func() error {
				_, err := io.Copy(pw, r)
				if errors.Is(err, syscall.EPIPE) || errors.Is(err, os.ErrClosed) {
					err = nil
				}
				if cerr := pw.Close(); err == nil && !errors.Is(cerr, os.ErrClosed) {
					err = cerr
				}
				return err
			})
		}
	}
	output := func(w io.Writer) (int, error) {
		if f, ok := w.(*os.File); ok {
			return add(f), nil
		}
		pr, pw, err := os.Pipe()
		if err != nil {
			return -1, err
		}
		sio.afterSend = append(sio.afterSend, pw)
		sio.afterWait = append(sio.afterWait, pr)
		sio.copies = append(sio.copies, func() error {
			_, err := io.Copy(w, pr)
			if errors.Is(err, os.ErrClosed) {
				err = nil
			}
			_ = pr.Close()
			return err
		})
		return add(pw), nil
	}
	var err error
	if s.Stdout != nil {
		if sio.stdout, err = output(s.Stdout); err != nil {
			sio.closeAll()
			return nil, err
		}
	}
	switch {
	case s.Stderr == nil:
	case s.Stdout != nil && interfaceEqual(s.Stderr, s.Stdout):
		sio.stderr = sio.stdout
	default:
		if sio.stderr, err = output(s.Stderr); err != nil {
			sio.closeAll()
			return nil, err
		}
	}
	return sio, nil
}

func interfaceEqual(a, b any) (equal bool) {
	defer func() {
		if recover() != nil {
			equal = false
		}
	}()
	return a == b
}

func (sio *spawnIO) startCopies() {
	sio.copyResults = make(chan error, len(sio.copies))
	for _, fn := range sio.copies {
		go func() { sio.copyResults <- fn() }()
	}
}

// wait waits for the copies, and after waitDelay closes the pipes so that a
// descendant still holding one cannot hold himorime.
func (sio *spawnIO) wait() error {
	var first error
	timer := time.NewTimer(waitDelay)
	defer timer.Stop()
	expired := false
	for range sio.copies {
		var err error
		if expired {
			err = <-sio.copyResults
		} else {
			select {
			case err = <-sio.copyResults:
			case <-timer.C:
				expired = true
				closeFiles(sio.afterWait)
				err = <-sio.copyResults
				if first == nil {
					first = exec.ErrWaitDelay
				}
			}
		}
		if err != nil && first == nil {
			first = err
		}
	}
	closeFiles(sio.afterWait)
	return first
}

// runSpawned runs s through a spawner. handled is false when no spawner could
// take the command before it was started, and the caller starts it itself.
func runSpawned(ctx context.Context, s Spec) (Result, bool, error) {
	cmd, err := command(s)
	if err != nil {
		return Result{}, true, fmt.Errorf("%w: %w", ErrStart, err)
	}
	if cmd.Err != nil {
		return Result{}, true, fmt.Errorf("%w: %w", ErrStart, cmd.Err)
	}
	select {
	case <-ctx.Done():
		return Result{Canceled: true, ExitCode: -1}, true, nil
	default:
	}
	req := spawnRequest{Path: cmd.Path, Args: cmd.Args, Dir: s.Dir, Env: s.Env, CollectUsage: s.CollectUsage, Terminal: s.Terminal}
	if req.Env == nil {
		// What os/exec gives a child without an explicit environment.
		req.Env = os.Environ()
		if s.Dir != "" {
			if pwd, err := filepath.Abs(s.Dir); err == nil {
				req.Env = append(req.Env, "PWD="+pwd)
			}
		}
	}
	if req.Dir == "" {
		// The child of himorime would run in himorime's working directory.
		if wd, err := os.Getwd(); err == nil {
			req.Dir = wd
		}
	}
	sp := takeSpawner()
	if sp == nil {
		return Result{}, false, nil
	}
	// Until the request is sent the command has not started, so a failure
	// leaves it to Run to start the command itself.
	sio, ioErr := newSpawnIO(s)
	if ioErr != nil {
		returnSpawner(sp)
		return Result{}, false, nil //nolint:nilerr // not started: Run starts it itself
	}
	req.Stdin, req.Stdout, req.Stderr = sio.stdin, sio.stdout, sio.stderr
	sent := req
	if sp.env != nil && slices.Equal(sp.env, req.Env) {
		sent.Env, sent.SameEnv = nil, true
	}
	if sendErr := sp.conn.send(sent, sio.send); sendErr != nil {
		sio.closeAll()
		sp.discard()
		return Result{}, false, nil //nolint:nilerr // not started: Run starts it itself
	}
	sp.env = req.Env
	closeFiles(sio.afterSend)
	sio.startCopies()

	// The timeout counts from the request, microseconds before the start.
	// The spawner stops the process tree when asked to.
	done := make(chan struct{})
	watcherDone := make(chan struct{})
	killed := make(chan killReason, 1)
	go func() {
		defer close(watcherDone)
		watch(ctx, s.Timeout, done, killed, func() {
			_ = sp.conn.send(spawnRequest{Kill: true}, nil)
		})
	}()
	var reply spawnReply
	_, recvErr := sp.conn.recv(&reply)
	if recvErr == nil && !reply.Done && reply.StartErr == nil {
		recvErr = errors.New("the spawner sent an unexpected message")
	}
	close(done)
	<-watcherDone
	copyErr := sio.wait()
	if recvErr != nil {
		// Whether the command started is unknown, so it is not started again.
		sp.discard()
		return Result{ExitCode: -1}, true, fmt.Errorf("wait for process: the spawner stopped: %w", recvErr)
	}
	returnSpawner(sp)
	if reply.StartErr != nil {
		return Result{}, true, fmt.Errorf("%w: %w", ErrStart, reply.StartErr.err())
	}

	res := Result{Elapsed: time.Duration(reply.Elapsed), ExitCode: reply.ExitCode, Usage: notCollected()}
	if s.CollectUsage {
		raw := rawUsage{Missing: "the spawner returned no resource usage"}
		if reply.Usage != nil {
			raw = *reply.Usage
		}
		res.Usage = usageFromRaw(raw)
		res.Usage.Floor = reply.Floor
	}
	select {
	case r := <-killed:
		res.TimedOut = r == killTimeout
		res.Canceled = r == killCancel
	default:
	}
	switch {
	case reply.WaitErr != "":
		return res, true, fmt.Errorf("wait for process: %s", reply.WaitErr)
	case copyErr != nil && reply.ExitCode == 0:
		return res, true, fmt.Errorf("wait for process: %w", copyErr)
	}
	return res, true, nil
}
