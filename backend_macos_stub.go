//go:build !darwin

package main

import "fmt"

func NewMacOSBackend(appName string) Backend {
	return &macOSStub{}
}

type macOSStub struct{}

func (m *macOSStub) Start() error { return fmt.Errorf("macOS backend is only available on darwin") }
func (m *macOSStub) Send(data []byte) error {
	return fmt.Errorf("macOS backend is only available on darwin")
}
func (m *macOSStub) Output() *RingBuffer        { return NewRingBuffer(0) }
func (m *macOSStub) Stderr() *RingBuffer        { return nil }
func (m *macOSStub) Screen() (string, error)    { return "", ErrNotSupported }
func (m *macOSStub) SendKeys(keys string) error { return ErrNotSupported }
func (m *macOSStub) Alive() bool                { return false }
func (m *macOSStub) Wait() (int, error)         { return -1, ErrNotSupported }
func (m *macOSStub) TargetPID() int             { return 0 }
func (m *macOSStub) Close() error               { return nil }
