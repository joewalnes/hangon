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
	cfg := DefaultDirectRenderConfig

	// Load font.
	face, err := loadFontFace(cfg)
	if err != nil {
		return "", fmt.Errorf("load font: %w", err)
	}

	// Calculate cell dimensions from font metrics.
	cellW := cfg.CellWidth
	cellH := cfg.CellHeight
	if cellW == 0 || cellH == 0 {
		metrics := face.Metrics()
		if cellW == 0 {
			// Use the advance width of 'M' as cell width.
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

	// Image dimensions.
	imgW := cfg.PadX*2 + grid.Cols*cellW
	imgH := cfg.PadY*2 + grid.Rows*cellH

	// Create image.
	img := image.NewRGBA(image.Rect(0, 0, imgW, imgH))

	// Fill background.
	bgColor := parseHexColor(cfg.BG)
	for y := 0; y < imgH; y++ {
		for x := 0; x < imgW; x++ {
			img.Set(x, y, bgColor)
		}
	}

	// Baseline offset: distance from top of cell to the font baseline.
	metrics := face.Metrics()
	baseline := metrics.Ascent.Ceil()

	// Render each cell.
	for row := 0; row < grid.Rows; row++ {
		for col := 0; col < grid.Cols; col++ {
			cell := grid.Cells[row][col]
			if cell.Width == 0 {
				continue // continuation of wide char
			}

			cellX := cfg.PadX + col*cellW
			cellY := cfg.PadY + row*cellH

			// Determine effective colors.
			fgHex := cell.Style.FG
			bgHex := cell.Style.BG
			if cell.Style.Inverse {
				fgHex, bgHex = bgHex, fgHex
			}
			if fgHex == "" {
				fgHex = cfg.FG
			}
			if bgHex == "" {
				bgHex = ""
			}

			// Draw cell background if non-default.
			if bgHex != "" {
				bgC := parseHexColor(bgHex)
				cellWidthPx := cellW * cell.Width
				for dy := 0; dy < cellH; dy++ {
					for dx := 0; dx < cellWidthPx; dx++ {
						img.Set(cellX+dx, cellY+dy, bgC)
					}
				}
			}

			// Draw character.
			ch := cell.Char
			if ch == 0 || ch == ' ' {
				continue
			}

			fgC := parseHexColor(fgHex)
			if cell.Style.Dim {
				// Reduce opacity by blending with background.
				bg := parseHexColor(cfg.BG)
				fgC = blendColor(fgC, bg, 0.5)
			}

			// Draw the glyph.
			dot := fixed.Point26_6{
				X: fixed.I(cellX),
				Y: fixed.I(cellY + baseline),
			}

			d := &font.Drawer{
				Dst:  img,
				Src:  image.NewUniform(fgC),
				Face: face,
				Dot:  dot,
			}
			d.DrawString(string(ch))

			// Bold: draw shifted by 1px for synthetic bold.
			if cell.Style.Bold {
				dot.X += fixed.I(1)
				d2 := &font.Drawer{
					Dst:  img,
					Src:  image.NewUniform(fgC),
					Face: face,
					Dot:  dot,
				}
				d2.DrawString(string(ch))
			}

			// Underline.
			if cell.Style.Underline {
				uy := cellY + cellH - 2
				cellWidthPx := cellW * cell.Width
				for dx := 0; dx < cellWidthPx; dx++ {
					img.Set(cellX+dx, uy, fgC)
				}
			}

			// Strikethrough.
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

// loadFontFace loads a font face for terminal rendering.
// It tries several common paths for JetBrains Mono Nerd Font.
func loadFontFace(cfg DirectRenderConfig) (font.Face, error) {
	fontPaths := []string{}

	if cfg.FontPath != "" {
		fontPaths = append(fontPaths, cfg.FontPath)
	}

	// Common font installation paths.
	home, _ := os.UserHomeDir()
	fontPaths = append(fontPaths,
		// User-local fonts (Linux)
		filepath.Join(home, ".local/share/fonts/JetBrainsMonoNerdFont-Regular.ttf"),
		filepath.Join(home, ".local/share/fonts/JetBrains Mono Nerd Font Regular.ttf"),
		filepath.Join(home, ".local/share/fonts/NerdFonts/JetBrainsMonoNerdFont-Regular.ttf"),
		// System fonts (Linux)
		"/usr/share/fonts/truetype/jetbrains-mono/JetBrainsMonoNerdFont-Regular.ttf",
		"/usr/share/fonts/jetbrains-mono-nerd/JetBrainsMonoNerdFont-Regular.ttf",
		"/usr/share/fonts/TTF/JetBrainsMonoNerdFont-Regular.ttf",
		// macOS
		filepath.Join(home, "Library/Fonts/JetBrainsMonoNerdFont-Regular.ttf"),
		"/Library/Fonts/JetBrainsMonoNerdFont-Regular.ttf",
		// Non-nerd-font fallbacks
		filepath.Join(home, ".local/share/fonts/JetBrainsMono-Regular.ttf"),
		"/usr/share/fonts/truetype/jetbrains-mono/JetBrainsMono-Regular.ttf",
		"/usr/share/fonts/jetbrains-mono/JetBrainsMono-Regular.ttf",
		filepath.Join(home, "Library/Fonts/JetBrainsMono-Regular.ttf"),
		"/Library/Fonts/JetBrainsMono-Regular.ttf",
		// Generic monospace fallbacks
		"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
		"/usr/share/fonts/TTF/DejaVuSansMono.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationMono-Regular.ttf",
	)

	for _, path := range fontPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		ft, err := opentype.Parse(data)
		if err != nil {
			continue
		}

		dpi := cfg.DPI
		if dpi <= 0 {
			dpi = 96
		}

		face, err := opentype.NewFace(ft, &opentype.FaceOptions{
			Size:    cfg.FontSize,
			DPI:     dpi,
			Hinting: font.HintingFull,
		})
		if err != nil {
			continue
		}

		return face, nil
	}

	return nil, fmt.Errorf("no suitable font found; install JetBrains Mono Nerd Font or set HANGON_FONT_PATH")
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
