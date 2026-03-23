package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// DirectRenderConfig controls the appearance of the direct PNG renderer.
type DirectRenderConfig struct {
	FontPath   string  // Path to a TTF/OTF font file (default: search common paths)
	FontSize   float64 // Font size in points (default: 14)
	CellWidth  int     // Override cell width in pixels (0 = auto from font metrics)
	CellHeight int     // Override cell height in pixels (0 = auto from font metrics)
	PadX       int     // Horizontal padding in pixels
	PadY       int     // Vertical padding in pixels
	DPI        float64 // DPI for font rendering (default: 96)
	BG         string  // Default background color "#rrggbb"
	FG         string  // Default foreground color "#rrggbb"
	CursorFG   string  // Cursor color "#rrggbb"
	ShowCursor bool
}

var DefaultDirectRenderConfig = DirectRenderConfig{
	FontSize:   14,
	PadX:       12,
	PadY:       12,
	DPI:        96,
	BG:         "#1e1e2e",
	FG:         "#cdd6f4",
	CursorFG:   "#f5e0dc",
	ShowCursor: true,
}

// RenderPNGDirect renders a ScreenGrid directly to a PNG file using Go's image library
// with TrueType/OpenType font rendering. This produces pixel-perfect results matching
// what the Ghostty terminal would display.
//
// Returns the file path written, or an error if font loading/rendering fails.
func RenderPNGDirect(grid *ScreenGrid, outPath string) (string, error) {
	return RenderPNGDirectWithMouse(grid, outPath, nil)
}

// MouseOverlay describes a mouse cursor to draw on a rendered frame.
type MouseOverlay struct {
	Row     int  // terminal row (0-based)
	Col     int  // terminal column (0-based)
	Pressed bool // true if a button is held
	HeldMs  int  // how long the button has been held (for visual feedback)
}

// RenderPNGDirectWithMouse renders a ScreenGrid to PNG with an optional mouse cursor overlay.
func RenderPNGDirectWithMouse(grid *ScreenGrid, outPath string, mouse *MouseOverlay) (string, error) {
	cfg := DefaultDirectRenderConfig

	// Load fonts.
	fonts, err := loadFontSet(cfg)
	if err != nil {
		return "", fmt.Errorf("load font: %w", err)
	}
	face := fonts.primary

	// Calculate cell dimensions.
	cellW := cfg.CellWidth
	cellH := cfg.CellHeight
	if cellW == 0 || cellH == 0 {
		metrics := face.Metrics()
		if cellW == 0 {
			adv, ok := face.GlyphAdvance('M')
			if ok {
				cellW = adv.Ceil()
			} else {
				cellW = int(cfg.FontSize * 0.6)
			}
		}
		if cellH == 0 {
			cellH = (metrics.Height).Ceil()
			if cellH == 0 {
				cellH = int(cfg.FontSize * 1.35)
			}
		}
	}

	imgW := cfg.PadX*2 + grid.Cols*cellW
	imgH := cfg.PadY*2 + grid.Rows*cellH

	img := image.NewRGBA(image.Rect(0, 0, imgW, imgH))

	// Fill background.
	bgColor := parseHexColor(cfg.BG)
	for y := 0; y < imgH; y++ {
		for x := 0; x < imgW; x++ {
			img.Set(x, y, bgColor)
		}
	}

	metrics := face.Metrics()
	baseline := metrics.Ascent.Ceil()

	// Render cells (same as RenderPNGDirect).
	for row := 0; row < grid.Rows; row++ {
		for col := 0; col < grid.Cols; col++ {
			cell := grid.Cells[row][col]
			if cell.Width == 0 {
				continue
			}

			cellX := cfg.PadX + col*cellW
			cellY := cfg.PadY + row*cellH

			fgHex := cell.Style.FG
			bgHex := cell.Style.BG
			if cell.Style.Inverse {
				fgHex, bgHex = bgHex, fgHex
			}
			if fgHex == "" {
				fgHex = cfg.FG
			}

			if bgHex != "" {
				bgC := parseHexColor(bgHex)
				cellWidthPx := cellW * cell.Width
				for dy := 0; dy < cellH; dy++ {
					for dx := 0; dx < cellWidthPx; dx++ {
						img.Set(cellX+dx, cellY+dy, bgC)
					}
				}
			}

			ch := cell.Char
			if ch == 0 || ch == ' ' {
				continue
			}

			fgC := parseHexColor(fgHex)
			if cell.Style.Dim {
				bg := parseHexColor(cfg.BG)
				fgC = blendColor(fgC, bg, 0.5)
			}

			glyphFace := fonts.faceForRune(ch)
			dot := fixed.Point26_6{
				X: fixed.I(cellX),
				Y: fixed.I(cellY + baseline),
			}
			d := &font.Drawer{Dst: img, Src: image.NewUniform(fgC), Face: glyphFace, Dot: dot}
			d.DrawString(string(ch))

			if cell.Style.Bold {
				dot.X += fixed.I(1)
				d2 := &font.Drawer{Dst: img, Src: image.NewUniform(fgC), Face: glyphFace, Dot: dot}
				d2.DrawString(string(ch))
			}
			if cell.Style.Underline {
				uy := cellY + cellH - 2
				cellWidthPx := cellW * cell.Width
				for dx := 0; dx < cellWidthPx; dx++ {
					img.Set(cellX+dx, uy, fgC)
				}
			}
			if cell.Style.Strikethrough {
				sy := cellY + cellH/2
				cellWidthPx := cellW * cell.Width
				for dx := 0; dx < cellWidthPx; dx++ {
					img.Set(cellX+dx, sy, fgC)
				}
			}
		}
	}

	// Render cursor.
	if cfg.ShowCursor && grid.HasCursor &&
		grid.CursorR >= 0 && grid.CursorR < grid.Rows &&
		grid.CursorC >= 0 && grid.CursorC < grid.Cols {
		cursorColor := parseHexColor(cfg.CursorFG)
		cx := cfg.PadX + grid.CursorC*cellW
		cy := cfg.PadY + grid.CursorR*cellH
		for dy := 0; dy < cellH; dy++ {
			for dx := 0; dx < cellW; dx++ {
				existing := img.RGBAAt(cx+dx, cy+dy)
				blended := blendColor(cursorColor, existing, 0.7)
				img.Set(cx+dx, cy+dy, blended)
			}
		}
	}

	// Draw mouse cursor overlay.
	drawMouseOverlay(img, mouse, cfg.PadX, cfg.PadY, cellW, cellH, grid.Rows, grid.Cols)

	// Write PNG.
	pngPath := outPath
	if !strings.HasSuffix(pngPath, ".png") {
		pngPath = strings.TrimSuffix(pngPath, filepath.Ext(pngPath)) + ".png"
	}
	f, err := os.Create(pngPath)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return "", fmt.Errorf("encode PNG: %w", err)
	}
	return pngPath, nil
}

