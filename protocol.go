package main

import "encoding/json"

// Request is sent from CLI client to session holder over Unix socket.
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is sent from session holder back to CLI client.
type Response struct {
	OK     bool   `json:"ok"`
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Param types for specific methods.

type SendParams struct {
	Data string `json:"data"`
}

type ExpectParams struct {
	Pattern string  `json:"pattern"`
	Timeout float64 `json:"timeout"` // seconds, 0 = use default
}

type KeysParams struct {
	Keys string `json:"keys"`
}

// Methods
const (
	MethodSend    = "send"
	MethodRead    = "read"
	MethodReadAll = "readall"
	MethodStderr  = "stderr"
	MethodExpect  = "expect"
	MethodScreen  = "screen"
	MethodKeys    = "keys"
	MethodAlive   = "alive"
	MethodWait    = "wait"
	MethodInfo    = "info"
	MethodPing    = "ping"

	// macOS-specific
	MethodAxTree     = "ax-tree"
	MethodAxFind     = "ax-find"
	MethodClick      = "click"
	MethodType       = "type"
	MethodScreenshot = "screenshot"
)

type AxFindParams struct {
	Role string `json:"role,omitempty"`
	Name string `json:"name,omitempty"`
}

type ClickParams struct {
	Element string `json:"element"`
}

type TypeParams struct {
	Text string `json:"text"`
}

type ScreenshotParams struct {
	File string `json:"file,omitempty"`
}
