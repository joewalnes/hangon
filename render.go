package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unicode/utf8"
)

// --- Cell grid model ---

// CellStyle holds the visual attributes of a single terminal cell.
type CellStyle struct {
	FG            string // hex color "#rrggbb" or "" for default
	BG            string // hex color "#rrggbb" or "" for default
	Bold          bool
	Italic        bool
	Underline     bool
	Strikethrough bool
	Inverse       bool
	Dim           bool
}

// Cell represents one character position on the terminal screen.
type Cell struct {
	Char  rune
	Width int // 1 for normal, 2 for wide (CJK/emoji), 0 for continuation
	Style CellStyle
}

// ScreenGrid is a 2D grid of cells representing the terminal display.
type ScreenGrid struct {
	Rows      int
	Cols      int
	Cells     [][]Cell
	CursorR   int
	CursorC   int
	HasCursor bool
}

// --- ANSI parser: text with escape codes → ScreenGrid ---

// ParseANSI parses the output of `tmux capture-pane -e -p` into a ScreenGrid.
// Each line of input corresponds to a row. ANSI SGR codes set style for
// subsequent characters.
func ParseANSI(ansiText string, rows, cols int) *ScreenGrid {
	grid := &ScreenGrid{
		Rows:  rows,
		Cols:  cols,
		Cells: make([][]Cell, rows),
	}
	for r := 0; r < rows; r++ {
		grid.Cells[r] = make([]Cell, cols)
		for c := 0; c < cols; c++ {
			grid.Cells[r][c] = Cell{Char: ' ', Width: 1}
		}
	}

	lines := strings.Split(ansiText, "\n")
	style := CellStyle{}

	for row := 0; row < rows && row < len(lines); row++ {
		line := lines[row]
		col := 0
		i := 0
		runes := []byte(line)

		for i < len(runes) && col < cols {
			if runes[i] == 0x1b && i+1 < len(runes) && runes[i+1] == '[' {
				// Parse CSI sequence.
				i += 2
				paramStart := i
				for i < len(runes) && ((runes[i] >= '0' && runes[i] <= '9') || runes[i] == ';') {
					i++
				}
				if i < len(runes) {
					params := string(runes[paramStart:i])
					final := runes[i]
					i++
					if final == 'm' {
						applySGR(&style, params)
					}
					// Ignore other CSI sequences.
				}
				continue
			}

			// Regular character.
			r, size := utf8.DecodeRune(runes[i:])
			if r == utf8.RuneError && size <= 1 {
				i++
				continue
			}
			i += size

			w := runeWidth(r)
			if col+w > cols {
				break
			}

			grid.Cells[row][col] = Cell{
				Char:  r,
				Width: w,
				Style: style,
			}
			col++

			// For wide characters, mark continuation cell.
			if w == 2 && col < cols {
				grid.Cells[row][col] = Cell{Char: 0, Width: 0, Style: style}
				col++
			}
		}
	}

	return grid
}

// applySGR applies SGR (Select Graphic Rendition) parameters to a style.
func applySGR(s *CellStyle, params string) {
	if params == "" || params == "0" {
		*s = CellStyle{}
		return
	}

	nums := parseSGRParams(params)
	i := 0
	for i < len(nums) {
		n := nums[i]
		switch {
		case n == 0:
			*s = CellStyle{}
		case n == 1:
			s.Bold = true
		case n == 2:
			s.Dim = true
		case n == 3:
			s.Italic = true
		case n == 4:
			s.Underline = true
		case n == 7:
			s.Inverse = true
		case n == 9:
			s.Strikethrough = true
		case n == 22:
			s.Bold = false
			s.Dim = false
		case n == 23:
			s.Italic = false
		case n == 24:
			s.Underline = false
		case n == 27:
			s.Inverse = false
		case n == 29:
			s.Strikethrough = false

		// Foreground colors.
		case n >= 30 && n <= 37:
			s.FG = ansi256[n-30]
		case n == 38:
			if i+1 < len(nums) && nums[i+1] == 5 && i+2 < len(nums) {
				s.FG = ansi256Color(nums[i+2])
				i += 2
			} else if i+1 < len(nums) && nums[i+1] == 2 && i+4 < len(nums) {
				s.FG = fmt.Sprintf("#%02x%02x%02x", nums[i+2], nums[i+3], nums[i+4])
				i += 4
			}
		case n == 39:
			s.FG = ""
		case n >= 90 && n <= 97:
			s.FG = ansi256[n-90+8]

		// Background colors.
		case n >= 40 && n <= 47:
			s.BG = ansi256[n-40]
		case n == 48:
			if i+1 < len(nums) && nums[i+1] == 5 && i+2 < len(nums) {
				s.BG = ansi256Color(nums[i+2])
				i += 2
			} else if i+1 < len(nums) && nums[i+1] == 2 && i+4 < len(nums) {
				s.BG = fmt.Sprintf("#%02x%02x%02x", nums[i+2], nums[i+3], nums[i+4])
				i += 4
			}
		case n == 49:
			s.BG = ""
		case n >= 100 && n <= 107:
			s.BG = ansi256[n-100+8]
		}
		i++
	}
}

