package main

import (
	"fmt"
	"os"
	"runtime"
)

// --- Help / Usage ---

// subcommandHelp maps command names to their help text, used for `hangon <cmd> --help`.
var subcommandHelp = map[string]string{
	"start": `hangon start <type> [--name NAME] [--local] [--no-pty] [--cols N] [--rows N] [-- command...]

Start a new persistent session.

Types:
  process   Spawn a process with a PTY (default) or raw pipes (--no-pty).
  tcp       Connect to a TCP socket.
  ws        Connect to a WebSocket endpoint.
  macos     Connect to a macOS desktop app (darwin only).

Examples:
  hangon start process -- python3 -i
  hangon start process --name server -- node app.js
  hangon start process --no-pty -- ./my-daemon
  hangon start process --cols 120 --rows 40 -- htop
  hangon start tcp localhost:6379
  hangon start ws wss://echo.websocket.events
  hangon start macos TextEdit

Options:
  --name NAME   Name this session (default: "default")
  --no-pty      Process only: use raw pipes instead of PTY
  --local       Store state in ./.hangon/ instead of ~/.hangon/
  --cols N      Process only: initial terminal width (default: 80)
  --rows N      Process only: initial terminal height (default: 24)

To change the size of an already-running session, use 'hangon resize'.
`,
	"stop": `hangon stop [SESSION]

Stop a session and kill its holder process. Default session: "default".

Examples:
  hangon stop
  hangon stop server
`,
	"list": `hangon list

List all active sessions with their type, PID, status, and target.
Alias: ls
`,
	"stopall": `hangon stopall [--force]

Stop every session tracked in the resolved state directory (~/.hangon by
default, or ./.hangon with --local).

Without --force, prints what would be stopped (name, type, holder PID,
alive status) and exits without touching anything. Because the default
state directory is shared machine-wide, stopall without scoping affects
every hangon session on the machine, including ones started by other
processes or agents you may not know about — --force is required so
that running it by mistake (muscle memory, a generic cleanup script)
cannot silently kill someone else's sessions.

Examples:
  hangon stopall              # preview only, stops nothing
  hangon stopall --force      # actually stop everything
  hangon stopall --local --force   # scope to ./.hangon only
`,
	"gc": `hangon gc [--dry-run] [--local|--global]

Clean up state/process/tmux-session drift in the resolved state
directory: entries never survive process crashes, force-kills, or
partial failures cleanly on their own, so gc reconciles all three
sources of truth (state.json, tmux, and running "hangon _serve"
processes) and fixes up whichever is stale:

  - state.json entries whose holder process is no longer running
    (crashed, OOM-killed, kill -9'd) are removed, and any tmux session
    or socket file they still reference is cleaned up too.
  - tmux sessions matching "hangon-<pid>" that aren't backed by any
    tracked, live holder are killed.
  - "hangon _serve" processes that aren't backed by any tracked
    session (e.g. orphaned by a crash before registration, or by a
    state write that didn't survive) are stopped.

gc only ever acts on a tmux session or "hangon _serve" process it can
positively confirm belongs to THIS run's state directory (matched
against the process's own --state-dir argument): a live holder or
session belonging to a different state directory (another --local
checkout, another hangon install, another agent's isolated state dir)
is left alone even though it isn't in this state dir's tracked set —
being untracked *here* is not the same as being orphaned.

--dry-run reports what gc would do without making any changes.

Examples:
  hangon gc
  hangon gc --dry-run
  hangon gc --local
`,
	"status": `hangon status [SESSION]

Show detailed information about a session.

Examples:
  hangon status
  hangon status server
`,
	"send": `hangon send [SESSION] <data>
hangon send [SESSION] --stdin < file

Send raw data to the session's target. Does NOT append a newline.
Use 'sendline' to send data with a trailing newline.

With --stdin, reads raw bytes from stdin instead of command-line arguments.
This is useful for sending binary data or bytes that cannot appear in argv
(e.g. NUL bytes).

Examples:
  hangon send "hello"
  hangon send server '{"action":"ping"}'
  printf '\x1b[1;2C' | hangon send --stdin
  printf '\x00' | hangon send server --stdin
`,
	"sendline": `hangon sendline [SESSION] <text>

Send text followed by a newline character to the session's target.
This is the most common way to send commands to interactive processes.

Examples:
  hangon sendline "print('hello')"
  hangon sendline server "GET / HTTP/1.0\r"
`,
	"read": `hangon read [SESSION]

Read new output that has appeared since the last 'read' call.
Returns empty string if no new output. This is a non-blocking operation.

Each session maintains a read cursor. Successive 'read' calls return
only the data produced between calls, never repeating output.

Examples:
  hangon read
  hangon read server
`,
	"readall": `hangon readall [SESSION]

Read the entire output buffer (up to 1MB ring buffer).
Unlike 'read', this always returns all buffered output regardless
of previous reads.

Examples:
  hangon readall
  hangon readall server
`,
	"expect": `hangon expect [SESSION] <pattern> [--timeout SEC]

Wait for a regex pattern to appear in output. Blocks until the pattern
matches or the timeout expires.

Exit code 0 on match (prints the output containing the match).
Exit code 1 on timeout.

The regex uses Go's regexp syntax (similar to PCRE without backreferences).

Without --timeout, the default is 30 seconds, or the value of the
HANGON_TIMEOUT environment variable (Go duration syntax, e.g. "10s",
"1m") if set.

Examples:
  hangon expect "ready"
  hangon expect "listening on port \d+"
  hangon expect server "200 OK" --timeout 60
  hangon expect ">>> " --timeout 5
  HANGON_TIMEOUT=10s hangon expect "ready"   # change the default
`,
	"screen": `hangon screen [SESSION]

Get the current terminal screen content (process sessions with PTY only).
Returns the visible terminal grid as plain text, with trailing whitespace
trimmed. This is essential for reading TUI applications. The grid size is
80x24 by default; see 'hangon start --help' and 'hangon resize --help' to
change it.

Examples:
  hangon screen
  hangon screen myapp
`,
	"resize": `hangon resize [SESSION] --cols N --rows N

Change the terminal size of an already-running process session. Works for
tmux-backed sessions (the default) and the legacy raw-PTY fallback used
when tmux isn't installed; other session types (tcp, ws, macos) have no
terminal grid to resize and return an error.

The target process is notified the same way a real terminal emulator
notifies it on resize (SIGWINCH via the PTY's TIOCSWINSZ ioctl, or tmux's
equivalent internal handling), so well-behaved programs (shells, editors,
REPLs that call shutil.get_terminal_size()/ioctl(TIOCGWINSZ)) pick up the
new size immediately. 'hangon screen' and 'hangon screenshot' reflect the
new geometry on the next call.

Both --cols and --rows are required, and must be between 1 and 2000.

To set the *initial* size when creating a session, use 'hangon start
--cols N --rows N' instead (default 80x24).

Examples:
  hangon resize --cols 120 --rows 40
  hangon resize myapp --cols 200 --rows 50
`,
	"keys": `hangon keys [SESSION] <key-sequence>

Send special key sequences to the session. Multiple keys separated by spaces.

Available keys:
  ctrl-a through ctrl-z     Control key combinations
  ctrl-space                Control+Space (NUL)
  ctrl-up/down/left/right   Control+Arrow keys
  enter, return             Enter/Return key
  tab                       Tab key
  escape, esc               Escape key
  backspace, delete         Backspace/Delete keys
  up, down, left, right     Arrow keys
  shift-up/down/left/right  Shift+Arrow keys
  shift-home, shift-end     Shift+Home/End
  alt-a through alt-z       Alt+letter combinations
  alt-. alt-, alt-= alt--   Alt+punctuation
  alt-up/down/left/right    Alt+Arrow keys
  home, end                 Home/End keys
  pageup, pagedown          Page Up/Down
  insert                    Insert key
  space                     Space bar
  f1 through f12            Function keys

Examples:
  hangon keys ctrl-c
  hangon keys "ctrl-c enter"
  hangon keys myapp "up up enter"
  hangon keys "shift-right shift-right ctrl-c"
  hangon keys alt-f
`,
	"mouse-click": `hangon mouse-click [SESSION] --x COL --y ROW [options]

Send a mouse click at terminal cell (COL, ROW). Coordinates are 1-based.

Options:
  --x COL              Column (required)
  --y ROW              Row (required)
  --button BUTTON      left (default), right, or middle
  --count N            Click count: 1=single, 2=double, 3=triple (default 1)
  --shift              Hold shift modifier
  --alt                Hold alt modifier
  --ctrl               Hold ctrl modifier

Examples:
  hangon mouse-click --x 10 --y 5
  hangon mouse-click myapp --x 1 --y 1 --button right
  hangon mouse-click --x 10 --y 5 --count 2
  hangon mouse-click --x 10 --y 5 --shift
`,
	"mouse-drag": `hangon mouse-drag [SESSION] --from COL,ROW --to COL,ROW [options]

Send a mouse drag (left button) between two terminal positions. Coordinates are 1-based.

Options:
  --from COL,ROW       Start position (required)
  --to COL,ROW         End position (required)
  --steps N            Intermediate move events (default 1 = direct)
  --shift              Hold shift modifier
  --alt                Hold alt modifier
  --ctrl               Hold ctrl modifier

Examples:
  hangon mouse-drag --from 1,5 --to 20,5
  hangon mouse-drag myapp --from 1,1 --to 40,1 --steps 10
`,
	"mouse-scroll": `hangon mouse-scroll [SESSION] --x COL --y ROW --delta N [options]

Send mouse scroll events at terminal cell (COL, ROW). Coordinates are 1-based.

Options:
  --x COL              Column (required)
  --y ROW              Row (required)
  --delta N            Scroll amount: negative=up, positive=down (required)
  --shift              Hold shift modifier
  --alt                Hold alt modifier
  --ctrl               Hold ctrl modifier

Examples:
  hangon mouse-scroll --x 10 --y 5 --delta -3
  hangon mouse-scroll myapp --x 10 --y 5 --delta 5
`,
	"alive": `hangon alive [SESSION]

Check if the session's target is still running.
Exit code 0 if alive, exit code 1 if not. Prints "true" or "false".

Examples:
  hangon alive
  hangon alive server
  hangon alive && echo "still running"
`,
	"wait": `hangon wait [SESSION]

Block until the session's process exits. Returns the process exit code.
For tcp/ws sessions, waits until the connection closes.

Examples:
  hangon wait
  hangon wait server
`,
	"stderr": `hangon stderr [SESSION]

Read new stderr output (process sessions with --no-pty only).
In PTY mode, stderr is merged with stdout.

Examples:
  hangon stderr
  hangon stderr server
`,
	"launch": `hangon launch [--name NAME] <app>

macOS only. Launch a desktop app and create a session for it.
Shorthand for: hangon start macos <app>

Examples:
  hangon launch TextEdit
  hangon launch --name editor TextEdit
`,
	"ax-tree": `hangon ax-tree [SESSION]

macOS only. Dump the accessibility tree of the app's front window.
Returns roles and descriptions of all UI elements.

Examples:
  hangon ax-tree
  hangon ax-tree editor
`,
	"ax-find": `hangon ax-find [SESSION] --role ROLE --name NAME

macOS only. Find accessibility nodes matching the given role and/or name.

Examples:
  hangon ax-find --role AXButton --name "Save"
  hangon ax-find editor --role AXTextField
`,
	"click": `hangon click [SESSION] <element-description>

macOS only. Click a UI element whose accessibility description matches.

Examples:
  hangon click "Save"
  hangon click editor "Open"
`,
	"type": `hangon type [SESSION] <text>

macOS only. Type text into the currently focused element of the app.

Examples:
  hangon type "Hello, world!"
  hangon type editor "Some text to insert"
`,
	"screenshot": `hangon screenshot [SESSION] [filename]

Capture a visual screenshot of the session's screen as SVG or PNG.
Works with process sessions (via tmux) and macOS app sessions.

For process sessions, captures the terminal with full ANSI color support
(fg/bg colors, bold, italic, underline, strikethrough), Unicode, emoji,
wide characters, and cursor position. Renders to SVG with Nerd Font
support in the font stack.

PNG output requires rsvg-convert (brew install librsvg) or ImageMagick.
Falls back to SVG if no PNG renderer is available.

Default filename: screenshot.png (or .svg if PNG unavailable)

Examples:
  hangon screenshot
  hangon screenshot myapp.png
  hangon screenshot server /tmp/server-state.png
`,
}

