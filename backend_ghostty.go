//go:build ghostty

package main

/*
#cgo CFLAGS: -I${SRCDIR}/ghostty/include
#cgo LDFLAGS: -L${SRCDIR}/ghostty/lib -lghostty-vt -lm

#include <ghostty/vt.h>
#include <stdlib.h>
#include <string.h>

// --- Wrapper: snapshot terminal grid into a flat C struct for Go ---

typedef struct {
	uint32_t codepoint;
	uint8_t  wide;       // GhosttyCellWide value
	uint8_t  has_text;
	uint8_t  bold;
	uint8_t  italic;
	uint8_t  faint;
	uint8_t  strikethrough;
	uint8_t  inverse;
	uint8_t  underline;
	// Colors: tag 0=none, 1=palette, 2=rgb
	uint8_t  fg_tag;
	uint8_t  fg_r, fg_g, fg_b;
	uint8_t  fg_palette;
	uint8_t  bg_tag;
	uint8_t  bg_r, bg_g, bg_b;
	uint8_t  bg_palette;
	// Grapheme buffer (for multi-codepoint graphemes)
	uint32_t grapheme_buf[8];
	size_t   grapheme_len;
} hangon_cell_t;

typedef struct {
	int rows;
	int cols;
	int cursor_x;
	int cursor_y;
	int cursor_visible;
	int cursor_style; // GhosttyRenderStateCursorVisualStyle
	uint8_t default_bg_r, default_bg_g, default_bg_b;
	uint8_t default_fg_r, default_fg_g, default_fg_b;
	hangon_cell_t *cells; // rows * cols flat array
} hangon_snapshot_t;

static hangon_snapshot_t* hangon_snapshot(GhosttyTerminal terminal) {
	GhosttyRenderState rs = NULL;
	if (ghostty_render_state_new(NULL, &rs) != GHOSTTY_SUCCESS) return NULL;
	if (ghostty_render_state_update(rs, terminal) != GHOSTTY_SUCCESS) {
		ghostty_render_state_free(rs);
		return NULL;
	}

	hangon_snapshot_t *snap = (hangon_snapshot_t*)calloc(1, sizeof(hangon_snapshot_t));

	// Get dimensions
	uint16_t cols = 0, rows = 0;
	ghostty_render_state_get(rs, GHOSTTY_RENDER_STATE_DATA_COLS, &cols);
	ghostty_render_state_get(rs, GHOSTTY_RENDER_STATE_DATA_ROWS, &rows);
	snap->cols = cols;
	snap->rows = rows;

	// Get cursor info
	bool cursor_visible = false;
	ghostty_render_state_get(rs, GHOSTTY_RENDER_STATE_DATA_CURSOR_VISIBLE, &cursor_visible);
	snap->cursor_visible = cursor_visible ? 1 : 0;

	GhosttyRenderStateCursorVisualStyle cursor_style = GHOSTTY_RENDER_STATE_CURSOR_VISUAL_STYLE_BLOCK;
	ghostty_render_state_get(rs, GHOSTTY_RENDER_STATE_DATA_CURSOR_VISUAL_STYLE, &cursor_style);
	snap->cursor_style = (int)cursor_style;

	bool cursor_has_value = false;
	ghostty_render_state_get(rs, GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_HAS_VALUE, &cursor_has_value);
	if (cursor_has_value) {
		uint16_t cx = 0, cy = 0;
		ghostty_render_state_get(rs, GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_X, &cx);
		ghostty_render_state_get(rs, GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_Y, &cy);
		snap->cursor_x = cx;
		snap->cursor_y = cy;
	}

	// Get default colors
	GhosttyRenderStateColors colors = GHOSTTY_INIT_SIZED(GhosttyRenderStateColors);
	if (ghostty_render_state_colors_get(rs, &colors) == GHOSTTY_SUCCESS) {
		snap->default_bg_r = colors.background.r;
		snap->default_bg_g = colors.background.g;
		snap->default_bg_b = colors.background.b;
		snap->default_fg_r = colors.foreground.r;
		snap->default_fg_g = colors.foreground.g;
		snap->default_fg_b = colors.foreground.b;
	}

	// Allocate cells
	snap->cells = (hangon_cell_t*)calloc(rows * cols, sizeof(hangon_cell_t));

	// Get row iterator
	GhosttyRenderStateRowIterator row_iter = NULL;
	ghostty_render_state_row_iterator_new(NULL, &row_iter);

	// Link iterator to render state
	ghostty_render_state_get(rs, GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR, &row_iter);

	int row_idx = 0;
	while (row_idx < rows && ghostty_render_state_row_iterator_next(row_iter)) {
		// Get cells for this row
		GhosttyRenderStateRowCells row_cells = NULL;
		ghostty_render_state_row_cells_new(NULL, &row_cells);

		// Link cells to current row
		ghostty_render_state_row_get(row_iter, GHOSTTY_RENDER_STATE_ROW_DATA_CELLS, &row_cells);

		int col_idx = 0;
		while (col_idx < cols && ghostty_render_state_row_cells_next(row_cells)) {
			hangon_cell_t *c = &snap->cells[row_idx * cols + col_idx];

			// Get style
			GhosttyStyle style;
			ghostty_style_default(&style);
			ghostty_render_state_row_cells_get(row_cells,
				GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_STYLE, &style);

			c->bold = style.bold ? 1 : 0;
			c->italic = style.italic ? 1 : 0;
			c->faint = style.faint ? 1 : 0;
			c->strikethrough = style.strikethrough ? 1 : 0;
			c->inverse = style.inverse ? 1 : 0;
			c->underline = style.underline > 0 ? 1 : 0;

			// FG color
			c->fg_tag = (uint8_t)style.fg_color.tag;
			if (style.fg_color.tag == GHOSTTY_STYLE_COLOR_RGB) {
				c->fg_r = style.fg_color.value.rgb.r;
				c->fg_g = style.fg_color.value.rgb.g;
				c->fg_b = style.fg_color.value.rgb.b;
			} else if (style.fg_color.tag == GHOSTTY_STYLE_COLOR_PALETTE) {
				c->fg_palette = style.fg_color.value.palette;
				// Resolve palette color
				if (c->fg_palette < 256) {
					c->fg_r = colors.palette[c->fg_palette].r;
					c->fg_g = colors.palette[c->fg_palette].g;
					c->fg_b = colors.palette[c->fg_palette].b;
					c->fg_tag = 2; // treat as RGB for Go
				}
			}

			// BG color
			c->bg_tag = (uint8_t)style.bg_color.tag;
			if (style.bg_color.tag == GHOSTTY_STYLE_COLOR_RGB) {
				c->bg_r = style.bg_color.value.rgb.r;
				c->bg_g = style.bg_color.value.rgb.g;
				c->bg_b = style.bg_color.value.rgb.b;
			} else if (style.bg_color.tag == GHOSTTY_STYLE_COLOR_PALETTE) {
				c->bg_palette = style.bg_color.value.palette;
				if (c->bg_palette < 256) {
					c->bg_r = colors.palette[c->bg_palette].r;
					c->bg_g = colors.palette[c->bg_palette].g;
					c->bg_b = colors.palette[c->bg_palette].b;
					c->bg_tag = 2;
				}
			}

			// Get graphemes
			size_t glen = 0;
			ghostty_render_state_row_cells_get(row_cells,
				GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_LEN, &glen);
			c->grapheme_len = glen < 8 ? glen : 8;
			if (glen > 0) {
				ghostty_render_state_row_cells_get(row_cells,
					GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_BUF, c->grapheme_buf);
			}

			// Get raw cell for wide/codepoint info
			GhosttyCell raw_cell = 0;
			ghostty_render_state_row_cells_get(row_cells,
				GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_RAW, &raw_cell);

			uint32_t cp = 0;
			ghostty_cell_get(raw_cell, GHOSTTY_CELL_DATA_CODEPOINT, &cp);
			c->codepoint = cp;

			GhosttyCellWide wide = GHOSTTY_CELL_WIDE_NARROW;
			ghostty_cell_get(raw_cell, GHOSTTY_CELL_DATA_WIDE, &wide);
			c->wide = (uint8_t)wide;

			bool has_text = false;
			ghostty_cell_get(raw_cell, GHOSTTY_CELL_DATA_HAS_TEXT, &has_text);
			c->has_text = has_text ? 1 : 0;

			col_idx++;
		}
		ghostty_render_state_row_cells_free(row_cells);
		row_idx++;
	}
	ghostty_render_state_row_iterator_free(row_iter);
	ghostty_render_state_free(rs);
	return snap;
}

static void hangon_snapshot_free(hangon_snapshot_t *snap) {
	if (!snap) return;
	free(snap->cells);
	free(snap);
}

// --- Key encoding wrapper ---
static int hangon_encode_key(GhosttyTerminal t, int key, int mods, int action,
                              const char *utf8, size_t utf8_len,
                              char *buf, size_t buf_len) {
	GhosttyKeyEncoder enc = NULL;
	if (ghostty_key_encoder_new(NULL, &enc) != GHOSTTY_SUCCESS) return 0;
	ghostty_key_encoder_setopt_from_terminal(enc, t);

	GhosttyKeyEvent event = NULL;
	if (ghostty_key_event_new(NULL, &event) != GHOSTTY_SUCCESS) {
		ghostty_key_encoder_free(enc);
		return 0;
	}

	ghostty_key_event_set_action(event, (GhosttyKeyAction)action);
	ghostty_key_event_set_key(event, (GhosttyKey)key);
	ghostty_key_event_set_mods(event, (GhosttyMods)mods);
	if (utf8 && utf8_len > 0) {
		ghostty_key_event_set_utf8(event, utf8, utf8_len);
	}

	size_t out_len = 0;
	GhosttyResult r = ghostty_key_encoder_encode(enc, event, buf, buf_len, &out_len);

	ghostty_key_event_free(event);
	ghostty_key_encoder_free(enc);
	return (r == GHOSTTY_SUCCESS) ? (int)out_len : 0;
}

// --- Mouse encoding wrapper ---
static int hangon_encode_mouse(GhosttyTerminal t, int button, int action,
                                float x, float y, int mods,
                                char *buf, size_t buf_len) {
	GhosttyMouseEncoder enc = NULL;
	if (ghostty_mouse_encoder_new(NULL, &enc) != GHOSTTY_SUCCESS) return 0;
	ghostty_mouse_encoder_setopt_from_terminal(enc, t);

	GhosttyMouseEvent event = NULL;
	if (ghostty_mouse_event_new(NULL, &event) != GHOSTTY_SUCCESS) {
		ghostty_mouse_encoder_free(enc);
		return 0;
	}

	ghostty_mouse_event_set_action(event, (GhosttyMouseAction)action);
	ghostty_mouse_event_set_button(event, (GhosttyMouseButton)button);
	ghostty_mouse_event_set_mods(event, (GhosttyMods)mods);
	GhosttyMousePosition pos = {.x = x, .y = y};
	ghostty_mouse_event_set_position(event, pos);

	size_t out_len = 0;
	GhosttyResult r = ghostty_mouse_encoder_encode(enc, event, buf, buf_len, &out_len);

	ghostty_mouse_event_free(event);
	ghostty_mouse_encoder_free(enc);
	return (r == GHOSTTY_SUCCESS) ? (int)out_len : 0;
}

// --- Focus encoding wrapper ---
static int hangon_encode_focus(GhosttyTerminal t, int gained, char *buf, size_t buf_len) {
	(void)t; // focus encoding doesn't need terminal in this API
	GhosttyFocusEvent event = gained ? GHOSTTY_FOCUS_GAINED : GHOSTTY_FOCUS_LOST;
	size_t out_len = 0;
	GhosttyResult r = ghostty_focus_encode(event, buf, buf_len, &out_len);
	return (r == GHOSTTY_SUCCESS) ? (int)out_len : 0;
}
*/
import "C"

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/creack/pty"
)

