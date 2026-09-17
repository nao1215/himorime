//go:build unix

package proc

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// spawnerSupported is true: on Unix the peak RSS of the process that starts a
// command is folded into the command's ru_maxrss at exec, so commands are
// started from a spawner that stays small.
const spawnerSupported = true

// The spawner protocol. himorime and its spawner are the same executable, so
// the version only guards against a spawner started from a different build
// of it.
//
// Every message is a 4-byte big-endian length and a JSON body, over a Unix
// socket pair. A request carries the command's standard input, output and
// error as descriptors (SCM_RIGHTS) on its length. The spawner answers it once:
// with a start failure, or after it reaped the command. While the command
// runs, himorime may send a kill request, and the spawner stops the command's
// process group. A run therefore costs one message each way.
const (
	spawnProtocol = 1
	// spawnerFD is where the spawner finds its end of the socket pair.
	spawnerFD = 3
	// maxMessage bounds a message; a request holds the whole environment.
	maxMessage = 64 << 20
	// spawnerHandshake bounds how long a new spawner may take to answer.
	spawnerHandshake = 10 * time.Second
)

type spawnRequest struct {
	// Kill asks the spawner to stop the running command's process tree; a
	// kill request carries nothing else.
	Kill bool     `json:"kill,omitempty"`
	Path string   `json:"path"`
	Args []string `json:"args"`
	Dir  string   `json:"dir"`
	Env  []string `json:"env"`
	// SameEnv reuses the environment of the previous request, so the
	// environment is not encoded again for every run of a benchmark.
	SameEnv bool `json:"same_env,omitempty"`
	// Stdin, Stdout and Stderr index the descriptors sent with the request;
	// -1 is the null device.
	Stdin        int  `json:"stdin"`
	Stdout       int  `json:"stdout"`
	Stderr       int  `json:"stderr"`
	CollectUsage bool `json:"collect_usage"`
}