func init() {
	// "ls" is a valid alias for "list" (see main()'s command switch), but
	// subcommandHelp only had a "list" entry, so `hangon ls --help` fell
	// through to "No help available for \"ls\"" and exited 2 even though
	// `hangon ls` itself works fine. Give it the same help text.
	subcommandHelp["ls"] = subcommandHelp["list"]
}

func printUsage() {
	fmt.Print(shortHelp)
}

func printHelp() {
	fmt.Print(helpOverview)
	if runtime.GOOS == "darwin" {
		fmt.Print(helpMacOSCommands)
	}
	fmt.Print(helpCore)
	if runtime.GOOS == "darwin" {
		fmt.Print(helpMacOSSessionType)
		fmt.Print(helpMacOSExample)
	}
	fmt.Print(helpTopicsFooter)
}

func printSubcommandHelp(cmd string) {
	if h, ok := subcommandHelp[cmd]; ok {
		fmt.Print(h)
	} else {
		fmt.Fprintf(os.Stderr, "No help available for %q. Run 'hangon --help' for all commands.\n", cmd)
		os.Exit(2)
	}
}

func printTopicHelp(topic string) {
	switch topic {
	case "macos", "ax", "accessibility":
		if runtime.GOOS != "darwin" {
			fmt.Fprintln(os.Stderr, "macOS accessibility commands are only available on darwin.")
			os.Exit(2)
		}
		fmt.Print(topicMacOS)
	case "output", "read", "expect":
		fmt.Print(topicOutput)
	case "keys":
		fmt.Print(topicKeys)
	case "screenshots", "screenshot":
		fmt.Print(topicScreenshots)
	case "topics":
		fmt.Print(topicList)
	default:
		// Might be a subcommand.
		if h, ok := subcommandHelp[topic]; ok {
			fmt.Print(h)
			return
		}
		fmt.Fprintf(os.Stderr, "Unknown help topic %q. Run 'hangon help topics' for available topics.\n", topic)
		os.Exit(2)
	}
}

