//go:build unix

package proc

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func spawnSocketPair(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	fd, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	left, right := os.NewFile(uintptr(fd[0]), "left"), os.NewFile(uintptr(fd[1]), "right")
	t.Cleanup(func() { _ = left.Close(); _ = right.Close() })
	return left, right
}

func TestSpawnConnectionTruncatedFrames(t *testing.T) {
	t.Parallel()
	for _, frame := range [][]byte{{0}, {0, 0, 0, 2, '{'}} {
		left, right := spawnSocketPair(t)
		if _, err := right.Write(frame); err != nil {
			t.Fatal(err)
		}
		if err := right.Close(); err != nil {
			t.Fatal(err)
		}
		var request spawnRequest
		files, err := newSpawnConn(left).recv(&request)
		closeFiles(files)
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("frame %v: %v", frame, err)
		}
	}
}

func TestSpawnConnectionClosedSocketErrors(t *testing.T) {
	t.Parallel()
	left, right := spawnSocketPair(t)
	conn := newSpawnConn(left)
	if err := conn.send(make(chan int), nil); err == nil {
		t.Fatal("serialized an unsupported message")
	}
	conn.close()
	conn = newSpawnConn(left)
	if err := conn.send(spawnRequest{}, nil); err == nil {
		t.Fatal("sent on a closed socket")
	}
	if err := conn.send(spawnRequest{}, []*os.File{right}); err == nil {
		t.Fatal("sent descriptors on a closed socket")
	}
	var req spawnRequest
	if files, err := conn.recv(&req); err == nil {
		closeFiles(files)
		t.Fatal("received on a closed socket")
	}
	if err := conn.read(make([]byte, 1)); err == nil {
		t.Fatal("read on a closed socket")
	}
}

type procFailWriter struct{}

func (procFailWriter) Write([]byte) (int, error) { return 0, errors.New("output failed") }

// idleSpawnerPids lists the process IDs of the idle spawners.
func idleSpawnerPids() []int {
	spawners.mu.Lock()
	defer spawners.mu.Unlock()
	pids := make([]int, 0, len(spawners.idle))
	for _, sp := range spawners.idle {
		pids = append(pids, sp.process.Pid)
	}
	return pids
}

// ownerHelper plays himorime: it runs commands one at a time, which must all
// go through one spawner, prints the spawner's process ID and, with
// HELPER_HANG, starts a long command and never returns, for the test to kill
// it.
func ownerHelper() int {
	EnableSpawner()
	exe, err := os.Executable()
	if err != nil {
		return 3
	}
	env := os.Environ()
	for range 3 {
		if _, err := Run(context.Background(), Spec{Path: exe, Env: append(env, "HIMORIME_PROC_HELPER=exit"), CollectUsage: true, MeasureMemory: true}, nil); err != nil {
			return 3
		}
	}
	pids := idleSpawnerPids()
	if len(pids) != 1 {
		return 4
	}
	fmt.Println(pids[0])
	if os.Getenv("HELPER_HANG") == "" {
		return 0
	}
	_, _ = Run(context.Background(), Spec{Path: exe, Env: append(env, "HIMORIME_PROC_HELPER=spawn"), CollectUsage: true, MeasureMemory: true}, nil)
	return 5
}

// processGone reports whether pid no longer runs; a zombie waiting for its
// new parent to reap it counts as gone.
func processGone(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return true
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	_, rest, ok := strings.Cut(string(stat), ") ")
	return ok && strings.HasPrefix(rest, "Z")
}

