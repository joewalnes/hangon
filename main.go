package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// version is set at build time via -ldflags "-X main.version=..."
// Falls back to "dev" for plain `go build` / `go run`.
var version = "dev"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		printUsage()
		os.Exit(2)
	}

	cmd := args[0]
	args = args[1:]

	// Per-subcommand --help support.
	if cmd != "--help" && cmd != "-h" && cmd != "help" && cmd != "--version" && cmd != "-v" && cmd != "version" && cmd != "_serve" {
		for _, a := range args {
			if a == "--help" || a == "-h" {
				printSubcommandHelp(cmd)
				return
			}
		}
	}

	switch cmd {
	case "--help", "-h", "help":
		if len(args) > 0 {
			printTopicHelp(args[0])
		} else {
			printHelp()
		}
	case "--version", "-v", "version":
		fmt.Println("hangon " + version)

	// Internal: session holder server (not user-facing).
	case "_serve":
		runServe(args)

	// Session management.
	case "start":
		runStart(args)
	case "list", "ls":
		runList(args)
	case "status":
		runStatus(args)
	case "stop":
		runStop(args)
	case "stopall":
		runStopAll(args)
	case "gc":
		runGC(args)

	// I/O commands.
	case "send":
		runIO(MethodSend, args, false)
	case "sendline":
		runIO(MethodSend, args, true)
	case "read":
		runIO(MethodRead, args, false)
	case "readall":
		runIO(MethodReadAll, args, false)
	case "stderr":
		runIO(MethodStderr, args, false)
	case "expect":
		runExpect(args)
	case "screen":
		runIO(MethodScreen, args, false)
	case "keys":
		runIO(MethodKeys, args, false)
	case "resize":
		runResize(args)
	case "alive":
		runAlive(args)
	case "wait":
		runWait(args)

	// Mouse events (terminal SGR sequences).
	case "mouse-click":
		runMouseClick(args)
	case "mouse-drag":
		runMouseDrag(args)
	case "mouse-scroll":
		runMouseScroll(args)

	// macOS commands.
	case "launch":
		runLaunch(args)
	case "ax-tree":
		runMacSimple(MethodAxTree, args)
	case "ax-find":
		runAxFind(args)
	case "click":
		runMacParam(MethodClick, args)
	case "type":
		runMacParam(MethodType, args)
	case "screenshot":
		runScreenshot(args)

	default:
		fmt.Fprintf(os.Stderr, "hangon: unknown command %q\n", cmd)
		fmt.Fprintln(os.Stderr, "Run 'hangon --help' for usage.")
		os.Exit(2)
	}
}

// --- Flag parsing helpers ---

type flags struct {
	name    string
	local   bool
	global  bool
	timeout float64
	noPty   bool
	stdin   bool
	force   bool
	dryRun  bool
	cols    int // 0 = unset/default
	rows    int // 0 = unset/default
	rest    []string
}

// parseFlags parses the flags common to all subcommands. Any token
// starting with "--" that isn't one of the recognized flags below (or
// in the caller-supplied extraFlags allowlist) is a hard error, not a
// silently-ignored no-op.
//
// This matters because an unrecognized flag used to be swallowed into
// f.rest and treated as a positional argument — e.g. a caller who
// mistakenly typed `hangon start process --dir /tmp/foo -- cmd...`
// (there is no --dir flag; only --local/--global exist) would have
// "--dir" and "/tmp/foo" silently absorbed into the command's
// positional args, with no error, while the command ran against the
// real (unscoped) state directory instead of the isolated one the
// caller believed they were requesting. That is exactly the kind of
// mistake that should fail loudly.
//
// extraFlags lists additional "--xxx" tokens (with or without a
// following value — the caller's own downstream parser decides that)
// that this particular subcommand accepts and wants passed through to
// f.rest untouched, e.g. mouse-click's --x/--y/--button or ax-find's
// --role. Pass nil for commands with no extra flags of their own.
func parseFlags(args []string, extraFlags ...string) flags {
	extra := make(map[string]bool, len(extraFlags))
	for _, e := range extraFlags {
		extra[e] = true
	}

	f := flags{timeout: 0}
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--name":
			if i+1 < len(args) {
				f.name = args[i+1]
				i += 2
			} else {
				fatal("--name requires a value")
			}
		case "--local":
			f.local = true
			i++
		case "--global":
			f.global = true
			i++
		case "--timeout":
			if i+1 < len(args) {
				v, err := strconv.ParseFloat(args[i+1], 64)
				if err != nil {
					fatal("--timeout: invalid number")
				}
				f.timeout = v
				i += 2
			} else {
				fatal("--timeout requires a value")
			}
		case "--no-pty":
			f.noPty = true
			i++
		case "--cols":
			if i+1 < len(args) {
				v, err := strconv.Atoi(args[i+1])
				if err != nil {
					fatal("--cols: invalid number")
				}
				f.cols = v
				i += 2
			} else {
				fatal("--cols requires a value")
			}
		case "--rows":
			if i+1 < len(args) {
				v, err := strconv.Atoi(args[i+1])
				if err != nil {
					fatal("--rows: invalid number")
				}
				f.rows = v
				i += 2
			} else {
				fatal("--rows requires a value")
			}
		case "--stdin":
			f.stdin = true
			i++
		case "--force":
			f.force = true
			i++
		case "--dry-run":
			f.dryRun = true
			i++
		case "--":
			f.rest = append(f.rest, args[i+1:]...)
			return f
		default:
			if strings.HasPrefix(args[i], "--") && !extra[args[i]] {
				fatal(fmt.Sprintf("unknown flag: %s\nRun 'hangon <command> --help' for usage, or use '--' to pass literal arguments that start with --.", args[i]))
			}
			f.rest = append(f.rest, args[i])
			i++
		}
	}
	return f
}

