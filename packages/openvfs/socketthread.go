package openvfs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Transpiled from src/openvfsfuse/socketthread.{h,cpp}.
//
// @see https://www.codeproject.com/Articles/1169105/Cplusplus-std-thread-Event-Loop-with-Message-Queue
// David Lafreniere, Feb 2017.
//
// Upstream drives a raw non-blocking AF_UNIX descriptor and polls it from a
// 250ms timer. A net.Conn with a short read deadline behaves the same way -- a
// read returns what is buffered, or reports a timeout the way EAGAIN does --
// and it is the same code on Linux, macOS and Windows, which has had AF_UNIX
// since Windows 10 1803.

const (
	msgExitThread   = 1
	msgPostUserData = 2
	msgTimer        = 3
)

// maxRxBufferSize is the upper bound for the receive buffer. A message from the
// socket API is a single JSON line and stays far below this; anything larger
// means the peer is not speaking the protocol.
const (
	maxRxBufferSize      = 1024 * 100
	singleReadBufferSize = 1024 * 4
)

// readPollTimeout is how long a poll of the socket waits before reporting that
// nothing is there. It stands in for the EAGAIN of upstream's O_NONBLOCK
// descriptor and only has to be long enough for the read to be attempted.
const readPollTimeout = time.Millisecond

type MsgData struct {
	Msg       string
	File      string
	FileID    string
	ID        int
	Requester string
}

type threadMsg struct {
	id  int
	msg *MsgData
}

type SocketThread struct {
	mu    sync.Mutex
	cv    *sync.Cond
	queue []threadMsg

	// running stands in for m_thread: set while a worker goroutine exists.
	running   bool
	exit      bool // m_exit
	timerExit bool // m_timerExit

	threadName string
	socketPath string

	// done and timerDone replace std::thread::join().
	done      chan struct{}
	timerDone chan struct{}

	// connMu guards socket, which upstream holds in a std::atomic<int>.
	connMu sync.Mutex
	socket net.Conn

	// rx is the receive buffer holding the bytes read from the socket that do
	// not form a complete message yet. Only touched by the worker goroutine.
	rx []byte

	sharedMap *SharedMap
}

func NewSocketThread(threadName string, m *SharedMap) *SocketThread {
	s := &SocketThread{threadName: threadName, sharedMap: m}
	s.cv = sync.NewCond(&s.mu)
	return s
}

// GetThreadName gets the thread name.
func (s *SocketThread) GetThreadName() string { return s.threadName }

func (s *SocketThread) conn() net.Conn {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return s.socket
}

// dropConn closes the connection and marks the descriptor as gone, which is
// upstream's `close(_socket); _socket = -1`.
func (s *SocketThread) dropConn() {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.socket != nil {
		_ = s.socket.Close()
		s.socket = nil
	}
}

// CreateThread is called once to create the worker goroutine. It reports
// whether the goroutine is created and the socket could be connected.
func (s *SocketThread) CreateThread(socketPath string) bool {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return true
	}
	s.running = true
	s.exit = false
	s.timerExit = false
	s.socketPath = socketPath
	s.done = make(chan struct{})
	s.timerDone = make(chan struct{})
	s.mu.Unlock()

	// Upstream synchronises on a promise so that the worker has entered
	// Process() before initSocket() posts to it. Posting takes the queue lock,
	// so the message is picked up whenever the goroutine gets scheduled and no
	// handshake is needed.
	go s.process()

	return s.initSocket()
}

// ExitThread is called once a program exit to exit the worker goroutine.
func (s *SocketThread) ExitThread() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	// Set under the queue mutex so PostMsg() can't enqueue behind the exit message
	s.exit = true
	s.queue = append(s.queue, threadMsg{id: msgExitThread})
	s.cv.Signal()
	done := s.done
	s.mu.Unlock()

	<-done

	// Clear the queue if anything added while waiting for join
	s.mu.Lock()
	s.running = false
	s.queue = nil
	s.mu.Unlock()

	s.dropConn()
}

// PostMsg adds a message to the thread queue. It returns false if the thread is
// shutting down and the message was dropped.
func (s *SocketThread) PostMsg(data *MsgData) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exit {
		return false
	}
	s.queue = append(s.queue, threadMsg{id: msgPostUserData, msg: data})
	s.cv.Signal()
	return true
}

// GetQueueSize gets the size of the thread message queue.
func (s *SocketThread) GetQueueSize() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

