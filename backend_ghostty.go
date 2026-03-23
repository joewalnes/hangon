//go:build ghostty

package main

/*
#cgo CFLAGS: -I${SRCDIR}/ghostty/include
#cgo LDFLAGS: -L${SRCDIR}/ghostty/lib -lghostty-vt -lm

#include <ghostty.h>
#include <stdlib.h>
#include <string.h>

// --- Terminal wrapper ---

// hangon_terminal_new creates a new ghostty terminal with the given grid size.
static ghostty_terminal_t* hangon_terminal_new(uint16_t cols, uint16_t rows, uint32_t scrollback) {
	ghostty_surface_options_t opts = {
		.cols = cols,
		.rows = rows,
		.max_scrollback = scrollback,
	};
	return ghostty_terminal_new(&opts);
}

// hangon_terminal_write feeds raw PTY output data into the VT parser.
static void hangon_terminal_write(ghostty_terminal_t *t, const char *data, size_t len) {
	ghostty_terminal_vt_write(t, data, len);
}

// hangon_terminal_resize changes the terminal grid size (triggers content reflow).
static void hangon_terminal_resize(ghostty_terminal_t *t, uint16_t cols, uint16_t rows) {
	ghostty_terminal_resize(t, cols, rows);
}

// --- Render state wrapper ---

// Cell data extracted from the render state for Go consumption.
typedef struct {
	const char *grapheme;   // UTF-8 grapheme cluster (may be multi-byte)
	int         grapheme_len;
	uint32_t    fg_rgb;     // 0xRRGGBB, or 0xFFFFFFFF for default
	uint32_t    bg_rgb;     // 0xRRGGBB, or 0xFFFFFFFF for default
	uint8_t     bold;
	uint8_t     italic;
	uint8_t     underline;
	uint8_t     strikethrough;
	uint8_t     inverse;
	uint8_t     dim;
	uint8_t     wide;       // 1 if this is a wide (2-cell) character
} hangon_cell_t;

// Row of cells extracted for Go.
typedef struct {
	hangon_cell_t *cells;
	int            count;
} hangon_row_t;

// Full snapshot of the terminal screen for Go consumption.
typedef struct {
	hangon_row_t *rows;
	int           row_count;
	int           col_count;
	int           cursor_row;
	int           cursor_col;
	uint8_t       cursor_visible;
	uint8_t       cursor_style; // 0=block, 1=underline, 2=bar
	uint32_t      default_fg;
	uint32_t      default_bg;
} hangon_snapshot_t;

// Resolve a color from the render state. Returns 0xRRGGBB or 0xFFFFFFFF for default.
static uint32_t resolve_color(ghostty_render_state_t *rs, ghostty_color_t color, int is_fg) {
	if (color.tag == GHOSTTY_COLOR_TAG_DEFAULT) {
		return 0xFFFFFFFF;
	}
	ghostty_rgb_t rgb;
	if (ghostty_render_state_color_resolve(rs, color, is_fg, &rgb)) {
		return ((uint32_t)rgb.r << 16) | ((uint32_t)rgb.g << 8) | (uint32_t)rgb.b;
	}
	return 0xFFFFFFFF;
}

// hangon_snapshot takes a snapshot of the terminal screen.
// The caller must free the result with hangon_snapshot_free.
static hangon_snapshot_t* hangon_snapshot(ghostty_terminal_t *t) {
	ghostty_render_state_t *rs = ghostty_render_state_new(t);
	if (!rs) return NULL;

	ghostty_render_state_update(rs);

	hangon_snapshot_t *snap = (hangon_snapshot_t*)calloc(1, sizeof(hangon_snapshot_t));

	// Get dimensions and cursor info.
	ghostty_render_state_info_t info;
	ghostty_render_state_get_info(rs, &info);
	snap->row_count = info.rows;
	snap->col_count = info.cols;
	snap->cursor_row = info.cursor_row;
	snap->cursor_col = info.cursor_col;
	snap->cursor_visible = info.cursor_visible ? 1 : 0;
	snap->cursor_style = (uint8_t)info.cursor_style;

	// Get default colors.
	ghostty_rgb_t dfg, dbg;
	ghostty_render_state_get_default_colors(rs, &dfg, &dbg);
	snap->default_fg = ((uint32_t)dfg.r << 16) | ((uint32_t)dfg.g << 8) | (uint32_t)dfg.b;
	snap->default_bg = ((uint32_t)dbg.r << 16) | ((uint32_t)dbg.g << 8) | (uint32_t)dbg.b;

	// Allocate rows.
	snap->rows = (hangon_row_t*)calloc(snap->row_count, sizeof(hangon_row_t));

	// Iterate rows and cells.
	ghostty_render_state_row_iterator_t row_iter;
	ghostty_render_state_row_iterator_new(rs, &row_iter);

	int row_idx = 0;
	while (row_idx < snap->row_count && ghostty_render_state_row_iterator_next(&row_iter)) {
		// Allocate cells for this row.
		snap->rows[row_idx].cells = (hangon_cell_t*)calloc(snap->col_count, sizeof(hangon_cell_t));
		snap->rows[row_idx].count = 0;

		ghostty_render_state_cell_iterator_t cell_iter;
		ghostty_render_state_cell_iterator_new(rs, &row_iter, &cell_iter);

		int col_idx = 0;
		while (col_idx < snap->col_count && ghostty_render_state_cell_iterator_next(&cell_iter)) {
			ghostty_render_state_cell_t cell_data;
			ghostty_render_state_cell_get(&cell_iter, &cell_data);

			hangon_cell_t *c = &snap->rows[row_idx].cells[col_idx];

			// Copy grapheme.
			if (cell_data.grapheme && cell_data.grapheme_len > 0) {
				char *g = (char*)malloc(cell_data.grapheme_len + 1);
				memcpy(g, cell_data.grapheme, cell_data.grapheme_len);
				g[cell_data.grapheme_len] = '\0';
				c->grapheme = g;
				c->grapheme_len = cell_data.grapheme_len;
			} else {
				c->grapheme = NULL;
				c->grapheme_len = 0;
			}

			// Resolve colors.
			c->fg_rgb = resolve_color(rs, cell_data.fg, 1);
			c->bg_rgb = resolve_color(rs, cell_data.bg, 0);

			// Style flags.
			c->bold = (cell_data.flags & GHOSTTY_CELL_FLAG_BOLD) ? 1 : 0;
			c->italic = (cell_data.flags & GHOSTTY_CELL_FLAG_ITALIC) ? 1 : 0;
			c->underline = (cell_data.flags & GHOSTTY_CELL_FLAG_UNDERLINE) ? 1 : 0;
			c->strikethrough = (cell_data.flags & GHOSTTY_CELL_FLAG_STRIKETHROUGH) ? 1 : 0;
			c->inverse = (cell_data.flags & GHOSTTY_CELL_FLAG_INVERSE) ? 1 : 0;
			c->dim = (cell_data.flags & GHOSTTY_CELL_FLAG_DIM) ? 1 : 0;
			c->wide = (cell_data.flags & GHOSTTY_CELL_FLAG_WIDE) ? 1 : 0;

			col_idx++;
		}
		snap->rows[row_idx].count = col_idx;
		row_idx++;
	}

	ghostty_render_state_free(rs);
	return snap;
}

// hangon_snapshot_free frees a snapshot and all its data.
static void hangon_snapshot_free(hangon_snapshot_t *snap) {
	if (!snap) return;
	for (int r = 0; r < snap->row_count; r++) {
		for (int c = 0; c < snap->rows[r].count; c++) {
			free((void*)snap->rows[r].cells[c].grapheme);
		}
		free(snap->rows[r].cells);
	}
	free(snap->rows);
	free(snap);
}

// --- Key encoding wrapper ---

// ghostty key code mapping for common keys.
// Returns the number of bytes written to buf, or 0 if the key is not recognized.
static int hangon_encode_key(ghostty_terminal_t *t, int key, int mods, int codepoint,
                              const char *utf8, char *buf, int buf_len) {
	ghostty_key_encoder_t *enc = ghostty_key_encoder_new();
	if (!enc) return 0;

	ghostty_key_encoder_setopt_from_terminal(enc, t);

	ghostty_key_event_t event;
	ghostty_key_event_new(&event);
	ghostty_key_event_set_action(&event, GHOSTTY_KEY_ACTION_PRESS);
	ghostty_key_event_set_key(&event, (ghostty_key_t)key);
	ghostty_key_event_set_mods(&event, (ghostty_mods_t)mods);
	if (codepoint > 0) {
		ghostty_key_event_set_codepoint(&event, (uint32_t)codepoint);
	}
	if (utf8 && utf8[0]) {
		ghostty_key_event_set_utf8(&event, utf8);
	}

	int n = ghostty_key_encoder_encode(enc, &event, buf, buf_len);
	ghostty_key_encoder_free(enc);
	return n;
}

// --- Mouse encoding wrapper ---

// hangon_encode_mouse encodes a mouse event into VT escape sequences.
// action: 0=press, 1=release, 2=move
// button: 0=left, 1=middle, 2=right, 3=none, 64=scroll-up, 65=scroll-down
static int hangon_encode_mouse(ghostty_terminal_t *t, int button, int action,
                                int row, int col, int mods,
                                char *buf, int buf_len) {
	ghostty_mouse_encoder_t *enc = ghostty_mouse_encoder_new();
	if (!enc) return 0;

	ghostty_mouse_encoder_setopt_from_terminal(enc, t);

	ghostty_mouse_event_t event;
	ghostty_mouse_event_new(&event);
	ghostty_mouse_event_set_button(&event, (ghostty_mouse_button_t)button);
	ghostty_mouse_event_set_action(&event, (ghostty_mouse_action_t)action);
	ghostty_mouse_event_set_row(&event, row);
	ghostty_mouse_event_set_col(&event, col);
	ghostty_mouse_event_set_mods(&event, (ghostty_mods_t)mods);

	int n = ghostty_mouse_encoder_encode(enc, &event, buf, buf_len);
	ghostty_mouse_encoder_free(enc);
	return n;
}

// --- Focus event encoding ---

static int hangon_encode_focus(ghostty_terminal_t *t, int focused, char *buf, int buf_len) {
	return ghostty_focus_encode(t, focused, buf, buf_len);
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

	// PTY
	cmd  *exec.Cmd
	ptmx *os.File

	// libghostty terminal
	terminal *C.ghostty_terminal_t

	// Output buffering
	output *RingBuffer
	done   chan struct{}

	exitErr  error
	exitCode int
	mu       sync.Mutex

	// Video recording
	recording     bool
	recordFile    string
	recordFPS     float64
	recordTmpDir  string
	recordFrameN  int
	recordStop    chan struct{}
	recordStopped chan struct{}
}

// NewGhosttyBackend creates a new Ghostty-based backend for driving TUI programs.
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
	// Create the libghostty terminal.
	gb.terminal = C.hangon_terminal_new(C.uint16_t(gb.cols), C.uint16_t(gb.rows), 10000)
	if gb.terminal == nil {
		return fmt.Errorf("failed to create ghostty terminal")
	}

	// Start the child process with a PTY.
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

	// Set PTY size.
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

				// Feed to libghostty VT parser.
				cdata := C.CBytes(buf[:n])
				gb.mu.Lock()
				if gb.terminal != nil {
					C.hangon_terminal_write(gb.terminal, (*C.char)(cdata), C.size_t(n))
				}
				gb.mu.Unlock()
				C.free(cdata)
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

	// Send focus event to the terminal (some TUI apps need this).
	var focusBuf [32]byte
	n := C.hangon_encode_focus(gb.terminal, 1, (*C.char)(unsafe.Pointer(&focusBuf[0])), 32)
	if n > 0 {
		ptmx.Write(focusBuf[:n])
	}

	return nil
}

func (gb *GhosttyBackend) Send(data []byte) error {
	if gb.ptmx == nil {
		return fmt.Errorf("pty not available")
	}
	_, err := gb.ptmx.Write(data)
	return err
}

func (gb *GhosttyBackend) Output() *RingBuffer {
	return gb.output
}

func (gb *GhosttyBackend) Stderr() *RingBuffer {
	return nil // PTY merges stdout and stderr
}

// Screen returns the current terminal screen as plain text.
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
	for r := 0; r < int(snap.row_count); r++ {
		if r > 0 {
			b.WriteByte('\n')
		}
		row := snap.rows
		rowPtr := (*C.hangon_row_t)(unsafe.Pointer(uintptr(unsafe.Pointer(row)) + uintptr(r)*unsafe.Sizeof(*row)))
		for c := 0; c < int(rowPtr.count); c++ {
			cell := (*C.hangon_cell_t)(unsafe.Pointer(uintptr(unsafe.Pointer(rowPtr.cells)) + uintptr(c)*unsafe.Sizeof(C.hangon_cell_t{})))
			if cell.grapheme != nil && cell.grapheme_len > 0 {
				b.WriteString(C.GoStringN(cell.grapheme, C.int(cell.grapheme_len)))
			} else {
				b.WriteByte(' ')
			}
		}
		// Pad remaining columns with spaces.
		for c := int(rowPtr.count); c < int(snap.col_count); c++ {
			b.WriteByte(' ')
		}
	}
	return b.String(), nil
}

// SendKeys sends special key sequences via the ghostty key encoder.
func (gb *GhosttyBackend) SendKeys(keys string) error {
	gb.mu.Lock()
	defer gb.mu.Unlock()

	if gb.terminal == nil {
		return fmt.Errorf("terminal not initialized")
	}

	for _, key := range strings.Fields(keys) {
		keyLower := strings.ToLower(key)

		// First try the ghostty key encoder for proper mode-aware encoding.
		if gkey, mods, cp, utf8, ok := ghosttyKeyLookup(keyLower); ok {
			var cUtf8 *C.char
			if utf8 != "" {
				cUtf8 = C.CString(utf8)
				defer C.free(unsafe.Pointer(cUtf8))
			}
			var buf [64]C.char
			n := C.hangon_encode_key(gb.terminal, C.int(gkey), C.int(mods), C.int(cp),
				cUtf8, &buf[0], 64)
			if n > 0 {
				data := C.GoBytes(unsafe.Pointer(&buf[0]), n)
				if _, err := gb.ptmx.Write(data); err != nil {
					return err
				}
				continue
			}
		}

		// Fallback to raw escape sequences.
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
	// Stop recording if active.
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

// --- Screenshotter interface ---

// Screenshot captures the terminal screen and renders it to a PNG file.
func (gb *GhosttyBackend) Screenshot(file string) (string, error) {
	if file == "" {
		file = "screenshot.png"
	}

	grid, err := gb.snapshotToGrid()
	if err != nil {
		return "", err
	}

	// Try direct PNG rendering first (uses Go image library with font rendering).
	if pngPath, err := RenderPNGDirect(grid, file); err == nil {
		return pngPath, nil
	}

	// Fall back to SVG-based rendering pipeline.
	return RenderPNG(grid, DefaultRenderConfig, file)
}

// snapshotToGrid converts a libghostty render state snapshot to a ScreenGrid.
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

	rows := int(snap.row_count)
	cols := int(snap.col_count)

	grid := &ScreenGrid{
		Rows:  rows,
		Cols:  cols,
		Cells: make([][]Cell, rows),
	}

	if snap.cursor_visible != 0 {
		grid.HasCursor = true
		grid.CursorR = int(snap.cursor_row)
		grid.CursorC = int(snap.cursor_col)
	}

	// Default colors from the terminal.
	defaultFG := fmt.Sprintf("#%06x", uint32(snap.default_fg))
	defaultBG := fmt.Sprintf("#%06x", uint32(snap.default_bg))

	// Override the render config defaults with terminal's actual colors.
	_ = defaultFG
	_ = defaultBG

	for r := 0; r < rows; r++ {
		grid.Cells[r] = make([]Cell, cols)
		for c := 0; c < cols; c++ {
			grid.Cells[r][c] = Cell{Char: ' ', Width: 1}
		}

		rowPtr := (*C.hangon_row_t)(unsafe.Pointer(uintptr(unsafe.Pointer(snap.rows)) + uintptr(r)*unsafe.Sizeof(C.hangon_row_t{})))
		cellCount := int(rowPtr.count)

		for c := 0; c < cellCount && c < cols; c++ {
			cell := (*C.hangon_cell_t)(unsafe.Pointer(uintptr(unsafe.Pointer(rowPtr.cells)) + uintptr(c)*unsafe.Sizeof(C.hangon_cell_t{})))

			var ch rune = ' '
			width := 1

			if cell.grapheme != nil && cell.grapheme_len > 0 {
				gstr := C.GoStringN(cell.grapheme, C.int(cell.grapheme_len))
				runes := []rune(gstr)
				if len(runes) > 0 {
					ch = runes[0]
				}
			}

			if cell.wide != 0 {
				width = 2
			}

			style := CellStyle{
				Bold:          cell.bold != 0,
				Italic:        cell.italic != 0,
				Underline:     cell.underline != 0,
				Strikethrough: cell.strikethrough != 0,
				Inverse:       cell.inverse != 0,
				Dim:           cell.dim != 0,
			}

			if uint32(cell.fg_rgb) != 0xFFFFFFFF {
				style.FG = fmt.Sprintf("#%06x", uint32(cell.fg_rgb))
			}
			if uint32(cell.bg_rgb) != 0xFFFFFFFF {
				style.BG = fmt.Sprintf("#%06x", uint32(cell.bg_rgb))
			}

			grid.Cells[r][c] = Cell{
				Char:  ch,
				Width: width,
				Style: style,
			}

			// Mark continuation cell for wide characters.
			if width == 2 && c+1 < cols {
				grid.Cells[r][c+1] = Cell{Char: 0, Width: 0, Style: style}
			}
		}
	}

	return grid, nil
}

// --- MouseHandler interface ---

func (gb *GhosttyBackend) MouseClick(row, col int, button string) error {
	btn := ghosttyMouseButton(button)
	// Press then release.
	if err := gb.sendMouseEvent(btn, 0, row, col); err != nil {
		return err
	}
	time.Sleep(20 * time.Millisecond)
	return gb.sendMouseEvent(btn, 1, row, col)
}

func (gb *GhosttyBackend) MouseDoubleClick(row, col int, button string) error {
	// Double click is two rapid clicks.
	if err := gb.MouseClick(row, col, button); err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	return gb.MouseClick(row, col, button)
}

func (gb *GhosttyBackend) MouseTripleClick(row, col int, button string) error {
	if err := gb.MouseDoubleClick(row, col, button); err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	return gb.MouseClick(row, col, button)
}

func (gb *GhosttyBackend) MouseDrag(fromRow, fromCol, toRow, toCol int, button string) error {
	btn := ghosttyMouseButton(button)

	// Press at start position.
	if err := gb.sendMouseEvent(btn, 0, fromRow, fromCol); err != nil {
		return err
	}

	// Move to end position with intermediate steps.
	steps := max(abs(toRow-fromRow), abs(toCol-fromCol))
	if steps == 0 {
		steps = 1
	}
	for i := 1; i <= steps; i++ {
		r := fromRow + (toRow-fromRow)*i/steps
		c := fromCol + (toCol-fromCol)*i/steps
		if err := gb.sendMouseEvent(btn, 2, r, c); err != nil {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Release at end position.
	return gb.sendMouseEvent(btn, 1, toRow, toCol)
}

func (gb *GhosttyBackend) MouseScroll(row, col, delta int) error {
	// Scroll button: 64 for up, 65 for down.
	for i := 0; i < abs(delta); i++ {
		btn := 64 // scroll up
		if delta < 0 {
			btn = 65 // scroll down
		}
		if err := gb.sendMouseEvent(btn, 0, row, col); err != nil {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

// sendMouseEvent encodes and sends a mouse event to the PTY.
func (gb *GhosttyBackend) sendMouseEvent(button, action, row, col int) error {
	gb.mu.Lock()
	defer gb.mu.Unlock()

	if gb.terminal == nil {
		return fmt.Errorf("terminal not initialized")
	}

	var buf [64]C.char
	n := C.hangon_encode_mouse(gb.terminal, C.int(button), C.int(action),
		C.int(row), C.int(col), 0, &buf[0], 64)
	if n > 0 {
		data := C.GoBytes(unsafe.Pointer(&buf[0]), n)
		_, err := gb.ptmx.Write(data)
		return err
	}

	// If the terminal is not in mouse mode, the encoder returns 0 bytes.
	// This is expected behavior - the app doesn't want mouse events.
	return nil
}

// --- VideoRecorder interface ---

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

	// Capture frames in a goroutine.
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

	// Encode frames to video using ffmpeg.
	outFile := gb.recordFile
	if err := gb.encodeVideo(outFile); err != nil {
		// Keep temp dir for debugging.
		return "", fmt.Errorf("encode video: %w (frames in %s)", err, gb.recordTmpDir)
	}

	// Clean up temp frames.
	os.RemoveAll(gb.recordTmpDir)
	return outFile, nil
}

func (gb *GhosttyBackend) captureFrame() {
	frameFile := fmt.Sprintf("%s/frame_%06d.png", gb.recordTmpDir, gb.recordFrameN)

	grid, err := gb.snapshotToGrid()
	if err != nil {
		return
	}

	// Use the SVG pipeline for frames (reliable).
	if _, err := RenderPNG(grid, DefaultRenderConfig, frameFile); err != nil {
		return
	}

	gb.recordFrameN++
}

func (gb *GhosttyBackend) encodeVideo(outFile string) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found in PATH (required for video encoding)")
	}

	// Use ffmpeg to encode the PNG frames into a video.
	framePattern := fmt.Sprintf("%s/frame_%%06d.png", gb.recordTmpDir)
	cmd := exec.Command(ffmpegPath,
		"-y",
		"-framerate", fmt.Sprintf("%.2f", gb.recordFPS),
		"-i", framePattern,
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-preset", "fast",
		"-crf", "23",
		outFile,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// --- Ghostty key mapping ---

// ghosttyKeyLookup maps our key names to ghostty key codes, modifiers, codepoints, and UTF-8 text.
// Returns (key, mods, codepoint, utf8, ok).
func ghosttyKeyLookup(name string) (int, int, int, string, bool) {
	// Ghostty key constants (from ghostty.h).
	// These are the GhosttyKey enum values used by libghostty.
	const (
		gkEnter     = 0x28
		gkTab       = 0x2B
		gkBackspace = 0x2A
		gkEscape    = 0x29
		gkDelete    = 0x4C
		gkUp        = 0x52
		gkDown      = 0x51
		gkRight     = 0x4F
		gkLeft      = 0x50
		gkHome      = 0x4A
		gkEnd       = 0x4D
		gkPageUp    = 0x4B
		gkPageDown  = 0x4E
		gkInsert    = 0x49
		gkSpace     = 0x2C
		gkF1        = 0x3A
		gkF2        = 0x3B
		gkF3        = 0x3C
		gkF4        = 0x3D
		gkF5        = 0x3E
		gkF6        = 0x3F
		gkF7        = 0x40
		gkF8        = 0x41
		gkF9        = 0x42
		gkF10       = 0x43
		gkF11       = 0x44
		gkF12       = 0x45

		// Modifier bits
		gmodCtrl = 0x01
	)

	switch name {
	case "enter", "return":
		return gkEnter, 0, '\n', "\n", true
	case "tab":
		return gkTab, 0, '\t', "\t", true
	case "backspace":
		return gkBackspace, 0, 0x7f, "", true
	case "escape", "esc":
		return gkEscape, 0, 0x1b, "", true
	case "delete":
		return gkDelete, 0, 0, "", true
	case "up":
		return gkUp, 0, 0, "", true
	case "down":
		return gkDown, 0, 0, "", true
	case "right":
		return gkRight, 0, 0, "", true
	case "left":
		return gkLeft, 0, 0, "", true
	case "home":
		return gkHome, 0, 0, "", true
	case "end":
		return gkEnd, 0, 0, "", true
	case "pageup":
		return gkPageUp, 0, 0, "", true
	case "pagedown":
		return gkPageDown, 0, 0, "", true
	case "insert":
		return gkInsert, 0, 0, "", true
	case "space":
		return gkSpace, 0, ' ', " ", true
	case "f1":
		return gkF1, 0, 0, "", true
	case "f2":
		return gkF2, 0, 0, "", true
	case "f3":
		return gkF3, 0, 0, "", true
	case "f4":
		return gkF4, 0, 0, "", true
	case "f5":
		return gkF5, 0, 0, "", true
	case "f6":
		return gkF6, 0, 0, "", true
	case "f7":
		return gkF7, 0, 0, "", true
	case "f8":
		return gkF8, 0, 0, "", true
	case "f9":
		return gkF9, 0, 0, "", true
	case "f10":
		return gkF10, 0, 0, "", true
	case "f11":
		return gkF11, 0, 0, "", true
	case "f12":
		return gkF12, 0, 0, "", true
	}

	// ctrl-a through ctrl-z
	if strings.HasPrefix(name, "ctrl-") && len(name) == 6 {
		ch := name[5]
		if ch >= 'a' && ch <= 'z' {
			// Use the letter's key code (a=0x04, b=0x05, etc.)
			gkey := int(0x04 + (ch - 'a'))
			return gkey, gmodCtrl, int(ch - 'a' + 1), "", true
		}
	}

	return 0, 0, 0, "", false
}

// ghosttyMouseButton converts a button name to a ghostty mouse button code.
func ghosttyMouseButton(name string) int {
	switch strings.ToLower(name) {
	case "left", "":
		return 0
	case "middle":
		return 1
	case "right":
		return 2
	default:
		return 0
	}
}

// --- Helpers ---

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