// GhosttyBackend drives a TUI program through a full terminal emulator using libghostty.
//
// Unlike the tmux-based ProcessBackend, this provides:
//   - Pixel-perfect terminal rendering (colors, unicode, emoji, nerdfonts, kitty images)
//   - Mouse interaction support (click, double-click, drag, scroll)
//   - Session video recording
//
// Requires libghostty-vt to be installed. Build with: go build -tags ghostty
type GhosttyBackend struct {
	command []string
	rows    int
	cols    int

	cmd  *exec.Cmd
	ptmx *os.File

	terminal C.GhosttyTerminal

	output *RingBuffer
	done   chan struct{}

	exitErr  error
	exitCode int
	mu       sync.Mutex

	recording     bool
	recordFile    string
	recordFPS     float64
	recordTmpDir  string
	recordFrameN  int
	recordStop    chan struct{}
	recordStopped chan struct{}

	// Mouse state for video overlay.
	mouseRow     int
	mouseCol     int
	mouseVisible bool
	mouseBtn     int // 0=none, 1=left held, 2=right held
	mouseBtnAt   time.Time
}

func NewGhosttyBackend(command []string) *GhosttyBackend {
	return &GhosttyBackend{
		command: command,
		rows:    24,
		cols:    80,
		output:  NewRingBuffer(defaultBufSize),
		done:    make(chan struct{}),
	}
}

