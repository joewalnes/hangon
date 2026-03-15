package main

import (
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

func NewProcessBackend(command []string, usePty bool) *ProcessBackend {
	return &ProcessBackend{
		command:  command,
		usePty:   usePty,
		tmuxRows: 24,
		tmuxCols: 80,
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

	// Start tmux session.
	tmux := exec.Command("tmux", "new-session", "-d",
		"-s", pb.tmuxSess,
		"-x", strconv.Itoa(pb.tmuxCols),
		"-y", strconv.Itoa(pb.tmuxRows),
		cmdStr)
	if err := tmux.Run(); err != nil {
		os.Remove(pb.fifoPath)
		return fmt.Errorf("tmux new-session: %w", err)
	}

	// Keep pane alive after the process exits so we can read the exit code.
	exec.Command("tmux", "set-option", "-t", pb.tmuxSess, "remain-on-exit", "on").Run()

	// Set up pipe-pane: stream pane output to our FIFO.
	pipePaneCmd := fmt.Sprintf("cat >> %s", pb.fifoPath)
	exec.Command("tmux", "pipe-pane", "-t", pb.tmuxSess, pipePaneCmd).Run()

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

	// Monitor tmux pane for process exit. With remain-on-exit, the session
	// stays alive after the process dies, so we poll pane_dead and read the
	// exit status from pane_dead_status.
	go func() {
		for {
			time.Sleep(500 * time.Millisecond)
			if !pb.tmuxSessionExists() {
				// Session was killed externally.
				close(pb.done)
				return
			}
			// Check if the pane's process has exited.
			out, err := exec.Command("tmux", "display", "-t", pb.tmuxSess, "-p", "#{pane_dead}").Output()
			if err != nil {
				continue
			}
			if strings.TrimSpace(string(out)) == "1" {
				// Process exited. Read the exit status.
				statusOut, err := exec.Command("tmux", "display", "-t", pb.tmuxSess, "-p", "#{pane_dead_status}").Output()
				if err == nil {
					code, _ := strconv.Atoi(strings.TrimSpace(string(statusOut)))
					if code != 0 {
						pb.mu.Lock()
						pb.exitErr = &exec.ExitError{}
						pb.exitCode = code
						pb.mu.Unlock()
					}
				}
				close(pb.done)
				return
			}
		}
	}()

	return nil
}

func (pb *ProcessBackend) tmuxSessionExists() bool {
	cmd := exec.Command("tmux", "has-session", "-t", pb.tmuxSess)
	return cmd.Run() == nil
}

func (pb *ProcessBackend) sendTmux(data []byte) error {
	// tmux send-keys -l sends literal text (no key name interpretation).
	cmd := exec.Command("tmux", "send-keys", "-t", pb.tmuxSess, "-l", string(data))
	return cmd.Run()
}

func (pb *ProcessBackend) sendKeysTmux(keys string) error {
	for _, key := range strings.Fields(keys) {
		tmuxKey, ok := tmuxKeyMap[strings.ToLower(key)]
		if !ok {
			return fmt.Errorf("unknown key: %s", key)
		}
		cmd := exec.Command("tmux", "send-keys", "-t", pb.tmuxSess, tmuxKey)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("send key %s: %w", key, err)
		}
	}
	return nil
}

func (pb *ProcessBackend) screenTmux() (string, error) {
	cmd := exec.Command("tmux", "capture-pane", "-t", pb.tmuxSess, "-p")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("capture-pane: %w", err)
	}
	return string(out), nil
}

// screenAnsiTmux returns the screen with ANSI color/style escape codes.
func (pb *ProcessBackend) screenAnsiTmux() (string, error) {
	cmd := exec.Command("tmux", "capture-pane", "-t", pb.tmuxSess, "-e", "-p")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("capture-pane -e: %w", err)
	}
	return string(out), nil
}

// cursorPosTmux returns the cursor position from tmux.
func (pb *ProcessBackend) cursorPosTmux() (int, int, error) {
	cmd := exec.Command("tmux", "display", "-t", pb.tmuxSess, "-p", "#{cursor_x},#{cursor_y}")
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
	cmd := exec.Command("tmux", "display", "-t", pb.tmuxSess, "-p", "#{pane_pid}")
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return pid
}

func (pb *ProcessBackend) closeTmux() {
	exec.Command("tmux", "kill-session", "-t", pb.tmuxSess).Run()
	if pb.fifo != nil {
		pb.fifo.Close()
	}
	os.Remove(pb.fifoPath)
}

// --- Legacy PTY/pipe mode ---

func (pb *ProcessBackend) startLegacy() error {
	pb.cmd = exec.Command(pb.command[0], pb.command[1:]...)

	if pb.usePty {
		pb.term = NewTerminal(24, 80)

		ptmx, err := pty.Start(pb.cmd)
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
	// In tmux mode, exitCode is set from pane_dead_status.
	if pb.useTmux {
		return pb.exitCode, nil
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

	curR, curC, curErr := pb.cursorPosTmux()
	grid := ParseANSI(ansi, pb.tmuxRows, pb.tmuxCols)
	if curErr == nil {
		grid.HasCursor = true
		grid.CursorR = curR
		grid.CursorC = curC
	}

	return RenderPNG(grid, DefaultRenderConfig, file)
}

// --- Utilities ---

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
	"f1":        {0x1b, 'O', 'P'},
	"f2":        {0x1b, 'O', 'Q'},
	"f3":        {0x1b, 'O', 'R'},
	"f4":        {0x1b, 'O', 'S'},
	"f5":        {0x1b, '[', '1', '5', '~'},
	"f6":        {0x1b, '[', '1', '7', '~'},
	"f7":        {0x1b, '[', '1', '8', '~'},
	"f8":        {0x1b, '[', '1', '9', '~'},
	"f9":        {0x1b, '[', '2', '0', '~'},
	"f10":       {0x1b, '[', '2', '1', '~'},
	"f11":       {0x1b, '[', '2', '3', '~'},
	"f12":       {0x1b, '[', '2', '4', '~'},
}