func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !processGone(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("the spawner %d still runs after the process that started it exited", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func startOwner(t *testing.T, env ...string) (*exec.Cmd, int) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(append(os.Environ(), "HIMORIME_PROC_HELPER=owner"), env...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("the owner printed no spawner: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	return cmd, pid
}

func TestSpawnerExitsWithHimorime(t *testing.T) {
	t.Parallel()
	owner, pid := startOwner(t)
	if err := owner.Wait(); err != nil {
		t.Fatalf("owner: %v", err)
	}
	waitGone(t, pid)
}

// TestSpawnerStopsTheCommandWhenHimorimeIsKilled: himorime killed with
// SIGKILL cannot stop the command it measures, so the spawner does, and then
// exits.
func TestSpawnerStopsTheCommandWhenHimorimeIsKilled(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "marker")
	owner, pid := startOwner(t, "HELPER_HANG=1", "HELPER_MARKER="+marker)
	// Let the spawner start the long command and its grandchild.
	time.Sleep(500 * time.Millisecond)
	_ = owner.Process.Kill()
	_ = owner.Wait()
	waitGone(t, pid)
	// The grandchild would write the marker 4s after it started.
	time.Sleep(4500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the command survived himorime and its spawner")
	}
}

// TestSpawnerReportsStartFailuresLikeADirectStart: a start failure carries
// the same message and the same errno on both paths.
func TestSpawnerReportsStartFailuresLikeADirectStart(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "missing")
	for _, s := range []Spec{
		{Path: missing, Env: os.Environ()},
		{Path: "/bin/sh", Args: []string{"-c", "true"}, Dir: missing, Env: os.Environ()},
	} {
		_, direct := run(context.Background(), s, nil, attach)
		_, handled, spawned := runSpawned(context.Background(), s)
		if !handled {
			t.Fatal("no spawner took the command")
		}
		if !errors.Is(direct, ErrStart) || !errors.Is(spawned, ErrStart) {
			t.Fatalf("errors = %v / %v, want ErrStart", direct, spawned)
		}
		if direct.Error() != spawned.Error() || !errors.Is(spawned, syscall.ENOENT) {
			t.Fatalf("spawner error %q (ENOENT: %v), want %q", spawned, errors.Is(spawned, syscall.ENOENT), direct)
		}
	}
}

func TestServeSpawnerRefusesToRunWithoutHimorime(t *testing.T) {
	t.Parallel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), SpawnerEnv+"=1")
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 || !strings.Contains(string(out), "internal use") {
		t.Fatalf("a spawner started by hand: %v, %q", err, out)
	}
}

// TestRunUsesTheSpawnerOnlyForMemory: only a command whose peak RSS is
// measured pays for the spawner; everything else is started by himorime.
func TestRunUsesTheSpawnerOnlyForMemory(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name            string
		usage, memory   bool
		fromThisProcess bool
	}{
		{"latency", false, false, true},
		{"cpu", true, false, true},
		{"memory", true, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out strings.Builder
			s := helper(t, "ppid")
			s.Stdout, s.CollectUsage, s.MeasureMemory = &out, tt.usage, tt.memory
			if _, err := Run(context.Background(), s, nil); err != nil {
				t.Fatal(err)
			}
			parent, err := strconv.Atoi(out.String())
			if err != nil {
				t.Fatal(err)
			}
			if (parent == os.Getpid()) != tt.fromThisProcess {
				t.Fatalf("parent %d, this process %d", parent, os.Getpid())
			}
		})
	}
}

func TestSpawnErrorRoundTripPreservesPathErrors(t *testing.T) {
	t.Parallel()
	original := &os.PathError{Op: "open", Path: "/missing", Err: syscall.ENOENT}
	encoded := newSpawnError(original)
	if encoded.Op != original.Op || encoded.Path != original.Path || encoded.Errno != int(syscall.ENOENT) {
		t.Fatalf("spawn error = %+v", encoded)
	}
	decoded := encoded.err()
	if !errors.Is(decoded, syscall.ENOENT) || decoded.Error() != original.Error() {
		t.Fatalf("decoded error = %v", decoded)
	}
	plain := newSpawnError(errors.New("plain failure"))
	if got := plain.err().Error(); got != "plain failure" {
		t.Fatalf("plain decoded error = %q", got)
	}
}

func TestInterfaceEqualHandlesUncomparableValues(t *testing.T) {
	t.Parallel()
	if !interfaceEqual("same", "same") || interfaceEqual("one", "two") {
		t.Fatal("interfaceEqual got the wrong result for comparable values")
	}
	if interfaceEqual([]byte("a"), []byte("a")) {
		t.Fatal("interfaceEqual compared uncomparable values as equal")
	}
}

