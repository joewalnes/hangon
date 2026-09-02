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

// envTimeoutOrDefault returns the duration named by HANGON_TIMEOUT (Go
// duration syntax, e.g. "30s", "1m"), or def if the env var is unset or
// fails to parse. Shared by NewSessionHolder (the holder's own connection
// deadline) and runExpect (main.go), the two places that are documented
// (see topicOutput's "Environment" section and `hangon expect --help`) to
// honor HANGON_TIMEOUT as the default expect timeout.
func envTimeoutOrDefault(def time.Duration) time.Duration {
	if v := os.Getenv("HANGON_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// SessionHolder manages a backend and serves CLI commands over a Unix socket.
type SessionHolder struct {
	backend    Backend
	socketPath string
	listener   net.Listener // guarded by mu: written by Serve, read by Close (may run on another goroutine)
	readCursor int64        // cursor for the default "read" client
	errCursor  int64        // cursor for stderr reads
	mu         sync.Mutex
	timeout    time.Duration
}

func NewSessionHolder(backend Backend, socketPath string) *SessionHolder {
	return &SessionHolder{
		backend:    backend,
		socketPath: socketPath,
		timeout:    envTimeoutOrDefault(defaultTimeout),
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
	sh.mu.Lock()
	sh.listener = ln
	sh.mu.Unlock()

	// Belt and braces alongside the 0700 runtime directory (see
	// runtimeDir in state.go): explicitly restrict the socket file
	// itself to the owner, in case net.Listen's umask-derived mode left
	// it wider than intended, or the containing directory's protection
	// is ever bypassed or misconfigured (e.g. a pre-existing socket path
	// passed in directly, bypassing runtimeDir entirely).
	if err := os.Chmod(sh.socketPath, 0600); err != nil {
		ln.Close()
		os.Remove(sh.socketPath)
		return fmt.Errorf("chmod socket %s: %w", sh.socketPath, err)
	}

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
	// Short deadline for reading the incoming request.
	conn.SetDeadline(time.Now().Add(30 * time.Second))

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		writeResponse(conn, &Response{OK: false, Error: "invalid request"})
		return
	}

	// Adjust deadline based on request type. Expect and wait can run
	// much longer than the default timeout.
	switch req.Method {
	case MethodExpect:
		var p ExpectParams
		json.Unmarshal(req.Params, &p)
		timeout := sh.timeout
		if p.Timeout > 0 {
			timeout = time.Duration(p.Timeout * float64(time.Second))
		}
		conn.SetDeadline(time.Now().Add(timeout + 10*time.Second))
	case MethodWait:
		conn.SetDeadline(time.Time{}) // no deadline
	default:
		conn.SetDeadline(time.Now().Add(sh.timeout + 5*time.Second))
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

	case MethodResize:
		var p ResizeParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return &Response{OK: false, Error: "bad params: " + err.Error()}
		}
		if p.Cols < minTerminalDim || p.Rows < minTerminalDim {
			return &Response{OK: false, Error: fmt.Sprintf("cols and rows must be >= %d", minTerminalDim)}
		}
		if p.Cols > maxTerminalDim || p.Rows > maxTerminalDim {
			return &Response{OK: false, Error: fmt.Sprintf("cols and rows must be <= %d", maxTerminalDim)}
		}
		rz, ok := sh.backend.(Resizer)
		if !ok {
			return &Response{OK: false, Error: "resize not supported by this backend type"}
		}
		if err := rz.Resize(p.Cols, p.Rows); err != nil {
			return &Response{OK: false, Error: err.Error()}
		}
		return &Response{OK: true, Result: fmt.Sprintf("resized to %dx%d", p.Cols, p.Rows)}

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

	case MethodMouseClick:
		var p MouseClickParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return &Response{OK: false, Error: "bad params: " + err.Error()}
		}
		seqs, err := mouseClick(p)
		if err != nil {
			return &Response{OK: false, Error: err.Error()}
		}
		if err := sendMouseSeqs(sh.backend, seqs, p.Count > 1); err != nil {
			return &Response{OK: false, Error: err.Error()}
		}
		return &Response{OK: true, Result: "ok"}

	case MethodMouseDrag:
		var p MouseDragParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return &Response{OK: false, Error: "bad params: " + err.Error()}
		}
		seqs, err := mouseDrag(p)
		if err != nil {
			return &Response{OK: false, Error: err.Error()}
		}
		if err := sendMouseSeqs(sh.backend, seqs, false); err != nil {
			return &Response{OK: false, Error: err.Error()}
		}
		return &Response{OK: true, Result: "ok"}

	case MethodMouseScroll:
		var p MouseScrollParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return &Response{OK: false, Error: "bad params: " + err.Error()}
		}
		seqs, err := mouseScroll(p)
		if err != nil {
			return &Response{OK: false, Error: err.Error()}
		}
		if err := sendMouseSeqs(sh.backend, seqs, false); err != nil {
			return &Response{OK: false, Error: err.Error()}
		}
		return &Response{OK: true, Result: "ok"}

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
	// Start searching from the current read cursor, not from the beginning.
	// This prevents matching patterns that were already consumed by previous
	// read/expect calls.
	sh.mu.Lock()
	cursor := sh.readCursor
	sh.mu.Unlock()

	result, endCursor, matched := expectFromBuffer(buf, cursor, re, deadline)
	if !matched {
		return &Response{OK: false, Error: fmt.Sprintf("expect %q timed out after %v", p.Pattern, timeout)}
	}

	// Advance the main read cursor to just past the match. Everything from
	// the original cursor through the match (including any pre-match bytes)
	// is returned in Result below, so nothing is lost; bytes after the
	// match are left on the buffer for a subsequent read/expect to see.
	sh.mu.Lock()
	if endCursor > sh.readCursor {
		sh.readCursor = endCursor
	}
	sh.mu.Unlock()
	return &Response{OK: true, Result: string(result)}
}