func (gb *GhosttyBackend) Start() error {
	opts := C.GhosttyTerminalOptions{
		cols:           C.uint16_t(gb.cols),
		rows:           C.uint16_t(gb.rows),
		max_scrollback: 10000,
	}
	if r := C.ghostty_terminal_new(nil, &gb.terminal, opts); r != C.GHOSTTY_SUCCESS {
		return fmt.Errorf("failed to create ghostty terminal: %d", r)
	}

	gb.cmd = exec.Command(gb.command[0], gb.command[1:]...)
	gb.cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		fmt.Sprintf("COLUMNS=%d", gb.cols),
		fmt.Sprintf("LINES=%d", gb.rows),
	)

	ptmx, err := pty.Start(gb.cmd)
	if err != nil {
		C.ghostty_terminal_free(gb.terminal)
		gb.terminal = nil
		return fmt.Errorf("pty start: %w", err)
	}
	gb.ptmx = ptmx

	pty.Setsize(ptmx, &pty.Winsize{
		Rows: uint16(gb.rows),
		Cols: uint16(gb.cols),
	})

	// Read PTY output, feed to both ring buffer and ghostty terminal.
	go func() {
		buf := make([]byte, 16384)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				gb.output.Write(buf[:n])

				gb.mu.Lock()
				if gb.terminal != nil {
					C.ghostty_terminal_vt_write(gb.terminal,
						(*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(n))
				}
				gb.mu.Unlock()
			}
			if err != nil {
				break
			}
		}
		gb.mu.Lock()
		gb.exitErr = gb.cmd.Wait()
		if gb.exitErr != nil {
			if exitErr, ok := gb.exitErr.(*exec.ExitError); ok {
				gb.exitCode = exitErr.ExitCode()
			}
		}
		gb.mu.Unlock()
		close(gb.done)
	}()

	return nil
}