func TestNewSpawnIOConnectsFilesAndCopies(t *testing.T) {
	t.Parallel()
	in, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	files, err := newSpawnIO(Spec{Stdin: in, Stdout: out, Stderr: out})
	if err != nil {
		t.Fatal(err)
	}
	if files.stdin != 0 || files.stdout != 1 || files.stderr != 1 || len(files.copies) != 0 {
		t.Fatalf("file spawn IO = %+v", files)
	}
	files.closeAll()

	var output strings.Builder
	pipeIO, err := newSpawnIO(Spec{Stdin: strings.NewReader("input"), Stdout: &output, Stderr: &output})
	if err != nil {
		t.Fatal(err)
	}
	if pipeIO.stdin < 0 || pipeIO.stdout < 0 || pipeIO.stderr != pipeIO.stdout || len(pipeIO.copies) != 2 {
		t.Fatalf("pipe spawn IO = %+v", pipeIO)
	}
	pipeIO.startCopies()
	pipeIO.closeAll()
	if err := pipeIO.wait(); err != nil {
		t.Fatalf("closed spawn IO copies: %v", err)
	}
}

func TestSpawnConnRoundTripsMessagesAndDescriptors(t *testing.T) {
	t.Parallel()
	left, right := spawnSocketPair(t)
	leftConn, rightConn := newSpawnConn(left), newSpawnConn(right)
	fixture, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close()
	want := spawnRequest{Path: "/bin/true", Args: []string{"arg"}, SameEnv: true}
	if err := leftConn.send(want, []*os.File{fixture}); err != nil {
		t.Fatal(err)
	}
	var got spawnRequest
	files, err := rightConn.recv(&got)
	if err != nil {
		t.Fatal(err)
	}
	closeFiles(files)
	if !reflect.DeepEqual(got, want) || len(files) != 1 {
		t.Fatalf("received request = %+v with %d files, want %+v with one file", got, len(files), want)
	}
}

func TestSpawnConnRejectsMalformedMessages(t *testing.T) {
	t.Parallel()
	testRaw := func(t *testing.T, body []byte, want string) {
		t.Helper()
		fd, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
		if err != nil {
			t.Fatal(err)
		}
		reader, writer := newSpawnConn(os.NewFile(uintptr(fd[0]), "reader")), os.NewFile(uintptr(fd[1]), "writer")
		defer reader.close()
		defer writer.Close()
		head := make([]byte, 4)
		binary.BigEndian.PutUint32(head, uint32(len(body)))
		if _, err := writer.Write(head); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(body); err != nil {
			t.Fatal(err)
		}
		var got spawnRequest
		_, err = reader.recv(&got)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("recv error = %v, want %q", err, want)
		}
	}
	testRaw(t, []byte("x"), "invalid character")
	fd, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := newSpawnConn(os.NewFile(uintptr(fd[0]), "reader")), os.NewFile(uintptr(fd[1]), "writer")
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, maxMessage+1)
	if _, err := writer.Write(header); err != nil {
		t.Fatal(err)
	}
	var got spawnRequest
	_, err = reader.recv(&got)
	reader.close()
	writer.Close()
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversized message error = %v", err)
	}
}

