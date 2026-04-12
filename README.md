# hangon

Persistent session manager for CLI-driven app interaction.

> **For AI agents:** hangon lets you start, interact with, and test long-running
> processes (servers, REPLs, TUIs), TCP sockets, WebSockets, and macOS desktop
> apps - all through simple shell commands. Install with
> `curl -Lo hangon https://github.com/joewalnes/hangon/releases/latest/download/hangon-linux-amd64 && chmod +x hangon`
> (or see Install below for other platforms) and run `hangon --help` for the
> full command reference. Each command is stateless: start a session, send
> input, read output, assert with `expect`, take screenshots, and stop - no
> persistent connection needed.

hangon lets you start a long-running process, TCP socket, WebSocket, or macOS
desktop app in the background and interact with it through short-lived shell
commands. Each command connects to the session, performs one action, and exits.

This makes it ideal for **shell scripts** and **AI coding agents** that need to
drive interactive applications without maintaining a persistent connection.

**What it works with:**

- **Interactive processes** - REPLs, servers, TUIs, anything with a terminal (Python, Node, Redis CLI, htop, vim, ...)
- **TCP sockets** - raw TCP connections (Redis, PostgreSQL wire protocol, SMTP, ...)
- **WebSockets** - persistent WebSocket connections
- **macOS desktop apps** - native GUI apps via Accessibility APIs (Calculator, TextEdit, any app)

