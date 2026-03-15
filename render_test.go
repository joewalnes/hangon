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
