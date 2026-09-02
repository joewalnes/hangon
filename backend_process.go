package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// exitCodeUnknown is the sentinel exit code (and errSessionVanished the
// paired error) reported by Wait when a tmux-backed session's pane and
// session disappear without ever reporting pane_dead_status — e.g.
// `tmux kill-session` from outside hangon, the tmux server dying, or (before
// this fix existed) a swallowed remain-on-exit error letting the pane clean
// itself up the instant the command exits. In all of those cases the real
// exit status is genuinely unknowable, and it must never be confused with
// the ordinary, known exit code 0.
const exitCodeUnknown = -1

var errSessionVanished = fmt.Errorf("session terminated externally: exit status unknown")

// ProcessBackend manages a long-running process via tmux (preferred) or raw PTY/pipes.
//
// When tmux is available, it provides rich screen capture with full ANSI color
// and attribute support, enabling screenshot rendering. Output is streamed
// via tmux pipe-pane to a FIFO for real-time read/expect support.
//
// When tmux is not available, falls back to creack/pty (PTY mode) or raw pipes.
type ProcessBackend struct {
	command []string
	usePty  bool // only relevant in non-tmux mode

	// Mode flag.
	useTmux bool

	// tmux mode fields.
	tmuxSess string // tmux session name
	fifoPath string // FIFO for pipe-pane output streaming
	fifo     *os.File

	// Current terminal geometry. Set at construction from --cols/--rows
	// (default 80x24) and kept in sync by Resize: in tmux mode it's used
	// for ParseANSI (screenshot rendering) since capture-pane's ANSI dump
	// doesn't self-describe its dimensions; in legacy PTY mode it's the
	// size handed to pty.Start/pty.Setsize and the embedded Terminal.
	// Guarded by mu since Resize (from a resize RPC) can race Screenshot
	// (from a concurrent screenshot RPC).
	tmuxRows int
	tmuxCols int

	// PTY mode fields.
	cmd  *exec.Cmd
	ptmx *os.File  // PTY master (nil if usePty=false)
	term *Terminal // VT100 screen tracker (only in PTY mode)

	// Pipe mode fields.
	stdin io.WriteCloser

	// Common fields.
	output   *RingBuffer
	stderr   *RingBuffer // Only used in non-PTY, non-tmux mode.
	done     chan struct{}
	exitErr  error
	exitCode int // tmux mode: exit code from pane_dead_status
	mu       sync.Mutex
}

// NewProcessBackend creates a process backend. cols/rows set the initial
// terminal geometry; a value <= 0 falls back to the default (80x24).
func NewProcessBackend(command []string, usePty bool, cols, rows int) *ProcessBackend {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	return &ProcessBackend{
		command:  command,
		usePty:   usePty,
		tmuxRows: rows,
		tmuxCols: cols,
		output:   NewRingBuffer(defaultBufSize),
		done:     make(chan struct{}),
	}
}

func (pb *ProcessBackend) Start() error {
	// Prefer tmux when available and PTY mode is requested.
	if pb.usePty {
		if _, err := exec.LookPath("tmux"); err == nil {
			return pb.startWithTmux()
		}
	}
	return pb.startLegacy()
}

// --- tmux mode ---

