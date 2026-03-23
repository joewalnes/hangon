#!/usr/bin/env python3
"""Simple TUI demo for testing the ghostty backend.
Displays colored text, handles mouse events, and shows a counter.
"""
import curses
import time

def main(stdscr):
    curses.start_color()
    curses.use_default_colors()
    curses.mousemask(curses.ALL_MOUSE_EVENTS | curses.REPORT_MOUSE_POSITION)
    curses.curs_set(1)
    stdscr.nodelay(True)
    stdscr.timeout(100)

    # Initialize color pairs
    for i in range(1, 8):
        curses.init_pair(i, i, -1)

    counter = 0
    mouse_x, mouse_y = 0, 0
    mouse_clicks = 0
    messages = []

    while True:
        stdscr.clear()
        rows, cols = stdscr.getmaxyx()

        # Title
        title = "=== Ghostty TUI Demo ==="
        stdscr.addstr(0, (cols - len(title)) // 2, title, curses.A_BOLD | curses.color_pair(3))

        # Colored text
        stdscr.addstr(2, 2, "Red text", curses.color_pair(1) | curses.A_BOLD)
        stdscr.addstr(2, 15, "Green text", curses.color_pair(2))
        stdscr.addstr(2, 30, "Blue text", curses.color_pair(4) | curses.A_UNDERLINE)
        stdscr.addstr(2, 45, "Magenta", curses.color_pair(5) | curses.A_ITALIC)

        # Unicode & emoji test
        stdscr.addstr(4, 2, "Unicode: α β γ δ ε ζ η θ ★ ♠ ♥ ♦ ♣")
        stdscr.addstr(5, 2, "CJK: 日本語テスト 中文测试 한국어")

        # Counter
        counter += 1
        stdscr.addstr(7, 2, f"Counter: {counter}", curses.color_pair(6))
        stdscr.addstr(7, 25, f"Time: {time.strftime('%H:%M:%S')}", curses.color_pair(7))

        # Mouse info
        stdscr.addstr(9, 2, f"Mouse: ({mouse_x}, {mouse_y})  Clicks: {mouse_clicks}", curses.color_pair(2))

        # Instructions
        stdscr.addstr(11, 2, "Click anywhere to test mouse | Press 'q' to quit", curses.A_DIM)

        # Box drawing
        for i in range(13, min(19, rows-1)):
            stdscr.addstr(i, 2, "│", curses.color_pair(4))
            stdscr.addstr(i, 50, "│", curses.color_pair(4))
        stdscr.addstr(13, 2, "┌" + "─" * 47 + "┐", curses.color_pair(4))
        stdscr.addstr(min(18, rows-2), 2, "└" + "─" * 47 + "┘", curses.color_pair(4))

        # Messages inside box
        for idx, msg in enumerate(messages[-4:]):
            if 14 + idx < min(18, rows-1):
                stdscr.addstr(14 + idx, 4, msg[:46], curses.color_pair(3))

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
                if bstate & curses.BUTTON1_CLICKED:
                    mouse_clicks += 1
                    messages.append(f"Click at ({mx}, {my})")
                elif bstate & curses.BUTTON1_DOUBLE_CLICKED:
                    mouse_clicks += 2
                    messages.append(f"Double-click at ({mx}, {my})")
            except curses.error:
                pass

if __name__ == "__main__":
    curses.wrapper(main)
