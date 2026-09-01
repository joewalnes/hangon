package main

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
)

// TestMain points this test run at its own tmux server socket, distinct
// from both the user's default server and hangon's normal dedicated
// server ("hangon"). Integration tests start real tmux sessions and run
// `hangon gc` against machine-global resources (tmux sessions, _serve
// processes); without this isolation a test run would see — and kill —
// the developer's genuinely live hangon sessions, and any test that
// died mid-run would leak hangon-<pid> sessions onto a shared server.
//
// The socket name is set via HANGON_TMUX_SOCKET, which both in-process
// helpers (tmuxCmd) and spawned hangon binaries (which inherit the test
// process environment, see envWithHome) pick up. kill-server afterwards
// reaps every session the run created in one shot, even on test
// failure.
func TestMain(m *testing.M) {
	sock := fmt.Sprintf("hangon-test-%d", os.Getpid())
	os.Setenv(tmuxSocketEnv, sock)
	code := m.Run()
	if _, err := exec.LookPath("tmux"); err == nil {
		exec.Command("tmux", "-L", sock, "kill-server").Run()
	}
	os.Exit(code)
}