// drawMouseOverlay draws a visible mouse cursor with click/hold indicators.
func drawMouseOverlay(img *image.RGBA, mouse *MouseOverlay, padX, padY, cellW, cellH, rows, cols int) {
	if mouse == nil || mouse.Row < 0 || mouse.Col < 0 {
		return
	}
	if mouse.Row >= rows || mouse.Col >= cols {
		return
	}

	// Center of the cursor cell.
	cx := padX + mouse.Col*cellW + cellW/2
	cy := padY + mouse.Row*cellH + cellH/2

	bounds := img.Bounds()

	// Choose cursor color based on button state.
	var cursorColor, ringColor color.RGBA
	if mouse.Pressed {
		cursorColor = color.RGBA{255, 80, 80, 255}   // red when pressed
		ringColor = color.RGBA{255, 120, 120, 200}    // lighter red ring
	} else {
		cursorColor = color.RGBA{255, 255, 255, 255}  // white pointer
		ringColor = color.RGBA{100, 100, 100, 180}    // gray outline
	}

	// Draw a filled circle (pointer dot) with outline.
	pointerR := max(cellW/3, 3)
	ringR := pointerR + 2

	// Outer ring.
	for dy := -ringR; dy <= ringR; dy++ {
		for dx := -ringR; dx <= ringR; dx++ {
			px, py := cx+dx, cy+dy
			if px < 0 || py < 0 || px >= bounds.Max.X || py >= bounds.Max.Y {
				continue
			}
			dist := dx*dx + dy*dy
			if dist <= ringR*ringR && dist > pointerR*pointerR {
				existing := img.RGBAAt(px, py)
				img.Set(px, py, blendColor(ringColor, existing, 0.8))
			}
		}
	}

	// Inner filled circle.
	for dy := -pointerR; dy <= pointerR; dy++ {
		for dx := -pointerR; dx <= pointerR; dx++ {
			px, py := cx+dx, cy+dy
			if px < 0 || py < 0 || px >= bounds.Max.X || py >= bounds.Max.Y {
				continue
			}
			if dx*dx+dy*dy <= pointerR*pointerR {
				existing := img.RGBAAt(px, py)
				img.Set(px, py, blendColor(cursorColor, existing, 0.9))
			}
		}
	}

	// When held, draw a pulsing ring to indicate held state.
	if mouse.Pressed && mouse.HeldMs > 0 {
		pulseR := ringR + 4 + (mouse.HeldMs/100)%6
		pulseColor := color.RGBA{255, 80, 80, 150}
		for dy := -pulseR; dy <= pulseR; dy++ {
			for dx := -pulseR; dx <= pulseR; dx++ {
				px, py := cx+dx, cy+dy
				if px < 0 || py < 0 || px >= bounds.Max.X || py >= bounds.Max.Y {
					continue
				}
				dist := dx*dx + dy*dy
				if dist <= pulseR*pulseR && dist > (pulseR-2)*(pulseR-2) {
					existing := img.RGBAAt(px, py)
					img.Set(px, py, blendColor(pulseColor, existing, 0.6))
				}
			}
		}
	}
}

