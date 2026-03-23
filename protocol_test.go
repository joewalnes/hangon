package main

import (
	"encoding/json"
	"testing"
)

// --- Protocol round-trip tests for mouse and video params ---

func TestProtocol_MouseClickParams(t *testing.T) {
	params := MouseClickParams{Row: 5, Col: 10, Button: "right"}
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	var decoded MouseClickParams
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Row != 5 || decoded.Col != 10 || decoded.Button != "right" {
		t.Errorf("got %+v, want {Row:5 Col:10 Button:right}", decoded)
	}
}

func TestProtocol_MouseClickDefaultButton(t *testing.T) {
	// When button is omitted, JSON should decode to empty string.
	raw := `{"row": 3, "col": 7}`
	var p MouseClickParams
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.Row != 3 || p.Col != 7 || p.Button != "" {
		t.Errorf("got %+v, want {Row:3 Col:7 Button:\"\"}", p)
	}
}

func TestProtocol_MouseDragParams(t *testing.T) {
	params := MouseDragParams{FromRow: 1, FromCol: 2, ToRow: 10, ToCol: 40, Button: "left"}
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	var decoded MouseDragParams
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != params {
		t.Errorf("got %+v, want %+v", decoded, params)
	}
}

func TestProtocol_MouseScrollParams(t *testing.T) {
	params := MouseScrollParams{Row: 5, Col: 20, Delta: -3}
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	var decoded MouseScrollParams
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Delta != -3 {
		t.Errorf("delta=%d, want -3", decoded.Delta)
	}
}

func TestProtocol_RecordStartParams(t *testing.T) {
	params := RecordStartParams{File: "demo.mp4", FPS: 15}
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	var decoded RecordStartParams
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.File != "demo.mp4" || decoded.FPS != 15 {
		t.Errorf("got %+v, want {File:demo.mp4 FPS:15}", decoded)
	}
}

func TestProtocol_RecordStartDefaultFPS(t *testing.T) {
	raw := `{"file": "out.mp4"}`
	var p RecordStartParams
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.FPS != 0 {
		t.Errorf("FPS=%f, want 0 (default handled by holder)", p.FPS)
	}
}

func TestProtocol_AllMouseMethods(t *testing.T) {
	methods := []string{
		MethodMouseClick, MethodMouseDown, MethodMouseUp,
		MethodMouseDoubleClick, MethodMouseTripleClick,
		MethodMouseDrag, MethodMouseScroll,
	}
	for _, m := range methods {
		req := Request{Method: m, Params: mustMarshal(MouseClickParams{Row: 1, Col: 1})}
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal %s: %v", m, err)
		}
		var decoded Request
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal %s: %v", m, err)
		}
		if decoded.Method != m {
			t.Errorf("method=%q, want %q", decoded.Method, m)
		}
	}
}

func TestProtocol_VideoMethods(t *testing.T) {
	for _, m := range []string{MethodRecordStart, MethodRecordStop} {
		req := Request{Method: m}
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal %s: %v", m, err)
		}
		var decoded Request
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal %s: %v", m, err)
		}
		if decoded.Method != m {
			t.Errorf("method=%q, want %q", decoded.Method, m)
		}
	}
}

// --- Holder dispatch tests with mock backend ---

type mockBackend struct {
	lastMethod string
	lastArgs   []interface{}
}

func (m *mockBackend) Start() error               { return nil }
func (m *mockBackend) Send(data []byte) error      { return nil }
func (m *mockBackend) Output() *RingBuffer         { return NewRingBuffer(1024) }
func (m *mockBackend) Stderr() *RingBuffer         { return nil }
func (m *mockBackend) Screen() (string, error)     { return "", nil }
func (m *mockBackend) SendKeys(keys string) error  { return nil }
func (m *mockBackend) Alive() bool                 { return true }
func (m *mockBackend) Wait() (int, error)          { return 0, nil }
func (m *mockBackend) TargetPID() int              { return 0 }
func (m *mockBackend) Close() error                { return nil }

// Implement MouseHandler.
func (m *mockBackend) MouseClick(row, col int, button string) error {
	m.lastMethod = "click"
	m.lastArgs = []interface{}{row, col, button}
	return nil
}
func (m *mockBackend) MouseDown(row, col int, button string) error {
	m.lastMethod = "down"
	m.lastArgs = []interface{}{row, col, button}
	return nil
}
func (m *mockBackend) MouseUp(row, col int, button string) error {
	m.lastMethod = "up"
	m.lastArgs = []interface{}{row, col, button}
	return nil
}
func (m *mockBackend) MouseDoubleClick(row, col int, button string) error {
	m.lastMethod = "double"
	m.lastArgs = []interface{}{row, col, button}
	return nil
}
func (m *mockBackend) MouseTripleClick(row, col int, button string) error {
	m.lastMethod = "triple"
	m.lastArgs = []interface{}{row, col, button}
	return nil
}
func (m *mockBackend) MouseDrag(fromRow, fromCol, toRow, toCol int, button string) error {
	m.lastMethod = "drag"
	m.lastArgs = []interface{}{fromRow, fromCol, toRow, toCol, button}
	return nil
}
func (m *mockBackend) MouseScroll(row, col, delta int) error {
	m.lastMethod = "scroll"
	m.lastArgs = []interface{}{row, col, delta}
	return nil
}

