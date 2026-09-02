package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestIntegration_ProcessSession tests the full lifecycle:
// start holder → send → read → expect → screen → keys → alive → stop.
// This test requires tmux to be installed.
func TestIntegration_ProcessSession(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed, skipping integration test")
	}

	// Build the binary.
	binary := filepath.Join(t.TempDir(), "hangon")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %s\n%s", err, out)
	}

	stateDir := t.TempDir()
	name := "integration-test"

	run := func(args ...string) (string, error) {
		cmd := exec.Command(binary, args...)
		cmd.Env = append(os.Environ(), "HOME="+stateDir)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}

	// Start a session.
	out, err := run("start", "process", "--name", name, "--", "python3", "-i")
	if err != nil {
		t.Fatalf("start failed: %s\n%s", err, out)
	}
	if !strings.Contains(out, "started") {
		t.Fatalf("unexpected start output: %s", out)
	}

	// Cleanup on exit.
	defer run("stop", name)

	// Wait for Python to be ready.
	out, err = run("expect", name, ">>>", "--timeout", "10")
	if err != nil {
		t.Fatalf("expect >>> failed: %s\n%s", err, out)
	}

	// List sessions.
	out, err = run("list")
	if err != nil {
		t.Fatalf("list failed: %s\n%s", err, out)
	}
	if !strings.Contains(out, name) {
		t.Errorf("list doesn't contain session name: %s", out)
	}

	// Send a command.
	out, err = run("sendline", name, "2 + 2")
	if err != nil {
		t.Fatalf("sendline failed: %s\n%s", err, out)
	}

	// Expect the result.
	out, err = run("expect", name, "4", "--timeout", "5")
	if err != nil {
		t.Fatalf("expect 4 failed: %s\n%s", err, out)
	}

	// expect only consumes through the end of its match ("4"); it must not
	// swallow the bytes that follow (the trailing newline and the next
	// ">>> " prompt), so a subsequent read should still see them.
	out, err = run("read", name)
	if err != nil {
		t.Fatalf("read failed: %s", err)
	}
	if !strings.Contains(out, ">>>") {
		t.Errorf("read after expect lost trailing output (want next prompt): %q", out)
	}

	// Screen should show terminal content.
	out, err = run("screen", name)
	if err != nil {
		t.Fatalf("screen failed: %s\n%s", err, out)
	}
	if !strings.Contains(out, ">>>") {
		t.Errorf("screen doesn't contain >>>: %s", out)
	}

	// Alive should return true.
	out, err = run("alive", name)
	if err != nil {
		t.Fatalf("alive failed: %s\n%s", err, out)
	}
	if out != "true" {
		t.Errorf("alive=%q, want true", out)
	}

	// Status.
	out, err = run("status", name)
	if err != nil {
		t.Fatalf("status failed: %s\n%s", err, out)
	}
	if !strings.Contains(out, "process") {
		t.Errorf("status doesn't show type: %s", out)
	}

	// Screenshot (to SVG since we likely don't have rsvg-convert in CI).
	screenshotPath := filepath.Join(t.TempDir(), "test-screenshot.png")
	out, err = run("screenshot", name, screenshotPath)
	if err != nil {
		t.Fatalf("screenshot failed: %s\n%s", err, out)
	}
	// Should have created either a .png or .svg file.
	if !strings.HasSuffix(out, ".svg") && !strings.HasSuffix(out, ".png") {
		t.Errorf("screenshot output doesn't look like a file path: %s", out)
	}

	// Send ctrl-d to exit Python.
	_, err = run("keys", name, "ctrl-d")
	if err != nil {
		t.Fatalf("keys failed: %s", err)
	}

	// Wait briefly for exit.
	time.Sleep(1 * time.Second)

	// Alive should now return false.
	_, err = run("alive", name)
	if err == nil {
		// alive returns exit 1 when not alive, which is an error.
		// If no error, it's still alive — that's unexpected.
		t.Log("process still alive after ctrl-d, proceeding to stop")
	}

	// Stop.
	out, err = run("stop", name)
	if err != nil {
		t.Fatalf("stop failed: %s\n%s", err, out)
	}
}

