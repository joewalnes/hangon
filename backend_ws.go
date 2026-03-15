package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

// WSBackend manages a persistent WebSocket connection.
type WSBackend struct {
	url    string
	conn   *websocket.Conn
	output *RingBuffer
	done   chan struct{}
	mu     sync.Mutex
	err    error
}

func NewWSBackend(url string) *WSBackend {
	return &WSBackend{
		url:    url,
		output: NewRingBuffer(defaultBufSize),
		done:   make(chan struct{}),
	}
}

func (wb *WSBackend) Start() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wb.url, nil)
	if err != nil {
		return fmt.Errorf("websocket connect to %s: %w", wb.url, err)
	}
	wb.conn = conn

	// Read loop - buffer messages as lines.
	go func() {
		for {
			_, msg, err := conn.Read(context.Background())
			if err != nil {
				wb.mu.Lock()
				wb.err = err
				wb.mu.Unlock()
				close(wb.done)
				return
			}
			// Write each message as a line.
			line := string(msg)
			if !strings.HasSuffix(line, "\n") {
				line += "\n"
			}
			wb.output.Write([]byte(line))
		}
	}()

	return nil
}

func (wb *WSBackend) Send(data []byte) error {
	if wb.conn == nil {
		return fmt.Errorf("not connected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return wb.conn.Write(ctx, websocket.MessageText, data)
}

func (wb *WSBackend) Output() *RingBuffer {
	return wb.output
}

func (wb *WSBackend) Stderr() *RingBuffer {
	return nil
}

func (wb *WSBackend) Screen() (string, error) {
	return "", ErrNotSupported
}

func (wb *WSBackend) SendKeys(keys string) error {
	return ErrNotSupported
}

func (wb *WSBackend) Alive() bool {
	select {
	case <-wb.done:
		return false
	default:
		return true
	}
}

func (wb *WSBackend) Wait() (int, error) {
	<-wb.done
	return 0, nil
}

func (wb *WSBackend) TargetPID() int {
	return 0
}

func (wb *WSBackend) Close() error {
	if wb.conn != nil {
		return wb.conn.Close(websocket.StatusNormalClosure, "hangon stop")
	}
	return nil
}