func (f flags) sessionName() string {
	if f.name != "" {
		return f.name
	}
	return "default"
}

func (f flags) dir() string {
	d, err := stateDir(f.local, f.global)
	if err != nil {
		fatal(err.Error())
	}
	return d
}

// resolveSession decides which session a command should target, given
// the parsed flags and the command's positional args (before any
// command-specific parsing of its own). It replaces ~11 duplicated
// copies of the same "does rest[0] look like a session name?" probe
// that used to be pasted at each call site.
//
// Background: with no --name, a bare word before a command's own
// arguments is inherently ambiguous — it could be a session name, or it
// could be the start of the command's own data (a message to send, a
// regex to expect, a filename). The previous behavior treated rest[0]
// as a session name only when it matched an existing session, and
// otherwise silently fell through to operating on the default session
// with rest[0] left as data. That is correct for commands whose data
// *can* start with any word (send, sendline, expect, keys, click,
// type), but for commands that take no data of their own at all (read,
// readall, stderr, screen, resize, alive, wait, mouse-click/drag/scroll,
// ax-tree, ax-find) an unmatched rest[0] can only be a mistyped session
// name — yet the same fallthrough applied there too, so e.g. `hangon
// read typo` would silently read the *default* session and exit 0
// instead of erroring, whenever a default session happened to exist.
//
// dataCommand selects between the two behaviors:
//   - false (no-positional-arg commands): an unmatched rest[0] is always
//     a hard error, even if a default session exists to (wrongly) fall
//     back to.
//   - true (data-taking commands): historical behavior is preserved —
//     rest[0] is consumed as the session name only on an exact match;
//     otherwise it's left in rest and the default/--name session is
//     used. This ambiguity is unavoidable without guessing intent and is
//     documented in --help; `hangon send typo hello` sends "typo hello"
//     to the default session if "typo" isn't a real session name.
//
// In both cases, if neither rest[0] nor the default session name an
// existing session, the error says so plainly (naming the word the
// caller actually typed) rather than deferring to a later getSession
// lookup that would only ever mention "default".
func resolveSession(dir string, f flags, rest []string, dataCommand bool) (name string, remaining []string) {
	remaining = rest
	if f.name != "" {
		return f.name, remaining
	}
	if len(rest) == 0 {
		return "default", remaining
	}
	if _, err := getSession(dir, rest[0]); err == nil {
		return rest[0], rest[1:]
	}

	// rest[0] does not name an existing session.
	if _, defaultErr := getSession(dir, "default"); defaultErr != nil {
		// Nothing to fall back to either way — say so plainly instead of
		// letting a later getSession(dir, "default") fail with a message
		// that talks about "default", a word the caller never typed.
		fatal(fmt.Sprintf("no session named %q, and no default session either", rest[0]))
	}
	if dataCommand {
		// Ambiguous, and historically resolved in data's favor: rest[0]
		// stays as data, targeting the default session. See doc comment.
		return "default", remaining
	}
	// No-positional-arg command: an unrecognized token here can't
	// legitimately be anything but a mistyped session name. Refuse to
	// silently operate on "default" just because it happens to exist.
	fatal(fmt.Sprintf("no session named %q", rest[0]))
	panic("unreachable")
}

// --- Commands ---