// Implement Screenshot.
func (m *mockBackend) Screenshot(file string) (string, error) {
	m.lastMethod = "screenshot"
	return file, nil
}

func TestHolder_DispatchMouseClick(t *testing.T) {
	mb := &mockBackend{}
	sh := &SessionHolder{backend: mb}

	req := &Request{
		Method: MethodMouseClick,
		Params: mustMarshal(MouseClickParams{Row: 5, Col: 10, Button: "left"}),
	}
	resp := sh.dispatchMouse(req)
	if !resp.OK {
		t.Fatalf("dispatchMouse failed: %s", resp.Error)
	}
	if mb.lastMethod != "click" {
		t.Errorf("method=%q, want click", mb.lastMethod)
	}
	if mb.lastArgs[0] != 5 || mb.lastArgs[1] != 10 {
		t.Errorf("args=%v, want [5 10 left]", mb.lastArgs)
	}
}

func TestHolder_DispatchMouseDown(t *testing.T) {
	mb := &mockBackend{}
	sh := &SessionHolder{backend: mb}

	req := &Request{
		Method: MethodMouseDown,
		Params: mustMarshal(MouseClickParams{Row: 3, Col: 7}),
	}
	resp := sh.dispatchMouse(req)
	if !resp.OK {
		t.Fatalf("dispatchMouse failed: %s", resp.Error)
	}
	if mb.lastMethod != "down" {
		t.Errorf("method=%q, want down", mb.lastMethod)
	}
	// Empty button should default to "left".
	if mb.lastArgs[2] != "left" {
		t.Errorf("button=%q, want left", mb.lastArgs[2])
	}
}

func TestHolder_DispatchMouseUp(t *testing.T) {
	mb := &mockBackend{}
	sh := &SessionHolder{backend: mb}

	req := &Request{
		Method: MethodMouseUp,
		Params: mustMarshal(MouseClickParams{Row: 3, Col: 7, Button: "right"}),
	}
	resp := sh.dispatchMouse(req)
	if !resp.OK {
		t.Fatalf("dispatchMouse failed: %s", resp.Error)
	}
	if mb.lastMethod != "up" {
		t.Errorf("method=%q, want up", mb.lastMethod)
	}
}

func TestHolder_DispatchMouseDrag(t *testing.T) {
	mb := &mockBackend{}
	sh := &SessionHolder{backend: mb}

	req := &Request{
		Method: MethodMouseDrag,
		Params: mustMarshal(MouseDragParams{FromRow: 1, FromCol: 2, ToRow: 10, ToCol: 40}),
	}
	resp := sh.dispatchMouse(req)
	if !resp.OK {
		t.Fatalf("dispatchMouse failed: %s", resp.Error)
	}
	if mb.lastMethod != "drag" {
		t.Errorf("method=%q, want drag", mb.lastMethod)
	}
	// Default button should be "left".
	if mb.lastArgs[4] != "left" {
		t.Errorf("button=%q, want left", mb.lastArgs[4])
	}
}

func TestHolder_DispatchMouseScroll(t *testing.T) {
	mb := &mockBackend{}
	sh := &SessionHolder{backend: mb}

	req := &Request{
		Method: MethodMouseScroll,
		Params: mustMarshal(MouseScrollParams{Row: 5, Col: 20, Delta: -3}),
	}
	resp := sh.dispatchMouse(req)
	if !resp.OK {
		t.Fatalf("dispatchMouse failed: %s", resp.Error)
	}
	if mb.lastMethod != "scroll" {
		t.Errorf("method=%q, want scroll", mb.lastMethod)
	}
	if mb.lastArgs[2] != -3 {
		t.Errorf("delta=%v, want -3", mb.lastArgs[2])
	}
}

