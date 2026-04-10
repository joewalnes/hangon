# hangon

Persistent session manager for CLI-driven app interaction.

> **For AI agents:** hangon lets you start, interact with, and test long-running
> processes (servers, REPLs, TUIs), TCP sockets, WebSockets, and macOS desktop
> apps — all through simple shell commands. Install with
> `curl -Lo hangon https://github.com/joewalnes/hangon/releases/latest/download/hangon-linux-amd64 && chmod +x hangon`
> (or see Install below for other platforms) and run `hangon --help` for the
> full command reference. Each command is stateless: start a session, send
> input, read output, assert with `expect`, take screenshots, and stop — no
> persistent connection needed.

hangon lets you start a long-running process, TCP socket, WebSocket, or macOS
desktop app in the background and interact with it through short-lived shell
commands. Each command connects to the session, performs one action, and exits.

This makes it ideal for **shell scripts** and **AI coding agents** that need to
drive interactive applications without maintaining a persistent connection.

```
$ hangon start process -- python3 -i
$ hangon expect ">>>"
$ hangon sendline "2 + 2"
$ hangon expect "4"
$ hangon screenshot repl.png
$ hangon stop
```

## Install

### Homebrew (macOS/Linux)

```sh
$ brew install joewalnes/tap/hangon
```

### Download binary

Download the latest binary for your platform:

```sh
# macOS (Apple Silicon)
$ curl -Lo hangon https://github.com/joewalnes/hangon/releases/latest/download/hangon-darwin-arm64

# Linux (x86_64)
$ curl -Lo hangon https://github.com/joewalnes/hangon/releases/latest/download/hangon-linux-amd64

# Linux (ARM)
$ curl -Lo hangon https://github.com/joewalnes/hangon/releases/latest/download/hangon-linux-arm64
```

Then make it executable and move it to your PATH:

```sh
$ chmod +x hangon
$ sudo mv hangon /usr/local/bin/
```

### From source

Requires Go 1.21+:

```sh
$ go install github.com/joewalnes/hangon@latest
```

### Optional dependencies