type spawnReply struct {
	// Version is set only in the spawner's first message.
	Version int `json:"version,omitempty"`
	// StartErr is set when the command could not be started.
	StartErr *spawnError `json:"start_error,omitempty"`
	// Done is set once the spawner reaped the command.
	Done bool `json:"done,omitempty"`
	// Elapsed, in nanoseconds, runs from just before the start until the
	// spawner reaped the command.
	Elapsed  int64  `json:"elapsed_ns,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
	WaitErr  string `json:"wait_error,omitempty"`
	// Usage is the rusage of the command and Floor the spawner's own peak RSS
	// just before it started the command, when usage was requested.
	Usage *rawUsage `json:"usage,omitempty"`
	Floor int64     `json:"floor,omitempty"`
}

// spawnError carries a start failure so that himorime reports it exactly as
// if it had started the command itself.
type spawnError struct {
	Message string `json:"message"`
	Op      string `json:"op,omitempty"`
	Path    string `json:"path,omitempty"`
	Errno   int    `json:"errno,omitempty"`
}

func newSpawnError(err error) *spawnError {
	e := &spawnError{Message: err.Error()}
	var pathErr *os.PathError
	var errno syscall.Errno
	if errors.As(err, &pathErr) && errors.As(pathErr.Err, &errno) {
		e.Op, e.Path, e.Errno = pathErr.Op, pathErr.Path, int(errno)
	}
	return e
}

func (e *spawnError) err() error {
	if e.Op != "" && e.Errno != 0 {
		return &os.PathError{Op: e.Op, Path: e.Path, Err: syscall.Errno(e.Errno)}
	}
	return errors.New(e.Message)
}

// spawnConn is one end of the socket pair, read and written with blocking
// system calls.
type spawnConn struct {
	file *os.File
	fd   int
}

func newSpawnConn(f *os.File) *spawnConn {
	return &spawnConn{file: f, fd: int(f.Fd())}
}

func (c *spawnConn) close() { _ = c.file.Close() }

func (c *spawnConn) send(msg any, files []*os.File) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	head := binary.BigEndian.AppendUint32(nil, uint32(len(body))) //nolint:gosec // bounded by maxMessage on the reading side
	if len(files) > 0 {
		fds := make([]int, len(files))
		for i, f := range files {
			fds[i] = int(f.Fd())
		}
		var n int
		for {
			n, err = syscall.SendmsgN(c.fd, head, syscall.UnixRights(fds...), nil, 0)
			if !errors.Is(err, syscall.EINTR) {
				break
			}
		}
		runtime.KeepAlive(files)
		if err != nil {
			return err
		}
		head = head[n:]
	}
	if err := c.write(head); err != nil {
		return err
	}
	return c.write(body)
}

func (c *spawnConn) write(b []byte) error {
	for len(b) > 0 {
		n, err := syscall.Write(c.fd, b)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		b = b[n:]
	}
	return nil
}

// recv reads one message into v and returns the descriptors that came with
// it, already marked close-on-exec so no command inherits them by accident.
func (c *spawnConn) recv(v any) ([]*os.File, error) {
	head := make([]byte, 4)
	oob := make([]byte, syscall.CmsgSpace(8*4))
	var n, oobn int
	var err error
	for {
		n, oobn, _, _, err = syscall.Recvmsg(c.fd, head, oob, 0)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	var files []*os.File
	if oobn > 0 {
		msgs, perr := syscall.ParseSocketControlMessage(oob[:oobn])
		if perr != nil {
			return nil, perr
		}
		for i := range msgs {
			fds, ferr := syscall.ParseUnixRights(&msgs[i])
			if ferr != nil {
				closeFiles(files)
				return nil, ferr
			}
			for _, fd := range fds {
				syscall.CloseOnExec(fd)
				files = append(files, os.NewFile(uintptr(fd), "descriptor"))
			}
		}
	}
	if n == 0 {
		closeFiles(files)
		return nil, io.EOF
	}
	if err := c.read(head[n:]); err != nil {
		closeFiles(files)
		return nil, err
	}
	size := binary.BigEndian.Uint32(head)
	if size > maxMessage {
		closeFiles(files)
		return nil, fmt.Errorf("message of %d bytes is too large", size)
	}
	body := make([]byte, size)
	if err := c.read(body); err != nil {
		closeFiles(files)
		return nil, err
	}
	if err := json.Unmarshal(body, v); err != nil {
		closeFiles(files)
		return nil, err
	}
	return files, nil
}

func (c *spawnConn) read(b []byte) error {
	for len(b) > 0 {
		n, err := syscall.Read(c.fd, b)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrUnexpectedEOF
		}
		b = b[n:]
	}
	return nil
}

func closeFiles(files []*os.File) {
	for _, f := range files {
		_ = f.Close()
	}
}

// ServeSpawner is the spawner's main function. It starts the commands
// himorime sends over the socket on descriptor 3, one at a time, until
// himorime closes the socket, which happens at the latest when himorime
// exits; a command still running then is killed with its process tree.
func ServeSpawner() int {
	var st syscall.Stat_t
	if err := syscall.Fstat(spawnerFD, &st); err != nil || st.Mode&syscall.S_IFMT != syscall.S_IFSOCK {
		fmt.Fprintf(os.Stderr, "himorime: %s is for himorime's internal use; unset it\n", SpawnerEnv)
		return 2
	}
	syscall.CloseOnExec(spawnerFD)
	// The spawner does little and allocates little; fewer threads and no
	// automatic garbage collection keep its RSS, the floor of every command,
	// small and steady.
	runtime.GOMAXPROCS(2)
	debug.SetGCPercent(-1)
	gc := newHeapGrowth()
	c := newSpawnConn(os.NewFile(spawnerFD, "himorime"))
	if err := c.send(spawnReply{Version: spawnProtocol}, nil); err != nil {
		return 1
	}
	// wake ends the watch on himorime's socket once a command was reaped, so
	// no goroutine is left reading it when the next request arrives.
	wakeR, wakeW, err := os.Pipe()
	if err != nil {
		return 1
	}
	var env []string
	for {
		var req spawnRequest
		files, err := c.recv(&req)
		if err != nil {
			return 0
		}
		if req.Kill {
			// Meant for a command that has already finished.
			closeFiles(files)
			continue
		}
		if req.SameEnv {
			req.Env = env
		}
		env = req.Env
		if err := serveRequest(c, req, files, wakeR, wakeW); err != nil {
			return 0
		}
		gc.collect()
	}
}

// serveRequest starts one command the way run does and reports it. Elapsed
// is measured here, around the start and the reaping, so no message to or
// from himorime is ever part of it.
func serveRequest(c *spawnConn, req spawnRequest, files []*os.File, wakeR, wakeW *os.File) error {
	cmd := exec.Command(req.Path) //nolint:noctx,gosec // stopped through himorime's kill request; G204: the suite's command, resolved by himorime
	cmd.Args = req.Args
	cmd.Dir = req.Dir
	cmd.Env = req.Env
	pick := func(i int) *os.File {
		if i < 0 || i >= len(files) {
			return nil
		}
		return files[i]
	}
	if f := pick(req.Stdin); f != nil {
		cmd.Stdin = f
	}
	if f := pick(req.Stdout); f != nil {
		cmd.Stdout = f
	}
	if f := pick(req.Stderr); f != nil {
		cmd.Stderr = f
	}
	configure(cmd)

	// The watch starts before the measured interval, so starting it never
	// competes with the command.
	t := &runningTree{}
	watched := make(chan bool, 1)
	go func() { watched <- watchHimorime(c, wakeR, t) }()
	var floor int64
	if req.CollectUsage {
		floor = startFloor()
	}
	start := time.Now()
	err := cmd.Start()
	if err == nil {
		t.started(cmd.Process.Pid)
	}
	// The command holds its own copies now. Keeping these open would keep a
	// pipe himorime reads from open after the command exits.
	closeFiles(files)
	var waitErr error
	if err == nil {
		waitErr = cmd.Wait()
	}
	elapsed := time.Since(start)
	_, _ = wakeW.Write([]byte{0})
	gone := <-watched
	if err != nil {
		if gone {
			return io.EOF
		}
		return c.send(spawnReply{StartErr: newSpawnError(err)}, nil)
	}
	reply := spawnReply{Done: true, Elapsed: int64(elapsed), ExitCode: exitCode(cmd, waitErr)}
	if req.CollectUsage {
		raw := rawUsageOf(cmd)
		reply.Usage, reply.Floor = &raw, floor
	}
	t.close()
	if gone {
		return io.EOF
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) {
		reply.WaitErr = waitErr.Error()
	}
	return c.send(reply, nil)
}

// runningTree is the process tree of the command a spawner runs. A kill
// request can arrive before the command started; it is then stopped as soon
// as it starts.
type runningTree struct {
	mu      sync.Mutex
	pgid    int
	stopped bool
}

func (r *runningTree) started(pid int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pgid = pid
	if r.stopped {
		_ = (&tree{pgid: pid}).kill()
	}
}

func (r *runningTree) kill() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = true
	if r.pgid > 0 {
		_ = (&tree{pgid: r.pgid}).kill()
	}
}

// close stops whatever the command left running in its process group.
func (r *runningTree) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pgid > 0 {
		(&tree{pgid: r.pgid}).close()
	}
}

// watchHimorime waits, while a command runs, for a kill request from himorime
// or for himorime to be gone, and stops the command's process tree in either
// case; with himorime gone nothing else would ever stop it. It returns once
// wake is readable, after the command was reaped, and reports whether
// himorime is gone.
func watchHimorime(c *spawnConn, wake *os.File, t *runningTree) bool {
	fds := []unix.PollFd{
		{Fd: int32(c.fd), Events: unix.POLLIN},      //nolint:gosec // a descriptor fits in int32
		{Fd: int32(wake.Fd()), Events: unix.POLLIN}, //nolint:gosec // a descriptor fits in int32
	}
	gone := false
	drain := func() bool {
		var b [1]byte
		_, _ = wake.Read(b[:])
		return gone
	}
	for {
		fds[0].Revents, fds[1].Revents = 0, 0
		if _, err := unix.Poll(fds, -1); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			// Nothing to watch with: the command runs to its end.
			return drain()
		}
		if fds[1].Revents != 0 {
			return drain()
		}
		if fds[0].Revents == 0 {
			continue
		}
		var req spawnRequest
		files, err := c.recv(&req)
		closeFiles(files)
		if err != nil {
			// At end of file the socket stays readable; stop polling it.
			gone = true
			fds[0].Fd = -1
		}
		if err != nil || req.Kill {
			t.kill()
		}
	}
}
