package main

import (
	"strings"
	"testing"
)

func TestParseANSI_PlainText(t *testing.T) {
	grid := ParseANSI("Hello world", 1, 80)
	if grid.Rows != 1 || grid.Cols != 80 {
		t.Errorf("grid size: %dx%d", grid.Rows, grid.Cols)
	}
	got := gridLineText(grid, 0)
	if got != "Hello world" {
		t.Errorf("got %q, want %q", got, "Hello world")
	}
}

func TestParseANSI_SGRColors(t *testing.T) {
	// Red text: ESC[31m RED ESC[0m
	input := "\x1b[31mRED\x1b[0m normal"
	grid := ParseANSI(input, 1, 80)

	// "RED" should have red foreground.
	for i := 0; i < 3; i++ {
		if grid.Cells[0][i].Style.FG != "#cd0000" {
			t.Errorf("cell %d FG=%q, want #cd0000", i, grid.Cells[0][i].Style.FG)
		}
	}
	// " normal" should have default (empty) foreground.
	if grid.Cells[0][4].Style.FG != "" {
		t.Errorf("cell 4 FG=%q, want empty (default)", grid.Cells[0][4].Style.FG)
	}
}

func TestParseANSI_BoldAndReset(t *testing.T) {
	input := "\x1b[1mBOLD\x1b[0m"
	grid := ParseANSI(input, 1, 80)

	if !grid.Cells[0][0].Style.Bold {
		t.Error("cell 0 should be bold")
	}
	// After reset, next cell should not be bold.
	if grid.Cells[0][4].Style.Bold {
		t.Error("cell 4 should not be bold")
	}
}

func TestParseANSI_256Color(t *testing.T) {
	// ESC[38;5;196m = 256-color FG index 196 (bright red)
	input := "\x1b[38;5;196mX"
	grid := ParseANSI(input, 1, 80)

	// Index 196: in the 6x6x6 cube. 196-16=180, b=180%6=0, g=(180/6)%6=0, r=180/36=5
	// r=5 → 55+5*40=255, g=0 → 0, b=0 → 0 → #ff0000
	if grid.Cells[0][0].Style.FG != "#ff0000" {
		t.Errorf("got FG=%q, want #ff0000", grid.Cells[0][0].Style.FG)
	}
}

func TestParseANSI_TrueColor(t *testing.T) {
	// ESC[38;2;100;200;50m = truecolor FG
	input := "\x1b[38;2;100;200;50mX"
	grid := ParseANSI(input, 1, 80)

	if grid.Cells[0][0].Style.FG != "#64c832" {
		t.Errorf("got FG=%q, want #64c832", grid.Cells[0][0].Style.FG)
	}
}

func TestParseANSI_BackgroundColor(t *testing.T) {
	// ESC[44m = blue background
	input := "\x1b[44mX"
	grid := ParseANSI(input, 1, 80)

	if grid.Cells[0][0].Style.BG != "#0000ee" {
		t.Errorf("got BG=%q, want #0000ee", grid.Cells[0][0].Style.BG)
	}
}

func TestParseANSI_MultipleAttributes(t *testing.T) {
	// Bold + italic + underline + green FG
	input := "\x1b[1;3;4;32mX"
	grid := ParseANSI(input, 1, 80)

	s := grid.Cells[0][0].Style
	if !s.Bold {
		t.Error("should be bold")
	}
	if !s.Italic {
		t.Error("should be italic")
	}
	if !s.Underline {
		t.Error("should be underline")
	}
	if s.FG != "#00cd00" {
		t.Errorf("FG=%q, want #00cd00", s.FG)
	}
}

func TestParseANSI_MultipleLines(t *testing.T) {
	input := "Line1\nLine2\nLine3"
	grid := ParseANSI(input, 4, 80)

	if gridLineText(grid, 0) != "Line1" {
		t.Errorf("row 0: %q", gridLineText(grid, 0))
	}
	if gridLineText(grid, 1) != "Line2" {
		t.Errorf("row 1: %q", gridLineText(grid, 1))
	}
	if gridLineText(grid, 2) != "Line3" {
		t.Errorf("row 2: %q", gridLineText(grid, 2))
	}
}

func TestAnsi256Color_Palette(t *testing.T) {
	tests := []struct {
		index int
		want  string
	}{
		{0, "#000000"},   // black
		{1, "#cd0000"},   // red
		{15, "#ffffff"},  // bright white
		{16, "#000000"},  // cube(0,0,0)
		{231, "#ffffff"}, // cube(5,5,5)
		{232, "#080808"}, // grayscale start
		{255, "#eeeeee"}, // grayscale end
	}
	for _, tt := range tests {
		got := ansi256Color(tt.index)
		if got != tt.want {
			t.Errorf("ansi256Color(%d)=%q, want %q", tt.index, got, tt.want)
		}
	}
}

