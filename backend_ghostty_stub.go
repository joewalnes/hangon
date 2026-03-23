//go:build !ghostty

package main

import "fmt"

// GhosttyBackend is unavailable when built without the "ghostty" build tag.
// Build with: go build -tags ghostty
type GhosttyBackend struct{}

func NewGhosttyBackend(command []string) *GhosttyBackend {
	return &GhosttyBackend{}
}

func (gb *GhosttyBackend) Start() error {
	return fmt.Errorf("ghostty backend not available: rebuild with -tags ghostty (requires libghostty-vt)")
}

func (gb *GhosttyBackend) Send(data []byte) error        { return ErrNotSupported }
func (gb *GhosttyBackend) Output() *RingBuffer            { return NewRingBuffer(1) }
func (gb *GhosttyBackend) Stderr() *RingBuffer            { return nil }
func (gb *GhosttyBackend) Screen() (string, error)        { return "", ErrNotSupported }
func (gb *GhosttyBackend) SendKeys(keys string) error     { return ErrNotSupported }
func (gb *GhosttyBackend) Alive() bool                    { return false }
func (gb *GhosttyBackend) Wait() (int, error)             { return -1, ErrNotSupported }
func (gb *GhosttyBackend) TargetPID() int                 { return 0 }
func (gb *GhosttyBackend) Close() error                   { return nil }
func (gb *GhosttyBackend) Screenshot(file string) (string, error) { return "", ErrNotSupported }