func (pb *ProcessBackend) startWithTmux() error {
	pb.useTmux = true
	pb.tmuxSess = fmt.Sprintf("hangon-%d", os.Getpid())

	// Create FIFO for output streaming.
	pb.fifoPath = filepath.Join(os.TempDir(), pb.tmuxSess+".fifo")
	os.Remove(pb.fifoPath) // Clean up any stale FIFO.
	if err := syscall.Mkfifo(pb.fifoPath, 0o600); err != nil {
		return fmt.Errorf("create FIFO: %w", err)
	}

	// Build the command string for tmux.
	cmdStr := shellQuoteArgs(pb.command)

	// Start the pane behind a gate instead of running cmdStr directly.
	// tmux begins executing the pane's command the instant new-session
	// returns, but pipe-pane (and our FIFO reader) aren't wired up until
	// several tmux round-trips later. Anything the real command prints in
	// that window is written straight to the pane and never reaches the
	// FIFO — pipe-pane only streams output produced after it's enabled,
	// it does not replay backlog — so a fast command (or a startup
	// banner) can print, and even exit, entirely inside the gap and its
	// output is gone forever. `hangon read`/`expect` on that output then
	// hangs or times out with no way to recover the data.
	//
	// The gate is a shell `read` that blocks until we release it: the
	// pane starts running `read -r _hangon_start; exec cmdStr`, which
	// produces no output of its own and cannot advance past the `read`
	// until we send it a line. Once remain-on-exit, pipe-pane, and the
	// FIFO reader goroutine are all live, we release the gate with
	// send-keys "Enter" — only then does `exec` replace the placeholder
	// shell with the real command, guaranteeing no output can be produced
	// before pipe-pane is listening for it. `exec` (rather than a plain
	// invocation) also means pane_pid ends up being the real command
	// process, matching pre-existing TargetPID behavior.
	gate := "read -r _hangon_start; exec " + cmdStr

	// Start tmux session (on hangon's dedicated server, see tmux.go).
	tmux := tmuxCmd("new-session", "-d",
		"-s", pb.tmuxSess,
		"-x", strconv.Itoa(pb.tmuxCols),
		"-y", strconv.Itoa(pb.tmuxRows),
		gate)
	if err := tmux.Run(); err != nil {
		os.Remove(pb.fifoPath)
		return fmt.Errorf("tmux new-session: %w", err)
	}

	// Keep pane alive after the process exits so we can read the exit
	// code. A swallowed error here would silently defeat remain-on-exit:
	// the pane (and session) would vanish the instant the command exits,
	// which is indistinguishable downstream from the session having been
	// killed externally — exactly the "vanished session reports exit 0"
	// bug this commit fixes. Fail loudly instead.
	if err := tmuxCmd("set-option", "-t", tmuxExact(pb.tmuxSess), "remain-on-exit", "on").Run(); err != nil {
		pb.closeTmux()
		return fmt.Errorf("tmux set-option remain-on-exit: %w", err)
	}

	// Set up pipe-pane: stream pane output to our FIFO. Checking this
	// error (unlike the pre-existing set-option call above, left as-is
	// here) matters for the gate mechanism specifically: if pipe-pane
	// silently failed to wire up, releasing the gate below would let the
	// real command run with nothing capturing its output at all — the
	// exact loss this fix exists to prevent, just moved one step later.
	// pb.fifoPath is derived from os.TempDir(), which is influenced by the
	// TMPDIR environment variable — not a fixed, trusted literal. It's
	// interpolated into a string that tmux hands to `sh -c` (pipe-pane
	// runs its argument through the shell), so it must be single-quoted:
	// unquoted, a TMPDIR containing a space silently misdirects `cat`'s
	// output to the wrong file (the FIFO is never written to, so
	// read/expect hang or see nothing), and a TMPDIR containing shell
	// metacharacters would be shell injection. See shellSingleQuote.
	pipePaneCmd := "cat >> " + shellSingleQuote(pb.fifoPath)
	if err := tmuxCmd("pipe-pane", "-t", tmuxExact(pb.tmuxSess), pipePaneCmd).Run(); err != nil {
		pb.closeTmux()
		return fmt.Errorf("tmux pipe-pane: %w", err)
	}

	// Open FIFO for reading. O_RDWR avoids blocking on open (we're both reader and writer-capable).
	fifo, err := os.OpenFile(pb.fifoPath, os.O_RDWR, 0)
	if err != nil {
		pb.closeTmux()
		return fmt.Errorf("open FIFO: %w", err)
	}
	pb.fifo = fifo

	// Read from FIFO into ring buffer.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := fifo.Read(buf)
			if n > 0 {
				pb.output.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
	}()

	// Release the gate. pipe-pane and the FIFO reader are live now, so
	// any output the real command produces from this point on is
	// guaranteed to be captured. The pane briefly echoes this keystroke
	// itself (normal PTY local-echo, same as any typed input); that's a
	// harmless blank line ahead of the real command's own output, not a
	// loss.
	if err := tmuxCmd("send-keys", "-t", tmuxExact(pb.tmuxSess), "Enter").Run(); err != nil {
		pb.closeTmux()
		return fmt.Errorf("tmux send-keys (release start gate): %w", err)
	}

	// Monitor tmux pane for process exit. With remain-on-exit, the session
	// stays alive after the process dies, so we poll pane_dead and read the
	// exit status from pane_dead_status.
	go func() {
		for {
			time.Sleep(500 * time.Millisecond)
			if !pb.tmuxSessionExists() {
				// The session vanished without ever reporting
				// pane_dead_status (killed externally, tmux server
				// died, remain-on-exit didn't take, ...). This is
				// NOT exit code 0 — the real status is unknown, and
				// reporting success here previously made `hangon
				// wait`/`status` claim a killed session succeeded.
				pb.mu.Lock()
				pb.exitCode = exitCodeUnknown
				pb.exitErr = errSessionVanished
				pb.mu.Unlock()
				close(pb.done)
				return
			}
			// Check if the pane's process has exited, and if so its exit
			// status, in one round-trip (also avoids a second, separate
			// has-session-passed-but-pane-gone race between the two
			// display calls this used to be split across).
			out, err := tmuxCmd("display", "-t", tmuxExact(pb.tmuxSess), "-p", "#{pane_dead},#{pane_dead_status}").Output()
			if err != nil {
				continue
			}
			fields := strings.SplitN(strings.TrimSpace(string(out)), ",", 2)
			if len(fields) == 2 && fields[0] == "1" {
				code, _ := strconv.Atoi(fields[1])
				pb.mu.Lock()
				pb.exitCode = code
				pb.mu.Unlock()
				close(pb.done)
				return
			}
		}
	}()

	return nil
}