func TestRenderSVG_ContainsExpectedElements(t *testing.T) {
	input := "\x1b[31mRED\x1b[0m OK"
	grid := ParseANSI(input, 2, 20)

	svg := RenderSVG(grid, DefaultRenderConfig)

	if !strings.Contains(svg, "<svg") {
		t.Error("missing <svg> tag")
	}
	if !strings.Contains(svg, "fill=\"#cd0000\"") {
		t.Error("missing red fill for RED text")
	}
	if !strings.Contains(svg, ">RED<") {
		t.Error("missing RED text content")
	}
	if !strings.Contains(svg, "OK") {
		t.Error("missing OK text content")
	}
	if !strings.Contains(svg, "Nerd Font") {
		t.Error("missing Nerd Font in font stack")
	}
}

func TestRenderSVG_Cursor(t *testing.T) {
	grid := ParseANSI("X", 2, 10)
	grid.HasCursor = true
	grid.CursorR = 0
	grid.CursorC = 1

	svg := RenderSVG(grid, DefaultRenderConfig)
	if !strings.Contains(svg, "opacity=\"0.7\"") {
		t.Error("missing cursor rectangle")
	}
}

func TestRenderSVG_BoldAttribute(t *testing.T) {
	input := "\x1b[1mBOLD\x1b[0m"
	grid := ParseANSI(input, 1, 20)
	svg := RenderSVG(grid, DefaultRenderConfig)

	if !strings.Contains(svg, "font-weight=\"bold\"") {
		t.Error("missing bold attribute")
	}
}

func TestRenderSVG_UnderlineAttribute(t *testing.T) {
	input := "\x1b[4mUNDER\x1b[0m"
	grid := ParseANSI(input, 1, 20)
	svg := RenderSVG(grid, DefaultRenderConfig)

	if !strings.Contains(svg, "text-decoration=\"underline\"") {
		t.Error("missing underline attribute")
	}
}

func TestRenderSVG_BackgroundRect(t *testing.T) {
	input := "\x1b[41mBG\x1b[0m"
	grid := ParseANSI(input, 1, 20)
	svg := RenderSVG(grid, DefaultRenderConfig)

	// Should have a red background rect.
	if !strings.Contains(svg, "fill=\"#cd0000\"") {
		t.Error("missing red background rectangle")
	}
}

func TestRuneWidth(t *testing.T) {
	tests := []struct {
		r    rune
		want int
	}{
		{'A', 1},
		{'z', 1},
		{' ', 1},
		{'中', 2}, // CJK
		{'あ', 2}, // Hiragana
		{'ア', 2}, // Katakana
		{0, 0},
	}
	for _, tt := range tests {
		got := runeWidth(tt.r)
		if got != tt.want {
			t.Errorf("runeWidth(%q)=%d, want %d", tt.r, got, tt.want)
		}
	}
}

// TestParseANSI_TrailingColoredSpacesPreserved documents that ParseANSI
// correctly assigns background color to trailing space cells when given
// them — i.e. the parser was never the problem for screenshot Bug 1. The
// actual bug was upstream: tmux's `capture-pane -e -p` trims trailing
// whitespace (and its color) before ParseANSI ever sees it, unless -N is
// passed. That half of the fix is covered by
// TestTmuxCaptureAnsiArgs_PreservesTrailingSpaces in
// backend_process_test.go. This test guards the other half: that *if* the
// color survives capture, ParseANSI carries it through to every cell,
// including trailing ones.
func TestParseANSI_TrailingColoredSpacesPreserved(t *testing.T) {
	// Blue background set once, then "hi" plus trailing spaces — mirrors
	// what `tmux capture-pane -e -p -N` emits for a colored, padded line.
	input := "\x1b[44mhi        " // "hi" + 8 spaces = 10 cells
	grid := ParseANSI(input, 1, 10)

	for col := 0; col < 10; col++ {
		if grid.Cells[0][col].Style.BG != "#0000ee" {
			t.Errorf("cell %d BG=%q, want #0000ee (blue) — trailing space lost its background color", col, grid.Cells[0][col].Style.BG)
		}
	}
}

