package main

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// TCPBackend manages a persistent TCP connection.
type TCPBackend struct {
	address string
	conn    net.Conn
	output  *RingBuffer
	done    chan struct{}
	mu      sync.Mutex
	err     error
}

func NewTCPBackend(address string) *TCPBackend {
	return &TCPBackend{
		address: address,
		output:  NewRingBuffer(defaultBufSize),
		done:    make(chan struct{}),
	}
}

func (tb *TCPBackend) Start() error {
	conn, err := net.DialTimeout("tcp", tb.address, 30*time.Second)
	if err != nil {
		return fmt.Errorf("tcp connect to %s: %w", tb.address, err)
	}
	tb.conn = conn

	// Read loop.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				tb.output.Write(buf[:n])
			}
			if err != nil {
				tb.mu.Lock()
				tb.err = err
				tb.mu.Unlock()
				close(tb.done)
				return
			}
		}
	}()

	return nil
}

func (tb *TCPBackend) Send(data []byte) error {
	if tb.conn == nil {
		return fmt.Errorf("not connected")
	}
	_, err := tb.conn.Write(data)
	return err
}

func (tb *TCPBackend) Output() *RingBuffer {
	return tb.output
}

func (tb *TCPBackend) Stderr() *RingBuffer {
	return nil
}

func (tb *TCPBackend) Screen() (string, error) {
	return "", ErrNotSupported
}

func (tb *TCPBackend) SendKeys(keys string) error {
	// For TCP, send keys as raw bytes using the same key map.
	for _, key := range strings.Fields(keys) {
		b, ok := keyMap[strings.ToLower(key)]
		if !ok {
			return fmt.Errorf("unknown key: %s", key)
		}
		if err := tb.Send(b); err != nil {
			return err
		}
	}
	return nil
}

func (tb *TCPBackend) Alive() bool {
	select {
	case <-tb.done:
		return false
	default:
		return true
	}
}

func (tb *TCPBackend) Wait() (int, error) {
	<-tb.done
	return 0, nil
}

func (tb *TCPBackend) TargetPID() int {
	return 0
}

func (tb *TCPBackend) Close() error {
	if tb.conn != nil {
		return tb.conn.Close()
	}
	return nil
}