func (s *SocketThread) initSocket() bool {
	if s.socketPath == "" {
		// upstream asserts
		return false
	}

	conn, err := net.Dial("unix", s.socketPath)
	if err != nil {
		perror("connect", err)
		return false
	}
	s.connMu.Lock()
	s.socket = conn
	s.connMu.Unlock()

	/* send  a welcome message to the SocketAPI */
	s.PostMsg(&MsgData{Msg: "VERSION"})

	return true
}

func (s *SocketThread) socketSendMsg(msgData *MsgData) bool {
	msg := msgData.Msg

	// prepare the OpenCloud message to be sent over the wire
	conn := s.conn()
	if msg == "" || conn == nil {
		return false
	}

	// nlohmann orders object keys, so "arguments" precedes "id" on the wire.
	type arguments struct {
		File      string `json:"file"`
		FileID    string `json:"fileId"`
		Requester string `json:"requester"`
	}
	j, err := json.Marshal(struct {
		Arguments arguments `json:"arguments"`
		ID        string    `json:"id"`
	}{
		Arguments: arguments{msgData.File, msgData.FileID, msgData.Requester},
		ID:        strconv.Itoa(msgData.ID),
	})
	if err != nil {
		return false
	}

	msg += ":"
	msg += string(j)
	msg += "\n"

	fmt.Println("Raw message: " + msg)

	// Upstream passes MSG_NOSIGNAL so that a peer closing the socket cannot
	// bring down the whole fuse layer with a SIGPIPE. The Go runtime already
	// ignores SIGPIPE on anything but stdout and stderr.
	if _, err := io.WriteString(conn, msg); err != nil {
		perror("send", err)
		// Mark the descriptor as gone so we do not keep reading from and
		// writing to a closed fd on every subsequent tick.
		s.dropConn()
		return false
	}

	fmt.Println("Sent message: " + msg)
	return true
}

// processSocketInput drains everything readable from the socket into rx and
// dispatches every complete (newline terminated) message it contains.
func (s *SocketThread) processSocketInput() {
	conn := s.conn()
	if conn == nil {
		// the socket can only be nil if the connection was closed by the peer.
		// But this fuse layer is completely controlled by the client, which is
		// the peer, so every reconnect attempt will fail.
		// Also, if the client goes down and closes the socket, this fuse is also
		// closed down.
		return
	}

	// The socket is a SOCK_STREAM and carries no message boundaries: a single
	// read may return a fragment of a message, several messages at once, or
	// both. Accumulate into rx and only dispatch complete lines.
	buf := make([]byte, singleReadBufferSize)

	peerClosed := false
	for contRead := true; contRead; {
		// A deadline just ahead of now turns the read into a poll: anything
		// already buffered comes back, and an idle socket reports a timeout
		// where a non-blocking descriptor reports EAGAIN. The deadline has to
		// be in the future, because the poller reports an expired one without
		// attempting the read at all, which would never drain the socket.
		_ = conn.SetReadDeadline(time.Now().Add(readPollTimeout))

		n, err := conn.Read(buf)

		if n > 0 {
			// all good, we read something and continue to do so
			s.rx = append(s.rx, buf[:n]...)
			continue
		}

		switch {
		case errors.Is(err, io.EOF):
			// Peer closed the connection. Whatever is left in the buffer will
			// be interpreted - and remaining incompleted reads will be erased.
			fmt.Fprintln(os.Stderr, "Socket closed by peer")
			peerClosed = true
			contRead = false
		case errors.Is(err, os.ErrDeadlineExceeded):
			// EAGAIN: no data is available at the moment, which is not an issue
			// for a non blocking socket.
			contRead = false
		case errors.Is(err, net.ErrClosed):
			contRead = false
		default:
			// a serious error was reported
			contRead = false
			if err != nil {
				perror("socket read", err)
			}
		}
	}

	// handle the contents of rx here
	pos := 0
	for {
		newpos := bytes.IndexByte(s.rx[pos:], '\n')
		if newpos < 0 {
			break
		}
		s.handleReceivedMsg(string(s.rx[pos : pos+newpos]))
		pos += newpos + 1
	}
	s.rx = s.rx[pos:] // clear everything that was processed

	// A peer that never sends a newline must not be able to grow our buffer
	// without bound.
	// Also, if the peer closed, because that are useless fragments after
	// connection closed
	if peerClosed || len(s.rx) > maxRxBufferSize {
		fmt.Fprintf(os.Stderr, "Discarding %d bytes from the socket API\n", len(s.rx))
		s.rx = nil
	}
}