func TestHolder_DispatchMouseBadParams(t *testing.T) {
	mb := &mockBackend{}
	sh := &SessionHolder{backend: mb}

	req := &Request{
		Method: MethodMouseClick,
		Params: json.RawMessage(`{invalid json`),
	}
	resp := sh.dispatchMouse(req)
	if resp.OK {
		t.Error("expected error for bad params")
	}
	if resp.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// --- Error path: mouse on non-mouse backend ---

type noMouseBackend struct {
	mockBackend
}

// Override: remove MouseHandler methods to make it NOT implement MouseHandler.
// Actually, Go embedding means it does implement it. Use a separate type.

type plainBackend struct{}

func (p *plainBackend) Start() error               { return nil }
func (p *plainBackend) Send(data []byte) error      { return nil }
func (p *plainBackend) Output() *RingBuffer         { return NewRingBuffer(1024) }
func (p *plainBackend) Stderr() *RingBuffer         { return nil }
func (p *plainBackend) Screen() (string, error)     { return "", nil }
func (p *plainBackend) SendKeys(keys string) error  { return nil }
func (p *plainBackend) Alive() bool                 { return true }
func (p *plainBackend) Wait() (int, error)          { return 0, nil }
func (p *plainBackend) TargetPID() int              { return 0 }
func (p *plainBackend) Close() error                { return nil }

func TestHolder_MouseUnsupportedBackend(t *testing.T) {
	sh := &SessionHolder{backend: &plainBackend{}}
	req := &Request{
		Method: MethodMouseClick,
		Params: mustMarshal(MouseClickParams{Row: 0, Col: 0}),
	}
	resp := sh.dispatchMouse(req)
	if resp.OK {
		t.Error("expected error for unsupported backend")
	}
	if resp.Error != "mouse interactions not supported by this backend type" {
		t.Errorf("error=%q, want unsupported message", resp.Error)
	}
}

func TestHolder_VideoUnsupportedBackend(t *testing.T) {
	sh := &SessionHolder{backend: &plainBackend{}}
	req := &Request{
		Method: MethodRecordStart,
		Params: mustMarshal(RecordStartParams{File: "test.mp4", FPS: 10}),
	}
	resp := sh.dispatchVideo(req)
	if resp.OK {
		t.Error("expected error for unsupported backend")
	}
	if resp.Error != "video recording not supported by this backend type" {
		t.Errorf("error=%q, want unsupported message", resp.Error)
	}
}

// --- Font fallback tests ---

func TestFontFallback_PrimaryForASCII(t *testing.T) {
	fs, err := loadFontSet(DefaultDirectRenderConfig)
	if err != nil {
		t.Fatal(err)
	}
	// ASCII characters should use primary font.
	face := fs.faceForRune('A')
	if face != fs.primary {
		t.Error("ASCII 'A' should use primary font")
	}
}

func TestFontFallback_FallbackForCJK(t *testing.T) {
	fs, err := loadFontSet(DefaultDirectRenderConfig)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs.fallbacks) == 0 {
		t.Skip("no fallback fonts available")
	}
	// CJK should use a fallback font (not primary, since JetBrains Mono doesn't have CJK).
	face := fs.faceForRune('日')
	if face == fs.primary {
		// This is only wrong if fallbacks have the glyph.
		for _, fb := range fs.fallbacks {
			if _, ok := fb.GlyphAdvance('日'); ok {
				t.Error("CJK '日' should use fallback font, not primary")
				break
			}
		}
	}
}

func TestFontFallback_UnknownRuneReturnsPrimary(t *testing.T) {
	fs, err := loadFontSet(DefaultDirectRenderConfig)
	if err != nil {
		t.Fatal(err)
	}
	// A very obscure rune unlikely to be in any font should fall back to primary.
	face := fs.faceForRune(0x10FFFD) // last valid private-use codepoint
	if face != fs.primary {
		t.Error("unknown rune should fall back to primary font")
	}
}

// --- CJK embedded font decompression test ---

func TestEmbeddedCJKFont_Decompresses(t *testing.T) {
	data := getEmbeddedCJKFont()
	if len(data) == 0 {
		t.Fatal("embedded CJK font decompressed to empty")
	}
	t.Logf("CJK font decompressed: %d bytes (%.1f MB)", len(data), float64(len(data))/1024/1024)

	// Should be larger than the compressed version.
	if len(data) <= len(embeddedCJKFontGz) {
		t.Errorf("decompressed (%d) should be larger than compressed (%d)", len(data), len(embeddedCJKFontGz))
	}
}

// --- Wide character rendering correctness ---

func TestRenderWideCharContinuation(t *testing.T) {
	grid := &ScreenGrid{
		Rows:  1,
		Cols:  10,
		Cells: make([][]Cell, 1),
	}
	grid.Cells[0] = make([]Cell, 10)
	for c := 0; c < 10; c++ {
		grid.Cells[0][c] = Cell{Char: ' ', Width: 1}
	}

	// Place a wide character at col 0 (takes cols 0-1).
	grid.Cells[0][0] = Cell{Char: '日', Width: 2, Style: CellStyle{FG: "#f38ba8"}}
	grid.Cells[0][1] = Cell{Char: 0, Width: 0} // continuation
	// Normal character at col 2.
	grid.Cells[0][2] = Cell{Char: 'A', Width: 1}

	tmpFile := t.TempDir() + "/wide.png"
	_, err := RenderPNGDirect(grid, tmpFile)
	if err != nil {
		t.Fatalf("render wide char: %v", err)
	}
}
