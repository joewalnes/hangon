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
	case "alive":
		runAlive(args)
	case "wait":
		runWait(args)

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
	rest    []string
}

func parseFlags(args []string) flags {
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
		case "--stdin":
			f.stdin = true
			i++
		case "--":
			f.rest = append(f.rest, args[i+1:]...)
			return f
		default:
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

	// Check for existing session.
	if info, err := getSession(dir, name); err == nil {
		if isProcessAlive(info.HolderPID) {
			fatal(fmt.Sprintf("session %q already exists (PID %d). Stop it first or use a different --name.", name, info.HolderPID))
		}
		// Stale session, clean up.
		removeSession(dir, name)
	}

	// Create socket path.
	socketPath := filepath.Join(os.TempDir(), fmt.Sprintf("hangon-%s-%d.sock", name, os.Getpid()))

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
		fatal("session holder did not start within 5 seconds")
	}

	// Determine target PID (for process type).
	targetPID := 0
	resp, err := clientSendSimple(socketPath, MethodInfo, 5*time.Second)
	if err == nil && resp.OK {
		// Parse from info if available.
		_ = resp
	}

	if err := addSession(dir, name, sessType, socketPath, holderPID, targetPID, typeArgs, strings.Join(typeArgs, " ")); err != nil {
		fatal("failed to save session state: " + err.Error())
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
	name := f.sessionName()
	if len(f.rest) > 0 {
		name = f.rest[0]
	}
	dir := f.dir()
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
	name := f.sessionName()
	if len(f.rest) > 0 {
		name = f.rest[0]
	}
	dir := f.dir()
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
		exec.Command("tmux", "kill-session", "-t", tmuxSess).Run()
	}

	// Clean up socket.
	os.Remove(info.Socket)

	if err := removeSession(dir, name); err != nil {
		fatal(err.Error())
	}
	fmt.Printf("Session %q stopped.\n", name)
}

func runStopAll(args []string) {
	f := parseFlags(args)
	dir := f.dir()
	sf, err := loadState(dir)
	if err != nil {
		fatal(err.Error())
	}

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
			exec.Command("tmux", "kill-session", "-t", fmt.Sprintf("hangon-%d", info.HolderPID)).Run()
		}
		os.Remove(info.Socket)
		fmt.Printf("Stopped %q\n", name)
	}

	sf.Sessions = make(map[string]*SessionInfo)
	saveState(dir, sf)
}

// --- I/O commands ---

