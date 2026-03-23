#!/usr/bin/env python3
"""TUI demo exercising nerd fonts, CJK, emoji, and mouse interaction.
Used by the e2e test suite and video recording demos.
"""
import curses
import time
import sys

def main(stdscr):
    curses.start_color()
    curses.use_default_colors()
    curses.mousemask(curses.ALL_MOUSE_EVENTS | curses.REPORT_MOUSE_POSITION)
    curses.curs_set(0)
    stdscr.nodelay(True)
    stdscr.timeout(100)

    # Enable mouse tracking (SGR mode for better coordinates)
    sys.stdout.write('\033[?1003h')  # any-event tracking
    sys.stdout.write('\033[?1006h')  # SGR extended mode
    sys.stdout.flush()

    # Initialize color pairs
    for i in range(1, 8):
        curses.init_pair(i, i, -1)
    # Extra: bright colors
    curses.init_pair(8, curses.COLOR_WHITE, curses.COLOR_BLUE)

    mouse_x, mouse_y = -1, -1
    mouse_btn = ""
    mouse_events = []
    frame = 0

    while True:
        stdscr.erase()
        rows, cols = stdscr.getmaxyx()
        frame += 1

        # ── Title ──
        title = " Terminal Rendering Demo "
        bar = "─" * ((cols - len(title) - 2) // 2)
        try:
            stdscr.addstr(0, 0, f"┌{bar}{title}{bar}┐", curses.color_pair(4) | curses.A_BOLD)
        except curses.error:
            pass

        # ── Section: Nerd Font Glyphs ──
        r = 2
        try:
            stdscr.addstr(r, 2, "▎ Nerd Font Glyphs", curses.color_pair(3) | curses.A_BOLD)
            r += 1
            # Powerline: \ue0b0-\ue0b3, Devicons, file icons, etc.
            nerd = (
                "  \ue0b0 \ue0b2 \ue0b1 \ue0b3"  # powerline
                "  \uf121 \uf09b \ue7a8 \ue606"  # dev: code, github, rust, python
                "  \uf0e7 \uf013 \uf023 \uf0c0"  # fa: bolt, gear, lock, users
                "  \uf07c \uf15b \uf1c9 \uf15c"  # fa: folder, file, file-code, file-text
            )
            stdscr.addstr(r, 2, nerd, curses.color_pair(6))
            r += 1
            # Git/weather/misc nerd font icons
            nerd2 = (
                "  \ue725 \ue702 \uf408 \uf1d3"  # git branch, merge, star, rocket
                "  \uf484 \uf0ac \uf120 \uf46a"  # heart, globe, terminal, fire
            )
            stdscr.addstr(r, 2, nerd2, curses.color_pair(2))
        except curses.error:
            pass

        # ── Section: CJK Characters ──
        r += 2
        try:
            stdscr.addstr(r, 2, "▎ CJK Characters", curses.color_pair(3) | curses.A_BOLD)
            r += 1
            stdscr.addstr(r, 2, "  日本語: ", curses.color_pair(7))
            stdscr.addstr("東京タワー こんにちは", curses.color_pair(1))
            r += 1
            stdscr.addstr(r, 2, "  中文:   ", curses.color_pair(7))
            stdscr.addstr("你好世界 程序设计", curses.color_pair(1))
            r += 1
            stdscr.addstr(r, 2, "  한국어: ", curses.color_pair(7))
            stdscr.addstr("안녕하세요 프로그래밍", curses.color_pair(1))
        except curses.error:
            pass

        # ── Section: Emoji ──
        r += 2
        try:
            stdscr.addstr(r, 2, "▎ Emoji & Symbols", curses.color_pair(3) | curses.A_BOLD)
            r += 1
            stdscr.addstr(r, 2, "  🚀 🎉 🔥 💡 ⚡ 🎯 🌍 🛠️  ✅ ❌ ⚠️  📦", curses.color_pair(0))
            r += 1
            stdscr.addstr(r, 2, "  ★ ♠ ♥ ♦ ♣ ● ○ ◆ ◇ ▲ ▼ ► ◄ ✓ ✗ ∞", curses.color_pair(5))
        except curses.error:
            pass

        # ── Section: Box Drawing & Styles ──
        r += 2
        try:
            stdscr.addstr(r, 2, "▎ Box Drawing & Text Styles", curses.color_pair(3) | curses.A_BOLD)
            r += 1
            stdscr.addstr(r, 2, "  ╔═══════════════╦═══════════════╗", curses.color_pair(4))
            r += 1
            stdscr.addstr(r, 2, "  ║", curses.color_pair(4))
            stdscr.addstr(" Bold text     ", curses.A_BOLD)
            stdscr.addstr("║", curses.color_pair(4))
            stdscr.addstr(" Italic text   ", curses.A_ITALIC if hasattr(curses, 'A_ITALIC') else curses.A_DIM)
            stdscr.addstr("║", curses.color_pair(4))
            r += 1
            stdscr.addstr(r, 2, "  ║", curses.color_pair(4))
            stdscr.addstr(" Underline     ", curses.A_UNDERLINE)
            stdscr.addstr("║", curses.color_pair(4))
            stdscr.addstr(" Reverse video ", curses.A_REVERSE)
            stdscr.addstr("║", curses.color_pair(4))
            r += 1
            stdscr.addstr(r, 2, "  ╚═══════════════╩═══════════════╝", curses.color_pair(4))
        except curses.error:
            pass

        # ── Section: Mouse Interaction ──
        r += 2
        try:
            stdscr.addstr(r, 2, "▎ Mouse Interaction", curses.color_pair(3) | curses.A_BOLD)
            r += 1
            if mouse_x >= 0:
                stdscr.addstr(r, 2, f"  Position: ({mouse_x:3d}, {mouse_y:3d})  Last: {mouse_btn}", curses.color_pair(2))
            else:
                stdscr.addstr(r, 2, "  Move mouse or click anywhere...", curses.color_pair(7))
            r += 1
            # Show recent events
            for evt in mouse_events[-3:]:
                r += 1
                if r < rows - 1:
                    stdscr.addstr(r, 4, evt[:cols-6], curses.color_pair(6))
        except curses.error:
            pass

        # ── Footer ──
        try:
            footer = f" Frame {frame} │ {time.strftime('%H:%M:%S')} │ Press 'q' to quit "
            stdscr.addstr(rows - 1, 0, "─" * cols, curses.color_pair(4))
            stdscr.addstr(rows - 1, 2, footer, curses.color_pair(7))
        except curses.error:
            pass

        stdscr.refresh()

        try:
            ch = stdscr.getch()
        except curses.error:
            continue

        if ch == ord('q'):
            break
        elif ch == curses.KEY_MOUSE:
            try:
                _, mx, my, _, bstate = curses.getmouse()
                mouse_x, mouse_y = mx, my
                if bstate & curses.BUTTON1_PRESSED:
                    mouse_btn = "LEFT_DOWN"
                    mouse_events.append(f"🖱 Click at ({mx}, {my})")
                elif bstate & curses.BUTTON1_RELEASED:
                    mouse_btn = "LEFT_UP"
                    mouse_events.append(f"🖱 Release at ({mx}, {my})")
                elif bstate & curses.BUTTON1_CLICKED:
                    mouse_btn = "CLICK"
                    mouse_events.append(f"🖱 Click at ({mx}, {my})")
                elif bstate & curses.BUTTON1_DOUBLE_CLICKED:
                    mouse_btn = "DOUBLE"
                    mouse_events.append(f"🖱 Double-click at ({mx}, {my})")
                elif bstate & curses.BUTTON3_PRESSED:
                    mouse_btn = "RIGHT"
                    mouse_events.append(f"🖱 Right-click at ({mx}, {my})")
                else:
                    mouse_btn = "MOVE"
            except curses.error:
                pass

    # Disable mouse tracking
    sys.stdout.write('\033[?1006l')
    sys.stdout.write('\033[?1003l')
    sys.stdout.flush()

if __name__ == "__main__":
    curses.wrapper(main)