func (pb *ProcessBackend) tmuxSessionExists() bool {
	cmd := tmuxCmd("has-session", "-t", tmuxExact(pb.tmuxSess))
	return cmd.Run() == nil
}

func (pb *ProcessBackend) sendTmux(data []byte) error {
	if bytes.ContainsRune(data, 0) {
		// NUL bytes cannot survive in exec argv (C strings are NUL-terminated).
		// Use tmux load-buffer from stdin + paste-buffer instead.
		// load-buffer's -t is a target-client, not a session; buffers
		// are server-global, so no target is needed (or meaningful).
		loadCmd := tmuxCmd("load-buffer", "-b", "_hangon_nul", "-")
		loadCmd.Stdin = bytes.NewReader(data)
		if err := loadCmd.Run(); err != nil {
			return fmt.Errorf("tmux load-buffer: %w", err)
		}
		pasteCmd := tmuxCmd("paste-buffer", "-t", tmuxExact(pb.tmuxSess), "-b", "_hangon_nul", "-d")
		return pasteCmd.Run()
	}
	// tmux send-keys -l sends literal text (no key name interpretation).
	cmd := tmuxCmd("send-keys", "-t", tmuxExact(pb.tmuxSess), "-l", string(data))
	return cmd.Run()
}

func (pb *ProcessBackend) sendKeysTmux(keys string) error {
	for _, key := range strings.Fields(keys) {
		tmuxKey, ok := tmuxKeyMap[strings.ToLower(key)]
		if !ok {
			return fmt.Errorf("unknown key: %s", key)
		}
		cmd := tmuxCmd("send-keys", "-t", tmuxExact(pb.tmuxSess), tmuxKey)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("send key %s: %w", key, err)
		}
	}
	return nil
}

func (pb *ProcessBackend) screenTmux() (string, error) {
	cmd := tmuxCmd("capture-pane", "-t", tmuxExact(pb.tmuxSess), "-p")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("capture-pane: %w", err)
	}
	return string(out), nil
}

