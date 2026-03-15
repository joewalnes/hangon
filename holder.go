package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

const defaultTimeout = 30 * time.Second

// SessionHolder manages a backend and serves CLI commands over a Unix socket.
type SessionHolder struct {
	backend    Backend
	socketPath string
	listener   net.Listener
	readCursor int64 // cursor for the default "read" client
	errCursor  int64 // cursor for stderr reads
	mu         sync.Mutex
	timeout    time.Duration
}

func NewSessionHolder(backend Backend, socketPath string) *SessionHolder {
	timeout := defaultTimeout
	if v := os.Getenv("HANGON_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			timeout = d
		}
	}
	return &SessionHolder{
		backend:    backend,
		socketPath: socketPath,
		timeout:    timeout,
	}
}

// Serve starts the Unix socket listener and handles connections.
func (sh *SessionHolder) Serve() error {
	// Clean up stale socket.
	os.Remove(sh.socketPath)

	ln, err := net.Listen("unix", sh.socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", sh.socketPath, err)
	}
	sh.listener = ln

	// Handle shutdown signals.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		sh.Close()
		os.Exit(0)
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				return nil
			}
			log.Printf("accept error: %v", err)
			continue
		}
		go sh.handleConn(conn)
	}
}

func (sh *SessionHolder) handleConn(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(sh.timeout + 5*time.Second))

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		writeResponse(conn, &Response{OK: false, Error: "invalid request"})
		return
	}

	resp := sh.dispatch(&req)
	writeResponse(conn, resp)
}

func (sh *SessionHolder) dispatch(req *Request) *Response {
	switch req.Method {
	case MethodPing:
		return &Response{OK: true, Result: "pong"}

	case MethodInfo:
		alive := sh.backend.Alive()
		return &Response{OK: true, Result: fmt.Sprintf("alive=%v", alive)}

	case MethodSend:
		var p SendParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return &Response{OK: false, Error: "bad params: " + err.Error()}
		}
		if err := sh.backend.Send([]byte(p.Data)); err != nil {
			return &Response{OK: false, Error: err.Error()}
		}
		return &Response{OK: true, Result: fmt.Sprintf("%d bytes sent", len(p.Data))}

	case MethodRead:
		sh.mu.Lock()
		data := sh.backend.Output().ReadFrom(&sh.readCursor)
		sh.mu.Unlock()
		return &Response{OK: true, Result: string(data)}

	case MethodReadAll:
		data := sh.backend.Output().ReadAll()
		return &Response{OK: true, Result: string(data)}

	case MethodStderr:
		buf := sh.backend.Stderr()
		if buf == nil {
			return &Response{OK: false, Error: "stderr not available for this backend"}
		}
		sh.mu.Lock()
		data := buf.ReadFrom(&sh.errCursor)
		sh.mu.Unlock()
		return &Response{OK: true, Result: string(data)}

	case MethodExpect:
		var p ExpectParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return &Response{OK: false, Error: "bad params: " + err.Error()}
		}
		return sh.doExpect(p)

	case MethodScreen:
		s, err := sh.backend.Screen()
		if err != nil {
			return &Response{OK: false, Error: err.Error()}
		}
		return &Response{OK: true, Result: s}

	case MethodKeys:
		var p KeysParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return &Response{OK: false, Error: "bad params: " + err.Error()}
		}
		if err := sh.backend.SendKeys(p.Keys); err != nil {
			return &Response{OK: false, Error: err.Error()}
		}
		return &Response{OK: true, Result: "ok"}

	case MethodAlive:
		if sh.backend.Alive() {
			return &Response{OK: true, Result: "true"}
		}
		return &Response{OK: true, Result: "false"}

	case MethodWait:
		code, err := sh.backend.Wait()
		if err != nil {
			return &Response{OK: false, Error: err.Error()}
		}
		return &Response{OK: true, Result: fmt.Sprintf("%d", code)}

	case MethodScreenshot:
		return sh.dispatchScreenshot(req)

	// macOS methods are dispatched to the backend if it supports them.
	case MethodAxTree, MethodAxFind, MethodClick, MethodType:
		return sh.dispatchMacOS(req)

	default:
		return &Response{OK: false, Error: fmt.Sprintf("unknown method: %s", req.Method)}
	}
}

