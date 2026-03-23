package main

import (
	"image/png"
	"os"
	"testing"
)

// TestEmbeddedFontLoads verifies the embedded Nerd Font can be loaded.
func TestEmbeddedFontLoads(t *testing.T) {
	if len(embeddedNerdFont) == 0 {
		t.Fatal("embedded nerd font is empty")
	}
	t.Logf("embedded font size: %d bytes", len(embeddedNerdFont))

	fs, err := loadFontSet(DefaultDirectRenderConfig)
	if err != nil {
		t.Fatalf("loadFontSet failed: %v", err)
	}
	if fs.primary == nil {
		t.Fatal("primary font face is nil")
	}
	t.Logf("loaded %d fallback fonts", len(fs.fallbacks))
}

// TestNerdFontGlyphs verifies that Nerd Font glyphs are resolved by the primary font.
func TestNerdFontGlyphs(t *testing.T) {
	fs, err := loadFontSet(DefaultDirectRenderConfig)
	if err != nil {
		t.Fatalf("loadFontSet: %v", err)
	}

	nerdGlyphs := map[string]rune{
		"powerline-right":  '\ue0b0',
		"powerline-left":   '\ue0b2',
		"dev-code":         '\uf121',
		"dev-github":       '\uf09b',
		"fa-bolt":          '\uf0e7',
		"fa-gear":          '\uf013',
		"fa-lock":          '\uf023',
		"fa-folder":        '\uf07c',
		"fa-terminal":      '\uf120',
	}

	for name, r := range nerdGlyphs {
		face := fs.faceForRune(r)
		adv, ok := face.GlyphAdvance(r)
		if !ok || adv == 0 {
			t.Errorf("nerd glyph %s (U+%04X): not found or zero advance", name, r)
		}
	}
}

// TestCJKGlyphs verifies CJK characters can be rendered (via fallback fonts).
func TestCJKGlyphs(t *testing.T) {
	fs, err := loadFontSet(DefaultDirectRenderConfig)
	if err != nil {
		t.Fatalf("loadFontSet: %v", err)
	}

	if len(fs.fallbacks) == 0 {
		t.Skip("no CJK fallback fonts available on this system")
	}

	cjkChars := map[string]rune{
		"ja-nihon":   '日',
		"ja-go":      '語',
		"zh-ni":      '你',
		"zh-hao":     '好',
		"zh-shi":     '世',
		"zh-jie":     '界',
		"ko-an":      '안',
		"ko-nyeong":  '녕',
		"hiragana-a": 'あ',
		"katakana-a": 'ア',
	}

	for name, r := range cjkChars {
		face := fs.faceForRune(r)
		adv, ok := face.GlyphAdvance(r)
		if !ok || adv == 0 {
			t.Errorf("CJK char %s (U+%04X '%c'): not found or zero advance", name, r, r)
		}
	}
}

// TestEmojiSymbols verifies common symbols render (text-form emoji).
func TestEmojiSymbols(t *testing.T) {
	fs, err := loadFontSet(DefaultDirectRenderConfig)
	if err != nil {
		t.Fatalf("loadFontSet: %v", err)
	}

	symbols := map[string]rune{
		"star":       '★',
		"spade":      '♠',
		"heart":      '♥',
		"diamond":    '♦',
		"club":       '♣',
		"check":      '✓',
		"cross":      '✗',
		"infinity":   '∞',
		"bullet":     '●',
		"triangle":   '▲',
		"box-horiz":  '─',
		"box-vert":   '│',
		"box-corner": '┌',
		"double-top": '╔',
	}

	for name, r := range symbols {
		face := fs.faceForRune(r)
		adv, ok := face.GlyphAdvance(r)
		if !ok || adv == 0 {
			t.Errorf("symbol %s (U+%04X '%c'): not found or zero advance", name, r, r)
		}
	}
}