| Dependency | Purpose | Install |
|---|---|---|
| [tmux](https://github.com/tmux/tmux) | Rich screen capture with ANSI colors for `screenshot` | `brew install tmux` / `apt install tmux` |
| [librsvg](https://wiki.gnome.org/Projects/LibRsvg) | SVG-to-PNG conversion for `screenshot` | `brew install librsvg` / `apt install librsvg2-bin` |

Without tmux, hangon falls back to a built-in PTY with basic screen capture.
Without librsvg, `screenshot` outputs SVG instead of PNG.

## Commands

### Session management

| Command | Description |
|---|---|
| `hangon start <type> [--name N] [-- args]` | Start a new session |
| `hangon list` | List all active sessions |
| `hangon status [SESSION]` | Show session details |
| `hangon stop [SESSION]` | Stop a session |
| `hangon stopall` | Stop all sessions |

### I/O

| Command | Description |
|---|---|
| `hangon send [SESSION] <data>` | Send raw data (no newline) |
| `hangon sendline [SESSION] <text>` | Send text + newline |
| `hangon read [SESSION]` | Read new output since last read |
| `hangon readall [SESSION]` | Read entire output buffer |
| `hangon stderr [SESSION]` | Read new stderr (`--no-pty` only) |
| `hangon expect [SESSION] <regex> [--timeout S]` | Wait for pattern in output |
| `hangon screen [SESSION]` | Terminal screen as text (process only) |
| `hangon keys [SESSION] <key...>` | Send special keys |
| `hangon alive [SESSION]` | Check if running (exit 0=yes, 1=no) |
| `hangon wait [SESSION]` | Block until process exits |
| `hangon screenshot [SESSION] [file]` | Visual screenshot as SVG/PNG |

### macOS desktop (darwin only)

| Command | Description |
|---|---|
| `hangon launch [--name N] <app>` | Launch app + create session |
| `hangon ax-tree [SESSION]` | Dump accessibility tree |
| `hangon ax-find [SESSION] --role R --name N` | Find accessibility node |
| `hangon click [SESSION] <element>` | Click UI element |
| `hangon type [SESSION] <text>` | Type into focused element |

### Key sequences (for `keys` command)

```
ctrl-a..ctrl-z    enter  tab  escape  backspace  delete  space
up  down  left  right  home  end  pageup  pagedown  insert
f1..f12
```

Multiple keys separated by spaces: `hangon keys "ctrl-c enter"`

## Session types

| Type | Target | Example |
|---|---|---|
| `process` | Local process via PTY (tmux when available) | `hangon start process -- python3 -i` |
| `tcp` | TCP socket | `hangon start tcp localhost:6379` |
| `ws` | WebSocket endpoint | `hangon start ws wss://echo.websocket.events` |
| `macos` | macOS desktop app via Accessibility APIs | `hangon start macos TextEdit` |

## Named sessions

Multiple sessions can run simultaneously. Default name is `"default"`.

```sh
$ hangon start process --name server -- python3 app.py
$ hangon start tcp --name db localhost:5432
$ hangon sendline server "start()"
$ hangon read db
$ hangon list
$ hangon stopall
```

## Screenshots

The `screenshot` command captures the terminal screen as a visual SVG or PNG
file with full support for:

- Foreground and background colors (16, 256, and 24-bit truecolor)
- Bold, italic, underline, strikethrough, dim, inverse text
- Unicode characters, CJK wide characters, emoji
- Cursor position indicator
- Nerd Font glyphs (via font stack in the SVG)

This requires tmux for the ANSI color capture. PNG output requires
`rsvg-convert` (from librsvg) or ImageMagick; otherwise falls back to SVG.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Check failed (expect timeout, alive=false) |
| 2 | Error (bad arguments, no session, connection failed) |

## Examples

### Interactive Python session

```sh
$ hangon start process -- python3 -i
$ hangon expect ">>>"
$ hangon sendline "import math; math.pi"
$ hangon expect "3.14"
$ hangon read
$ hangon stop
```

### Test a web server

```sh
$ hangon start process --name srv -- python3 -m http.server 8080
$ hangon expect srv "Serving HTTP"
$ curl http://localhost:8080
$ hangon stop srv
```

### Screenshot a TUI with colors

```sh
$ hangon start process -- htop
$ hangon screenshot htop.png    # full-color SVG/PNG
$ hangon keys "q"
$ hangon stop
```

### TCP (e.g. Redis)

```sh
$ hangon start tcp --name redis localhost:6379
$ hangon sendline redis "SET hello world"
$ hangon expect redis "OK"
$ hangon sendline redis "GET hello"
$ hangon expect redis "world"
$ hangon stop redis
```

### WebSocket

```sh
$ hangon start ws wss://echo.websocket.events
$ hangon send "hello"
$ hangon expect "hello"
$ hangon stop
```

### macOS desktop app (darwin only)

```sh
$ hangon launch --name editor TextEdit
$ hangon type editor "Hello from hangon"
$ hangon screenshot editor textedit.png
$ hangon ax-tree editor
$ hangon stop editor
```

## macOS accessibility (ax) commands

On macOS, hangon can drive desktop GUI apps through the
[Accessibility API](https://developer.apple.com/documentation/accessibility).
This lets scripts and agents interact with native apps the same way a user
would -- clicking buttons, reading UI state, and typing text.

**Prerequisite:** The terminal (or process) running hangon must have
**Accessibility** permission. Grant it in System Settings → Privacy & Security
→ Accessibility. Screenshot capture additionally requires **Screen Recording**
permission.

### Workflow

1. **Launch the app** and give it a session name:

```sh
$ hangon launch --name calc Calculator
```

2. **Inspect the UI** with `ax-tree` to see every element in the front window:

```sh
$ hangon ax-tree calc
```

Output looks like:

```
Window: Calculator
AXGroup:
AXButton: clear [AC]
AXButton: percentage [%]
AXButton: divide [÷]
AXButton: seven [7]
AXButton: eight [8]
...
AXStaticText: main display [0]
```

Each line shows `Role: description [value]`. Roles follow Apple's
`AX` naming convention (AXButton, AXTextField, AXStaticText, etc.).

3. **Find specific elements** with `ax-find` to narrow the tree:

```sh
# Find all buttons:
$ hangon ax-find calc --role AXButton

# Find elements with "save" in their description:
$ hangon ax-find calc --name save

# Combine both (AND logic):
$ hangon ax-find calc --role AXButton --name clear
```

4. **Click elements** by their description:

```sh
$ hangon click calc "seven"
$ hangon click calc "plus"
$ hangon click calc "three"
$ hangon click calc "equals"
```

5. **Type text** into the focused element:

```sh
$ hangon launch --name notes Notes
$ hangon type notes "Meeting notes for today"
$ hangon keys notes "enter"
$ hangon type notes "- Action item one"
```

6. **Take a screenshot** of the app window:

```sh
$ hangon screenshot calc result.png
```

7. **Stop** when done (quits the app):

```sh
$ hangon stop calc
```

### Full example: automate Calculator

```sh
$ hangon launch --name calc Calculator
$ sleep 1

# Inspect to discover element names.
$ hangon ax-tree calc

# Compute 7 + 3.
$ hangon click calc "seven"
$ hangon click calc "plus"
$ hangon click calc "three"
$ hangon click calc "equals"

# Screenshot the result.
$ hangon screenshot calc answer.png

$ hangon stop calc
```

### Full example: type into TextEdit and verify

```sh
$ hangon launch --name doc TextEdit
$ sleep 1

$ hangon type doc "Hello from hangon!"
$ hangon screenshot doc hello.png

# Inspect the UI to verify text was entered.
$ hangon ax-tree doc

$ hangon stop doc
```

### Tips

- `ax-tree` output can be large for complex apps. Pipe through `grep` to
  find what you need: `hangon ax-tree calc | grep -i button`
- Element descriptions are app-specific. Always run `ax-tree` first to
  discover the correct names before scripting `click` or `ax-find`.
- `click` matches against the accessibility **description** field. If
  multiple elements share a description, the first match is clicked.
- `type` sends keystrokes to whatever element currently has focus. Use
  `click` first to focus the right field.
- `keys` works for macOS sessions too -- use it for keyboard shortcuts
  like `hangon keys doc "cmd-s"` to save, or `hangon keys doc "cmd-a"`
  to select all.

## How it works

```
                    ┌──────────────────────────┐
  CLI commands      │    Session Holder        │
  (short-lived)     │    (background process)  │
                    │                          │
  hangon sendline ──┤► Unix socket ──► Backend │──► Target process/socket/app
  hangon read    ◄──┤◄ JSON resp   ◄── Ring    │◄── stdout/received data
  hangon expect  ◄──┤  (cursored     Buffer    │
  hangon screen  ◄──┤   reads)                 │
  hangon screenshot ┤              ──► Render  │──► SVG/PNG file
                    └──────────────────────────┘
```

`hangon start` spawns a **session holder** as a detached background process.
The holder manages the connection to the target (process, TCP socket, WebSocket,
or macOS app) and serves commands over a Unix domain socket.

Each CLI invocation (`sendline`, `read`, `expect`, etc.) connects to the holder,
sends a JSON request, receives a response, and exits. This stateless-client
design means any shell, script, or agent can interact with long-running sessions
without managing connection state.

Output is buffered in a 1MB ring buffer with **cursored reads**: each `read`
call returns only new data since the previous read, so you never see the same
output twice. `expect` blocks until a regex matches, making it easy to
synchronize with application output.

When tmux is available, the process backend uses it for terminal emulation,
giving `screen` and `screenshot` access to the full terminal state including
ANSI colors, Unicode, wide characters, and cursor position.

## Acknowledgments

hangon is directly inspired by Simon Willison's
**[Rodney](https://github.com/simonw/rodney)**, a CLI tool that drives a
persistent headless Chrome instance for browser automation. Rodney's core
architecture -- a long-lived holder process that CLI commands connect to via
short-lived requests over a socket -- is the foundation of hangon's design.
hangon generalizes this pattern from browser automation to processes, sockets,
and desktop apps. The self-describing `--help` as the primary API documentation,
the exit code conventions, and the session state file approach all follow
Rodney's lead. Thank you Simon.

### Dependencies

- **[creack/pty](https://github.com/creack/pty)** -- PTY handling for Go
  (fallback when tmux is not available)
- **[nhooyr.io/websocket](https://github.com/nhooyr/websocket)** -- WebSocket
  client for the `ws` session type
- **[tmux](https://github.com/tmux/tmux)** -- terminal multiplexer, used as
  the PTY host for rich screen capture with ANSI color support
- **[librsvg](https://wiki.gnome.org/Projects/LibRsvg)** -- optional
  SVG-to-PNG rasterizer for the `screenshot` command

The ANSI-to-SVG rendering pipeline was informed by
**[Charmbracelet's freeze](https://github.com/charmbracelet/freeze)**, which
uses a similar ANSI parser and SVG generation approach.

## Author

Joe Walnes ([@joewalnes](https://github.com/joewalnes)) &lt;joe@walnes.com&gt;

## License

Apache 2.0. See [LICENSE](LICENSE).