func runStart(args []string) {
	f := parseFlags(args)
	if len(f.rest) < 1 {
		fatal("usage: hangon start <type> [options] [-- args...]")
	}

	sessType := f.rest[0]
	typeArgs := f.rest[1:]
	name := f.sessionName()
	dir := f.dir()

	// Initial terminal geometry (process sessions only; default 80x24).
	// Validated here (not just at resize time) so a bad value fails fast
	// instead of silently falling back deep inside the holder.
	cols, rows := f.cols, f.rows
	if cols != 0 && (cols < minTerminalDim || cols > maxTerminalDim) {
		fatal(fmt.Sprintf("--cols must be between %d and %d", minTerminalDim, maxTerminalDim))
	}
	if rows != 0 && (rows < minTerminalDim || rows > maxTerminalDim) {
		fatal(fmt.Sprintf("--rows must be between %d and %d", minTerminalDim, maxTerminalDim))
	}

	// Create socket path under a per-user 0700 runtime dir, not bare
	// os.TempDir() — see runtimeDir's doc comment for why (control
	// socket == keystroke injection == code execution as the session
	// owner, under a lax umask) and why that directory is a fixed,
	// short, uid-scoped path rather than nested under the state dir.
	runDir, err := runtimeDir()
	if err != nil {
		fatal(err.Error())
	}
	socketPath := filepath.Join(runDir, fmt.Sprintf("hangon-%s-%d.sock", name, os.Getpid()))
	if err := checkUnixSocketPathLen(socketPath); err != nil {
		fatal(err.Error())
	}

	// Build args for the _serve subprocess.
	serveArgs := []string{"_serve",
		"--name", name,
		"--type", sessType,
		"--socket", socketPath,
		"--state-dir", dir,
	}
	if f.noPty {
		serveArgs = append(serveArgs, "--no-pty")
	}
	if cols != 0 {
		serveArgs = append(serveArgs, "--cols", strconv.Itoa(cols))
	}
	if rows != 0 {
		serveArgs = append(serveArgs, "--rows", strconv.Itoa(rows))
	}
	serveArgs = append(serveArgs, "--")
	serveArgs = append(serveArgs, typeArgs...)

	exe, err := os.Executable()
	if err != nil {
		fatal("cannot find own executable: " + err.Error())
	}

	cmd := exec.Command(exe, serveArgs...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	setSysProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		fatal("failed to start session holder: " + err.Error())
	}

	holderPID := cmd.Process.Pid

	// Atomically claim the session name now that the holder is spawned
	// and its PID is known — see claimSessionName's doc comment for why
	// this must happen here rather than via an earlier unlocked check.
	// If someone else holds the name (including a concurrent `start`
	// that won the same race), kill the holder we just spawned instead
	// of leaking it.
	claimed, err := claimSessionName(dir, name, sessType, socketPath, holderPID, 0, typeArgs, strings.Join(typeArgs, " "))
	if !claimed {
		cmd.Process.Kill()
		fatal(err.Error())
	}
	if err != nil {
		// Claimed but saveState failed after the in-memory map was
		// already mutated — surface it, but the holder is registered
		// in memory only, not on disk, so kill it too rather than
		// leaving an untracked orphan.
		cmd.Process.Kill()
		fatal("failed to save session state: " + err.Error())
	}

	// Wait briefly for the socket to appear.
	ready := false
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		if _, err := os.Stat(socketPath); err == nil {
			// Try a ping.
			resp, err := clientSendSimple(socketPath, MethodPing, 5*time.Second)
			if err == nil && resp.OK {
				ready = true
				break
			}
		}
	}

	if !ready {
		// We already registered this session (to close the name-claim
		// race above); since the holder never became healthy, undo
		// that registration so we don't leave a broken entry — and a
		// leaked process — behind for `gc` to find later.
		removeSession(dir, name)
		cmd.Process.Kill()
		fatal("session holder did not start within 5 seconds")
	}

	fmt.Printf("Session %q started (type=%s, holder PID=%d)\n", name, sessType, holderPID)
}

func runList(args []string) {
	f := parseFlags(args)
	dir := f.dir()
	sf, err := loadState(dir)
	if err != nil {
		fatal(err.Error())
	}

	if len(sf.Sessions) == 0 {
		fmt.Println("No active sessions.")
		return
	}

	fmt.Printf("%-15s %-10s %-8s %-6s %s\n", "NAME", "TYPE", "HOLDER", "ALIVE", "TARGET")
	for name, info := range sf.Sessions {
		alive := isProcessAlive(info.HolderPID)
		aliveStr := "no"
		if alive {
			aliveStr = "yes"
		}
		target := info.Target
		if len(info.Command) > 0 {
			target = strings.Join(info.Command, " ")
		}
		if len(target) > 50 {
			target = target[:47] + "..."
		}
		fmt.Printf("%-15s %-10s %-8d %-6s %s\n", name, info.Type, info.HolderPID, aliveStr, target)
	}
}