func TestServeRequestReportsCompletionAndStartFailure(t *testing.T) {
	t.Parallel()
	serve := func(t *testing.T, req spawnRequest, files ...*os.File) (spawnReply, error) {
		t.Helper()
		local, peer := spawnSocketPair(t)
		wakeR, wakeW, err := os.Pipe()
		if err != nil {
			return spawnReply{}, err
		}
		defer wakeR.Close()
		defer wakeW.Close()
		done := make(chan error, 1)
		go func() { done <- serveRequest(newSpawnConn(local), req, files, wakeR, wakeW) }()
		var reply spawnReply
		if _, err := newSpawnConn(peer).recv(&reply); err != nil {
			return spawnReply{}, err
		}
		return reply, <-done
	}

	reply, err := serve(t, spawnRequest{Path: "/bin/sh", Args: []string{"sh", "-c", "exit 7"}, CollectUsage: true})
	if err != nil || !reply.Done || reply.ExitCode != 7 || reply.Usage == nil {
		t.Fatalf("successful serveRequest = %+v, %v", reply, err)
	}
	reply, err = serve(t, spawnRequest{Path: "/definitely/missing/himorime-helper"})
	if err != nil || reply.StartErr == nil || reply.Done {
		t.Fatalf("failed serveRequest = %+v, %v", reply, err)
	}

	var streams []*os.File
	for range 3 {
		f, err := os.CreateTemp(t.TempDir(), "stream")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = f.Close() })
		streams = append(streams, f)
	}
	if _, err := streams[0].WriteString("input\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := streams[0].Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	reply, err = serve(t, spawnRequest{Path: "/bin/sh", Args: []string{"sh", "-c", `read value; printf '%s' "$value"; printf diagnostic >&2`}, Stdin: 0, Stdout: 1, Stderr: 2}, streams...)
	if err != nil || !reply.Done || reply.ExitCode != 0 {
		t.Fatalf("stdio request: %+v, %v", reply, err)
	}
	for i, want := range []string{"input", "diagnostic"} {
		got, err := os.ReadFile(streams[i+1].Name())
		if err != nil || string(got) != want {
			t.Fatalf("stream %d = %q, %v; want %q", i+1, got, err, want)
		}
	}
}

