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

	// Mouse interaction methods (ghostty backend)
	MethodMouseClick       = "mouse-click"
	MethodMouseDoubleClick = "mouse-double-click"
	MethodMouseTripleClick = "mouse-triple-click"
	MethodMouseDrag        = "mouse-drag"
	MethodMouseScroll      = "mouse-scroll"

	// Video recording methods
	MethodRecordStart = "record-start"
	MethodRecordStop  = "record-stop"
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

// Mouse interaction params.

type MouseClickParams struct {
	Row    int    `json:"row"`
	Col    int    `json:"col"`
	Button string `json:"button,omitempty"` // "left" (default), "right", "middle"
}

type MouseDragParams struct {
	FromRow int    `json:"from_row"`
	FromCol int    `json:"from_col"`
	ToRow   int    `json:"to_row"`
	ToCol   int    `json:"to_col"`
	Button  string `json:"button,omitempty"` // default "left"
}

type MouseScrollParams struct {
	Row   int `json:"row"`
	Col   int `json:"col"`
	Delta int `json:"delta"` // positive=up, negative=down
}

// Video recording params.

type RecordStartParams struct {
	File string  `json:"file"`
	FPS  float64 `json:"fps,omitempty"` // default 10
}

