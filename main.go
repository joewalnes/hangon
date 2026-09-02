package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// stopSessionHolderGrace is the grace period runStop and runStopAll
// give a holder process to exit cleanly after SIGINT before escalating
// to SIGKILL (see killProcessGracefully). gc.go's gcOrphanedServeProcesses
// uses a shorter 1s grace of its own — see its call site.
const stopSessionHolderGrace = 2 * time.Second

// stopSessionHolder stops the tracked holder process for one session —
// verifying its identity against procs first — and cleans up its tmux
// session (for type=="process") and control socket. It is the shared
// per-session teardown for runStop and runStopAll, so both apply the
// same PID-reuse guard (holderIdentityConfirmed) and the same
// killProcessGracefully grace/poll behavior instead of two
// independently hand-rolled copies of the SIGINT-then-poll-then-SIGKILL
// dance.
//
// procs is the result of listServeProcesses. Ideally it is scanned
// once by the caller and shared across every session being stopped in
// one invocation (a single `ps -A` rather than one per session). A nil
// procs (scan failed) means no PID can be positively identified, so
// the holder is treated as unconfirmed and left unsignalled — the same
// safe-by-default behavior as a genuine identity mismatch.
//
// Returns a non-empty note when the holder PID looked alive but failed
// the identity check — i.e. state.json's HolderPID has been reused by
// an unrelated process since the real holder exited — for the caller
// to surface in its own output. In that case the reused PID is never
// signalled; the session is treated as already-dead and its tmux
// session/socket are cleaned up exactly as they would be for a
// holder confirmed to be gone.
func stopSessionHolder(dir string, info *SessionInfo, procs map[int]string) string {
	note := ""
	if isProcessAlive(info.HolderPID) {
		if holderIdentityConfirmed(info.HolderPID, dir, procs) {
			killProcessGracefully(info.HolderPID, stopSessionHolderGrace)
		} else {
			note = fmt.Sprintf("holder PID %d was reused by another process; not signalling", info.HolderPID)
		}
	}

	// Clean up any orphaned tmux session.
	if info.Type == "process" {
		tmuxCmd("kill-session", "-t", tmuxExact(sessionNameForPID(info.HolderPID))).Run()
	}

	// Clean up socket.
	os.Remove(info.Socket)

	return note
}

// scanServeProcessesForStop is listServeProcesses with a warning
// printed to stderr (and a nil result, so callers safely fall back to
// "identity unconfirmed") on failure, shared by runStop and
// runStopAll so a `ps` scan failure degrades to the safe default
// (refuse to signal) rather than panicking or silently skipping the
// identity check.
func scanServeProcessesForStop() map[int]string {
	procs, err := listServeProcesses()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not scan for hangon holder processes to verify PID identity: %v\n", err)
		return nil
	}
	return procs
}

func runStop(args []string) {
	f := parseFlags(args)
	dir := f.dir()
	name, _ := resolveSession(dir, f, f.rest, false)
	info, err := getSession(dir, name)
	if err != nil {
		fatal(err.Error())
	}

	procs := scanServeProcessesForStop()
	note := stopSessionHolder(dir, info, procs)

	if err := removeSession(dir, name); err != nil {
		fatal(err.Error())
	}
	if note != "" {
		fmt.Println(note)
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
	// is unsafe: killing each session can take up to a couple of
	// seconds, during which another process can legitimately register
	// a new session (possibly reusing a name we're about to touch);
	// overwriting wholesale would silently drop that entry from
	// state.json while its holder and tmux session are still alive,
	// producing exactly the kind of untracked orphan `hangon gc` has
	// to clean up later.
	//
	// One `ps -A` scan (scanServeProcessesForStop) is shared across
	// every session below rather than one per session, since
	// stopSessionHolder's PID-reuse identity check needs it for each.
	procs := scanServeProcessesForStop()

	// Each session's teardown (stopSessionHolder: signal-and-wait the
	// holder, kill its tmux session, remove its socket) is independent
	// of every other session's — they touch disjoint PIDs, disjoint
	// tmux sessions, and disjoint socket files — so they run
	// concurrently rather than serially. This is what makes stopall's
	// wall time roughly the slowest single holder's shutdown instead of
	// the sum of all of them; see CHANGELOG for measured before/after.
	//
	// Each goroutine writes to its own reserved index in results (no
	// shared map, no lock needed) so this stays race-free without
	// synchronizing anything beyond the WaitGroup itself.
	type stopResult struct {
		name      string
		holderPID int
		note      string
	}
	names := make([]string, 0, len(sf.Sessions))
	for name := range sf.Sessions {
		names = append(names, name)
	}
	results := make([]stopResult, len(names))
	var wg sync.WaitGroup
	wg.Add(len(names))
	for i, name := range names {
		info := sf.Sessions[name]
		go func(i int, name string, info *SessionInfo) {
			defer wg.Done()
			note := stopSessionHolder(dir, info, procs)
			results[i] = stopResult{name: name, holderPID: info.HolderPID, note: note}
		}(i, name, info)
	}
	wg.Wait()

	// Sort by name before printing so output is deterministic despite
	// the concurrent teardown above — goroutine completion order isn't.
	sort.Slice(results, func(i, j int) bool { return results[i].name < results[j].name })

	processed := make(map[string]int, len(results))
	for _, r := range results {
		if r.note != "" {
			fmt.Printf("Stopped %q (%s)\n", r.name, r.note)
		} else {
			fmt.Printf("Stopped %q\n", r.name)
		}
		processed[r.name] = r.holderPID
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

// Ensure json import is used.
var _ = json.Marshal
