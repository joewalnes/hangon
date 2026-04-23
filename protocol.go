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

	// Mouse events (SGR terminal mouse sequences)
	MethodMouseClick  = "mouse-click"
	MethodMouseDrag   = "mouse-drag"
	MethodMouseScroll = "mouse-scroll"

	// macOS-specific
	MethodAxTree     = "ax-tree"
	MethodAxFind     = "ax-find"
	MethodClick      = "click"
	MethodType       = "type"
	MethodScreenshot = "screenshot"
)

// Mouse event params.

type MouseClickParams struct {
	X      int    `json:"x"`      // column (1-based)
	Y      int    `json:"y"`      // row (1-based)
	Button string `json:"button"` // "left", "right", "middle" (default "left")
	Count  int    `json:"count"`  // click count: 1=single, 2=double, 3=triple (default 1)
	Shift  bool   `json:"shift"`
	Alt    bool   `json:"alt"`
	Ctrl   bool   `json:"ctrl"`
}

type MouseDragParams struct {
	FromX int  `json:"from_x"` // start column (1-based)
	FromY int  `json:"from_y"` // start row (1-based)
	ToX   int  `json:"to_x"`   // end column (1-based)
	ToY   int  `json:"to_y"`   // end row (1-based)
	Steps int  `json:"steps"`  // intermediate move events (default 1 = direct)
	Shift bool `json:"shift"`
	Alt   bool `json:"alt"`
	Ctrl  bool `json:"ctrl"`
}

type MouseScrollParams struct {
	X     int  `json:"x"`     // column (1-based)
	Y     int  `json:"y"`     // row (1-based)
	Delta int  `json:"delta"` // negative=up, positive=down
	Shift bool `json:"shift"`
	Alt   bool `json:"alt"`
	Ctrl  bool `json:"ctrl"`
}

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