func (s *SocketThread) handleReceivedMsg(msg string) {
	if msg == "" {
		return
	}

	fmt.Println("Handle single message " + msg)

	msgType, msgAttr, found := strings.Cut(msg, ":")
	if !found {
		fmt.Fprintln(os.Stderr, "Invalid message format: "+msg)
		return
	}

	switch msgType {
	case "V2/HYDRATE_FILE_RESULT":
		status := ""

		// Every get<> upstream throws on a missing or mistyped field and is
		// caught as an invalid message, so each one is checked here.
		var j struct {
			ID        string          `json:"id"`
			Arguments json.RawMessage `json:"arguments"`
		}
		invalid := func(reason string) {
			fmt.Fprintln(os.Stderr, "Invalid JSON message: "+msgAttr+reason)
		}
		if err := json.Unmarshal([]byte(msgAttr), &j); err != nil {
			invalid(err.Error())
			return
		}
		id, err := strconv.Atoi(j.ID)
		if err != nil {
			invalid(err.Error())
			return
		}
		var arguments map[string]any
		if err := json.Unmarshal(j.Arguments, &arguments); err != nil {
			invalid(err.Error())
			return
		}
		if errValue, contains := arguments["error"]; contains {
			fmt.Fprintf(os.Stderr, "Error from socket API for Id %d: %v\n", id, errValue)
		} else {
			statusValue, isString := arguments["status"].(string)
			if !isString {
				invalid("missing status")
				return
			}
			status = statusValue
		}

		if id > 0 {
			res := HydFailed // Default set to fail
			if status == "OK" {
				res = HydSuccess // good!
			} else {
				fmt.Printf("ERROR from socket API for Id%d\n", id)
			}

			hj := HydJob{State: res}
			if !s.sharedMap.Set(id, hj) {
				// the id could not be set. That means, the job was not inserted.
				fmt.Printf("Job not found:%d\n", id)
			} else {
				fmt.Printf("Setting Job ID %d to result %s\n", id, hj.State)
			}
		}
	case "VERSION":
		attribs := Split(msgAttr, ':')
		if len(attribs) == 3 {
			fmt.Println("Got PID of the Desktop Client: " + attribs[2])
			pid, err := strconv.ParseInt(attribs[2], 10, 64)
			if err == nil {
				s.sharedMap.SetDesktopClientPID(pid)
			}
		}
	}
}

// timerThread is the entry point for the timer goroutine.
func (s *SocketThread) timerThread() {
	defer close(s.timerDone)
	for {
		s.mu.Lock()
		stop := s.timerExit
		s.mu.Unlock()
		if stop {
			return
		}

		// Sleep for 250mS then put a MSG_TIMER into the message queue
		time.Sleep(250 * time.Millisecond)

		// Add timer msg to queue and notify worker goroutine
		s.mu.Lock()
		s.queue = append(s.queue, threadMsg{id: msgTimer})
		s.cv.Signal()
		s.mu.Unlock()
	}
}

// process is the entry point for the worker goroutine.
func (s *SocketThread) process() {
	defer close(s.done)

	s.mu.Lock()
	s.timerExit = false
	s.mu.Unlock()
	go s.timerThread()

	for {
		var msg threadMsg
		{
			// Wait for a message to be added to the queue
			s.mu.Lock()
			for len(s.queue) == 0 {
				s.cv.Wait()
			}
			msg = s.queue[0]
			s.queue = s.queue[1:]
			s.mu.Unlock()
		}

		// The msg.id is an Id to identify the kind of thread msg. It is
		// not the transfer id of the client job
		switch msg.id {
		case msgPostUserData:
			if msg.msg == nil {
				// upstream asserts
				break
			}
			fmt.Println("Received user data message")

			msgData := msg.msg
			fmt.Printf("Sending %d: %s %s on %s\n", msgData.ID, msgData.Msg, msgData.File, s.threadName)

			if !s.socketSendMsg(msgData) {
				fmt.Printf("Failed to send msg %d: %s\n", msgData.ID, msgData.Msg)
				// The job was registered by the caller before it posted the
				// message, so a failure to send has to be published as such.
				// Otherwise the caller waits out its full timeout for a
				// request that never left the machine.
				if msgData.ID > 0 {
					s.sharedMap.Set(msgData.ID, HydJob{State: HydFailed})
				}
			}

		case msgTimer:
			s.processSocketInput()

		case msgExitThread:
			s.mu.Lock()
			s.timerExit = true
			s.mu.Unlock()
			<-s.timerDone
			return
		}
	}
}

// perror is perror(3).
func perror(what string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", what, err)
}