func (gb *GhosttyBackend) Send(data []byte) error {
	if gb.ptmx == nil {
		return fmt.Errorf("pty not available")
	}
	_, err := gb.ptmx.Write(data)
	return err
}

func (gb *GhosttyBackend) Output() *RingBuffer { return gb.output }
func (gb *GhosttyBackend) Stderr() *RingBuffer  { return nil }

func (gb *GhosttyBackend) Screen() (string, error) {
	gb.mu.Lock()
	defer gb.mu.Unlock()
	if gb.terminal == nil {
		return "", fmt.Errorf("terminal not initialized")
	}

	snap := C.hangon_snapshot(gb.terminal)
	if snap == nil {
		return "", fmt.Errorf("failed to take snapshot")
	}
	defer C.hangon_snapshot_free(snap)

	var b strings.Builder
	cols := int(snap.cols)
	rows := int(snap.rows)
	for r := 0; r < rows; r++ {
		if r > 0 {
			b.WriteByte('\n')
		}
		for c := 0; c < cols; c++ {
			cell := (*C.hangon_cell_t)(unsafe.Pointer(
				uintptr(unsafe.Pointer(snap.cells)) +
					uintptr(r*cols+c)*unsafe.Sizeof(C.hangon_cell_t{})))
			if cell.has_text != 0 && cell.codepoint > 0 {
				b.WriteRune(rune(cell.codepoint))
			} else {
				b.WriteByte(' ')
			}
		}
	}
	return b.String(), nil
}

