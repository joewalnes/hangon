# hangon

Persistent session manager for CLI-driven app interaction.

> **For AI agents:** hangon lets you start, interact with, and test long-running
> processes (servers, REPLs, TUIs), TCP sockets, WebSockets, and macOS desktop
> apps - all through simple shell commands. Install with
> `go install github.com/joewalnes/hangon@latest`
> (see Install below for other options) and run `hangon --help` for the
> full command reference. Each command is stateless: start a session, send
> input, read output, assert with `expect`, take screenshots, and stop - no
> persistent connection needed. State is shared machine-wide by default
> (`~/.hangon`), so if other hangon sessions might be running concurrently
> (other agents, other terminals), use a unique `--name`. Before ever running
> `hangon stopall`, run it *without* `--force` first — it only previews what
> it would stop (name, PID, session type) and touches nothing; only add
> `--force` once you've confirmed the preview lists sessions you actually own,
> so an accidental invocation can't kill sessions belonging to someone else.

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
$ hangon send "q"                       # "keys" is for named keys only (ctrl-c, enter, ...); "q" is a literal character
$ hangon stop
```

## Install

### From source (currently the only working install path — see note below)

Requires Go 1.25+ (matches the `go` directive in `go.mod`):

```sh
$ go install github.com/joewalnes/hangon@latest
```

Building from a local checkout: use `make install` (which runs `go install`),
not `go build` followed by a manual `cp` into place. `go install` (like `go
build -o`) writes the new binary to a temp file and atomically renames it into
place, so any already-running `hangon` process keeps executing the old,
untouched file. A plain `cp` onto an existing path (including through a
symlink, e.g. `cp hangon ~/.local/bin/hangon` where that's a symlink to
`~/go/bin/hangon`) instead overwrites the target file *in place*. If any other
`hangon` process still has that exact file mapped and executing — easy to hit
if you're iterating on a fix while other `hangon` sessions are active — macOS
can SIGKILL it (exit 137) the moment it pages in a now-mutated, signature-
mismatched code page, sometimes for only *some* subcommands and not others,
which looks exactly like a broken build even though the binary itself is
fine. Always reinstall with `make install` / `go install`.

### Homebrew / pre-built binaries (not currently working)

A Homebrew tap repo (`joewalnes/homebrew-tap`, with a `Formula/hangon.rb`)
exists, and it points at binary downloads of the form
`https://github.com/joewalnes/hangon/releases/latest/download/hangon-<platform>`.
Neither currently works: as of this writing, the
[hangon releases page](https://github.com/joewalnes/hangon/releases) has
zero published releases — the `Release` GitHub Actions workflow that's
supposed to build and publish a `latest` release on every push to `main`
has failed on every one of its last several runs. That means both of the
following 404:

```sh
$ brew install joewalnes/tap/hangon                                                  # fails: no release published
$ curl -Lo hangon https://github.com/joewalnes/hangon/releases/latest/download/hangon-darwin-arm64  # fails: no release published
```

Use "From source" above until the release pipeline is fixed (tracked in
`TODO.md`).

### Optional dependencies

| Dependency | Purpose | Install |
|---|---|---|
| [tmux](https://github.com/tmux/tmux) | Rich screen capture with ANSI colors for `screenshot` | `brew install tmux` / `apt install tmux` |
| [librsvg](https://wiki.gnome.org/Projects/LibRsvg) | SVG-to-PNG conversion for `screenshot` | `brew install librsvg` / `apt install librsvg2-bin` |

Without tmux, hangon falls back to a built-in PTY with basic screen capture.
Without librsvg, `screenshot` outputs SVG instead of PNG.

hangon runs its tmux sessions on a dedicated server socket (`tmux -L hangon`),
so they never appear in your regular `tmux ls` and hangon's cleanup
(`stop`, `stopall`, `gc`) can never touch your personal tmux sessions.
To inspect hangon's sessions directly: `tmux -L hangon ls` (override the
socket name with `HANGON_TMUX_SOCKET`).

**If you're driving hangon sessions from outside hangon itself** (a script
that shells out to `tmux` directly, e.g. `tmux resize-window -t
"hangon-$PID"`), it must target the `hangon` socket the same way: `tmux -L
hangon <command> ...` (or `-L "$HANGON_TMUX_SOCKET"` if you've overridden
it). Plain `tmux <command> -t "hangon-$PID"` runs against your *default*
tmux server, which has never heard of hangon's sessions — it will silently
report success (or "session not found") without doing anything, since
that's a different server entirely. Prefer `hangon resize` (see below)
over reaching for `tmux` directly where possible.

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

# Type some text (vim starts in normal mode). "keys" only understands named
# key sequences (ctrl-c, enter, escape, ...) - literal characters like "i",
# ":", and "w" go through "send" instead.
$ hangon send "i"                        # enter insert mode
$ hangon send "Hello from hangon"
$ hangon keys "escape"                   # back to normal mode ("escape" IS a named key)

# Save and take a screenshot
$ hangon send ":w"
$ hangon keys "enter"
$ hangon screenshot vim-session.png

# Quit
$ hangon send ":q"
$ hangon keys "enter"
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

$ hangon stopall --force
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
| `hangon start <type> [--name N] [--cols N] [--rows N] [-- args]` | Start a new session |
| `hangon list` (alias: `ls`) | List all active sessions |
| `hangon status [SESSION]` | Show session details |
| `hangon stop [SESSION]` | Stop a session |
| `hangon stopall --force` | Stop all sessions (previews without `--force`) |
| `hangon gc [--dry-run]` | Reap orphaned state entries, tmux sessions, and holder processes |

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
| `hangon resize [SESSION] --cols N --rows N` | Resize the session's terminal (process only) |
| `hangon mouse-click [SESSION] --x N --y N [--button B] [--count N]` | Click at a terminal cell (1-based coords) |
| `hangon mouse-drag [SESSION] --from X,Y --to X,Y [--steps N]` | Drag between two terminal cells |
| `hangon mouse-scroll [SESSION] --x N --y N --delta N` | Scroll wheel at a terminal cell (negative=up) |
| `hangon alive [SESSION]` | Check if running (exit 0=yes, 1=no) |
| `hangon wait [SESSION]` | Block until process exits |
| `hangon screenshot [SESSION] [file]` | Visual screenshot as SVG/PNG |

Mouse commands send SGR mouse-mode escape sequences (xterm protocol 1006) to
the session; they work with process sessions and any target that has
enabled mouse reporting. `--shift`/`--alt`/`--ctrl` add modifiers to any of
the three:

```sh
$ hangon start process -- python3 -i
$ hangon mouse-click --x 10 --y 5              # single left click at column 10, row 5
$ hangon mouse-click --x 10 --y 5 --button right --count 2  # double right-click
$ hangon mouse-drag --from 1,5 --to 20,5 --steps 10         # drag with 10 intermediate move events
$ hangon mouse-scroll --x 10 --y 5 --delta -3  # scroll up 3 notches
$ hangon stop
```

### macOS desktop (darwin only)

| Command | Description |
|---|---|
| `hangon launch [--name N] <app>` | Launch app + create session |
| `hangon ax-tree [SESSION]` | Dump accessibility tree |
| `hangon ax-find [SESSION] --role R --name N` | Find accessibility node |
| `hangon click [SESSION] <element>` | Click UI element |
| `hangon type [SESSION] <text>` | Type into focused element |

### Key sequences (for `keys` command)

`keys` only understands named key sequences below - not literal characters
(use `send`/`sendline` for those, e.g. `hangon send "q"` to send a literal
"q"). Run `hangon keys --help` for the authoritative, always-current list
(a unit test keeps it in sync with what the process backend actually
supports):

```
enter  return  tab  escape  esc  backspace  delete  space
up  down  left  right  home  end  pageup  pagedown  insert
ctrl-a..ctrl-z    ctrl-space    ctrl-up/down/left/right
shift-up/down/left/right    shift-home  shift-end
alt-a..alt-z    alt-.  alt-,  alt-=  alt--    alt-up/down/left/right
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
$ hangon stopall --force
```

## Stopping everything, and cleaning up after crashes

`hangon stopall` stops every session in the current state directory
(`~/.hangon` by default, or `./.hangon` with `--local`). Because the default
state directory is shared machine-wide, running it without scoping affects
every hangon session on the machine — including ones started by other
processes or agents you don't know about. To make that hard to trigger by
accident, `stopall` requires `--force`; without it, it just prints what it
*would* stop:

```sh
$ hangon stopall
stopall would stop 2 session(s) in /Users/you/.hangon:

  server          type=process holder PID=1234    alive=true
  db              type=tcp     holder PID=1235    alive=true

Refusing to stop sessions without --force: this affects every session in this
state directory, including ones started by other processes or agents sharing
it. Re-run as 'hangon stopall --force' to proceed.

$ hangon stopall --force
Stopped "server"
Stopped "db"
```

Separately, `hangon gc` reconciles state.json against reality rather than
stopping anything you're actively using. A session's state entry, its holder
process, and (for `process` sessions) its tmux session are all supposed to
move together, but an ungraceful death — a crash, an OOM kill, a `kill -9` —
can leave any of them behind without the others: a stale state.json entry
pointing at a dead process, an orphaned tmux session nothing will ever stop,
or a holder process with no tracked session at all. `gc` finds and cleans up
all three:

```sh
$ hangon gc
hangon gc: scanning /Users/you/.hangon
  removed stale state entry "crashed-session" (holder process not running)
  killed orphaned tmux session "hangon-48213" (no tracked session for holder PID 48213)
  stopped orphaned holder process PID 48311 (/usr/local/bin/hangon _serve --name ...)

hangon gc summary: 1 stale state entry, 1 orphaned tmux session, 1 orphaned holder process
```

Use `hangon gc --dry-run` to preview what it would do without making any
changes. It's safe to run at any time, including alongside other hangon
commands — it only ever acts on state/processes/sessions it can positively
confirm are unreferenced by anything live *and* belong to the same state
directory it's scanning. Machine-wide resources like tmux sessions and
"hangon _serve" processes can belong to other, independent state
directories (another `--local` checkout, another hangon install, another
agent's isolated state dir) — `gc` checks each candidate's own
`--state-dir` before touching it, so a session or holder just being absent
from *this* state dir's tracked set is never enough on its own to mark it
orphaned.

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

Background colors and cell boundaries are captured and rendered pixel-for-pixel
against the live tmux pane state, including colored padding at the end of a
line and adjacent same-colored cells (no seams between them). Box-drawing-style
full-cell characters (currently the ◢ ◣ ◤ ◥ quadrant triangles) are drawn as
vector shapes sized to the actual cell rather than relying on a font glyph, so
they render solid and full-size the way a real terminal emulator draws them,
regardless of what fonts are installed on the machine running hangon.

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
or macOS app) and serves commands over a Unix domain socket. Anyone who can
connect to that socket can read the session's output and inject keystrokes, so
sockets live under a per-user, owner-only directory (`<TMPDIR>/hangon-<uid>/`,
overridable with `HANGON_RUN_DIR`) rather than being scattered directly in the
shared, world-readable `/tmp` — that directory is created (and re-enforced on
every start) `0700`, and each socket file is additionally `chmod 0600` right
after it starts listening, so only the owning user's account can reach it
regardless of the process's ambient umask.

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

Apache 2.0. See [LICENSE](LICENSE). Third-party dependency licenses
(MIT, ISC) ship in [THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES).
