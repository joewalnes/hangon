package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pointInBounds reports whether (x, y) falls within image rectangle r.
func pointInBounds(r image.Rectangle, x, y int) bool {
	return x >= r.Min.X && x < r.Max.X && y >= r.Min.Y && y < r.Max.Y
}

// TestIntegration_ScreenshotFullFrameBackground is a real, end-to-end
// pixel-level regression test for screenshot rendering Bug 1 and Bug 2:
//
//   - Bug 1: `tmux capture-pane -e -p` (without -N) trims trailing colored
//     whitespace, so lines padded with background color lost that color in
//     the screenshot and fell back to RenderConfig's default background —
//     a large wrong-colored region on any realistic full-width app frame.
//   - Bug 2: background cells were drawn as one <rect> per cell, which
//     produces hairline seams between adjacent same-colored cells when
//     rasterized (an SVG anti-aliasing conflation artifact).
//
// This test drives a real tmux session (not a mock), takes a real
// screenshot through the same code path `hangon screenshot` uses, and
// samples actual pixels from the resulting PNG — decoded via Go's
// image/png, not an external tool — to verify against the known-correct
// color. It requires tmux and either rsvg-convert or ImageMagick's
// `convert` to produce a real PNG; it skips (rather than fails) when
// those aren't available, matching how TestIntegration_ProcessSession
// already handles a missing tmux.
func TestIntegration_ScreenshotFullFrameBackground(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed, skipping integration test")
	}
	if _, err := exec.LookPath("rsvg-convert"); err != nil {
		if _, err := exec.LookPath("convert"); err != nil {
			t.Skip("neither rsvg-convert nor ImageMagick convert installed, skipping PNG-producing test")
		}
	}

	binary := filepath.Join(t.TempDir(), "hangon")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %s\n%s", err, out)
	}

	stateDir := t.TempDir()
	name := "screenshot-bg-test"

	run := func(args ...string) (string, error) {
		cmd := exec.Command(binary, args...)
		cmd.Env = append(os.Environ(), "HOME="+stateDir)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}

	// A frame with a white background line, dark text, and a run of
	// trailing colored blank space at the end — the exact shape that
	// triggered Bug 1 (color-carrying trailing whitespace) and, since the
	// blank run is many cells wide, would show Bug 2's seams throughout if
	// they were still present.
	ansiFile := filepath.Join(t.TempDir(), "frame.ansi")
	// ESC[2J ESC[H clear+home, then white bg + dark fg, short text, then
	// spaces out to column 80, no trailing reset (mirrors a real app that
	// just moves on to the next line).
	frame := "\x1b[2J\x1b[H\x1b[48;2;255;255;255m\x1b[38;2;20;20;20mHELLO" + strings.Repeat(" ", 75)
	if err := os.WriteFile(ansiFile, []byte(frame), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := run("start", "process", "--name", name, "--", "bash", "-c", fmt.Sprintf("cat %q; sleep 30", ansiFile))
	if err != nil {
		t.Fatalf("start failed: %s\n%s", err, out)
	}
	defer run("stop", name)

	// Give the pane time to render the frame.
	time.Sleep(500 * time.Millisecond)

	screenshotPath := filepath.Join(t.TempDir(), "test-screenshot.png")
	out, err = run("screenshot", name, screenshotPath)
	if err != nil {
		t.Fatalf("screenshot failed: %s\n%s", err, out)
	}
	if !strings.HasSuffix(out, ".png") {
		t.Skipf("no PNG converter produced a .png (got %q), skipping pixel assertions", out)
	}

	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open screenshot: %v", err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode screenshot PNG: %v", err)
	}

	// Cell geometry matches DefaultRenderConfig (FontSize 14, LineHeight
	// 1.35, cellW = FontSize*0.6, PadX/PadY 12).
	const cellW = 14 * 0.6
	const cellH = 14 * 1.35
	const padX = 12.0
	const padY = 12.0

	cellCenter := func(row, col int) (int, int) {
		x := padX + float64(col)*cellW + cellW/2
		y := padY + float64(row)*cellH + cellH/2
		return int(x), int(y)
	}

	isNearWhite := func(c color.Color) bool {
		r, g, b, _ := c.RGBA()
		// 16-bit channels; roughly require all channels > ~90%.
		return r > 0xe000 && g > 0xe000 && b > 0xe000
	}

	// Sample several cells across the trailing padding region (col 10..79
	// on row 0) — before the fix these fell back to RenderConfig's default
	// dark background (#1e1e2e) instead of the white the app actually set.
	failures := 0
	for _, col := range []int{10, 30, 50, 70, 79} {
		x, y := cellCenter(0, col)
		if !pointInBounds(img.Bounds(), x, y) {
			t.Fatalf("sample point (%d,%d) for col %d is outside image bounds %v", x, y, col, img.Bounds())
		}
		px := img.At(x, y)
		if !isNearWhite(px) {
			r, g, b, _ := px.RGBA()
			t.Errorf("row 0 col %d: pixel=(%d,%d,%d), want near-white (255,255,255) — trailing colored background was lost", col, r>>8, g>>8, b>>8)
			failures++
		}
	}
	if failures > 0 {
		t.Logf("failures indicate the tmux capture-pane -N flag (Bug 1) may have regressed")
	}

	// Seam check (Bug 2): scan a horizontal line through the padding
	// region and make sure there's no non-white pixel between the white
	// cells — a seam would show as a thin gray/dark line at regular
	// ~cellW intervals.
	_, y := cellCenter(0, 10)
	xStart, _ := cellCenter(0, 10)
	xEnd, _ := cellCenter(0, 79)
	seamPixels := 0
	for x := xStart; x < xEnd; x++ {
		if !pointInBounds(img.Bounds(), x, y) {
			continue
		}
		if !isNearWhite(img.At(x, y)) {
			seamPixels++
		}
	}
	if seamPixels > 0 {
		t.Errorf("found %d non-white pixel(s) in what should be a solid white background run (row 0, cols 10-79) — background rects may not be merged into contiguous runs, reintroducing seams", seamPixels)
	}
}