func (gb *GhosttyBackend) SendKeys(keys string) error {
	gb.mu.Lock()
	defer gb.mu.Unlock()
	if gb.terminal == nil {
		return fmt.Errorf("terminal not initialized")
	}

	for _, key := range strings.Fields(keys) {
		keyLower := strings.ToLower(key)

		if gkey, mods, utf8, ok := ghosttyKeyLookup(keyLower); ok {
			var cUtf8 *C.char
			var utf8Len C.size_t
			if utf8 != "" {
				cUtf8 = C.CString(utf8)
				defer C.free(unsafe.Pointer(cUtf8))
				utf8Len = C.size_t(len(utf8))
			}
			var buf [64]C.char
			n := C.hangon_encode_key(gb.terminal, C.int(gkey), C.int(mods),
				C.int(C.GHOSTTY_KEY_ACTION_PRESS), cUtf8, utf8Len, &buf[0], 64)
			if n > 0 {
				data := C.GoBytes(unsafe.Pointer(&buf[0]), C.int(n))
				if _, err := gb.ptmx.Write(data); err != nil {
					return err
				}
				continue
			}
		}

		rawBytes, ok := keyMap[keyLower]
		if !ok {
			return fmt.Errorf("unknown key: %s", key)
		}
		if _, err := gb.ptmx.Write(rawBytes); err != nil {
			return err
		}
	}
	return nil
}

func (gb *GhosttyBackend) Alive() bool {
	select {
	case <-gb.done:
		return false
	default:
		return true
	}
}

func (gb *GhosttyBackend) Wait() (int, error) {
	<-gb.done
	gb.mu.Lock()
	defer gb.mu.Unlock()
	if gb.exitErr == nil {
		return 0, nil
	}
	if exitErr, ok := gb.exitErr.(*exec.ExitError); ok {
		return exitErr.ExitCode(), nil
	}
	return gb.exitCode, gb.exitErr
}

func (gb *GhosttyBackend) TargetPID() int {
	if gb.cmd != nil && gb.cmd.Process != nil {
		return gb.cmd.Process.Pid
	}
	return 0
}

func (gb *GhosttyBackend) Close() error {
	if gb.recording {
		gb.RecordStop()
	}
	if gb.cmd != nil && gb.cmd.Process != nil {
		gb.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-gb.done:
		case <-time.After(2 * time.Second):
			gb.cmd.Process.Kill()
			<-gb.done
		}
	}
	if gb.ptmx != nil {
		gb.ptmx.Close()
	}
	gb.mu.Lock()
	if gb.terminal != nil {
		C.ghostty_terminal_free(gb.terminal)
		gb.terminal = nil
	}
	gb.mu.Unlock()
	return nil
}

// --- Screenshotter ---

func (gb *GhosttyBackend) Screenshot(file string) (string, error) {
	if file == "" {
		file = "screenshot.png"
	}
	grid, err := gb.snapshotToGrid()
	if err != nil {
		return "", err
	}
	if pngPath, err := RenderPNGDirect(grid, file); err == nil {
		return pngPath, nil
	}
	return RenderPNG(grid, DefaultRenderConfig, file)
}

