package main

import (
	"fmt"
	"os"
	"os/exec"
)

// tmuxSocketEnv overrides the tmux server socket name hangon uses.
// Tests set it to a per-run name so parallel test runs can't see each
// other's sessions and a single kill-server cleans everything up.
const tmuxSocketEnv = "HANGON_TMUX_SOCKET"

// tmuxSocketName returns the socket name for hangon's dedicated tmux
// server. hangon never talks to the user's default tmux server: every
// session it creates, lists, or kills lives on its own `tmux -L` server,
// so hangon-side cleanup (stop, stopall, gc) can never touch the user's
// personal sessions, and hangon's sessions never clutter `tmux ls`.
func tmuxSocketName() string {
	if s := os.Getenv(tmuxSocketEnv); s != "" {
		return s
	}
	return "hangon"
}

// tmuxCmd builds a tmux command pinned to hangon's dedicated server
// socket. All tmux invocations in hangon must go through this (never
// exec.Command("tmux", ...) directly) so none of them can leak onto the
// user's default tmux server.
func tmuxCmd(args ...string) *exec.Cmd {
	return exec.Command("tmux", append([]string{"-L", tmuxSocketName()}, args...)...)
}

// tmuxExact returns a target spec that matches the session sess
// exactly. Bare `-t name` in tmux falls back to prefix (and then
// fnmatch) matching when no exact match exists, which can silently
// resolve to a different, similarly-named session; the `=` prefix
// disables that. The trailing colon makes the spec a "session:window"
// form with an empty window part: session-scoped commands
// (kill-session, has-session) accept it unchanged, while pane/window-
// scoped commands (set-option, pipe-pane, display, send-keys,
// capture-pane, paste-buffer) reject a bare `=name` ("can't find
// pane") but resolve `=name:` to the session's active pane.
func tmuxExact(sess string) string {
	return "=" + sess + ":"
}

// sessionNameForPID returns the tmux session name the process backend
// uses for a holder with the given PID (see backend_process.go:
// pb.tmuxSess = fmt.Sprintf("hangon-%d", os.Getpid())). Every other
// site that needs to derive a holder's tmux session name from its PID
// (runStop, runStopAll, gc's stale-state cleanup) goes through this
// instead of re-formatting "hangon-%d" itself, so the four sites can't
// drift out of sync with the format the process backend actually
// creates.
func sessionNameForPID(pid int) string {
	return fmt.Sprintf("hangon-%d", pid)
}
