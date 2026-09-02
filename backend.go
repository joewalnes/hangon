package main

import "errors"

// ErrNotSupported is returned when a backend doesn't support an operation.
var ErrNotSupported = errors.New("not supported by this backend")

// Backend is the interface all session types implement.
type Backend interface {
	// Start initializes the connection/process.
	Start() error

	// Send sends raw data to the target.
	Send(data []byte) error

	// Output returns the stdout/received data ring buffer.
	Output() *RingBuffer

	// Stderr returns the stderr ring buffer (nil if not applicable).
	Stderr() *RingBuffer

	// Screen returns the current terminal screen state.
	// Returns ErrNotSupported if the backend doesn't support it.
	Screen() (string, error)

	// SendKeys sends special key sequences (e.g., ctrl-c, up arrow).
	// Returns ErrNotSupported if the backend doesn't support it.
	SendKeys(keys string) error

	// Alive returns true if the target is still running/connected.
	Alive() bool

	// Wait blocks until the target exits, returning the exit code.
	// Returns ErrNotSupported for backends that don't have an exit concept.
	Wait() (int, error)

	// TargetPID returns the PID of the target process (0 if N/A).
	TargetPID() int

	// Close tears down the backend and cleans up resources.
	Close() error
}

// Screenshotter is an optional interface for backends that support visual screenshots.
type Screenshotter interface {
	Screenshot(file string) (string, error)
}

// Resizer is an optional interface for backends that support changing the
// terminal's size after it has started. Backends that have no notion of a
// terminal grid (tcp, ws, macos) don't implement it; the session holder
// dispatch returns a clear "not supported" error for those rather than
// silently accepting and ignoring a resize request.
type Resizer interface {
	Resize(cols, rows int) error
}