// screenAnsiTmux returns the screen with ANSI color/style escape codes.
//
// -N tells tmux to preserve trailing spaces at the end of each line. Without
// it, tmux silently trims trailing whitespace-only cells from the capture —
// discarding their background color along with them. For any full-frame app
// that pads lines with colored blank space (status bars, filled panels, most
// realistic TUI layouts), that trimming drops the background color for the
// trimmed region entirely, and it renders back as RenderConfig's default
// background instead of the pane's actual color. -J (not used here) would
// additionally join wrapped lines, which would break our fixed row/col grid
// model, so we only pass -N.
func (pb *ProcessBackend) screenAnsiTmux() (string, error) {
	cmd := tmuxCmd(tmuxCaptureAnsiArgs(tmuxExact(pb.tmuxSess))...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("capture-pane -e: %w", err)
	}
	return string(out), nil
}

// tmuxCaptureAnsiArgs builds the `tmux capture-pane` args used to capture a
// screenshot-ready, color-preserving dump of a pane. Split out from
// screenAnsiTmux so the flags — in particular -N, see its comment above —
// can be asserted on directly in a unit test without needing a live tmux
// session.
func tmuxCaptureAnsiArgs(sess string) []string {
	return []string{"capture-pane", "-t", sess, "-e", "-p", "-N"}
}

// cursorPosTmux returns the cursor position from tmux.
func (pb *ProcessBackend) cursorPosTmux() (int, int, error) {
	cmd := tmuxCmd("display", "-t", tmuxExact(pb.tmuxSess), "-p", "#{cursor_x},#{cursor_y}")
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected cursor output: %s", out)
	}
	x, _ := strconv.Atoi(parts[0])
	y, _ := strconv.Atoi(parts[1])
	return y, x, nil // row, col
}

func (pb *ProcessBackend) targetPIDTmux() int {
	cmd := tmuxCmd("display", "-t", tmuxExact(pb.tmuxSess), "-p", "#{pane_pid}")
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return pid
}

func (pb *ProcessBackend) closeTmux() {
	tmuxCmd("kill-session", "-t", tmuxExact(pb.tmuxSess)).Run()
	if pb.fifo != nil {
		pb.fifo.Close()
	}
	os.Remove(pb.fifoPath)
}

// --- Legacy PTY/pipe mode ---

func (pb *ProcessBackend) startLegacy() error {
	pb.cmd = exec.Command(pb.command[0], pb.command[1:]...)

	if pb.usePty {
		pb.term = NewTerminal(pb.tmuxRows, pb.tmuxCols)

		ptmx, err := pty.StartWithSize(pb.cmd, &pty.Winsize{
			Rows: uint16(pb.tmuxRows),
			Cols: uint16(pb.tmuxCols),
		})
		if err != nil {
			return fmt.Errorf("pty start: %w", err)
		}
		pb.ptmx = ptmx

		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := ptmx.Read(buf)
				if n > 0 {
					pb.output.Write(buf[:n])
					pb.term.Write(buf[:n])
				}
				if err != nil {
					break
				}
			}
			pb.mu.Lock()
			pb.exitErr = pb.cmd.Wait()
			pb.mu.Unlock()
			close(pb.done)
		}()
	} else {
		pb.stderr = NewRingBuffer(defaultBufSize)

		stdin, err := pb.cmd.StdinPipe()
		if err != nil {
			return fmt.Errorf("stdin pipe: %w", err)
		}
		pb.stdin = stdin

		stdout, err := pb.cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("stdout pipe: %w", err)
		}

		stderrPipe, err := pb.cmd.StderrPipe()
		if err != nil {
			return fmt.Errorf("stderr pipe: %w", err)
		}

		if err := pb.cmd.Start(); err != nil {
			return fmt.Errorf("start: %w", err)
		}

		go func() {
			io.Copy(pb.output, stdout)
		}()

		go func() {
			io.Copy(pb.stderr, stderrPipe)
		}()

		go func() {
			pb.mu.Lock()
			pb.exitErr = pb.cmd.Wait()
			pb.mu.Unlock()
			close(pb.done)
		}()
	}

	return nil
}