func (gb *GhosttyBackend) snapshotToGrid() (*ScreenGrid, error) {
	gb.mu.Lock()
	defer gb.mu.Unlock()
	if gb.terminal == nil {
		return nil, fmt.Errorf("terminal not initialized")
	}

	snap := C.hangon_snapshot(gb.terminal)
	if snap == nil {
		return nil, fmt.Errorf("failed to take snapshot")
	}
	defer C.hangon_snapshot_free(snap)

	rows := int(snap.rows)
	cols := int(snap.cols)
	grid := &ScreenGrid{Rows: rows, Cols: cols, Cells: make([][]Cell, rows)}

	if snap.cursor_visible != 0 {
		grid.HasCursor = true
		grid.CursorR = int(snap.cursor_y)
		grid.CursorC = int(snap.cursor_x)
	}

	for r := 0; r < rows; r++ {
		grid.Cells[r] = make([]Cell, cols)
		for c := 0; c < cols; c++ {
			grid.Cells[r][c] = Cell{Char: ' ', Width: 1}

			cell := (*C.hangon_cell_t)(unsafe.Pointer(
				uintptr(unsafe.Pointer(snap.cells)) +
					uintptr(r*cols+c)*unsafe.Sizeof(C.hangon_cell_t{})))

			ch := rune(' ')
			if cell.has_text != 0 && cell.codepoint > 0 {
				ch = rune(cell.codepoint)
			}

			width := 1
			if cell.wide == C.uint8_t(C.GHOSTTY_CELL_WIDE_WIDE) {
				width = 2
			} else if cell.wide == C.uint8_t(C.GHOSTTY_CELL_WIDE_SPACER_TAIL) {
				grid.Cells[r][c] = Cell{Char: 0, Width: 0}
				continue
			}

			style := CellStyle{
				Bold:          cell.bold != 0,
				Italic:        cell.italic != 0,
				Dim:           cell.faint != 0,
				Strikethrough: cell.strikethrough != 0,
				Inverse:       cell.inverse != 0,
				Underline:     cell.underline != 0,
			}

			if cell.fg_tag == 2 { // RGB
				style.FG = fmt.Sprintf("#%02x%02x%02x", cell.fg_r, cell.fg_g, cell.fg_b)
			}
			if cell.bg_tag == 2 {
				style.BG = fmt.Sprintf("#%02x%02x%02x", cell.bg_r, cell.bg_g, cell.bg_b)
			}

			grid.Cells[r][c] = Cell{Char: ch, Width: width, Style: style}
		}
	}
	return grid, nil
}

// --- MouseHandler ---

func (gb *GhosttyBackend) MouseClick(row, col int, button string) error {
	btn := ghosttyMouseButton(button)
	gb.trackMouse(row, col, 1)
	if err := gb.sendMouseEvent(btn, C.GHOSTTY_MOUSE_ACTION_PRESS, row, col); err != nil {
		return err
	}
	time.Sleep(20 * time.Millisecond)
	gb.trackMouse(row, col, 0)
	return gb.sendMouseEvent(btn, C.GHOSTTY_MOUSE_ACTION_RELEASE, row, col)
}

func (gb *GhosttyBackend) MouseDoubleClick(row, col int, button string) error {
	gb.trackMouse(row, col, 1)
	if err := gb.MouseClick(row, col, button); err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	return gb.MouseClick(row, col, button)
}

func (gb *GhosttyBackend) MouseTripleClick(row, col int, button string) error {
	gb.trackMouse(row, col, 1)
	if err := gb.MouseDoubleClick(row, col, button); err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	return gb.MouseClick(row, col, button)
}

