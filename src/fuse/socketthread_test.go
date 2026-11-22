package fuse

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Transpiled from src/openvfsfuse/tests/socketthreadtest.cpp.
//
// Drives SocketThread and SharedMap over a real AF_UNIX socket, standing in for
// the desktop client. Covers the stream framing and the hydration wait, both of
// which are timing dependent and regress silently.

// makeSocketPath keeps the socket path short and out of the (potentially
// deeply nested) build directory, because it has to fit into
// sockaddr_un::sun_path.
func makeSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "openvfs-test-")
	if err != nil {
		t.Fatalf("Failed to create a temporary directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "s")
}

func hydrateResult(id int, argumentsBody string) string {
	return fmt.Sprintf("V2/HYDRATE_FILE_RESULT:{\"id\":\"%d\",\"arguments\":{%s}}\n", id, argumentsBody)
}

func TestSocketThread(t *testing.T) {
	socketPath := makeSocketPath(t)

	server, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to listen on %s: %v", socketPath, err)
	}
	defer server.Close()

	jobs := NewSharedMap()
	socketThread := NewSocketThread("TestSocketThread", jobs)

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := server.Accept()
		if err != nil {
			close(accepted)
			return
		}
		accepted <- conn
	}()

	if !socketThread.CreateThread(socketPath) {
		t.Fatal("SocketThread did not connect")
	}
	defer socketThread.ExitThread()

	client, ok := <-accepted
	if !ok {
		t.Fatal("SocketThread did not connect")
	}
	defer client.Close()

	writeAll := func(data string) {
		t.Helper()
		if _, err := writeAllTo(client, data); err != nil {
			t.Fatalf("write to the test client failed: %v", err)
		}
	}

	// Register a job the way openVFSfuse_open() does, before posting it.
	post := func(id int) {
		data := &MsgData{Msg: "V2/HYDRATE_FILE", File: "/tmp/some/file", ID: id}
		jobs.Insert(id, HydJob{State: HydRunning})
		socketThread.PostMsg(data)
	}

	// A message split across two writes must not be acted on until it is complete.
	writeAll("VERSION:1.2.3:2")
	time.Sleep(750 * time.Millisecond)
	check(t, jobs.DesktopClientPID() == 0, "an incomplete message is not dispatched")
	writeAll(":4242\n")
	time.Sleep(750 * time.Millisecond)
	check(t, jobs.DesktopClientPID() == 4242, "a message split across two writes is reassembled")

	// A reply well past any single read buffer must arrive as one message. An
	// error string carrying a path and a description clears 1 KB easily.
	post(101)
	writeAll(hydrateResult(101, "\"error\":\""+strings.Repeat("E", 8000)+"\""))
	check(t, jobs.WaitForJob(101, 10*time.Second) == HydResultFailed, "an 8 KB reply is parsed as a single message")

	// Several replies batched into one write must all be dispatched.
	post(102)
	post(103)
	writeAll(hydrateResult(102, "\"status\":\"OK\"") + hydrateResult(103, "\"status\":\"FAILED\""))
	check(t, jobs.WaitForJob(102, 10*time.Second) == HydResultSucceeded, "first of two batched replies is dispatched")
	check(t, jobs.WaitForJob(103, 10*time.Second) == HydResultFailed, "second of two batched replies is dispatched")

	// ... and the stream is still in sync after all of the above.
	post(104)
	writeAll(hydrateResult(104, "\"status\":\"OK\""))
	check(t, jobs.WaitForJob(104, 10*time.Second) == HydResultSucceeded, "the stream stays in sync")

	// A silent client must time out, bounded by wall-clock time.
	post(105)
	beforeTimeout := time.Now()
	timedOut := jobs.WaitForJob(105, 500*time.Millisecond)
	waited := time.Since(beforeTimeout)
	check(t, timedOut == HydResultTimedOut, "a silent client is reported as a timeout")
	check(t, waited >= 450*time.Millisecond && waited < 30*time.Second, "the timeout is bounded by wall-clock time")

	// A reply arriving later must be observed without waiting out a long backoff.
	post(106)
	late := make(chan struct{})
	go func() {
		defer close(late)
		time.Sleep(300 * time.Millisecond)
		_, _ = writeAllTo(client, hydrateResult(106, "\"status\":\"OK\""))
	}()
	beforeLate := time.Now()
	lateResult := jobs.WaitForJob(106, 60*time.Second)
	latency := time.Since(beforeLate)
	<-late
	check(t, lateResult == HydResultSucceeded, "a late reply is picked up")
	check(t, latency < 10*time.Second, "a late reply is not delayed by a growing backoff")

	// A caller must never be left waiting for a message that was never queued.
	socketThread.ExitThread()
	dropped := &MsgData{Msg: "V2/HYDRATE_FILE", ID: 107}
	check(t, !socketThread.PostMsg(dropped), "PostMsg reports messages dropped during shutdown")
}

// writeAllTo is writeAll(): keep writing until everything is out.
func writeAllTo(conn net.Conn, data string) (int, error) {
	offset := 0
	for offset < len(data) {
		n, err := conn.Write([]byte(data[offset:]))
		if n <= 0 || err != nil {
			return offset, err
		}
		offset += n
	}
	return offset, nil
}