var shortHelp = `hangon - persistent session manager for CLI-driven app interaction
Usage: hangon <command> [options] [args...]
Run 'hangon --help' for full documentation.
Run 'hangon <command> --help' for help on a specific command.
Run 'hangon help topics' for detailed guides.
`

// --- Main help (assembled dynamically based on platform) ---

var helpOverview = `hangon ` + version + ` - persistent session manager for CLI-driven app interaction

hangon lets you start a long-running process, socket, or app in the
background and interact with it through short-lived shell commands.
Each command connects to the session, performs one action, and exits.
This makes it ideal for shell scripts and AI coding agents.

QUICK START
  hangon start process -- python3 -i
  hangon expect ">>>"
  hangon sendline "2 + 2"
  hangon expect "4"
  hangon read
  hangon stop

COMMANDS

  Session Management:
    start <type> [opts] [-- args]  Start a new session
    list                           List all active sessions
    status [SESSION]               Show session details
    stop [SESSION]                 Stop a session
    stopall --force                Stop all sessions (preview without --force)
    gc [--dry-run]                 Reap orphaned state entries/tmux/processes

  I/O:
    send [SESSION] <data>          Send raw data (no newline)
    sendline [SESSION] <text>      Send text + newline
    read [SESSION]                 Read new output since last read
    readall [SESSION]              Read entire output buffer
    stderr [SESSION]               Read new stderr (--no-pty only)
    expect [SESSION] <regex>       Wait for pattern (exit 1 on timeout)
    screen [SESSION]               Terminal screen as text (PTY only)
    keys [SESSION] <key...>        Send special keys (ctrl-c, up, etc.)
    resize [SESSION] --cols --rows Resize the session's terminal
    mouse-click [SESSION] --x --y  Click at terminal cell
    mouse-drag [SESSION] --from --to  Drag between positions
    mouse-scroll [SESSION] --x --y --delta  Scroll wheel
    alive [SESSION]                Check if running (exit 0=yes, 1=no)
    wait [SESSION]                 Block until process exits
    screenshot [SESSION] [file]    Visual screenshot (SVG/PNG)
`