func (sh *SessionHolder) dispatchScreenshot(req *Request) *Response {
	var p ScreenshotParams
	if req.Params != nil {
		json.Unmarshal(req.Params, &p)
	}

	// Try the Screenshotter interface first (process backend with tmux).
	if ss, ok := sh.backend.(Screenshotter); ok {
		file, err := ss.Screenshot(p.File)
		if err != nil {
			return &Response{OK: false, Error: err.Error()}
		}
		return &Response{OK: true, Result: file}
	}

	// Try the MacOSBackend interface.
	if mb, ok := sh.backend.(MacOSBackend); ok {
		file, err := mb.Screenshot(p.File)
		if err != nil {
			return &Response{OK: false, Error: err.Error()}
		}
		return &Response{OK: true, Result: file}
	}

	return &Response{OK: false, Error: "screenshot not supported by this backend type"}
}

func (sh *SessionHolder) dispatchMacOS(req *Request) *Response {
	mb, ok := sh.backend.(MacOSBackend)
	if !ok {
		return &Response{OK: false, Error: "method not supported by this backend type"}
	}
	switch req.Method {
	case MethodAxTree:
		result, err := mb.AxTree()
		if err != nil {
			return &Response{OK: false, Error: err.Error()}
		}
		return &Response{OK: true, Result: result}
	case MethodAxFind:
		var p AxFindParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return &Response{OK: false, Error: "bad params: " + err.Error()}
		}
		result, err := mb.AxFind(p.Role, p.Name)
		if err != nil {
			return &Response{OK: false, Error: err.Error()}
		}
		return &Response{OK: true, Result: result}
	case MethodClick:
		var p ClickParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return &Response{OK: false, Error: "bad params: " + err.Error()}
		}
		if err := mb.Click(p.Element); err != nil {
			return &Response{OK: false, Error: err.Error()}
		}
		return &Response{OK: true, Result: "ok"}
	case MethodType:
		var p TypeParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return &Response{OK: false, Error: "bad params: " + err.Error()}
		}
		if err := mb.TypeText(p.Text); err != nil {
			return &Response{OK: false, Error: err.Error()}
		}
		return &Response{OK: true, Result: "ok"}
	}
	return &Response{OK: false, Error: "unknown macOS method"}
}

func (sh *SessionHolder) doExpect(p ExpectParams) *Response {
	re, err := regexp.Compile(p.Pattern)
	if err != nil {
		return &Response{OK: false, Error: "bad regex: " + err.Error()}
	}

	timeout := sh.timeout
	if p.Timeout > 0 {
		timeout = time.Duration(p.Timeout * float64(time.Second))
	}
	deadline := time.Now().Add(timeout)

	buf := sh.backend.Output()
	// Read all current content and search.
	var cursor int64
	for {
		data := buf.ReadFrom(&cursor)
		if loc := re.Find(data); loc != nil {
			// Advance the main read cursor past the match.
			sh.mu.Lock()
			if cursor > sh.readCursor {
				sh.readCursor = cursor
			}
			sh.mu.Unlock()
			return &Response{OK: true, Result: string(data)}
		}

		if time.Now().After(deadline) {
			return &Response{OK: false, Error: fmt.Sprintf("expect %q timed out after %v", p.Pattern, timeout)}
		}

		// Wait for more data.
		select {
		case <-buf.Notify():
		case <-time.After(time.Until(deadline)):
			// Check one more time.
			data = buf.ReadFrom(&cursor)
			if loc := re.Find(data); loc != nil {
				sh.mu.Lock()
				if cursor > sh.readCursor {
					sh.readCursor = cursor
				}
				sh.mu.Unlock()
				return &Response{OK: true, Result: string(data)}
			}
			return &Response{OK: false, Error: fmt.Sprintf("expect %q timed out after %v", p.Pattern, timeout)}
		}
	}
}

func (sh *SessionHolder) Close() {
	if sh.listener != nil {
		sh.listener.Close()
	}
	if sh.backend != nil {
		sh.backend.Close()
	}
	os.Remove(sh.socketPath)
}

func writeResponse(conn net.Conn, resp *Response) {
	json.NewEncoder(conn).Encode(resp)
}

// MacOSBackend is an extended interface for macOS-specific operations.
type MacOSBackend interface {
	Backend
	AxTree() (string, error)
	AxFind(role, name string) (string, error)
	Click(element string) error
	TypeText(text string) error
	Screenshot(file string) (string, error)
}