func (gb *GhosttyBackend) MouseDrag(fromRow, fromCol, toRow, toCol int, button string) error {
	btn := ghosttyMouseButton(button)
	gb.trackMouse(fromRow, fromCol, 1)
	if err := gb.sendMouseEvent(btn, C.GHOSTTY_MOUSE_ACTION_PRESS, fromRow, fromCol); err != nil {
		return err
	}
	steps := max(abs(toRow-fromRow), abs(toCol-fromCol))
	if steps == 0 {
		steps = 1
	}
	for i := 1; i <= steps; i++ {
		r := fromRow + (toRow-fromRow)*i/steps
		c := fromCol + (toCol-fromCol)*i/steps
		gb.trackMouse(r, c, 1)
		if err := gb.sendMouseEvent(btn, C.GHOSTTY_MOUSE_ACTION_MOTION, r, c); err != nil {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	gb.trackMouse(toRow, toCol, 0)
	return gb.sendMouseEvent(btn, C.GHOSTTY_MOUSE_ACTION_RELEASE, toRow, toCol)
}

func (gb *GhosttyBackend) MouseScroll(row, col, delta int) error {
	gb.trackMouse(row, col, 0)
	for i := 0; i < abs(delta); i++ {
		btn := C.GHOSTTY_MOUSE_BUTTON_FOUR // scroll up
		if delta < 0 {
			btn = C.GHOSTTY_MOUSE_BUTTON_FIVE // scroll down
		}
		if err := gb.sendMouseEvent(int(btn), C.GHOSTTY_MOUSE_ACTION_PRESS, row, col); err != nil {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

// trackMouse updates the mouse position and button state for video overlay.
func (gb *GhosttyBackend) trackMouse(row, col, btn int) {
	gb.mouseRow = row
	gb.mouseCol = col
	gb.mouseVisible = true
	gb.mouseBtn = btn
	if btn > 0 {
		gb.mouseBtnAt = time.Now()
	}
}

func (gb *GhosttyBackend) sendMouseEvent(button, action, row, col int) error {
	gb.mu.Lock()
	defer gb.mu.Unlock()
	if gb.terminal == nil {
		return fmt.Errorf("terminal not initialized")
	}
	var buf [64]C.char
	n := C.hangon_encode_mouse(gb.terminal, C.int(button), C.int(action),
		C.float(col), C.float(row), 0, &buf[0], 64)
	if n > 0 {
		data := C.GoBytes(unsafe.Pointer(&buf[0]), C.int(n))
		_, err := gb.ptmx.Write(data)
		return err
	}
	return nil
}

// --- VideoRecorder ---

func (gb *GhosttyBackend) RecordStart(file string, fps float64) error {
	if gb.recording {
		return fmt.Errorf("already recording")
	}
	if file == "" {
		file = "recording.mp4"
	}
	if fps <= 0 {
		fps = 10
	}
	tmpDir, err := os.MkdirTemp("", "hangon-record-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	gb.recording = true
	gb.recordFile = file
	gb.recordFPS = fps
	gb.recordTmpDir = tmpDir
	gb.recordFrameN = 0
	gb.recordStop = make(chan struct{})
	gb.recordStopped = make(chan struct{})

	go func() {
		defer close(gb.recordStopped)
		interval := time.Duration(float64(time.Second) / fps)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-gb.recordStop:
				return
			case <-gb.done:
				return
			case <-ticker.C:
				gb.captureFrame()
			}
		}
	}()
	return nil
}

func (gb *GhosttyBackend) RecordStop() (string, error) {
	if !gb.recording {
		return "", fmt.Errorf("not recording")
	}
	gb.recording = false
	close(gb.recordStop)
	<-gb.recordStopped

	if gb.recordFrameN == 0 {
		os.RemoveAll(gb.recordTmpDir)
		return "", fmt.Errorf("no frames captured")
	}

	outFile := gb.recordFile
	if err := gb.encodeVideo(outFile); err != nil {
		return "", fmt.Errorf("encode video: %w (frames in %s)", err, gb.recordTmpDir)
	}
	os.RemoveAll(gb.recordTmpDir)
	return outFile, nil
}

func (gb *GhosttyBackend) captureFrame() {
	frameFile := fmt.Sprintf("%s/frame_%06d.png", gb.recordTmpDir, gb.recordFrameN)
	grid, err := gb.snapshotToGrid()
	if err != nil {
		return
	}

	// Build mouse overlay if mouse is visible.
	var overlay *MouseOverlay
	if gb.mouseVisible {
		overlay = &MouseOverlay{
			Row:     gb.mouseRow,
			Col:     gb.mouseCol,
			Pressed: gb.mouseBtn > 0,
			HeldMs:  0,
		}
		if gb.mouseBtn > 0 {
			overlay.HeldMs = int(time.Since(gb.mouseBtnAt).Milliseconds())
		}
	}

	// Try direct PNG first, then SVG pipeline.
	if _, err := RenderPNGDirectWithMouse(grid, frameFile, overlay); err != nil {
		if _, err := RenderPNG(grid, DefaultRenderConfig, frameFile); err != nil {
			return
		}
	}
	gb.recordFrameN++
}

func (gb *GhosttyBackend) encodeVideo(outFile string) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found in PATH")
	}
	framePattern := fmt.Sprintf("%s/frame_%%06d.png", gb.recordTmpDir)
	cmd := exec.Command(ffmpegPath,
		"-y", "-framerate", fmt.Sprintf("%.2f", gb.recordFPS),
		"-i", framePattern,
		"-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-preset", "fast", "-crf", "23",
		outFile)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// --- Key mapping ---

func ghosttyKeyLookup(name string) (int, int, string, bool) {
	type keyInfo struct {
		key  int
		mods int
		utf8 string
	}
	table := map[string]keyInfo{
		"enter":     {int(C.GHOSTTY_KEY_ENTER), 0, "\n"},
		"return":    {int(C.GHOSTTY_KEY_ENTER), 0, "\n"},
		"tab":       {int(C.GHOSTTY_KEY_TAB), 0, "\t"},
		"backspace": {int(C.GHOSTTY_KEY_BACKSPACE), 0, ""},
		"escape":    {int(C.GHOSTTY_KEY_ESCAPE), 0, ""},
		"esc":       {int(C.GHOSTTY_KEY_ESCAPE), 0, ""},
		"delete":    {int(C.GHOSTTY_KEY_DELETE), 0, ""},
		"up":        {int(C.GHOSTTY_KEY_ARROW_UP), 0, ""},
		"down":      {int(C.GHOSTTY_KEY_ARROW_DOWN), 0, ""},
		"right":     {int(C.GHOSTTY_KEY_ARROW_RIGHT), 0, ""},
		"left":      {int(C.GHOSTTY_KEY_ARROW_LEFT), 0, ""},
		"home":      {int(C.GHOSTTY_KEY_HOME), 0, ""},
		"end":       {int(C.GHOSTTY_KEY_END), 0, ""},
		"pageup":    {int(C.GHOSTTY_KEY_PAGE_UP), 0, ""},
		"pagedown":  {int(C.GHOSTTY_KEY_PAGE_DOWN), 0, ""},
		"insert":    {int(C.GHOSTTY_KEY_INSERT), 0, ""},
		"space":     {int(C.GHOSTTY_KEY_SPACE), 0, " "},
		"f1":        {int(C.GHOSTTY_KEY_F1), 0, ""},
		"f2":        {int(C.GHOSTTY_KEY_F2), 0, ""},
		"f3":        {int(C.GHOSTTY_KEY_F3), 0, ""},
		"f4":        {int(C.GHOSTTY_KEY_F4), 0, ""},
		"f5":        {int(C.GHOSTTY_KEY_F5), 0, ""},
		"f6":        {int(C.GHOSTTY_KEY_F6), 0, ""},
		"f7":        {int(C.GHOSTTY_KEY_F7), 0, ""},
		"f8":        {int(C.GHOSTTY_KEY_F8), 0, ""},
		"f9":        {int(C.GHOSTTY_KEY_F9), 0, ""},
		"f10":       {int(C.GHOSTTY_KEY_F10), 0, ""},
		"f11":       {int(C.GHOSTTY_KEY_F11), 0, ""},
		"f12":       {int(C.GHOSTTY_KEY_F12), 0, ""},
	}

	if info, ok := table[name]; ok {
		return info.key, info.mods, info.utf8, true
	}

	// ctrl-a through ctrl-z
	if strings.HasPrefix(name, "ctrl-") && len(name) == 6 {
		ch := name[5]
		if ch >= 'a' && ch <= 'z' {
			gkey := int(C.GHOSTTY_KEY_A) + int(ch-'a')
			return gkey, int(C.GHOSTTY_MODS_CTRL), "", true
		}
	}

	return 0, 0, "", false
}

func ghosttyMouseButton(name string) int {
	switch strings.ToLower(name) {
	case "left", "":
		return int(C.GHOSTTY_MOUSE_BUTTON_LEFT)
	case "middle":
		return int(C.GHOSTTY_MOUSE_BUTTON_MIDDLE)
	case "right":
		return int(C.GHOSTTY_MOUSE_BUTTON_RIGHT)
	default:
		return int(C.GHOSTTY_MOUSE_BUTTON_LEFT)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