func runStatus(args []string) {
	f := parseFlags(args)
	dir := f.dir()
	name, _ := resolveSession(dir, f, f.rest, false)
	info, err := getSession(dir, name)
	if err != nil {
		fatal(err.Error())
	}

	alive := isProcessAlive(info.HolderPID)
	fmt.Printf("Session:    %s\n", name)
	fmt.Printf("Type:       %s\n", info.Type)
	fmt.Printf("Holder PID: %d\n", info.HolderPID)
	fmt.Printf("Alive:      %v\n", alive)
	fmt.Printf("Socket:     %s\n", info.Socket)
	fmt.Printf("Started:    %s\n", info.Started)
	if len(info.Command) > 0 {
		fmt.Printf("Command:    %s\n", strings.Join(info.Command, " "))
	}
	if info.Target != "" && len(info.Command) == 0 {
		fmt.Printf("Target:     %s\n", info.Target)
	}
}

func runStop(args []string) {
	f := parseFlags(args)
	dir := f.dir()
	name, _ := resolveSession(dir, f, f.rest, false)
	info, err := getSession(dir, name)
	if err != nil {
		fatal(err.Error())
	}

	// Signal the holder to stop.
	if isProcessAlive(info.HolderPID) {
		proc, err := os.FindProcess(info.HolderPID)
		if err == nil {
			proc.Signal(os.Interrupt)
			// Give it time to clean up (tmux kill-session, etc.)
			for i := 0; i < 20; i++ {
				time.Sleep(100 * time.Millisecond)
				if !isProcessAlive(info.HolderPID) {
					break
				}
			}
			if isProcessAlive(info.HolderPID) {
				proc.Kill()
			}
		}
	}

	// Clean up any orphaned tmux session.
	if info.Type == "process" {
		tmuxSess := fmt.Sprintf("hangon-%d", info.HolderPID)
		tmuxCmd("kill-session", "-t", tmuxExact(tmuxSess)).Run()
	}

	// Clean up socket.
	os.Remove(info.Socket)

	if err := removeSession(dir, name); err != nil {
		fatal(err.Error())
	}
	fmt.Printf("Session %q stopped.\n", name)
}

// runStopAll stops every session tracked in the resolved state
// directory. Because that directory defaults to the shared ~/.hangon
// (unless --local is used), stopall is a machine-wide "kill everything"
// as far as any other process/agent using the same default state dir
// is concerned — including ones this invocation knows nothing about.
//
// That made stopall a recurring, high-blast-radius footgun in practice:
// it has been run by mistake (muscle memory, a script's generic
// cleanup step, a misremembered command) and killed unrelated sessions
// started by other concurrent hangon users on the same machine. To
// make the accidental case hard and the deliberate case easy, stopall
// now requires --force to actually take action; without it, it prints
// exactly what it *would* stop (so the caller can sanity-check the
// blast radius) and exits nonzero without touching anything.
func runStopAll(args []string) {
	f := parseFlags(args)
	dir := f.dir()
	sf, err := loadState(dir)
	if err != nil {
		fatal(err.Error())
	}

	if len(sf.Sessions) == 0 {
		fmt.Println("No active sessions.")
		return
	}

	if !f.force {
		fmt.Printf("stopall would stop %d session(s) in %s:\n\n", len(sf.Sessions), dir)
		for name, info := range sf.Sessions {
			fmt.Printf("  %-15s type=%-8s holder PID=%-8d alive=%v\n", name, info.Type, info.HolderPID, isProcessAlive(info.HolderPID))
		}
		fmt.Fprintln(os.Stderr, "\nRefusing to stop sessions without --force: this affects every session in this")
		fmt.Fprintln(os.Stderr, "state directory, including ones started by other processes or agents sharing")
		fmt.Fprintln(os.Stderr, "it. Re-run as 'hangon stopall --force' to proceed.")
		os.Exit(2)
	}

	// Track exactly which (name, holderPID) pairs we actually process,
	// so the final state cleanup only removes those — not whatever
	// happens to be in state.json by the time we get there. See
	// mergeRemoveSessions for why a blind "write back an empty state"
	// is unsafe: killing each session can take up to ~600ms, during
	// which another process can legitimately register a new session
	// (possibly reusing a name we're about to touch); overwriting
	// wholesale would silently drop that entry from state.json while
	// its holder and tmux session are still alive, producing exactly
	// the kind of untracked orphan `hangon gc` has to clean up later.
	processed := make(map[string]int, len(sf.Sessions))
	for name, info := range sf.Sessions {
		if isProcessAlive(info.HolderPID) {
			proc, _ := os.FindProcess(info.HolderPID)
			if proc != nil {
				proc.Signal(os.Interrupt)
				time.Sleep(500 * time.Millisecond)
				if isProcessAlive(info.HolderPID) {
					proc.Kill()
				}
			}
		}
		if info.Type == "process" {
			tmuxCmd("kill-session", "-t", tmuxExact(fmt.Sprintf("hangon-%d", info.HolderPID))).Run()
		}
		os.Remove(info.Socket)
		fmt.Printf("Stopped %q\n", name)
		processed[name] = info.HolderPID
	}

	if err := mergeRemoveSessions(dir, processed); err != nil {
		fatal(err.Error())
	}
}