// TestIntegration_Resize reproduces the downstream regression from
// TODO.md ("No way to resize a running session's terminal") and proves
// the fix end to end: a process session starts at the default 80x24,
// `hangon resize` changes both the target process's view of its own
// terminal size (via shutil.get_terminal_size(), which reads the PTY's
// TIOCGWINSZ the same way any real program would) and the underlying
// tmux window geometry (verified directly against hangon's dedicated
// tmux server, bypassing hangon entirely) and the `screen` output's
// width.
//
// Before this fix, `resize` isn't a recognized subcommand at all, so
// this test fails at the very first `run("resize", ...)` call with
// "unknown command" — which is itself the regression: a downstream
// caller had no supported way to resize a session's terminal after
// commit 21ddf4e moved sessions onto hangon's own dedicated tmux server
// socket (making the caller's direct `tmux resize-window -t
// "hangon-$PID"` against the *default* server silently find nothing).
func TestIntegration_Resize(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed, skipping integration test")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed, skipping integration test")
	}

	_, run := buildHangonForTest(t)
	name := "resize-test"

	out, err := run(nil, "start", "process", "--name", name, "--", "python3", "-i")
	if err != nil {
		t.Fatalf("start failed: %s\n%s", err, out)
	}
	defer run(nil, "stop", name)

	if out, err := run(nil, "expect", name, ">>>", "--timeout", "10"); err != nil {
		t.Fatalf("expect >>> failed: %s\n%s", err, out)
	}

	// Default size is 80x24.
	sizeRe := `columns=(\d+), lines=(\d+)\)`
	out, err = run(nil, "sendline", name, "import shutil; print(shutil.get_terminal_size())")
	if err != nil {
		t.Fatalf("sendline failed: %s\n%s", err, out)
	}
	out, err = run(nil, "expect", name, sizeRe, "--timeout", "5")
	if err != nil {
		t.Fatalf("expect terminal_size failed: %s\n%s", err, out)
	}
	if !strings.Contains(out, "columns=80, lines=24") {
		t.Fatalf("default terminal size wrong: got %q, want columns=80, lines=24", out)
	}

	// Resize to a distinctive, non-default size.
	out, err = run(nil, "resize", name, "--cols", "120", "--rows", "40")
	if err != nil {
		t.Fatalf("resize failed: %s\n%s", err, out)
	}
	if !strings.Contains(out, "120") || !strings.Contains(out, "40") {
		t.Errorf("resize output doesn't mention the new size: %s", out)
	}

	// The target process itself now sees the new size (proves the
	// resize reached the real PTY, not just hangon's bookkeeping).
	out, err = run(nil, "sendline", name, "print(shutil.get_terminal_size())")
	if err != nil {
		t.Fatalf("sendline failed: %s\n%s", err, out)
	}
	out, err = run(nil, "expect", name, sizeRe, "--timeout", "5")
	if err != nil {
		t.Fatalf("expect terminal_size failed: %s\n%s", err, out)
	}
	if !strings.Contains(out, "columns=120, lines=40") {
		t.Fatalf("post-resize terminal size wrong: got %q, want columns=120, lines=40", out)
	}

	// Verify the underlying tmux window geometry directly, independent
	// of what the target process reports, using the tmux session's
	// pid — this rules out a bug where hangon updates its own
	// bookkeeping (tmuxCols/tmuxRows) but never actually resized tmux.
	statusOut, err := run(nil, "status", name)
	if err != nil {
		t.Fatalf("status failed: %s\n%s", err, statusOut)
	}
	holderPID := ""
	for _, line := range strings.Split(statusOut, "\n") {
		if strings.HasPrefix(line, "Holder PID:") {
			holderPID = strings.TrimSpace(strings.TrimPrefix(line, "Holder PID:"))
		}
	}
	if holderPID == "" {
		t.Fatalf("could not find holder PID in status output: %s", statusOut)
	}
	tmuxSess := "hangon-" + holderPID
	geomOut, err := tmuxCmd("display", "-t", tmuxExact(tmuxSess), "-p", "#{window_width}x#{window_height}").Output()
	if err != nil {
		t.Fatalf("tmux display failed: %v", err)
	}
	if got := strings.TrimSpace(string(geomOut)); got != "120x40" {
		t.Errorf("tmux window geometry = %q, want 120x40", got)
	}

	// `screen` output should now fit within (not exceed) the new width.
	screenOut, err := run(nil, "screen", name)
	if err != nil {
		t.Fatalf("screen failed: %s\n%s", err, screenOut)
	}
	for _, line := range strings.Split(screenOut, "\n") {
		if len(line) > 120 {
			t.Errorf("screen line exceeds new width 120: %q (len=%d)", line, len(line))
		}
	}

	// Bounds validation: absurd or non-positive sizes are rejected, not
	// silently clamped or accepted.
	if out, err := run(nil, "resize", name, "--cols", "0", "--rows", "40"); err == nil {
		t.Errorf("resize --cols 0 should fail, got: %s", out)
	}
	if out, err := run(nil, "resize", name, "--cols", "999999", "--rows", "40"); err == nil {
		t.Errorf("resize --cols 999999 should fail, got: %s", out)
	}

	// A --no-pty (raw pipe) session has no terminal grid: resize must
	// return a clear error, not silently succeed.
	noptyName := "resize-test-nopty"
	if out, err := run(nil, "start", "process", "--name", noptyName, "--no-pty", "--", "cat"); err != nil {
		t.Fatalf("start --no-pty failed: %s\n%s", err, out)
	}
	defer run(nil, "stop", noptyName)
	if out, err := run(nil, "resize", noptyName, "--cols", "100", "--rows", "30"); err == nil {
		t.Errorf("resize on a --no-pty session should fail, got: %s", out)
	}

	// `start --cols/--rows` sets the initial size (not just post-hoc resize).
	startedName := "resize-test-started"
	if out, err := run(nil, "start", "process", "--name", startedName, "--cols", "100", "--rows", "30", "--", "python3", "-i"); err != nil {
		t.Fatalf("start --cols/--rows failed: %s\n%s", err, out)
	}
	defer run(nil, "stop", startedName)
	if out, err := run(nil, "expect", startedName, ">>>", "--timeout", "10"); err != nil {
		t.Fatalf("expect >>> failed: %s\n%s", err, out)
	}
	if out, err := run(nil, "sendline", startedName, "import shutil; print(shutil.get_terminal_size())"); err != nil {
		t.Fatalf("sendline failed: %s\n%s", err, out)
	}
	out, err = run(nil, "expect", startedName, sizeRe, "--timeout", "5")
	if err != nil {
		t.Fatalf("expect terminal_size failed: %s\n%s", err, out)
	}
	if !strings.Contains(out, "columns=100, lines=30") {
		t.Fatalf("start --cols 100 --rows 30 not honored: got %q, want columns=100, lines=30", out)
	}
}