// TestRenderSVG_BackgroundRunsAreMerged guards the fix for screenshot Bug
// 2: hangon used to emit one <rect> per cell for backgrounds, even across
// a long run of identical color. Adjacent independently-anti-aliased
// <rect> elements produce visible hairline seams when rasterized (a
// standard vector-rasterization conflation artifact — see RenderSVG's
// comment above the background-run-building code for the full
// explanation), which showed up as thin gray seams between character
// cells, most visible on light backgrounds. The fix groups same-color
// cells into a single merged rect per contiguous run, the same way text
// glyphs are already grouped into runs below.
func TestRenderSVG_BackgroundRunsAreMerged(t *testing.T) {
	// 10 cells of identical blue background on an otherwise plain row.
	input := "\x1b[44m          \x1b[0m"
	grid := ParseANSI(input, 1, 10)
	svg := RenderSVG(grid, DefaultRenderConfig)

	got := strings.Count(svg, `fill="#0000ee"`)
	if got != 1 {
		t.Errorf("background rect count for one uniform-color run = %d, want 1 (cells should be merged into a single rect, not one per cell)", got)
	}
}

// TestRenderSVG_BackgroundRunsSplitOnColorChange makes sure the run-merging
// fix (see TestRenderSVG_BackgroundRunsAreMerged) doesn't over-merge: two
// adjacent but differently-colored runs must still produce two rects, not
// one averaged or incorrect one.
func TestRenderSVG_BackgroundRunsSplitOnColorChange(t *testing.T) {
	input := "\x1b[44m     \x1b[41m     \x1b[0m"
	grid := ParseANSI(input, 1, 10)
	svg := RenderSVG(grid, DefaultRenderConfig)

	if strings.Count(svg, `fill="#0000ee"`) != 1 {
		t.Errorf("expected exactly one blue background rect")
	}
	if strings.Count(svg, `fill="#cd0000"`) != 1 {
		t.Errorf("expected exactly one red background rect")
	}
}

// TestRenderSVG_TriangleGlyphsRenderAsPolygon guards the fix for
// screenshot Bug 3: the Unicode Geometric Shapes quadrant triangles
// (◢ ◣ ◤ ◥) were rendered as <text> glyphs, and the font actually used to
// rasterize them draws them as small dingbats centered well within the
// cell rather than filling it edge to edge — unlike real terminal
// emulators, which render these procedurally to exactly fill the cell.
// The fix special-cases these characters to draw as a full-cell <polygon>
// instead of relying on any font's glyph outline. See cellFillShapes.
func TestRenderSVG_TriangleGlyphsRenderAsPolygon(t *testing.T) {
	for ch := range cellFillShapes {
		input := "\x1b[38;2;255;100;100m" + string(ch)
		grid := ParseANSI(input, 1, 5)
		svg := RenderSVG(grid, DefaultRenderConfig)

		if !strings.Contains(svg, "<polygon") {
			t.Errorf("%q: expected a <polygon> element, got none. SVG:\n%s", string(ch), svg)
		}
		// Must NOT also be emitted as a text glyph relying on font coverage.
		escaped := xmlEscape(string(ch))
		if strings.Contains(svg, ">"+escaped+"<") {
			t.Errorf("%q: character was emitted as <text> content, expected it to be drawn as a polygon instead", string(ch))
		}
	}
}

// TestRenderSVG_TrianglePolygonOrientation locks in the exact fill
// geometry for ◢ (BLACK LOWER RIGHT TRIANGLE), verified against real
// rendered output: the empty corner is top-left, the filled corner is
// bottom-right. Getting this backwards would silently produce a
// wrong-looking (but not obviously broken) screenshot.
func TestRenderSVG_TrianglePolygonOrientation(t *testing.T) {
	pts, ok := cellFillShapes['◢']
	if !ok {
		t.Fatal("◢ missing from cellFillShapes")
	}
	// Expect vertices at top-right, bottom-right, bottom-left — i.e. no
	// vertex at top-left (that's the empty corner).
	for _, p := range pts {
		if p[0] == 0 && p[1] == 0 {
			t.Errorf("◢ polygon has a vertex at top-left (0,0); that corner should be empty. pts=%v", pts)
		}
	}
	hasBottomRight := false
	for _, p := range pts {
		if p[0] == 1 && p[1] == 1 {
			hasBottomRight = true
		}
	}
	if !hasBottomRight {
		t.Errorf("◢ polygon has no vertex at bottom-right (1,1); expected it filled. pts=%v", pts)
	}
}

// gridLineText extracts the text content of a grid row, trimming trailing spaces.
func gridLineText(grid *ScreenGrid, row int) string {
	if row >= grid.Rows {
		return ""
	}
	var b strings.Builder
	for _, cell := range grid.Cells[row] {
		if cell.Width == 0 {
			continue
		}
		b.WriteRune(cell.Char)
	}
	return strings.TrimRight(b.String(), " ")
}