// --- Backend interface ---

func (pb *ProcessBackend) Send(data []byte) error {
	if pb.useTmux {
		return pb.sendTmux(data)
	}
	if pb.usePty {
		_, err := pb.ptmx.Write(data)
		return err
	}
	if pb.stdin == nil {
		return fmt.Errorf("stdin not available")
	}
	_, err := pb.stdin.Write(data)
	return err
}

func (pb *ProcessBackend) Output() *RingBuffer {
	return pb.output
}

func (pb *ProcessBackend) Stderr() *RingBuffer {
	return pb.stderr
}

func (pb *ProcessBackend) Screen() (string, error) {
	if pb.useTmux {
		return pb.screenTmux()
	}
	if pb.term == nil {
		return "", ErrNotSupported
	}
	return pb.term.String(), nil
}

func (pb *ProcessBackend) SendKeys(keys string) error {
	if pb.useTmux {
		return pb.sendKeysTmux(keys)
	}
	for _, key := range strings.Fields(keys) {
		b, ok := keyMap[strings.ToLower(key)]
		if !ok {
			return fmt.Errorf("unknown key: %s", key)
		}
		if err := pb.Send(b); err != nil {
			return err
		}
	}
	return nil
}

func (pb *ProcessBackend) Alive() bool {
	select {
	case <-pb.done:
		return false
	default:
		return true
	}
}

func (pb *ProcessBackend) Wait() (int, error) {
	<-pb.done
	pb.mu.Lock()
	defer pb.mu.Unlock()
	// In tmux mode, exitCode is set from pane_dead_status, or to
	// exitCodeUnknown alongside errSessionVanished if the session
	// disappeared without ever reporting one (see the poll goroutine in
	// startWithTmux).
	if pb.useTmux {
		return pb.exitCode, pb.exitErr
	}
	if pb.exitErr == nil {
		return 0, nil
	}
	if exitErr, ok := pb.exitErr.(*exec.ExitError); ok {
		return exitErr.ExitCode(), nil
	}
	return -1, pb.exitErr
}

func (pb *ProcessBackend) TargetPID() int {
	if pb.useTmux {
		return pb.targetPIDTmux()
	}
	if pb.cmd != nil && pb.cmd.Process != nil {
		return pb.cmd.Process.Pid
	}
	return 0
}

func (pb *ProcessBackend) Close() error {
	if pb.useTmux {
		pb.closeTmux()
		return nil
	}
	if pb.cmd != nil && pb.cmd.Process != nil {
		pb.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-pb.done:
			// Process exited cleanly after SIGTERM.
		case <-time.After(2 * time.Second):
			pb.cmd.Process.Kill()
			<-pb.done
		}
	}
	if pb.ptmx != nil {
		pb.ptmx.Close()
	}
	if pb.stdin != nil {
		pb.stdin.Close()
	}
	return nil
}

// Screenshot captures the screen with ANSI colors and renders to PNG/SVG.
func (pb *ProcessBackend) Screenshot(file string) (string, error) {
	if !pb.useTmux {
		return "", fmt.Errorf("screenshot requires tmux (available when starting without --no-pty)")
	}

	if file == "" {
		file = "screenshot.png"
	}

	ansi, err := pb.screenAnsiTmux()
	if err != nil {
		return "", err
	}

	pb.mu.Lock()
	rows, cols := pb.tmuxRows, pb.tmuxCols
	pb.mu.Unlock()

	curR, curC, curErr := pb.cursorPosTmux()
	grid := ParseANSI(ansi, rows, cols)
	if curErr == nil {
		grid.HasCursor = true
		grid.CursorR = curR
		grid.CursorC = curC
	}

	return RenderPNG(grid, DefaultRenderConfig, file)
}