// --- I/O commands ---

func runIO(method string, args []string, appendNewline bool) {
	f := parseFlags(args)
	dir := f.dir()
	data := ""

	// send/sendline/keys carry free-form data that can start with any
	// word, so an unmatched rest[0] must be preserved as data (see
	// resolveSession's doc comment). read/readall/stderr/screen take no
	// data at all, so an unmatched rest[0] can only be a mistyped
	// session name and should error.
	dataCommand := method == MethodSend || method == MethodKeys
	name, rest := resolveSession(dir, f, f.rest, dataCommand)

	info, err := getSession(dir, name)
	if err != nil {
		fatal(err.Error())
	}

	timeout := 30 * time.Second
	if f.timeout > 0 {
		timeout = time.Duration(f.timeout * float64(time.Second))
	}

	switch method {
	case MethodSend:
		if f.stdin {
			raw, err := io.ReadAll(os.Stdin)
			if err != nil {
				fatal("reading stdin: " + err.Error())
			}
			data = string(raw)
		} else {
			if len(rest) < 1 {
				fatal("usage: hangon send [SESSION] <data>")
			}
			data = strings.Join(rest, " ")
		}
		if appendNewline {
			data += "\n"
		}
		resp, err := clientSendJSON(info.Socket, MethodSend, SendParams{Data: data}, timeout)
		if err != nil {
			fatal(err.Error())
		}
		printResp(resp)

	case MethodKeys:
		if len(rest) < 1 {
			fatal("usage: hangon keys [SESSION] <key-sequence>")
		}
		keys := strings.Join(rest, " ")
		resp, err := clientSendJSON(info.Socket, MethodKeys, KeysParams{Keys: keys}, timeout)
		if err != nil {
			fatal(err.Error())
		}
		printResp(resp)

	default:
		// No-param methods: read, readall, stderr, screen.
		resp, err := clientSendSimple(info.Socket, method, timeout)
		if err != nil {
			fatal(err.Error())
		}
		printResp(resp)
	}
}

func runResize(args []string) {
	f := parseFlags(args)
	dir := f.dir()
	name, rest := resolveSession(dir, f, f.rest, false)
	if len(rest) > 0 {
		fatal("usage: hangon resize [SESSION] --cols N --rows N")
	}
	if f.cols == 0 || f.rows == 0 {
		fatal("usage: hangon resize [SESSION] --cols N --rows N (both required)")
	}
	if f.cols < minTerminalDim || f.cols > maxTerminalDim || f.rows < minTerminalDim || f.rows > maxTerminalDim {
		fatal(fmt.Sprintf("--cols/--rows must be between %d and %d", minTerminalDim, maxTerminalDim))
	}

	info, err := getSession(dir, name)
	if err != nil {
		fatal(err.Error())
	}

	resp, err := clientSendJSON(info.Socket, MethodResize, ResizeParams{Cols: f.cols, Rows: f.rows}, 30*time.Second)
	if err != nil {
		fatal(err.Error())
	}
	printResp(resp)
}

// --- Mouse event commands ---