// expectFromBuffer polls buf starting at startCursor until re matches the
// accumulated bytes, or deadline passes.
//
// Each RingBuffer.ReadFrom call only returns bytes written since the last
// call, so a pattern that straddles two FIFO reads (e.g. ">>" arrives, then
// "> " arrives in the next chunk) would never match if matched chunk-by-chunk
// in isolation. To fix that, chunks are accumulated into a rolling buffer
// (acc) and re is matched against the accumulation as a whole, not against
// each new chunk alone.
//
// acc is bounded to buf.Size() bytes (the same capacity as the underlying
// ring buffer, so accumulation never holds more than the ring buffer itself
// could ever retain) so a chatty process that never produces a match can't
// grow acc without bound; once the cap is hit, the oldest bytes are dropped
// from the front. This means a match cannot be found once its start has
// aged out past that many bytes of intervening output — an accepted
// tradeoff for bounded memory, and no worse than the ring buffer's own
// retention limit.
//
// On success it returns every byte from startCursor through the end of the
// match (so pre-match bytes are never silently discarded) plus the absolute
// cursor position just past the match, which the caller should use to
// advance the session's read cursor. Bytes after the match are not
// consumed here and remain available to a later read.
func expectFromBuffer(buf *RingBuffer, startCursor int64, re *regexp.Regexp, deadline time.Time) (result []byte, endCursor int64, matched bool) {
	capBytes := buf.Size()

	cursor := startCursor
	accStart := startCursor
	var acc []byte

	// A single timer for the whole call, reused across loop iterations, so
	// a chatty backend producing many small writes doesn't allocate a new
	// unfired timer per iteration (time.After would leak one each time).
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()

	for {
		before := cursor
		if chunk := buf.ReadFrom(&cursor); len(chunk) > 0 {
			chunkStart := cursor - int64(len(chunk))
			if chunkStart != before {
				// The ring buffer overwrote bytes between `before` and
				// chunkStart before we got to read them (producer outran
				// the buffer between iterations). Those bytes are already
				// gone; resynchronize instead of silently splicing
				// non-contiguous data together.
				acc = nil
				accStart = chunkStart
			}
			acc = append(acc, chunk...)
			if len(acc) > capBytes {
				excess := len(acc) - capBytes
				acc = acc[excess:]
				accStart += int64(excess)
			}
		}

		if loc := re.FindIndex(acc); loc != nil {
			matchEnd := loc[1]
			out := make([]byte, matchEnd)
			copy(out, acc[:matchEnd])
			return out, accStart + int64(matchEnd), true
		}

		if time.Now().After(deadline) {
			return nil, 0, false
		}

		select {
		case <-buf.Notify():
		case <-timer.C:
			// Loop back around: one more read+match attempt, then the
			// deadline check above will report the timeout.
		}
	}
}

func (sh *SessionHolder) Close() {
	sh.mu.Lock()
	ln := sh.listener
	sh.mu.Unlock()
	if ln != nil {
		ln.Close()
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