// TestIntegration_ImmediateOutputNotLost reproduces the TODO.md bug
// "Output printed before pipe-pane activates is lost": tmux used to run
// the pane's command the instant `new-session` returned, but pipe-pane
// (and hangon's FIFO reader) were only wired up several tmux round-trips
// later. Anything the command printed in that gap was written straight
// to the pane and never streamed to the FIFO — pipe-pane doesn't replay
// backlog — so output from fast, short-lived commands (or a slow-to-wire
// startup banner from a longer one) vanished with no way to recover it.
//
// Before the fix this test is flaky-to-deterministic depending on
// machine speed (see this task's written repro against the unfixed
// binary: 3/3 runs of a one-shot `echo hi` produced empty `readall`,
// every time, on this machine). This test asserts the invariant that
// must hold regardless of timing: output produced immediately at
// startup is never lost. It exercises both ends of the timing spectrum
// that made this bug hard to see reliably: a one-shot command that
// prints and exits before any client has a chance to even ask, and a
// longer-lived command whose very first line is a startup banner that
// `expect` needs to see promptly.
func TestIntegration_ImmediateOutputNotLost(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed, skipping integration test")
	}

	_, run := buildHangonForTest(t)

	t.Run("one-shot command output survives", func(t *testing.T) {
		name := "immediate-oneshot"
		out, err := run(nil, "start", "process", "--name", name, "--", "echo", "hi")
		if err != nil {
			t.Fatalf("start failed: %s\n%s", err, out)
		}
		defer run(nil, "stop", name)

		// Give the (already-exited) one-shot process's output every
		// chance to have been lost: wait well past both tmux's own
		// setup and the pipe-pane wiring race window before reading.
		time.Sleep(1 * time.Second)

		out, err = run(nil, "readall", name)
		if err != nil {
			t.Fatalf("readall failed: %s\n%s", err, out)
		}
		if !strings.Contains(out, "hi") {
			t.Errorf("readall = %q, want it to contain the command's output %q (output printed before pipe-pane activates must not be lost)", out, "hi")
		}
	})

	t.Run("startup banner is seen by expect immediately", func(t *testing.T) {
		name := "immediate-banner"
		out, err := run(nil, "start", "process", "--name", name, "--", "sh", "-c", "echo BANNER; sleep 5")
		if err != nil {
			t.Fatalf("start failed: %s\n%s", err, out)
		}
		defer run(nil, "stop", name)

		// No sleep here: `expect` must see BANNER even when asked
		// immediately after start returns, which is exactly the
		// window the pipe-pane race used to lose.
		out, err = run(nil, "expect", name, "BANNER", "--timeout", "3")
		if err != nil {
			t.Fatalf("expect BANNER failed (output printed before pipe-pane activates was lost): %s\n%s", err, out)
		}
	})
}

