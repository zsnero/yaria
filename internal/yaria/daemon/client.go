package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
)

// Send sends a request to the daemon and returns the response
func Send(req Request) (Response, error) {
	conn, err := net.Dial("unix", SocketPath())
	if err != nil {
		return Response{}, fmt.Errorf("cannot connect to daemon: %w", err)
	}
	defer conn.Close()

	data, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	fmt.Fprintf(conn, "%s\n", data)

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	if !scanner.Scan() {
		return Response{}, fmt.Errorf("no response from daemon")
	}

	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}