// Resize changes the session's terminal geometry after it has started.
//
// In tmux mode this runs `resize-window` on hangon's dedicated tmux
// server (always via tmuxCmd/tmuxExact, never a bare exec.Command("tmux",
// ...) — see tmux.go) and updates tmuxCols/tmuxRows so that Screenshot's
// ParseANSI call and screen captures reflect the new size; tmux delivers
// SIGWINCH to the pane's process itself once the window is resized, the
// same as any terminal emulator would.
//
// In legacy PTY mode (no tmux, or tmux unavailable at start time) it
// calls pty.Setsize on the master, which is a single TIOCSWINSZ ioctl;
// the kernel raises SIGWINCH to the foreground process group of the
// slave side as a side effect of that ioctl, so no separate signal is
// needed. The embedded Terminal's grid is reallocated to match so
// `hangon screen` reflects the new dimensions too.
//
// Non-PTY pipe mode (--no-pty, no tmux) has no terminal at all, so
// there's nothing to resize.
func (pb *ProcessBackend) Resize(cols, rows int) error {
	if pb.useTmux {
		cmd := tmuxCmd("resize-window", "-t", tmuxExact(pb.tmuxSess), "-x", strconv.Itoa(cols), "-y", strconv.Itoa(rows))
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("tmux resize-window: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		pb.mu.Lock()
		pb.tmuxCols = cols
		pb.tmuxRows = rows
		pb.mu.Unlock()
		return nil
	}
	if pb.usePty {
		if pb.ptmx == nil {
			return fmt.Errorf("resize: pty not started")
		}
		if err := pty.Setsize(pb.ptmx, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)}); err != nil {
			return fmt.Errorf("pty setsize: %w", err)
		}
		pb.mu.Lock()
		pb.tmuxCols = cols
		pb.tmuxRows = rows
		pb.mu.Unlock()
		if pb.term != nil {
			pb.term.Resize(rows, cols)
		}
		return nil
	}
	return fmt.Errorf("resize not supported without tmux or a PTY (session started with --no-pty)")
}

// --- Utilities ---

// shellSingleQuote escapes s for safe interpolation inside a POSIX shell
// command line, using single quotes. Single quotes are the only POSIX
// quoting form with no special characters at all inside them (no escapes,
// no expansion, no globbing) — the sole exception is that a single quote
// itself cannot appear inside a single-quoted string. The standard idiom
// handles that: close the quote, emit an escaped literal quote, reopen the
// quote. The result is safe to embed directly in a shell command string
// regardless of what s contains — spaces, `$(...)`, backticks, `;`, other
// quotes, etc.
//
// Note: this does not (and does not need to) handle newlines specially;
// a literal newline inside single quotes is passed through as data, which
// is exactly the correct, safe behavior — it does not terminate the quote
// or the command.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shellQuoteArgs joins args into a shell-safe command string for tmux.
func shellQuoteArgs(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t\n\"'\\$`!#&|;(){}[]<>?*~") {
			quoted[i] = "'" + strings.ReplaceAll(a, "'", "'\\''") + "'"
		} else {
			quoted[i] = a
		}
	}
	return strings.Join(quoted, " ")
}