func runIO(method string, args []string, appendNewline bool) {
	f := parseFlags(args)
	dir := f.dir()

	// Determine session name: if rest[0] matches a session name, use it.
	name := f.sessionName()
	data := ""
	rest := f.rest

	if len(rest) > 0 {
		// Check if first arg is a session name.
		if _, err := getSession(dir, rest[0]); err == nil && f.name == "" {
			name = rest[0]
			rest = rest[1:]
		}
	}

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

func runExpect(args []string) {
	f := parseFlags(args)
	dir := f.dir()

	name := f.sessionName()
	rest := f.rest

	if len(rest) > 0 {
		if _, err := getSession(dir, rest[0]); err == nil && f.name == "" {
			name = rest[0]
			rest = rest[1:]
		}
	}

	if len(rest) < 1 {
		fatal("usage: hangon expect [SESSION] <pattern> [--timeout SEC]")
	}

	info, err := getSession(dir, name)
	if err != nil {
		fatal(err.Error())
	}

	pattern := rest[0]
	timeout := 30.0
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
	name := f.sessionName()
	if len(f.rest) > 0 {
		if _, err := getSession(dir, f.rest[0]); err == nil && f.name == "" {
			name = f.rest[0]
		}
	}
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
	name := f.sessionName()
	if len(f.rest) > 0 {
		if _, err := getSession(dir, f.rest[0]); err == nil && f.name == "" {
			name = f.rest[0]
		}
	}
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
	name := f.sessionName()
	if len(f.rest) > 0 {
		if _, err := getSession(dir, f.rest[0]); err == nil && f.name == "" {
			name = f.rest[0]
		}
	}
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
	f := parseFlags(args)
	dir := f.dir()
	name := f.sessionName()
	rest := f.rest
	if len(rest) > 0 {
		if _, err := getSession(dir, rest[0]); err == nil && f.name == "" {
			name = rest[0]
			rest = rest[1:]
		}
	}
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
	name := f.sessionName()
	rest := f.rest
	if len(rest) > 0 {
		if _, err := getSession(dir, rest[0]); err == nil && f.name == "" {
			name = rest[0]
			rest = rest[1:]
		}
	}
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
	name := f.sessionName()
	rest := f.rest
	if len(rest) > 0 {
		if _, err := getSession(dir, rest[0]); err == nil && f.name == "" {
			name = rest[0]
			rest = rest[1:]
		}
	}
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
		backend = NewProcessBackend(typeArgs, !noPty)
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
		if info, err := getSession(stateDir, name); err == nil {
			info.TargetPID = backend.TargetPID()
			sf, _ := loadState(stateDir)
			sf.Sessions[name] = info
			saveState(stateDir, sf)
		}
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
	"start": `hangon start <type> [--name NAME] [--local] [--no-pty] [-- command...]

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
  hangon start tcp localhost:6379
  hangon start ws wss://echo.websocket.events
  hangon start macos TextEdit

Options:
  --name NAME   Name this session (default: "default")
  --no-pty      Process only: use raw pipes instead of PTY
  --local       Store state in ./.hangon/ instead of ~/.hangon/
`,
	"stop": `hangon stop [SESSION]

Stop a session and kill its holder process. Default session: "default".

Examples:
  hangon stop
  hangon stop server
`,
	"list": `hangon list

List all active sessions with their type, PID, status, and target.
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

Examples:
  hangon expect "ready"
  hangon expect "listening on port \d+"
  hangon expect server "200 OK" --timeout 60
  hangon expect ">>> " --timeout 5
`,
	"screen": `hangon screen [SESSION]

Get the current terminal screen content (process sessions with PTY only).
Returns the visible 80x24 terminal grid as plain text, with trailing
whitespace trimmed. This is essential for reading TUI applications.

Examples:
  hangon screen
  hangon screen myapp
`,
	"keys": `hangon keys [SESSION] <key-sequence>

Send special key sequences to the session. Multiple keys separated by spaces.

Available keys:
  ctrl-a through ctrl-z    Control key combinations
  ctrl-space               Control+Space (NUL)
  ctrl-up/down/left/right  Control+Arrow keys
  enter, return            Enter/Return key
  tab                      Tab key
  escape, esc              Escape key
  backspace, delete        Backspace/Delete keys
  up, down, left, right    Arrow keys
  shift-up/down/left/right Shift+Arrow keys
  shift-home, shift-end    Shift+Home/End
  alt-a through alt-z      Alt+letter combinations
  alt-. alt-, alt-= alt--  Alt+punctuation
  alt-up/down/left/right   Alt+Arrow keys
  home, end                Home/End keys
  pageup, pagedown         Page Up/Down
  insert                   Insert key
  space                    Space bar
  f1 through f12           Function keys

Examples:
  hangon keys ctrl-c
  hangon keys "ctrl-c enter"
  hangon keys myapp "up up enter"
  hangon keys "shift-right shift-right ctrl-c"
  hangon keys alt-f
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
    stopall                        Stop all sessions

  I/O:
    send [SESSION] <data>          Send raw data (no newline)
    sendline [SESSION] <text>      Send text + newline
    read [SESSION]                 Read new output since last read
    readall [SESSION]              Read entire output buffer
    stderr [SESSION]               Read new stderr (--no-pty only)
    expect [SESSION] <regex>       Wait for pattern (exit 1 on timeout)
    screen [SESSION]               Terminal screen as text (PTY only)
    keys [SESSION] <key...>        Send special keys (ctrl-c, up, etc.)
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

OUTPUT READING
  'read' returns only new output since the last read (cursored).
  'readall' returns the entire buffer. 'expect' blocks until a regex
  matches, then advances the cursor. See 'hangon help output'.

OPTIONS
  --name NAME    Session name (default: "default")
  --timeout SEC  Timeout for expect (default: 30)
  --no-pty       Process: use raw pipes instead of PTY
  --local        Use ./.hangon/ for state (project-scoped)

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
    HANGON_TIMEOUT   Default expect timeout (Go duration: "30s", "1m")
`

var topicKeys = `KEY SEQUENCES

  The 'keys' command sends special key sequences to the session.
  Multiple keys are separated by spaces.

  Control keys:
    ctrl-a through ctrl-z

  Navigation:
    up  down  left  right
    home  end  pageup  pagedown

  Editing:
    enter  return  tab  backspace  delete  insert  space  escape  esc

  Function keys:
    f1 through f12

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
    hangon keys "q"
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
