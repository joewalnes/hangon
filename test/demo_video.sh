#!/bin/bash
#
# Generate a demo video showcasing:
# 1. Nerd fonts, CJK, emoji rendering
# 2. Mouse interaction with visible pointer + click indicators
#
# Prerequisites: hangon built with -tags ghostty, ffmpeg, python3
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
HANGON="$PROJECT_DIR/hangon-demo"
OUT_DIR="$PROJECT_DIR/test/output"
mkdir -p "$OUT_DIR"

if [ ! -x "$HANGON" ]; then
    echo "Error: hangon-demo not found. Build with: go build -tags ghostty -o hangon-demo ."
    exit 1
fi

echo "=== Demo Video: Font Rendering & Mouse Interaction ==="
echo ""

# Clean up any prior sessions.
"$HANGON" stopall 2>/dev/null || true
sleep 0.3

# --- Part 1: Start the font demo TUI ---
echo "Starting TUI font demo..."
"$HANGON" start tty --name demo -- python3 "$SCRIPT_DIR/tui_font_demo.py"
sleep 2

# Start recording at 10 FPS.
echo "Starting video recording..."
"$HANGON" record-start demo "$OUT_DIR/demo.mp4" 10
sleep 1

# --- Part 2: Let the TUI render for a few seconds (shows nerd fonts, CJK, emoji) ---
echo "Recording font rendering..."
sleep 3

# --- Part 3: Screenshot the initial state ---
echo "Taking screenshot..."
"$HANGON" screenshot demo "$OUT_DIR/demo_fonts.png"

# --- Part 4: Mouse interactions ---
echo "Performing mouse interactions..."

# Click on the title area.
"$HANGON" mouse-click demo 0 20 left
sleep 0.5

# Click on the Nerd Font section.
"$HANGON" mouse-click demo 3 10 left
sleep 0.5

# Click on the CJK section.
"$HANGON" mouse-click demo 7 15 left
sleep 0.5

# Click on the Emoji section.
"$HANGON" mouse-click demo 11 10 left
sleep 0.5

# Click on Box Drawing section.
"$HANGON" mouse-click demo 15 15 left
sleep 0.5

# Click in the mouse interaction section.
"$HANGON" mouse-click demo 19 10 left
sleep 0.5

# Double-click.
"$HANGON" mouse-double-click demo 19 20 left
sleep 0.5

# Drag across the screen.
"$HANGON" mouse-drag demo 2 5 8 35 left
sleep 0.5

# More clicks at different positions.
"$HANGON" mouse-click demo 5 30 left
sleep 0.3
"$HANGON" mouse-click demo 10 40 left
sleep 0.3
"$HANGON" mouse-click demo 15 25 left
sleep 0.5

# Scroll.
"$HANGON" mouse-scroll demo 10 20 3
sleep 0.5

# Let it settle.
sleep 1

# --- Part 5: Stop recording ---
echo "Stopping recording..."
"$HANGON" record-stop demo

echo ""
echo "Stopping session..."
"$HANGON" send demo "q"
sleep 0.5
"$HANGON" stop demo 2>/dev/null || true

echo ""
echo "=== Output ==="
echo "  Video:      $OUT_DIR/demo.mp4"
echo "  Screenshot: $OUT_DIR/demo_fonts.png"
ls -la "$OUT_DIR/demo.mp4" "$OUT_DIR/demo_fonts.png" 2>/dev/null || true
echo ""
echo "Done!"