var helpMacOSCommands = `
  macOS Desktop (this platform):
    launch [--name N] <app>        Launch app + create session
    ax-tree [SESSION]              Dump accessibility tree
    ax-find [SESSION] --role R     Find accessibility node
    click [SESSION] <element>      Click UI element
    type [SESSION] <text>          Type into focused element
`

var helpCore = `
SESSION TYPES
  process   Local process via PTY. Uses tmux when available for rich
            screen capture. Falls back to raw PTY without tmux.
            --no-pty uses raw pipes with separate stderr.
            hangon start process -- python3 -i

  tcp       TCP socket connection.
            hangon start tcp localhost:6379

  ws        WebSocket endpoint.
            hangon start ws wss://echo.websocket.events
`

var helpMacOSSessionType = `
  macos     macOS desktop app via Accessibility APIs.
            Requires Accessibility permission in System Settings.
            hangon launch --name calc Calculator
`

var helpMacOSExample = `
  macOS desktop app:
    hangon launch --name editor TextEdit
    hangon type editor "Hello from hangon!"
    hangon ax-tree editor             # inspect the UI
    hangon screenshot editor out.png
    hangon stop editor
    (Run 'hangon help macos' for the full accessibility guide.)
`

var helpTopicsFooter = `
NAMED SESSIONS
  Multiple sessions run simultaneously. Default name is "default".
    hangon start process --name server -- python3 app.py
    hangon sendline server "start()"
    hangon read server

  A leading word that doesn't match any --name'd flag is only ever
  treated as a session name if it exactly matches an existing session.
  For commands with no other arguments (read, readall, stderr, screen,
  resize, alive, wait, status, stop, mouse-*, ax-tree, ax-find), a word
  that matches nothing is always an error — it can't be anything else,
  so it's never silently sent to "default". For commands that take data
  of their own (send, sendline, expect, keys, click, type, screenshot),
  an unmatched word is ambiguous and, for backward compatibility, is
  treated as data against "default" rather than an error: 'hangon send
  typo hello' sends the literal text "typo hello" to the default session
  if no session is named "typo". Use an exact session name, or --name,
  if this matters to you.

OUTPUT READING
  'read' returns only new output since the last read (cursored).
  'readall' returns the entire buffer. 'expect' blocks until a regex
  matches, then advances the cursor. See 'hangon help output'.

OPTIONS
  --name NAME    Session name (default: "default")
  --timeout SEC  Timeout for expect (default: 30, or $HANGON_TIMEOUT)
  --no-pty       Process: use raw pipes instead of PTY
  --local        Use ./.hangon/ for state (project-scoped)
  --cols N       start: initial terminal width (default: 80)
  --rows N       start: initial terminal height (default: 24)

EXIT CODES
  0  Success
  1  Check failed (expect timeout, alive=false)
  2  Error (bad args, no session, connection failed)

EXAMPLES

  Python REPL:
    hangon start process -- python3 -i
    hangon expect ">>>"
    hangon sendline "import math; math.pi"
    hangon expect "3.14"
    hangon stop

  Test a server:
    hangon start process --name srv -- python3 -m http.server 8080
    hangon expect srv "Serving HTTP"
    curl http://localhost:8080
    hangon stop srv

  Redis:
    hangon start tcp --name redis localhost:6379
    hangon sendline redis "PING"
    hangon expect redis "PONG"
    hangon stop redis

MORE HELP
  hangon <command> --help    Help for a specific command
  hangon help topics         List all detailed guides
  hangon help output         How output reading and expect work
  hangon help keys           Key sequences reference
  hangon help screenshots    Screenshot capabilities

AUTHOR
  Joe Walnes <joe@walnes.com>
  https://github.com/joewalnes/hangon
  Inspired by Simon Willison's Rodney (https://github.com/simonw/rodney).
`