func parseMouseFlags(rest []string) (x, y, count, steps, delta int, button string, fromX, fromY, toX, toY int, shift, alt, ctrl bool) {
	count = 1
	steps = 1
	button = "left"
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--x":
			if i+1 < len(rest) {
				x, _ = strconv.Atoi(rest[i+1])
				i++
			}
		case "--y":
			if i+1 < len(rest) {
				y, _ = strconv.Atoi(rest[i+1])
				i++
			}
		case "--button":
			if i+1 < len(rest) {
				button = rest[i+1]
				i++
			}
		case "--count":
			if i+1 < len(rest) {
				count, _ = strconv.Atoi(rest[i+1])
				i++
			}
		case "--delta":
			if i+1 < len(rest) {
				delta, _ = strconv.Atoi(rest[i+1])
				i++
			}
		case "--steps":
			if i+1 < len(rest) {
				steps, _ = strconv.Atoi(rest[i+1])
				i++
			}
		case "--from":
			if i+1 < len(rest) {
				fmt.Sscanf(rest[i+1], "%d,%d", &fromX, &fromY)
				i++
			}
		case "--to":
			if i+1 < len(rest) {
				fmt.Sscanf(rest[i+1], "%d,%d", &toX, &toY)
				i++
			}
		case "--shift":
			shift = true
		case "--alt":
			alt = true
		case "--ctrl":
			ctrl = true
		}
	}
	return
}

func runMouseClick(args []string) {
	f := parseFlags(args, "--x", "--y", "--button", "--count", "--shift", "--alt", "--ctrl")
	dir := f.dir()
	name, rest := resolveSession(dir, f, f.rest, false)
	info, err := getSession(dir, name)
	if err != nil {
		fatal(err.Error())
	}

	x, y, count, _, _, button, _, _, _, _, shift, alt, ctrl := parseMouseFlags(rest)
	if x < 1 || y < 1 {
		fatal("usage: hangon mouse-click [SESSION] --x COL --y ROW [--button left|right|middle] [--count N] [--shift] [--alt] [--ctrl]")
	}

	p := MouseClickParams{X: x, Y: y, Button: button, Count: count, Shift: shift, Alt: alt, Ctrl: ctrl}
	resp, err := clientSendJSON(info.Socket, MethodMouseClick, p, 30*time.Second)
	if err != nil {
		fatal(err.Error())
	}
	printResp(resp)
}

func runMouseDrag(args []string) {
	f := parseFlags(args, "--from", "--to", "--steps", "--shift", "--alt", "--ctrl")
	dir := f.dir()
	name, rest := resolveSession(dir, f, f.rest, false)
	info, err := getSession(dir, name)
	if err != nil {
		fatal(err.Error())
	}

	_, _, _, steps, _, _, fromX, fromY, toX, toY, shift, alt, ctrl := parseMouseFlags(rest)
	if fromX < 1 || fromY < 1 || toX < 1 || toY < 1 {
		fatal("usage: hangon mouse-drag [SESSION] --from COL,ROW --to COL,ROW [--steps N] [--shift] [--alt] [--ctrl]")
	}

	p := MouseDragParams{FromX: fromX, FromY: fromY, ToX: toX, ToY: toY, Steps: steps, Shift: shift, Alt: alt, Ctrl: ctrl}
	resp, err := clientSendJSON(info.Socket, MethodMouseDrag, p, 30*time.Second)
	if err != nil {
		fatal(err.Error())
	}
	printResp(resp)
}

func runMouseScroll(args []string) {
	f := parseFlags(args, "--x", "--y", "--delta", "--shift", "--alt", "--ctrl")
	dir := f.dir()
	name, rest := resolveSession(dir, f, f.rest, false)
	info, err := getSession(dir, name)
	if err != nil {
		fatal(err.Error())
	}

	x, y, _, _, delta, _, _, _, _, _, shift, alt, ctrl := parseMouseFlags(rest)
	if x < 1 || y < 1 || delta == 0 {
		fatal("usage: hangon mouse-scroll [SESSION] --x COL --y ROW --delta N [--shift] [--alt] [--ctrl]")
	}

	p := MouseScrollParams{X: x, Y: y, Delta: delta, Shift: shift, Alt: alt, Ctrl: ctrl}
	resp, err := clientSendJSON(info.Socket, MethodMouseScroll, p, 30*time.Second)
	if err != nil {
		fatal(err.Error())
	}
	printResp(resp)
}

