package openvfs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
)

// ServeAPI is the standalone process' side of the socket API. In the desktop
// client this role is played by the client itself.

type apiMessage struct {
	ID        string         `json:"id"`
	Arguments map[string]any `json:"arguments"`
}

func ServeAPI(ctx context.Context, socketPath string, ready chan<- error) error {
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		if ready != nil {
			ready <- err
		}
		return err
	}
	if ready != nil {
		ready <- nil
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return nil
		}
		go serveAPIConn(conn)
	}
}

func serveAPIConn(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		kind, payload, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		switch kind {
		case "VERSION":
			_, _ = fmt.Fprintf(conn, "VERSION:0.1:1:%d\n", os.Getpid())
		case "V2/HYDRATE_FILE":
			var msg apiMessage
			if err := json.Unmarshal([]byte(payload), &msg); err != nil {
				continue
			}
			_, _ = fmt.Fprintf(conn, "V2/HYDRATE_FILE_RESULT:{\"id\":%q,\"arguments\":{\"status\":\"ERROR\"}}\n", msg.ID)
		}
	}
}