// --- Topic guides (shown via 'hangon help <topic>') ---

var topicList = `Available help topics:

  output        How read, readall, and expect work (cursored reads)
  keys          Key sequences for the 'keys' command
  screenshots   Screenshot capabilities (process + macOS)
` + func() string {
	if runtime.GOOS == "darwin" {
		return "  macos         macOS accessibility guide (ax-tree, click, type)\n"
	}
	return ""
}() + `
Run 'hangon help <topic>' for details.
Run 'hangon <command> --help' for help on a specific command.
`

var topicOutput = `HOW OUTPUT READING WORKS

  hangon buffers all output from the target in a 1MB ring buffer.
  Each session tracks a read cursor so successive reads never repeat.

  Commands:
    read      Returns only NEW output since the previous 'read' call.
              Non-blocking: returns immediately (empty if nothing new).
    readall   Returns the entire buffer regardless of cursor position.
    expect    Blocks until a regex matches new (unread) output, then
              returns the chunk containing the match. The cursor advances
              past the match. Exits with code 1 on timeout.

  Typical pattern:
    hangon sendline "some command"
    hangon expect "expected output"    # blocks until it appears
    hangon read                        # get any remaining new output

  How cursors work:
    After 'expect' matches, the cursor advances to the end of the
    matched chunk. A subsequent 'read' returns only data that arrived
    after the match. This means expect + read never return the same
    data twice.

  Tips:
    - Use 'expect' to synchronize: wait for a prompt or expected output
      before sending the next command.
    - 'readall' is useful for debugging: it shows everything in the
      buffer regardless of what's been read.
    - The ring buffer is 1MB. If more than 1MB of output accumulates,
      the oldest data is overwritten and cursors are adjusted.
    - 'expect' uses Go's regexp syntax (similar to PCRE, no backrefs).

  Environment:
    HANGON_TIMEOUT       Default expect timeout (Go duration: "30s", "1m")
    HANGON_TMUX_SOCKET   tmux server socket name (tmux -L) hangon uses.
                         Defaults to "hangon" — a dedicated server, so
                         hangon never touches your personal tmux sessions.
                         Inspect with: tmux -L hangon ls
`

