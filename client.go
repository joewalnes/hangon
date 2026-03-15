package main

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// clientSend connects to a session holder via Unix socket, sends a request,
// and returns the response.
func clientSend(socketPath string, req *Request, timeout time.Duration) (*Response, error) {
	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to session holder: %w", err)
	}
	defer conn.Close()

	if timeout > 0 {
		conn.SetDeadline(time.Now().Add(timeout))
	}

	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	var resp Response
	dec := json.NewDecoder(conn)
	if err := dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	return &resp, nil
}

// clientSendSimple is a convenience for methods with no params.
func clientSendSimple(socketPath, method string, timeout time.Duration) (*Response, error) {
	return clientSend(socketPath, &Request{Method: method}, timeout)
}

// clientSendJSON is a convenience for methods with JSON params.
func clientSendJSON(socketPath, method string, params interface{}, timeout time.Duration) (*Response, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return clientSend(socketPath, &Request{Method: method, Params: raw}, timeout)
}