// fontSet holds primary and fallback font faces for rendering.
type fontSet struct {
	primary   font.Face
	fallbacks []font.Face
}

// faceForRune returns the best font face for the given rune.
func (fs *fontSet) faceForRune(r rune) font.Face {
	if _, ok := fs.primary.GlyphAdvance(r); ok {
		return fs.primary
	}
	for _, fb := range fs.fallbacks {
		if _, ok := fb.GlyphAdvance(r); ok {
			return fb
		}
	}
	return fs.primary
}

// loadFontSet loads the primary font and fallback fonts for CJK/emoji.
func loadFontSet(cfg DirectRenderConfig) (*fontSet, error) {
	dpi := cfg.DPI
	if dpi <= 0 {
		dpi = 96
	}
	opts := &opentype.FaceOptions{
		Size:    cfg.FontSize,
		DPI:     dpi,
		Hinting: font.HintingFull,
	}

	// Try loading primary font.
	var primary font.Face

	// 1. User-specified font path.
	if cfg.FontPath != "" {
		if f, err := loadFontFromFile(cfg.FontPath, opts); err == nil {
			primary = f
		}
	}

	// 2. Embedded Nerd Font (always available, no install needed).
	if primary == nil && len(embeddedNerdFont) > 0 {
		if f, err := loadFontFromData(embeddedNerdFont, opts); err == nil {
			primary = f
		}
	}

	// 3. System-installed fonts as last resort.
	if primary == nil {
		home, _ := os.UserHomeDir()
		systemPaths := []string{
			filepath.Join(home, ".local/share/fonts/JetBrainsMonoNerdFont-Regular.ttf"),
			"/usr/share/fonts/truetype/jetbrains-mono/JetBrainsMonoNerdFont-Regular.ttf",
			filepath.Join(home, "Library/Fonts/JetBrainsMonoNerdFont-Regular.ttf"),
			"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationMono-Regular.ttf",
		}
		for _, path := range systemPaths {
			if f, err := loadFontFromFile(path, opts); err == nil {
				primary = f
				break
			}
		}
	}

	if primary == nil {
		return nil, fmt.Errorf("no suitable font found")
	}

	fs := &fontSet{primary: primary}

	// Load CJK fallback: embedded Noto Sans Mono CJK SC first, then system fonts.
	if cjkData := getEmbeddedCJKFont(); len(cjkData) > 0 {
		if f, err := loadFontFromData(cjkData, opts); err == nil {
			fs.fallbacks = append(fs.fallbacks, f)
		}
	}

	// System CJK/emoji fallback fonts.
	fallbackPaths := []string{
		// CJK fonts
		"/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
		"/usr/share/fonts/opentype/ipafont-gothic/ipag.ttf",
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/truetype/noto/NotoSansMono-Regular.ttf",
		// Unifont (huge Unicode coverage)
		"/usr/share/fonts/opentype/unifont/unifont.otf",
		// macOS CJK
		"/System/Library/Fonts/PingFang.ttc",
		"/System/Library/Fonts/Hiragino Sans GB.ttc",
	}
	for _, path := range fallbackPaths {
		if f, err := loadFontFromFile(path, opts); err == nil {
			fs.fallbacks = append(fs.fallbacks, f)
		}
	}

	return fs, nil
}

func loadFontFromFile(path string, opts *opentype.FaceOptions) (font.Face, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return loadFontFromData(data, opts)
}

func loadFontFromData(data []byte, opts *opentype.FaceOptions) (font.Face, error) {
	ft, err := opentype.Parse(data)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(ft, opts)
}

// loadFontFace loads a font face for terminal rendering (compatibility wrapper).
func loadFontFace(cfg DirectRenderConfig) (font.Face, error) {
	fs, err := loadFontSet(cfg)
	if err != nil {
		return nil, err
	}
	return fs.primary, nil
}

// parseHexColor parses a "#rrggbb" string into a color.RGBA.
func parseHexColor(hex string) color.RGBA {
	if hex == "" {
		return color.RGBA{0, 0, 0, 255}
	}
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return color.RGBA{0, 0, 0, 255}
	}
	var r, g, b uint8
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return color.RGBA{r, g, b, 255}
}

// blendColor blends two colors. alpha=1.0 means fully fg, 0.0 means fully bg.
func blendColor(fg, bg color.RGBA, alpha float64) color.RGBA {
	return color.RGBA{
		R: uint8(float64(fg.R)*alpha + float64(bg.R)*(1-alpha)),
		G: uint8(float64(fg.G)*alpha + float64(bg.G)*(1-alpha)),
		B: uint8(float64(fg.B)*alpha + float64(bg.B)*(1-alpha)),
		A: 255,
	}
}