// TestIntegration_VanishedSessionExitCode reproduces the TODO.md bug
// "Vanished tmux session reported as exit code 0": when a tmux-backed
// session's pane/session disappears without ever reporting
// pane_dead_status (e.g. `tmux kill-session` from outside hangon), the
// poll goroutine used to close `done` with exitCode still at its zero
// value — so `hangon wait` printed "exit code: 0" and exited 0 for a
// session that was killed, not one that succeeded.
//
// This test kills the tmux session directly (bypassing hangon) and
// asserts `hangon wait` reports something other than success — a
// distinct, documented failure (exit 2, "hangon: session terminated
// externally: exit status unknown"), never exit code 0. It also proves
// the fix didn't collateral-damage real exit codes: a process that
// actually exits 0 must still report 0, and a process that exits
// nonzero must still report its real code.
func TestIntegration_VanishedSessionExitCode(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed, skipping integration test")
	}

	_, run := buildHangonForTest(t)

	t.Run("externally killed session is not exit code 0", func(t *testing.T) {
		name := "vanish-test"
		out, err := run(nil, "start", "process", "--name", name, "--", "python3", "-i")
		if err != nil {
			// python3 may not be present in every CI image; sh -i as a
			// fallback would change semantics too much, so just skip.
			if _, lookErr := exec.LookPath("python3"); lookErr != nil {
				t.Skip("python3 not installed, skipping")
			}
			t.Fatalf("start failed: %s\n%s", err, out)
		}
		defer run(nil, "stop", name) // best-effort; the tmux session gets killed mid-test

		if out, err := run(nil, "expect", name, ">>>", "--timeout", "10"); err != nil {
			t.Fatalf("expect >>> failed: %s\n%s", err, out)
		}

		// Find the tmux session's holder PID so we can target it exactly
		// (mirrors how TestIntegration_Resize locates it), then kill the
		// tmux session directly on hangon's dedicated test socket —
		// never the default server, never a plain "tmux" invocation.
		statusOut, err := run(nil, "status", name)
		if err != nil {
			t.Fatalf("status failed: %s\n%s", err, statusOut)
		}
		holderPID := ""
		for _, line := range strings.Split(statusOut, "\n") {
			if strings.HasPrefix(line, "Holder PID:") {
				holderPID = strings.TrimSpace(strings.TrimPrefix(line, "Holder PID:"))
			}
		}
		if holderPID == "" {
			t.Fatalf("could not find holder PID in status output: %s", statusOut)
		}
		tmuxSess := "hangon-" + holderPID
		if out, err := tmuxCmd("kill-session", "-t", tmuxExact(tmuxSess)).CombinedOutput(); err != nil {
			t.Fatalf("tmux kill-session failed (test can't reach its subject): %s\n%s", err, out)
		}

		waitOut, waitErr := run(nil, "wait", name)
		if waitErr == nil {
			t.Fatalf("wait on an externally killed session succeeded (reported exit 0): %s", waitOut)
		}
		if strings.Contains(waitOut, "exit code: 0") {
			t.Errorf("wait output claims exit code 0 for a killed session: %s", waitOut)
		}
		if exitErr, ok := waitErr.(*exec.ExitError); ok && exitErr.ExitCode() == 0 {
			t.Errorf("wait process exit code = 0 for a killed session, want nonzero")
		}
	})

	t.Run("real exit codes still propagate", func(t *testing.T) {
		cases := []struct {
			shellExit string
			want      int
		}{
			{"exit 7", 7},
			{"exit 0", 0},
		}
		for _, tc := range cases {
			name := "realexit-" + strings.Fields(tc.shellExit)[1]
			if out, err := run(nil, "start", "process", "--name", name, "--", "sh", "-c", tc.shellExit); err != nil {
				t.Fatalf("start failed: %s\n%s", err, out)
			}
			defer run(nil, "stop", name)
			waitOut, waitErr := run(nil, "wait", name)
			gotExit := 0
			if exitErr, ok := waitErr.(*exec.ExitError); ok {
				gotExit = exitErr.ExitCode()
			} else if waitErr != nil {
				t.Fatalf("wait for %q errored unexpectedly: %s\n%s", tc.shellExit, waitErr, waitOut)
			}
			if gotExit != tc.want {
				t.Errorf("wait after %q: process exit code = %d, want %d (output: %s)", tc.shellExit, gotExit, tc.want, waitOut)
			}
			if !strings.Contains(waitOut, "exit code: "+strconv.Itoa(tc.want)) {
				t.Errorf("wait after %q: output = %q, want it to contain %q", tc.shellExit, waitOut, "exit code: "+strconv.Itoa(tc.want))
			}
		}
	})
}

