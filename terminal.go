package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Terminal is a minimal VT100 screen state tracker.
// It processes escape sequences and maintains a 2D character grid
// representing the current visible terminal content.
type Terminal struct {
	mu     sync.Mutex
	rows   int
	cols   int
	screen [][]rune
	curRow int
	curCol int
}

func NewTerminal(rows, cols int) *Terminal {
	if rows <= 0 {
		rows = 24
	}
	if cols <= 0 {
		cols = 80
	}
	t := &Terminal{rows: rows, cols: cols}
	t.screen = make([][]rune, rows)
	for i := range t.screen {
		t.screen[i] = make([]rune, cols)
		for j := range t.screen[i] {
			t.screen[i][j] = ' '
		}
	}
	return t
}

// Write processes bytes as terminal output, updating the screen state.
func (t *Terminal) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	i := 0
	for i < len(p) {
		b := p[i]

		if b == 0x1b && i+1 < len(p) && p[i+1] == '[' {
			// CSI escape sequence.
			i += 2
			i = t.parseCSI(p, i)
			continue
		}

		switch b {
		case '\n':
			t.curCol = 0
			t.lineFeed()
		case '\r':
			t.curCol = 0
		case '\b':
			if t.curCol > 0 {
				t.curCol--
			}
		case '\t':
			t.curCol = (t.curCol + 8) & ^7
			if t.curCol >= t.cols {
				t.curCol = t.cols - 1
			}
		case 0x07: // BEL - ignore
		default:
			if b >= 0x20 {
				t.putChar(rune(b))
			}
		}
		i++
	}
	return len(p), nil
}

func (t *Terminal) putChar(ch rune) {
	if t.curRow >= t.rows {
		t.curRow = t.rows - 1
	}
	if t.curCol >= t.cols {
		// Wrap to next line.
		t.curCol = 0
		t.lineFeed()
	}
	t.screen[t.curRow][t.curCol] = ch
	t.curCol++
}

func (t *Terminal) lineFeed() {
	t.curRow++
	if t.curRow >= t.rows {
		// Scroll up.
		t.curRow = t.rows - 1
		copy(t.screen, t.screen[1:])
		t.screen[t.rows-1] = make([]rune, t.cols)
		for j := range t.screen[t.rows-1] {
			t.screen[t.rows-1][j] = ' '
		}
	}
}

func (t *Terminal) parseCSI(p []byte, start int) int {
	// Collect parameter bytes and the final byte.
	i := start
	var params []byte
	for i < len(p) {
		b := p[i]
		if b >= 0x30 && b <= 0x3f {
			// Parameter byte (0-9, ;, etc.)
			params = append(params, b)
			i++
		} else if b >= 0x20 && b <= 0x2f {
			// Intermediate byte - skip.
			i++
		} else {
			// Final byte.
			t.handleCSI(string(params), b)
			return i + 1
		}
	}
	return i
}

func (t *Terminal) handleCSI(params string, final byte) {
	nums := parseCSIParams(params)

	switch final {
	case 'A': // Cursor Up
		n := intOr(nums, 0, 1)
		t.curRow -= n
		if t.curRow < 0 {
			t.curRow = 0
		}
	case 'B': // Cursor Down
		n := intOr(nums, 0, 1)
		t.curRow += n
		if t.curRow >= t.rows {
			t.curRow = t.rows - 1
		}
	case 'C': // Cursor Forward
		n := intOr(nums, 0, 1)
		t.curCol += n
		if t.curCol >= t.cols {
			t.curCol = t.cols - 1
		}
	case 'D': // Cursor Back
		n := intOr(nums, 0, 1)
		t.curCol -= n
		if t.curCol < 0 {
			t.curCol = 0
		}
	case 'H', 'f': // Cursor Position
		row := intOr(nums, 0, 1) - 1
		col := intOr(nums, 1, 1) - 1
		if row < 0 {
			row = 0
		}
		if row >= t.rows {
			row = t.rows - 1
		}
		if col < 0 {
			col = 0
		}
		if col >= t.cols {
			col = t.cols - 1
		}
		t.curRow = row
		t.curCol = col
	case 'J': // Erase in Display
		n := intOr(nums, 0, 0)
		switch n {
		case 0: // Clear from cursor to end.
			t.clearLine(t.curRow, t.curCol, t.cols)
			for r := t.curRow + 1; r < t.rows; r++ {
				t.clearLine(r, 0, t.cols)
			}
		case 1: // Clear from start to cursor.
			for r := 0; r < t.curRow; r++ {
				t.clearLine(r, 0, t.cols)
			}
			t.clearLine(t.curRow, 0, t.curCol+1)
		case 2, 3: // Clear entire screen.
			for r := 0; r < t.rows; r++ {
				t.clearLine(r, 0, t.cols)
			}
			t.curRow = 0
			t.curCol = 0
		}
	case 'K': // Erase in Line
		n := intOr(nums, 0, 0)
		switch n {
		case 0: // Clear from cursor to end of line.
			t.clearLine(t.curRow, t.curCol, t.cols)
		case 1: // Clear from start of line to cursor.
			t.clearLine(t.curRow, 0, t.curCol+1)
		case 2: // Clear entire line.
			t.clearLine(t.curRow, 0, t.cols)
		}
	case 'm': // SGR (Select Graphic Rendition) - ignore colors/styles.
	case 'h', 'l': // Set/Reset Mode - ignore.
	case 'r': // Set Scrolling Region - ignore for now.
	case 'G': // Cursor Horizontal Absolute
		n := intOr(nums, 0, 1) - 1
		if n < 0 {
			n = 0
		}
		if n >= t.cols {
			n = t.cols - 1
		}
		t.curCol = n
	case 'd': // Cursor Vertical Absolute
		n := intOr(nums, 0, 1) - 1
		if n < 0 {
			n = 0
		}
		if n >= t.rows {
			n = t.rows - 1
		}
		t.curRow = n
	}
}

func (t *Terminal) clearLine(row, from, to int) {
	if row < 0 || row >= t.rows {
		return
	}
	if from < 0 {
		from = 0
	}
	if to > t.cols {
		to = t.cols
	}
	for c := from; c < to; c++ {
		t.screen[row][c] = ' '
	}
}

// String returns the current screen content as a string.
// Trailing spaces on each line are trimmed, and trailing blank lines are removed.
func (t *Terminal) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	var lines []string
	for _, row := range t.screen {
		lines = append(lines, strings.TrimRight(string(row), " "))
	}

	// Remove trailing empty lines.
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// CursorPos returns current cursor position (for debugging/status).
func (t *Terminal) CursorPos() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return fmt.Sprintf("row=%d col=%d", t.curRow, t.curCol)
}

func parseCSIParams(s string) []int {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ";")
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, _ := strconv.Atoi(p)
		nums[i] = n
	}
	return nums
}

func intOr(nums []int, idx, def int) int {
	if idx < len(nums) && nums[idx] > 0 {
		return nums[idx]
	}
	return def
}