**For web apps and browsers**, see Simon Willison's
[Rodney](https://github.com/simonw/rodney) which takes a similar approach for
headless Chrome automation. Hangon was inspired by Rodney.

## Quick examples

### Python REPL

```sh
$ hangon start process -- python3 -i    # start a Python REPL
$ hangon expect ">>>"                   # wait for the prompt
$ hangon sendline "2 + 2"               # type an expression
$ hangon expect "4"                     # verify the result
$ hangon stop                           # done
```

### Test a web server

```sh
$ hangon start process -- python3 -m http.server 8080
$ hangon expect "Serving HTTP"          # wait until server is ready
$ curl http://localhost:8080            # make a request
$ hangon stop
```

### Redis over TCP

```sh
$ hangon start tcp localhost:6379
$ hangon sendline "SET hello world"
$ hangon expect "OK"
$ hangon sendline "GET hello"
$ hangon expect "world"
$ hangon stop
```

### Screenshot a TUI

```sh
$ hangon start process -- htop
$ hangon screenshot htop.png            # full-color SVG/PNG
$ hangon keys "q"
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

## Tutorials

### Interactive processes

Start any command-line program and interact with it through send/expect cycles.
This works with REPLs, servers, TUIs - anything that runs in a terminal.

#### Node.js REPL - test a function

```sh
# Start Node and wait for it to be ready
$ hangon start process -- node -i
$ hangon expect ">"

# Define a function
$ hangon sendline "function fib(n) { return n <= 1 ? n : fib(n-1) + fib(n-2); }"
$ hangon expect ">"

# Test it
$ hangon sendline "fib(10)"
$ hangon expect "55"

# Clean up
$ hangon stop
```

#### Flask dev server - start, verify, and test

```sh
# Start a Flask app in the background
$ hangon start process -- python3 -m flask run --port 5000
$ hangon expect "Running on"             # wait for startup

# Test endpoints with curl
$ curl -s http://localhost:5000/api/health
$ curl -s http://localhost:5000/api/users

# Check server logs for errors
$ hangon read

# Stop the server
$ hangon stop
```

#### vim - drive a TUI with keystrokes

```sh
# Open vim with a new file
$ hangon start process -- vim test.txt

# Type some text (vim starts in normal mode)
$ hangon keys "i"                        # enter insert mode
$ hangon send "Hello from hangon"
$ hangon keys "escape"                   # back to normal mode

# Save and take a screenshot
$ hangon keys ": w enter"
$ hangon screenshot vim-session.png

# Quit
$ hangon keys ": q enter"
$ hangon stop
```

### TCP sockets

Connect to any TCP service and send/receive raw data. Useful for testing
database servers, caches, mail servers, and custom TCP protocols.

#### Redis - set and retrieve values

```sh
# Connect to Redis (must already be running)
$ hangon start tcp localhost:6379

# Redis speaks a simple text protocol
$ hangon sendline "PING"
$ hangon expect "PONG"

$ hangon sendline "SET user:1 alice"
$ hangon expect "OK"

$ hangon sendline "GET user:1"
$ hangon expect "alice"

# Check all keys
$ hangon sendline "KEYS *"
$ hangon read

$ hangon stop
```

#### SMTP - test an email server

```sh
# Connect to a local SMTP server
$ hangon start tcp localhost:25
$ hangon expect "220"                    # server greeting

# SMTP handshake
$ hangon sendline "EHLO localhost"
$ hangon expect "250"

$ hangon sendline "MAIL FROM:<test@example.com>"
$ hangon expect "250"

$ hangon sendline "RCPT TO:<user@example.com>"
$ hangon expect "250"

# Send message body
$ hangon sendline "DATA"
$ hangon expect "354"
$ hangon sendline "Subject: Test"
$ hangon sendline ""
$ hangon sendline "Hello from hangon"
$ hangon sendline "."
$ hangon expect "250"                    # message accepted

$ hangon sendline "QUIT"
$ hangon stop
```

### WebSockets

Connect to a WebSocket endpoint and exchange messages. Useful for testing
real-time APIs, chat servers, and streaming services.

#### Echo server - basic round-trip

```sh
# Connect to a public WebSocket echo service
$ hangon start ws wss://echo.websocket.events
$ hangon expect "connected"              # wait for connection confirmation

# Send a message and verify it echoes back
$ hangon send "hello world"
$ hangon expect "hello world"

# Send JSON
$ hangon send '{"action":"ping","ts":1234}'
$ hangon expect "ping"

$ hangon stop
```

#### Test your own WebSocket server

```sh
# Two sessions running at once - use --name to distinguish them
$ hangon start process --name srv -- node server.js
$ hangon expect srv "listening on 3000"

$ hangon start ws --name ws ws://localhost:3000/ws

$ hangon send ws '{"type":"subscribe","channel":"updates"}'
$ hangon expect ws "subscribed"

# Read what the server logged
$ hangon read srv

$ hangon stopall
```

### macOS desktop apps

Drive native macOS GUI apps through the Accessibility API. Launch apps, inspect
UI elements, click buttons, type text, and take screenshots.

**Prerequisite:** Grant **Accessibility** permission to your terminal in
System Settings → Privacy & Security → Accessibility. For screenshots, also
grant **Screen Recording** permission.

#### Calculator - compute 7 + 3

```sh
$ hangon launch Calculator
$ sleep 1                                # wait for the app to open

# Discover button names
$ hangon ax-tree | grep AXButton

# Click buttons to compute 7 + 3
$ hangon click "seven"
$ hangon click "plus"
$ hangon click "three"
$ hangon click "equals"

# Screenshot the result
$ hangon screenshot answer.png

$ hangon stop                            # quits the app
```

#### TextEdit - type and inspect

```sh
$ hangon launch TextEdit
$ sleep 1

# Type some text
$ hangon type "Meeting notes for today"
$ hangon keys "enter"
$ hangon type "- Action item one"
$ hangon keys "enter"
$ hangon type "- Action item two"

# Take a screenshot
$ hangon screenshot notes.png

# Inspect the UI to verify text was entered
$ hangon ax-tree

# Save with Cmd-S
$ hangon keys "cmd-s"

$ hangon stop
```

#### Tips for macOS automation

- Run `ax-tree` first to discover element names - they're app-specific.
- `click` matches the accessibility **description** field. If multiple elements
  share a name, the first match is clicked.
- `type` sends keystrokes to whatever has focus. Use `click` first to focus the
  right field.
- `keys` supports macOS shortcuts: `hangon keys "cmd-a"` (select all),
  `hangon keys "cmd-s"` (save), `hangon keys "cmd-z"` (undo).
- Pipe `ax-tree` through `grep` for large apps:
  `hangon ax-tree | grep -i button`

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