func runExpect(args []string) {
	f := parseFlags(args)
	dir := f.dir()

	// expect's pattern is data (it can start with any word, e.g. a
	// literal string to wait for), so an unmatched rest[0] stays as data
	// against the default session rather than erroring — see
	// resolveSession's doc comment.
	name, rest := resolveSession(dir, f, f.rest, true)

	if len(rest) < 1 {
		fatal("usage: hangon expect [SESSION] <pattern> [--timeout SEC]")
	}

	info, err := getSession(dir, name)
	if err != nil {
		fatal(err.Error())
	}

	pattern := rest[0]
	timeout := envTimeoutOrDefault(defaultTimeout).Seconds()
	if f.timeout > 0 {
		timeout = f.timeout
	}

	resp, err := clientSendJSON(info.Socket, MethodExpect, ExpectParams{
		Pattern: pattern,
		Timeout: timeout,
	}, time.Duration(timeout+10)*time.Second)
	if err != nil {
		fatal(err.Error())
	}
	if !resp.OK {
		fmt.Fprintln(os.Stderr, resp.Error)
		os.Exit(1) // Check failed, not error.
	}
	if resp.Result != "" {
		fmt.Print(resp.Result)
	}
}

func runAlive(args []string) {
	f := parseFlags(args)
	dir := f.dir()
	name, _ := resolveSession(dir, f, f.rest, false)
	info, err := getSession(dir, name)
	if err != nil {
		fatal(err.Error())
	}

	resp, err := clientSendSimple(info.Socket, MethodAlive, 5*time.Second)
	if err != nil {
		fatal(err.Error())
	}
	if resp.Result == "true" {
		fmt.Println("true")
		os.Exit(0)
	}
	fmt.Println("false")
	os.Exit(1)
}

func runWait(args []string) {
	f := parseFlags(args)
	dir := f.dir()
	name, _ := resolveSession(dir, f, f.rest, false)
	info, err := getSession(dir, name)
	if err != nil {
		fatal(err.Error())
	}

	resp, err := clientSendSimple(info.Socket, MethodWait, 0) // No timeout for wait.
	if err != nil {
		fatal(err.Error())
	}
	if !resp.OK {
		fatal(resp.Error)
	}
	code, _ := strconv.Atoi(resp.Result)
	fmt.Printf("exit code: %d\n", code)
	os.Exit(code)
}

// --- macOS commands ---

func runLaunch(args []string) {
	f := parseFlags(args)
	if len(f.rest) < 1 {
		fatal("usage: hangon launch [--name NAME] <app-name-or-path>")
	}
	// Re-route to start with macos type.
	startArgs := []string{"macos"}
	if f.name != "" {
		// name was already parsed, but we need to pass it through start
	}
	startArgs = append(startArgs, f.rest...)

	newArgs := []string{}
	if f.name != "" {
		newArgs = append(newArgs, "--name", f.name)
	}
	if f.local {
		newArgs = append(newArgs, "--local")
	}
	if f.global {
		newArgs = append(newArgs, "--global")
	}
	newArgs = append(newArgs, startArgs...)
	runStart(newArgs)
}

func runMacSimple(method string, args []string) {
	f := parseFlags(args)
	dir := f.dir()
	name, _ := resolveSession(dir, f, f.rest, false)
	info, err := getSession(dir, name)
	if err != nil {
		fatal(err.Error())
	}
	resp, err := clientSendSimple(info.Socket, method, 30*time.Second)
	if err != nil {
		fatal(err.Error())
	}
	printResp(resp)
}

func runAxFind(args []string) {
	f := parseFlags(args, "--role")
	dir := f.dir()
	name, rest := resolveSession(dir, f, f.rest, false)
	info, err := getSession(dir, name)
	if err != nil {
		fatal(err.Error())
	}

	// Parse --role and --name from rest.
	p := AxFindParams{}
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--role":
			if i+1 < len(rest) {
				p.Role = rest[i+1]
				i++
			}
		case "--name":
			if i+1 < len(rest) {
				p.Name = rest[i+1]
				i++
			}
		}
	}

	resp, err := clientSendJSON(info.Socket, MethodAxFind, p, 30*time.Second)
	if err != nil {
		fatal(err.Error())
	}
	printResp(resp)
}

func runMacParam(method string, args []string) {
	f := parseFlags(args)
	dir := f.dir()
	// click/type's value is data (element name / literal text to type),
	// so an unmatched rest[0] stays as data — see resolveSession's doc
	// comment.
	name, rest := resolveSession(dir, f, f.rest, true)
	if len(rest) < 1 {
		fatal(fmt.Sprintf("usage: hangon %s [SESSION] <value>", method))
	}
	info, err := getSession(dir, name)
	if err != nil {
		fatal(err.Error())
	}

	var params interface{}
	switch method {
	case MethodClick:
		params = ClickParams{Element: strings.Join(rest, " ")}
	case MethodType:
		params = TypeParams{Text: strings.Join(rest, " ")}
	}

	resp, err := clientSendJSON(info.Socket, method, params, 30*time.Second)
	if err != nil {
		fatal(err.Error())
	}
	printResp(resp)
}