// tmuxKeyMap maps our key names to tmux send-keys key names.
var tmuxKeyMap = map[string]string{
	"enter":     "Enter",
	"return":    "Enter",
	"tab":       "Tab",
	"escape":    "Escape",
	"esc":       "Escape",
	"backspace": "BSpace",
	"delete":    "DC",
	"up":        "Up",
	"down":      "Down",
	"right":     "Right",
	"left":      "Left",
	"home":      "Home",
	"end":       "End",
	"pageup":    "PPage",
	"pagedown":  "NPage",
	"insert":    "IC",
	"space":     "Space",
	"ctrl-a":    "C-a",
	"ctrl-b":    "C-b",
	"ctrl-c":    "C-c",
	"ctrl-d":    "C-d",
	"ctrl-e":    "C-e",
	"ctrl-f":    "C-f",
	"ctrl-g":    "C-g",
	"ctrl-h":    "C-h",
	"ctrl-i":    "C-i",
	"ctrl-j":    "C-j",
	"ctrl-k":    "C-k",
	"ctrl-l":    "C-l",
	"ctrl-m":    "C-m",
	"ctrl-n":    "C-n",
	"ctrl-o":    "C-o",
	"ctrl-p":    "C-p",
	"ctrl-q":    "C-q",
	"ctrl-r":    "C-r",
	"ctrl-s":    "C-s",
	"ctrl-t":    "C-t",
	"ctrl-u":    "C-u",
	"ctrl-v":    "C-v",
	"ctrl-w":    "C-w",
	"ctrl-x":    "C-x",
	"ctrl-y":    "C-y",
	"ctrl-z":    "C-z",
	"f1":        "F1",
	"f2":        "F2",
	"f3":        "F3",
	"f4":        "F4",
	"f5":        "F5",
	"f6":        "F6",
	"f7":        "F7",
	"f8":        "F8",
	"f9":        "F9",
	"f10":       "F10",
	"f11":       "F11",
	"f12":       "F12",
	// Modifier combos: shift+arrow/nav
	"shift-up":    "S-Up",
	"shift-down":  "S-Down",
	"shift-right": "S-Right",
	"shift-left":  "S-Left",
	"shift-home":  "S-Home",
	"shift-end":   "S-End",
	// Modifier combos: alt+arrow/nav
	"alt-up":    "M-Up",
	"alt-down":  "M-Down",
	"alt-right": "M-Right",
	"alt-left":  "M-Left",
	// Modifier combos: ctrl+arrow/nav
	"ctrl-up":    "C-Up",
	"ctrl-down":  "C-Down",
	"ctrl-right": "C-Right",
	"ctrl-left":  "C-Left",
	// ctrl-space
	"ctrl-space": "C-Space",
	// alt+letter
	"alt-a": "M-a", "alt-b": "M-b", "alt-c": "M-c", "alt-d": "M-d",
	"alt-e": "M-e", "alt-f": "M-f", "alt-g": "M-g", "alt-h": "M-h",
	"alt-i": "M-i", "alt-j": "M-j", "alt-k": "M-k", "alt-l": "M-l",
	"alt-m": "M-m", "alt-n": "M-n", "alt-o": "M-o", "alt-p": "M-p",
	"alt-q": "M-q", "alt-r": "M-r", "alt-s": "M-s", "alt-t": "M-t",
	"alt-u": "M-u", "alt-v": "M-v", "alt-w": "M-w", "alt-x": "M-x",
	"alt-y": "M-y", "alt-z": "M-z",
	// alt+punctuation
	"alt-.": "M-.", "alt-,": "M-,", "alt-=": "M-=", "alt--": "M--",
}