// TestIntegration_ProtocolRoundtrip tests the JSON protocol directly.
func TestIntegration_ProtocolRoundtrip(t *testing.T) {
	// Test encoding/decoding of protocol types.
	req := Request{
		Method: MethodSend,
		Params: mustMarshal(SendParams{Data: "hello\n"}),
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Request
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Method != MethodSend {
		t.Errorf("method=%q, want %q", decoded.Method, MethodSend)
	}

	var params SendParams
	if err := json.Unmarshal(decoded.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.Data != "hello\n" {
		t.Errorf("data=%q, want %q", params.Data, "hello\n")
	}
}

func mustMarshal(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

// TestIntegration_Start_SpacedTMPDIR reproduces TODO.md's "Session start
// fails outright when TMPDIR contains a space" and proves a full
// start/read/stop cycle succeeds with a space in $TMPDIR, as long as the
// resulting socket path still fits a sockaddr_un (see
// TestIntegration_Start_TooLongTMPDIRFailsFast below for the separate,
// deeper bug the original repro actually hit).
//
// The TMPDIR here is built from a short explicit base via os.MkdirTemp,
// not t.TempDir() (whose long per-test-name path would itself blow past
// the sun_path limit for reasons unrelated to the space — see
// checkUnixSocketPathLen's doc comment and gc_test.go's identical
// caveat), so this test isolates the "space" variable on its own.
func TestIntegration_Start_SpacedTMPDIR(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed, skipping integration test")
	}

	binary := buildHangonBinary(t)
	home := t.TempDir()

	spacedTMPDIR, err := os.MkdirTemp("", "hangon test dir")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(spacedTMPDIR) })
	if !strings.Contains(spacedTMPDIR, " ") {
		t.Fatalf("test setup bug: MkdirTemp pattern should have produced a space in the path: %s", spacedTMPDIR)
	}

	name := "spaced-tmpdir-test"
	sock := fmt.Sprintf("hangon-wk-spacedtmpdir-%d", os.Getpid())
	env := append(envWithHome(home), "TMPDIR="+spacedTMPDIR, tmuxSocketEnv+"="+sock)
	run := func(args ...string) (string, error) {
		cmd := exec.Command(binary, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	t.Cleanup(func() {
		run("stop", name)
		exec.Command("tmux", "-L", sock, "kill-server").Run()
	})

	out, err := run("start", "process", "--name", name, "--", "echo", "HI")
	if err != nil {
		t.Fatalf("start under spaced TMPDIR failed: %s\n%s", err, out)
	}
	if !strings.Contains(out, "started") {
		t.Fatalf("unexpected start output: %s", out)
	}

	out, err = run("read", name)
	if err != nil {
		t.Fatalf("read failed: %s\n%s", err, out)
	}
	if !strings.Contains(out, "HI") {
		t.Errorf("read output = %q, want it to contain HI", out)
	}

	out, err = run("stop", name)
	if err != nil {
		t.Fatalf("stop failed: %s\n%s", err, out)
	}
}

// TestIntegration_Start_TooLongTMPDIRFailsFast covers the bug actually
// hit by the foreman's original repro (TODO.md, diagnosed 2026-09-01):
// running the failing `_serve` invocation directly and capturing its
// stderr (instead of runStart's normal cmd.Stderr = nil, which discards
// it) showed the holder dying with `bind: invalid argument` — the
// AF_UNIX sun_path length limit, not the space character. A long TMPDIR
// with NO space reproduces identically; a short spaced TMPDIR (the case
// above) does not reproduce at all.
//
// checkUnixSocketPathLen (main.go) turns that into an immediate, clear
// error instead of runStart's old 5-second "did not start" poll timeout.
// This test proves both properties: the command fails fast (well under
// the 5s poll it used to exhaust) and the error names the real cause.
//
// Confirmed this fails on unfixed code (checkUnixSocketPathLen's call
// site in runStart commented out): the test's own 4-second budget check
// fails because the command instead takes ~5s, and the message
// assertion fails too — the process instead prints the old generic
// "hangon: session holder did not start within 5 seconds", exit code 2
// still holds but neither "too long" nor "Unix domain socket" appear.
func TestIntegration_Start_TooLongTMPDIRFailsFast(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed, skipping integration test")
	}

	binary := buildHangonBinary(t)
	home := t.TempDir()

	longBase, err := os.MkdirTemp("", "hangon-toolongtmpdir-test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(longBase) })
	// Pad well past the sun_path limit (103 bytes, see
	// checkUnixSocketPathLen) with an oversized subdirectory name —
	// no space involved, to keep this test isolated to path length.
	longTMPDIR := filepath.Join(longBase, strings.Repeat("x", 120))
	if err := os.MkdirAll(longTMPDIR, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	name := "toolong-tmpdir-test"
	sock := fmt.Sprintf("hangon-wk-toolongtmpdir-%d", os.Getpid())
	env := append(envWithHome(home), "TMPDIR="+longTMPDIR, tmuxSocketEnv+"="+sock)
	t.Cleanup(func() {
		exec.Command("tmux", "-L", sock, "kill-server").Run()
	})

	start := time.Now()
	cmd := exec.Command(binary, "start", "process", "--name", name, "--", "echo", "HI")
	cmd.Env = env
	outBytes, runErr := cmd.CombinedOutput()
	elapsed := time.Since(start)
	out := strings.TrimSpace(string(outBytes))

	if runErr == nil {
		t.Fatalf("start under too-long TMPDIR unexpectedly succeeded: %s", out)
	}
	if elapsed > 4*time.Second {
		t.Errorf("start took %s to fail — expected a fast preflight rejection, not the old 5-second holder-startup poll timeout", elapsed)
	}
	if !strings.Contains(out, "too long") || !strings.Contains(out, "Unix domain socket") {
		t.Errorf("error output = %q, want it to explain the socket path is too long for a Unix domain socket", out)
	}
}
