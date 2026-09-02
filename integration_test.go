package main

import (
	"encoding/json"
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

	// Read should return something (may be empty if expect consumed it).
	_, err = run("read", name)
	if err != nil {
		t.Fatalf("read failed: %s", err)
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