var topicKeys = `KEY SEQUENCES

  The 'keys' command sends special key sequences to the session.
  Multiple keys are separated by spaces.

  For the full, authoritative list of key names (control keys,
  navigation, shift/alt/ctrl combos, function keys, etc.), run:

    hangon keys --help

  (Kept here in one place, not duplicated, so this guide can't drift
  out of sync with the key names hangon actually recognizes.)

  Note: plain single characters like "q" are NOT key names — 'keys'
  only understands the named sequences from 'hangon keys --help' above.
  To send a literal character (e.g. to quit htop), use 'send' or
  'sendline' instead: hangon send "q".

  Examples:
    hangon keys ctrl-c                # interrupt
    hangon keys "ctrl-c enter"        # interrupt then newline
    hangon keys "up up enter"         # navigate history
    hangon keys ctrl-l                # clear screen
    hangon keys escape                # exit mode (vim, etc.)
` + func() string {
	if runtime.GOOS == "darwin" {
		return `
  macOS shortcuts (in macOS sessions):
    hangon keys editor "cmd-s"        # save
    hangon keys editor "cmd-a"        # select all
    hangon keys editor "cmd-c"        # copy
    hangon keys editor "cmd-v"        # paste
`
	}
	return ""
}()

var topicScreenshots = `SCREENSHOTS

  The 'screenshot' command captures a visual image of the session.

  Process sessions (requires tmux):
    Captures the terminal screen with full support for:
    - Foreground and background colors (16, 256, 24-bit truecolor)
    - Bold, italic, underline, strikethrough, dim, inverse text
    - Unicode characters, CJK wide characters, emoji
    - Cursor position indicator
    - Nerd Font glyphs (via font stack in the SVG)

    Output is SVG by default. PNG output requires rsvg-convert
    (brew install librsvg) or ImageMagick; falls back to SVG.

    hangon start process -- python3 -i
    hangon expect ">>>"
    hangon sendline "print('\033[32mGreen!\033[0m')"
    hangon screenshot repl.png

    hangon start process -- htop
    hangon screenshot htop.png
    hangon send "q"
    hangon stop
` + func() string {
	if runtime.GOOS == "darwin" {
		return `
  macOS app sessions:
    Captures the app window as PNG using screencapture.
    Requires Screen Recording permission in System Settings.

    hangon launch --name editor TextEdit
    hangon screenshot editor textedit.png
    hangon stop editor
`
	}
	return ""
}()