func TestServeSpawnerProcessesRequestsUntilOwnerEOF(t *testing.T) {
	serverFile, clientFile := spawnSocketPair(t)
	server, client := newSpawnConn(serverFile), newSpawnConn(clientFile)
	done := make(chan int, 1)
	go func() { done <- serveSpawner(server, newHeapGrowth()) }()
	first := spawnRequest{Path: "/bin/sh", Args: []string{"sh", "-c", "exit 0"}, Env: []string{"FOO=bar"}}
	if err := client.send(first, nil); err != nil {
		t.Fatal(err)
	}
	var reply spawnReply
	if _, err := client.recv(&reply); err != nil || !reply.Done || reply.ExitCode != 0 {
		t.Fatalf("first reply = %+v, %v", reply, err)
	}
	if err := client.send(spawnRequest{Kill: true}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.send(spawnRequest{Path: "/bin/sh", Args: []string{"sh", "-c", `test "$FOO" = bar`}, SameEnv: true}, nil); err != nil {
		t.Fatal(err)
	}
	reply = spawnReply{}
	if _, err := client.recv(&reply); err != nil || !reply.Done || reply.ExitCode != 0 {
		t.Fatalf("reused environment reply = %+v, %v", reply, err)
	}
	if err := clientFile.Close(); err != nil {
		t.Fatal(err)
	}
	if status := <-done; status != 0 {
		t.Fatalf("serveSpawner status = %d, want 0 after owner EOF", status)
	}
}

func TestWatchHimorimeHandlesKillRequest(t *testing.T) {
	t.Parallel()
	local, peer := spawnSocketPair(t)
	wakeR, wakeW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer wakeR.Close()
	defer wakeW.Close()
	tree := &runningTree{}
	done := make(chan bool, 1)
	go func() { done <- watchHimorime(newSpawnConn(local), wakeR, tree) }()
	if err := newSpawnConn(peer).send(spawnRequest{Kill: true}, nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		tree.mu.Lock()
		stopped := tree.stopped
		tree.mu.Unlock()
		if stopped {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("kill request was not handled")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := wakeW.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	if gone := <-done; gone {
		t.Fatal("watchHimorime reported a live connection as gone")
	}
}

func TestWatchHimorimeDetectsDisconnectedOwner(t *testing.T) {
	local, peer := spawnSocketPair(t)
	wakeR, wakeW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer wakeR.Close()
	defer wakeW.Close()
	tree := &runningTree{}
	done := make(chan bool, 1)
	go func() { done <- watchHimorime(newSpawnConn(local), wakeR, tree) }()
	_ = peer.Close()
	deadline := time.Now().Add(time.Second)
	for {
		tree.mu.Lock()
		stopped := tree.stopped
		tree.mu.Unlock()
		if stopped {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owner disconnect was not handled")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := wakeW.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	if gone := <-done; !gone {
		t.Fatal("watchHimorime reported a disconnected owner as alive")
	}
}

func TestRunSpawnedRepliesAndReusesEnvironment(t *testing.T) {
	client, server := spawnSocketPair(t)
	env := os.Environ()
	sp := &spawner{conn: newSpawnConn(client), env: env}
	spawners.mu.Lock()
	previousIdle, previousFailed := spawners.idle, spawners.failed
	spawners.idle, spawners.failed = []*spawner{sp}, false
	spawners.mu.Unlock()
	defer func() {
		spawners.mu.Lock()
		spawners.idle, spawners.failed = previousIdle, previousFailed
		spawners.mu.Unlock()
	}()
	request := make(chan spawnRequest, 1)
	go func() {
		defer server.Close()
		var req spawnRequest
		files, recvErr := newSpawnConn(server).recv(&req)
		closeFiles(files)
		request <- req
		if recvErr == nil {
			_ = newSpawnConn(server).send(spawnReply{Done: true, Elapsed: int64(time.Millisecond), ExitCode: 4, Usage: &rawUsage{UserCPU: 10, MaxRSS: 2}, Floor: 3}, nil)
		}
	}()
	res, handled, err := runSpawned(context.Background(), Spec{Path: "/bin/true", Env: env, CollectUsage: true})
	if err != nil || !handled || res.ExitCode != 4 || res.Elapsed != time.Millisecond || res.Usage.UserCPU != 10 || res.Usage.PeakRSS != 2*maxRSSUnit || res.Usage.Floor != 3 {
		t.Fatalf("runSpawned = %+v, %v, handled=%v", res, err, handled)
	}
	req := <-request
	if !req.SameEnv || req.Env != nil {
		t.Fatalf("request did not reuse environment: %+v", req)
	}
}

func TestRunSpawnedDefaultsDirectoryAndEnvironment(t *testing.T) {
	client, server := spawnSocketPair(t)
	sp := &spawner{conn: newSpawnConn(client)}
	spawners.mu.Lock()
	previousIdle, previousFailed := spawners.idle, spawners.failed
	spawners.idle, spawners.failed = []*spawner{sp}, false
	spawners.mu.Unlock()
	defer func() {
		spawners.mu.Lock()
		spawners.idle, spawners.failed = previousIdle, previousFailed
		spawners.mu.Unlock()
	}()
	dir := t.TempDir()
	request := make(chan spawnRequest, 1)
	go func() {
		defer server.Close()
		var req spawnRequest
		files, recvErr := newSpawnConn(server).recv(&req)
		closeFiles(files)
		request <- req
		if recvErr == nil {
			_ = newSpawnConn(server).send(spawnReply{Done: true}, nil)
		}
	}()
	res, handled, err := runSpawned(context.Background(), Spec{Path: "/bin/true", Dir: dir})
	if err != nil || !handled || res.ExitCode != 0 {
		t.Fatalf("default environment runSpawned = %+v, %v, handled=%v", res, err, handled)
	}
	req := <-request
	if req.Dir != dir || len(req.Env) == 0 {
		t.Fatalf("request did not default directory and environment: %+v", req)
	}
}

func TestRunSpawnedReportsCopyFailureForSuccessfulCommand(t *testing.T) {
	client, server := spawnSocketPair(t)
	sp := &spawner{conn: newSpawnConn(client)}
	spawners.mu.Lock()
	previousIdle, previousFailed := spawners.idle, spawners.failed
	spawners.idle, spawners.failed = []*spawner{sp}, false
	spawners.mu.Unlock()
	defer func() {
		spawners.mu.Lock()
		spawners.idle, spawners.failed = previousIdle, previousFailed
		spawners.mu.Unlock()
	}()
	go func() {
		var req spawnRequest
		files, recvErr := newSpawnConn(server).recv(&req)
		if recvErr == nil && len(files) == 1 {
			_, _ = files[0].Write([]byte("output"))
		}
		closeFiles(files)
		if recvErr == nil {
			_ = newSpawnConn(server).send(spawnReply{Done: true}, nil)
		}
	}()
	_, handled, err := runSpawned(context.Background(), Spec{Path: "/bin/true", Stdout: procFailWriter{}})
	if !handled || err == nil || !strings.Contains(err.Error(), "wait for process") {
		t.Fatalf("copy failure = handled %v, error %v", handled, err)
	}
}

func TestRunSpawnedHandlesStartAndProtocolFailures(t *testing.T) {
	for _, tt := range []struct {
		name       string
		reply      spawnReply
		closePeer  bool
		wantStart  bool
		wantReason string
	}{
		{name: "start failure", reply: spawnReply{StartErr: &spawnError{Message: "not found"}}, wantStart: true},
		{name: "unexpected reply", reply: spawnReply{Version: spawnProtocol}, wantReason: "unexpected message"},
		{name: "connection failure", closePeer: true, wantReason: "spawner stopped"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client, server := spawnSocketPair(t)
			fakeProcess := exec.Command("sleep", "10")
			if err := fakeProcess.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = fakeProcess.Process.Kill(); _ = fakeProcess.Wait() }()
			sp := &spawner{conn: newSpawnConn(client), process: fakeProcess.Process}
			spawners.mu.Lock()
			previousIdle, previousFailed := spawners.idle, spawners.failed
			spawners.idle, spawners.failed = []*spawner{sp}, false
			spawners.mu.Unlock()
			defer func() {
				spawners.mu.Lock()
				spawners.idle, spawners.failed = previousIdle, previousFailed
				spawners.mu.Unlock()
			}()
			go func() {
				var req spawnRequest
				files, recvErr := newSpawnConn(server).recv(&req)
				closeFiles(files)
				if recvErr != nil || tt.closePeer {
					_ = server.Close()
					return
				}
				_ = newSpawnConn(server).send(tt.reply, nil)
			}()
			res, handled, err := runSpawned(context.Background(), Spec{Path: "/bin/true"})
			if !handled {
				t.Fatal("runSpawned did not handle the fake spawner")
			}
			if tt.wantStart {
				if !errors.Is(err, ErrStart) {
					t.Fatalf("error = %v, want ErrStart", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantReason) || res.ExitCode != -1 {
				t.Fatalf("result = %+v, error = %v", res, err)
			}
		})
	}
}

func TestRunSpawnedCanceledAndWithoutSpawner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, handled, err := runSpawned(ctx, Spec{Path: "/bin/true"})
	if err != nil || !handled || !res.Canceled || res.ExitCode != -1 {
		t.Fatalf("canceled runSpawned = %+v, %v, handled=%v", res, err, handled)
	}
	spawners.mu.Lock()
	previousIdle, previousFailed := spawners.idle, spawners.failed
	spawners.idle, spawners.failed = nil, true
	spawners.mu.Unlock()
	defer func() {
		spawners.mu.Lock()
		spawners.idle, spawners.failed = previousIdle, previousFailed
		spawners.mu.Unlock()
	}()
	res, handled, err = runSpawned(context.Background(), Spec{Path: "/bin/true"})
	if err != nil || handled || res != (Result{}) {
		t.Fatalf("without spawner = %+v, %v, handled=%v", res, err, handled)
	}
}

func TestSpawnerPreparationAndHeapGrowth(t *testing.T) {
	previousEnabled := spawnerEnabled.Swap(false)
	defer spawnerEnabled.Store(previousEnabled)
	PrepareSpawner()
	spawners.mu.Lock()
	previousIdle, previousFailed := spawners.idle, spawners.failed
	spawners.idle, spawners.failed = nil, true
	spawners.mu.Unlock()
	if got := takeSpawner(); got != nil {
		t.Fatal("takeSpawner returned a spawner after a recorded failure")
	}
	prepareSpawner()
	spawners.mu.Lock()
	spawners.idle, spawners.failed = previousIdle, previousFailed
	spawners.mu.Unlock()

	h := newHeapGrowth()
	buf := make([]byte, spawnerGCEvery*2)
	for i := range buf {
		buf[i] = byte(i)
	}
	runtime.KeepAlive(buf)
	h.last = 0
	h.collect()
	if h.last == 0 {
		t.Fatal("heap growth collection did not update its allocation baseline")
	}
}