// keyMap maps key names to raw byte sequences (used in legacy PTY/pipe mode).
var keyMap = map[string][]byte{
	"enter":     {'\n'},
	"return":    {'\n'},
	"tab":       {'\t'},
	"escape":    {0x1b},
	"esc":       {0x1b},
	"backspace": {0x7f},
	"delete":    {0x1b, '[', '3', '~'},
	"up":        {0x1b, '[', 'A'},
	"down":      {0x1b, '[', 'B'},
	"right":     {0x1b, '[', 'C'},
	"left":      {0x1b, '[', 'D'},
	"home":      {0x1b, '[', 'H'},
	"end":       {0x1b, '[', 'F'},
	"pageup":    {0x1b, '[', '5', '~'},
	"pagedown":  {0x1b, '[', '6', '~'},
	"insert":    {0x1b, '[', '2', '~'},
	"ctrl-a":    {0x01},
	"ctrl-b":    {0x02},
	"ctrl-c":    {0x03},
	"ctrl-d":    {0x04},
	"ctrl-e":    {0x05},
	"ctrl-f":    {0x06},
	"ctrl-g":    {0x07},
	"ctrl-h":    {0x08},
	"ctrl-i":    {0x09},
	"ctrl-j":    {0x0a},
	"ctrl-k":    {0x0b},
	"ctrl-l":    {0x0c},
	"ctrl-m":    {0x0d},
	"ctrl-n":    {0x0e},
	"ctrl-o":    {0x0f},
	"ctrl-p":    {0x10},
	"ctrl-q":    {0x11},
	"ctrl-r":    {0x12},
	"ctrl-s":    {0x13},
	"ctrl-t":    {0x14},
	"ctrl-u":    {0x15},
	"ctrl-v":    {0x16},
	"ctrl-w":    {0x17},
	"ctrl-x":    {0x18},
	"ctrl-y":    {0x19},
	"ctrl-z":    {0x1a},
	"space":     {' '},
	// Modifier combos: shift+arrow/nav (modifier 2 = shift)
	"shift-up":    {0x1b, '[', '1', ';', '2', 'A'},
	"shift-down":  {0x1b, '[', '1', ';', '2', 'B'},
	"shift-right": {0x1b, '[', '1', ';', '2', 'C'},
	"shift-left":  {0x1b, '[', '1', ';', '2', 'D'},
	"shift-home":  {0x1b, '[', '1', ';', '2', 'H'},
	"shift-end":   {0x1b, '[', '1', ';', '2', 'F'},
	// Modifier combos: alt+arrow/nav (modifier 3 = alt)
	"alt-up":    {0x1b, '[', '1', ';', '3', 'A'},
	"alt-down":  {0x1b, '[', '1', ';', '3', 'B'},
	"alt-right": {0x1b, '[', '1', ';', '3', 'C'},
	"alt-left":  {0x1b, '[', '1', ';', '3', 'D'},
	// Modifier combos: ctrl+arrow/nav (modifier 5 = ctrl)
	"ctrl-up":    {0x1b, '[', '1', ';', '5', 'A'},
	"ctrl-down":  {0x1b, '[', '1', ';', '5', 'B'},
	"ctrl-right": {0x1b, '[', '1', ';', '5', 'C'},
	"ctrl-left":  {0x1b, '[', '1', ';', '5', 'D'},
	// ctrl-space = NUL byte
	"ctrl-space": {0x00},
	// alt+letter = ESC followed by letter
	"alt-a": {0x1b, 'a'}, "alt-b": {0x1b, 'b'}, "alt-c": {0x1b, 'c'}, "alt-d": {0x1b, 'd'},
	"alt-e": {0x1b, 'e'}, "alt-f": {0x1b, 'f'}, "alt-g": {0x1b, 'g'}, "alt-h": {0x1b, 'h'},
	"alt-i": {0x1b, 'i'}, "alt-j": {0x1b, 'j'}, "alt-k": {0x1b, 'k'}, "alt-l": {0x1b, 'l'},
	"alt-m": {0x1b, 'm'}, "alt-n": {0x1b, 'n'}, "alt-o": {0x1b, 'o'}, "alt-p": {0x1b, 'p'},
	"alt-q": {0x1b, 'q'}, "alt-r": {0x1b, 'r'}, "alt-s": {0x1b, 's'}, "alt-t": {0x1b, 't'},
	"alt-u": {0x1b, 'u'}, "alt-v": {0x1b, 'v'}, "alt-w": {0x1b, 'w'}, "alt-x": {0x1b, 'x'},
	"alt-y": {0x1b, 'y'}, "alt-z": {0x1b, 'z'},
	// alt+punctuation
	"alt-.": {0x1b, '.'}, "alt-,": {0x1b, ','}, "alt-=": {0x1b, '='}, "alt--": {0x1b, '-'},
	"f1":  {0x1b, 'O', 'P'},
	"f2":  {0x1b, 'O', 'Q'},
	"f3":  {0x1b, 'O', 'R'},
	"f4":  {0x1b, 'O', 'S'},
	"f5":  {0x1b, '[', '1', '5', '~'},
	"f6":  {0x1b, '[', '1', '7', '~'},
	"f7":  {0x1b, '[', '1', '8', '~'},
	"f8":  {0x1b, '[', '1', '9', '~'},
	"f9":  {0x1b, '[', '2', '0', '~'},
	"f10": {0x1b, '[', '2', '1', '~'},
	"f11": {0x1b, '[', '2', '3', '~'},
	"f12": {0x1b, '[', '2', '4', '~'},
}