var topicMacOS = `macOS ACCESSIBILITY (AX) GUIDE

  hangon drives native macOS GUI apps through the Accessibility API.
  The workflow: launch → inspect (ax-tree) → interact (click/type) → verify.

  Prerequisites:
    - Accessibility permission: System Settings → Privacy & Security
      → Accessibility. Grant to your terminal app.
    - Screen Recording permission (for screenshot only).

  STEP 1: Launch an app and inspect its UI.

    hangon launch --name calc Calculator
    hangon ax-tree calc

    ax-tree output shows every UI element in the front window:

      Window: Calculator
      AXGroup:
      AXButton: clear [AC]
      AXButton: seven [7]
      AXButton: plus [+]
      AXStaticText: main display [0]

    Each line: Role: description [value]
    Roles use Apple's naming: AXButton, AXTextField, AXStaticText, etc.

  STEP 2: Find specific elements with ax-find.

    hangon ax-find calc --role AXButton            # all buttons
    hangon ax-find calc --name save                # match description
    hangon ax-find calc --role AXButton --name ok  # both (AND)

  STEP 3: Click elements by their description.

    hangon click calc "seven"
    hangon click calc "plus"
    hangon click calc "three"
    hangon click calc "equals"

  STEP 4: Type text into the focused element.

    hangon type editor "Hello, world!"

  STEP 5: Use keyboard shortcuts.

    hangon keys editor "cmd-s"      # save
    hangon keys editor "cmd-a"      # select all
    hangon keys editor "cmd-c"      # copy

  STEP 6: Screenshot the app window.

    hangon screenshot calc result.png

  STEP 7: Stop (quits the app).

    hangon stop calc

  TIPS:
    - Always run ax-tree first to discover element names.
    - click matches the accessibility "description" field. First match wins.
    - type sends keystrokes to whatever has focus. click first to focus.
    - Pipe ax-tree through grep: hangon ax-tree calc | grep -i button
    - For complex apps, ax-tree can be large. Use ax-find to filter.

  FULL EXAMPLE: Automate Calculator (7 + 3).

    hangon launch --name calc Calculator
    sleep 1
    hangon click calc "seven"
    hangon click calc "plus"
    hangon click calc "three"
    hangon click calc "equals"
    hangon screenshot calc answer.png
    hangon stop calc

  FULL EXAMPLE: Type into TextEdit and verify.

    hangon launch --name doc TextEdit
    sleep 1
    hangon type doc "Hello from hangon!"
    hangon screenshot doc hello.png
    hangon ax-tree doc                   # verify text was entered
    hangon stop doc
`