// TestRenderGridWithMixedContent creates a ScreenGrid with nerd fonts, CJK, and
// emoji, then renders it to PNG to verify the full pipeline works.
func TestRenderGridWithMixedContent(t *testing.T) {
	grid := &ScreenGrid{
		Rows:  10,
		Cols:  50,
		Cells: make([][]Cell, 10),
	}

	// Initialize all cells.
	for r := 0; r < grid.Rows; r++ {
		grid.Cells[r] = make([]Cell, grid.Cols)
		for c := 0; c < grid.Cols; c++ {
			grid.Cells[r][c] = Cell{Char: ' ', Width: 1}
		}
	}

	// Row 0: ASCII
	setRow(grid, 0, "Hello, Terminal World!")

	// Row 1: Nerd font glyphs (powerline + devicons)
	nerd := []rune{'\ue0b0', ' ', '\uf121', ' ', '\uf09b', ' ', '\ue7a8', ' ', '\uf0e7', ' ', '\uf013'}
	for i, r := range nerd {
		if i < grid.Cols {
			grid.Cells[1][i] = Cell{Char: r, Width: 1, Style: CellStyle{FG: "#89b4fa"}}
		}
	}

	// Row 3: CJK (wide characters)
	cjk := []rune{'日', '本', '語', ' ', '中', '文', ' ', '한', '국', '어'}
	col := 0
	for _, r := range cjk {
		if col >= grid.Cols-1 {
			break
		}
		if r == ' ' {
			grid.Cells[3][col] = Cell{Char: ' ', Width: 1}
			col++
		} else {
			grid.Cells[3][col] = Cell{Char: r, Width: 2, Style: CellStyle{FG: "#f38ba8"}}
			col++
			grid.Cells[3][col] = Cell{Char: 0, Width: 0} // continuation
			col++
		}
	}

	// Row 5: Symbols
	setRow(grid, 5, "★ ♠ ♥ ♦ ♣ ● ✓ ✗ ∞ ▲ ▼")

	// Row 7: Box drawing
	setRow(grid, 7, "╔══════════╦══════════╗")

	// Row 8: Styled text
	for i, r := range []rune("Bold & Color") {
		if i < grid.Cols {
			grid.Cells[8][i] = Cell{Char: r, Width: 1, Style: CellStyle{Bold: true, FG: "#a6e3a1"}}
		}
	}

	// Render to a temp file.
	tmpFile := t.TempDir() + "/mixed_content.png"
	outPath, err := RenderPNGDirect(grid, tmpFile)
	if err != nil {
		t.Fatalf("RenderPNGDirect failed: %v", err)
	}

	// Verify the output is a valid PNG.
	f, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer f.Close()

	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode PNG: %v", err)
	}

	bounds := img.Bounds()
	t.Logf("rendered image: %dx%d", bounds.Dx(), bounds.Dy())
	if bounds.Dx() < 100 || bounds.Dy() < 100 {
		t.Errorf("image too small: %dx%d", bounds.Dx(), bounds.Dy())
	}
}

// TestMouseOverlay verifies that the mouse cursor overlay renders correctly.
func TestMouseOverlay(t *testing.T) {
	grid := &ScreenGrid{
		Rows:  10,
		Cols:  40,
		Cells: make([][]Cell, 10),
	}
	for r := 0; r < grid.Rows; r++ {
		grid.Cells[r] = make([]Cell, grid.Cols)
		for c := 0; c < grid.Cols; c++ {
			grid.Cells[r][c] = Cell{Char: ' ', Width: 1}
		}
	}
	setRow(grid, 0, "Click here to test mouse overlay")

	// Test without mouse (should match RenderPNGDirect).
	noMouse := t.TempDir() + "/no_mouse.png"
	_, err := RenderPNGDirectWithMouse(grid, noMouse, nil)
	if err != nil {
		t.Fatalf("render without mouse: %v", err)
	}

	// Test with mouse (not pressed).
	withMouse := t.TempDir() + "/with_mouse.png"
	_, err = RenderPNGDirectWithMouse(grid, withMouse, &MouseOverlay{Row: 0, Col: 10})
	if err != nil {
		t.Fatalf("render with mouse: %v", err)
	}

	// Test with mouse pressed.
	pressed := t.TempDir() + "/pressed.png"
	_, err = RenderPNGDirectWithMouse(grid, pressed, &MouseOverlay{Row: 5, Col: 20, Pressed: true, HeldMs: 500})
	if err != nil {
		t.Fatalf("render with pressed mouse: %v", err)
	}

	// Verify all files are valid PNGs.
	for _, path := range []string{noMouse, withMouse, pressed} {
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}
		_, err = png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
}

func setRow(grid *ScreenGrid, row int, text string) {
	col := 0
	for _, r := range text {
		if col >= grid.Cols {
			break
		}
		grid.Cells[row][col] = Cell{Char: r, Width: 1}
		col++
	}
}