func runScreenshot(args []string) {
	f := parseFlags(args)
	dir := f.dir()
	// screenshot's optional positional arg is a *filename*, not a
	// session name — `hangon screenshot out.png` (no session given at
	// all) must keep working, so this can't be hardened into the
	// no-positional-arg error case the way read/alive/etc. were: there
	// is no way to tell "typo'd session name" apart from "intentional
	// filename with no session given" from rest[0] alone. Treated as a
	// data command (like send/keys): rest[0] is consumed as the session
	// name only on an exact match, otherwise it's left as the filename
	// against the default session. Documented ambiguity, not a fixable
	// case — see resolveSession's doc comment for the general rule.
	name, rest := resolveSession(dir, f, f.rest, true)
	info, err := getSession(dir, name)
	if err != nil {
		fatal(err.Error())
	}

	file := ""
	if len(rest) > 0 {
		file = rest[0]
	}

	resp, err := clientSendJSON(info.Socket, MethodScreenshot, ScreenshotParams{File: file}, 30*time.Second)
	if err != nil {
		fatal(err.Error())
	}
	printResp(resp)
}

// --- _serve (session holder) ---

func runServe(args []string) {
	// Parse _serve flags.
	var name, sessType, socketPath, stateDir string
	noPty := false
	cols, rows := 0, 0
	var typeArgs []string

	i := 0
	for i < len(args) {
		switch args[i] {
		case "--name":
			name = args[i+1]
			i += 2
		case "--type":
			sessType = args[i+1]
			i += 2
		case "--socket":
			socketPath = args[i+1]
			i += 2
		case "--state-dir":
			stateDir = args[i+1]
			i += 2
		case "--no-pty":
			noPty = true
			i++
		case "--cols":
			cols, _ = strconv.Atoi(args[i+1])
			i += 2
		case "--rows":
			rows, _ = strconv.Atoi(args[i+1])
			i += 2
		case "--":
			typeArgs = args[i+1:]
			i = len(args)
		default:
			i++
		}
	}

	if sessType == "" || socketPath == "" {
		fmt.Fprintln(os.Stderr, "_serve: missing required flags")
		os.Exit(2)
	}

	// Create the backend.
	var backend Backend
	switch sessType {
	case "process":
		if len(typeArgs) < 1 {
			fmt.Fprintln(os.Stderr, "process backend requires a command")
			os.Exit(2)
		}
		backend = NewProcessBackend(typeArgs, !noPty, cols, rows)
	case "tcp":
		if len(typeArgs) < 1 {
			fmt.Fprintln(os.Stderr, "tcp backend requires host:port")
			os.Exit(2)
		}
		backend = NewTCPBackend(typeArgs[0])
	case "ws":
		if len(typeArgs) < 1 {
			fmt.Fprintln(os.Stderr, "ws backend requires a URL")
			os.Exit(2)
		}
		backend = NewWSBackend(typeArgs[0])
	case "macos":
		if len(typeArgs) < 1 {
			fmt.Fprintln(os.Stderr, "macos backend requires an app name")
			os.Exit(2)
		}
		backend = NewMacOSBackend(typeArgs[0])
	default:
		fmt.Fprintf(os.Stderr, "unknown session type: %s\n", sessType)
		os.Exit(2)
	}

	if err := backend.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "backend start failed: %v\n", err)
		os.Exit(2)
	}

	// Update state with target PID if applicable.
	if stateDir != "" && name != "" && backend.TargetPID() > 0 {
		// Best-effort: if the session was removed concurrently, there's
		// nothing to update.
		_ = setSessionTargetPID(stateDir, name, backend.TargetPID())
	}

	holder := NewSessionHolder(backend, socketPath)
	if err := holder.Serve(); err != nil {
		fmt.Fprintf(os.Stderr, "holder serve error: %v\n", err)
		os.Exit(2)
	}
}

// --- Helpers ---

func printResp(resp *Response) {
	if !resp.OK {
		fmt.Fprintln(os.Stderr, resp.Error)
		os.Exit(2)
	}
	if resp.Result != "" {
		fmt.Print(resp.Result)
		if !strings.HasSuffix(resp.Result, "\n") {
			fmt.Println()
		}
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "hangon: "+msg)
	os.Exit(2)
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds. Send signal 0 to check.
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

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

// Ensure json import is used.
var _ = json.Marshal