func parseSGRParams(s string) []int {
	parts := strings.Split(s, ";")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			n = 0
		}
		nums = append(nums, n)
	}
	return nums
}

// --- SVG generation ---

// RenderConfig controls the appearance of the SVG output.
type RenderConfig struct {
	FontFamily string  // e.g. "JetBrains Mono, Fira Code, monospace"
	FontSize   float64 // px
	LineHeight float64 // multiplier
	PadX       float64 // horizontal padding px
	PadY       float64 // vertical padding px
	BG         string  // default background color
	FG         string  // default foreground color
	CursorFG   string  // cursor block color
	Radius     float64 // corner radius
	ShowCursor bool
}

var DefaultRenderConfig = RenderConfig{
	FontFamily: "'JetBrainsMono Nerd Font', 'JetBrains Mono', 'FiraCode Nerd Font', 'Fira Code', 'Hack Nerd Font', 'Cascadia Code', monospace",
	FontSize:   14,
	LineHeight: 1.35,
	PadX:       12,
	PadY:       12,
	BG:         "#1e1e2e",
	FG:         "#cdd6f4",
	CursorFG:   "#f5e0dc",
	Radius:     8,
	ShowCursor: true,
}

// RenderSVG converts a ScreenGrid to an SVG string.
func RenderSVG(grid *ScreenGrid, cfg RenderConfig) string {
	cellW := cfg.FontSize * 0.6 // approximate monospace char width
	cellH := cfg.FontSize * cfg.LineHeight
	width := cfg.PadX*2 + float64(grid.Cols)*cellW
	height := cfg.PadY*2 + float64(grid.Rows)*cellH

	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" viewBox="0 0 %.0f %.0f">`, width, height, width, height))
	b.WriteString("\n")

	// Style.
	b.WriteString("<style>\n")
	b.WriteString(fmt.Sprintf(`  .t { font-family: %s; font-size: %.0fpx; white-space: pre; }`, cfg.FontFamily, cfg.FontSize))
	b.WriteString("\n</style>\n")

	// Background.
	b.WriteString(fmt.Sprintf(`<rect width="100%%" height="100%%" rx="%.0f" fill="%s"/>`, cfg.Radius, cfg.BG))
	b.WriteString("\n")

	// Render each row.
	for row := 0; row < grid.Rows; row++ {
		y := cfg.PadY + float64(row)*cellH + cfg.FontSize // baseline

		// First pass: background rectangles.
		for col := 0; col < grid.Cols; col++ {
			cell := grid.Cells[row][col]
			if cell.Width == 0 {
				continue // continuation of wide char
			}

			bg := cell.Style.BG
			if cell.Style.Inverse {
				bg = cell.Style.FG
				if bg == "" {
					bg = cfg.FG
				}
			}

			if bg != "" {
				rx := cfg.PadX + float64(col)*cellW
				ry := cfg.PadY + float64(row)*cellH
				rw := cellW * float64(cell.Width)
				b.WriteString(fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`, rx, ry, rw, cellH, bg))
				b.WriteString("\n")
			}
		}

		// Second pass: text, grouped into same-style runs.
		// We emit one <text> per contiguous run of same-styled characters.
		// Spaces are included in runs (white-space:pre preserves them).
		// Trailing spaces on a row are trimmed to avoid bloat.
		type run struct {
			startCol int
			text     string
			style    CellStyle
		}

		// Find last non-space column.
		lastNonSpace := -1
		for col := grid.Cols - 1; col >= 0; col-- {
			c := grid.Cells[row][col]
			if c.Char != ' ' && c.Char != 0 {
				lastNonSpace = col
				break
			}
		}

		var runs []run
		var curRun *run

		for col := 0; col <= lastNonSpace; col++ {
			cell := grid.Cells[row][col]
			if cell.Width == 0 {
				continue
			}
			ch := cell.Char
			if ch == 0 {
				ch = ' '
			}

			if curRun != nil && curRun.style == cell.Style {
				curRun.text += string(ch)
			} else {
				if curRun != nil {
					runs = append(runs, *curRun)
				}
				curRun = &run{startCol: col, text: string(ch), style: cell.Style}
			}
		}
		if curRun != nil {
			runs = append(runs, *curRun)
		}

		// Emit text elements for each run.
		for _, r := range runs {
			fg := r.style.FG
			if r.style.Inverse {
				fg = r.style.BG
				if fg == "" {
					fg = cfg.BG
				}
			}
			if fg == "" {
				fg = cfg.FG
			}

			x := cfg.PadX + float64(r.startCol)*cellW
			attrs := fmt.Sprintf(`class="t" x="%.1f" y="%.1f" fill="%s"`, x, y, fg)
			if r.style.Bold {
				attrs += ` font-weight="bold"`
			}
			if r.style.Dim {
				attrs += ` opacity="0.5"`
			}
			if r.style.Italic {
				attrs += ` font-style="italic"`
			}

			var deco []string
			if r.style.Underline {
				deco = append(deco, "underline")
			}
			if r.style.Strikethrough {
				deco = append(deco, "line-through")
			}
			if len(deco) > 0 {
				attrs += fmt.Sprintf(` text-decoration="%s"`, strings.Join(deco, " "))
			}

			// XML-escape the text.
			text := xmlEscape(r.text)
			b.WriteString(fmt.Sprintf(`<text %s>%s</text>`, attrs, text))
			b.WriteString("\n")
		}
	}

	// Cursor.
	if cfg.ShowCursor && grid.HasCursor && grid.CursorR >= 0 && grid.CursorR < grid.Rows && grid.CursorC >= 0 && grid.CursorC < grid.Cols {
		cx := cfg.PadX + float64(grid.CursorC)*cellW
		cy := cfg.PadY + float64(grid.CursorR)*cellH
		b.WriteString(fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s" opacity="0.7"/>`,
			cx, cy, cellW, cellH, cfg.CursorFG))
		b.WriteString("\n")
	}

	b.WriteString("</svg>\n")
	return b.String()
}

// --- PNG conversion ---

// RenderPNG writes a terminal screenshot to a PNG file.
// It first generates SVG, then converts to PNG via rsvg-convert if available,
// otherwise falls back to saving as SVG.
// Returns the actual file path written (may have .svg extension if PNG unavailable).
func RenderPNG(grid *ScreenGrid, cfg RenderConfig, outPath string) (string, error) {
	svg := RenderSVG(grid, cfg)

	// Try rsvg-convert (from librsvg, available via: brew install librsvg).
	if rsvgPath, err := exec.LookPath("rsvg-convert"); err == nil {
		pngPath := outPath
		if !strings.HasSuffix(pngPath, ".png") {
			pngPath = strings.TrimSuffix(pngPath, ".svg") + ".png"
		}

		cmd := exec.Command(rsvgPath, "-o", pngPath)
		cmd.Stdin = strings.NewReader(svg)
		if err := cmd.Run(); err == nil {
			return pngPath, nil
		}
		// Fall through on error.
	}

	// Try convert (ImageMagick).
	if convertPath, err := exec.LookPath("convert"); err == nil {
		pngPath := outPath
		if !strings.HasSuffix(pngPath, ".png") {
			pngPath = strings.TrimSuffix(pngPath, ".svg") + ".png"
		}

		cmd := exec.Command(convertPath, "svg:-", pngPath)
		cmd.Stdin = strings.NewReader(svg)
		if err := cmd.Run(); err == nil {
			return pngPath, nil
		}
	}

	// Fallback: save as SVG.
	svgPath := outPath
	if strings.HasSuffix(svgPath, ".png") {
		svgPath = strings.TrimSuffix(svgPath, ".png") + ".svg"
	} else if !strings.HasSuffix(svgPath, ".svg") {
		svgPath += ".svg"
	}

	if err := os.WriteFile(svgPath, []byte(svg), 0o644); err != nil {
		return "", fmt.Errorf("write SVG: %w", err)
	}
	return svgPath, nil
}

// --- Utilities ---

// runeWidth returns the display width of a rune.
// Wide characters (CJK, some emoji) are 2 cells wide.
func runeWidth(r rune) int {
	if r == 0 {
		return 0
	}
	// CJK Unified Ideographs.
	if r >= 0x4E00 && r <= 0x9FFF {
		return 2
	}
	// CJK Unified Ideographs Extension A.
	if r >= 0x3400 && r <= 0x4DBF {
		return 2
	}
	// CJK Compatibility Ideographs.
	if r >= 0xF900 && r <= 0xFAFF {
		return 2
	}
	// Fullwidth Forms.
	if r >= 0xFF01 && r <= 0xFF60 {
		return 2
	}
	if r >= 0xFFE0 && r <= 0xFFE6 {
		return 2
	}
	// Hangul Syllables.
	if r >= 0xAC00 && r <= 0xD7AF {
		return 2
	}
	// CJK Radicals Supplement, Kangxi Radicals.
	if r >= 0x2E80 && r <= 0x2FDF {
		return 2
	}
	// CJK Symbols and Punctuation, Hiragana, Katakana.
	if r >= 0x3000 && r <= 0x30FF {
		return 2
	}
	// Emoji presentation (approximate: many emoji are rendered as 2-wide).
	if r >= 0x1F300 && r <= 0x1F9FF {
		return 2
	}
	if r >= 0x2600 && r <= 0x27BF {
		return 2 // miscellaneous symbols, dingbats
	}
	return 1
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

// --- ANSI 256-color palette ---

// First 16 colors (standard + bright).
var ansi256 = [16]string{
	"#000000", // 0  black
	"#cd0000", // 1  red
	"#00cd00", // 2  green
	"#cdcd00", // 3  yellow
	"#0000ee", // 4  blue
	"#cd00cd", // 5  magenta
	"#00cdcd", // 6  cyan
	"#e5e5e5", // 7  white
	"#7f7f7f", // 8  bright black
	"#ff0000", // 9  bright red
	"#00ff00", // 10 bright green
	"#ffff00", // 11 bright yellow
	"#5c5cff", // 12 bright blue
	"#ff00ff", // 13 bright magenta
	"#00ffff", // 14 bright cyan
	"#ffffff", // 15 bright white
}

// ansi256Color returns the hex color for a 256-color index.
func ansi256Color(n int) string {
	if n < 0 || n > 255 {
		return ""
	}
	if n < 16 {
		return ansi256[n]
	}
	if n < 232 {
		// 6x6x6 color cube: indices 16-231.
		n -= 16
		b := n % 6
		n /= 6
		g := n % 6
		r := n / 6
		return fmt.Sprintf("#%02x%02x%02x", cubeVal(r), cubeVal(g), cubeVal(b))
	}
	// Grayscale: indices 232-255 → 24 shades from #080808 to #eeeeee.
	v := 8 + (n-232)*10
	return fmt.Sprintf("#%02x%02x%02x", v, v, v)
}

func cubeVal(i int) int {
	if i == 0 {
		return 0
	}
	return 55 + i*40
}
