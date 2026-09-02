package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestIntegration_GC_ReapsCrashedHolderAndOrphanedTmux reproduces the
// most common real-world orphan pattern reported in practice: a
// session's holder process dies ungracefully (crash, OOM-kill, `kill
// -9` — anything that skips the holder's own SIGTERM/SIGINT cleanup
// path in holder.go's Serve()), which leaves state.json still
// pointing at a dead PID AND leaves the tmux session it created
// (independent of the holder process; tmux's own server keeps it
// alive) completely orphaned, since nothing else knows to kill it.
//
// This test starts a real session (real tmux, real holder process),
// force-kills the holder out-of-band exactly like a crash would, then
// runs `hangon gc` and confirms it notices on both sides: the stale
// state.json entry is removed AND the orphaned tmux session is
// actually killed (verified via `tmux has-session`, not just by
// trusting gc's own report).
func TestIntegration_GC_ReapsCrashedHolderAndOrphanedTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed, skipping")
	}
	_, run := buildHangonForTest(t)

	out, err := run(nil, "start", "process", "--name", "crashme", "--", "python3", "-i")
	if err != nil {
		t.Fatalf("start failed: %s", out)
	}
	defer run(nil, "stopall", "--force")

	out, err = run(nil, "status", "crashme")
	if err != nil {
		t.Fatalf("status failed: %s", out)
	}
	holderPID := extractHolderPID(t, out)
	tmuxSess := fmt.Sprintf("hangon-%d", holderPID)

	if !tmuxHasSession(tmuxSess) {
		t.Fatalf("expected tmux session %q to exist before crash simulation", tmuxSess)
	}

	// Simulate a crash: SIGKILL the holder directly, bypassing its own
	// signal handler / cleanup path entirely (holder.go only cleans up
	// on SIGTERM/SIGINT — SIGKILL can't be caught by anything).
	if err := syscall.Kill(holderPID, syscall.SIGKILL); err != nil {
		t.Fatalf("failed to SIGKILL holder PID %d: %v", holderPID, err)
	}
	waitUntil(t, 5*time.Second, func() bool { return !isProcessAlive(holderPID) })

	// The tmux session must still be there — this is the orphan.
	if !tmuxHasSession(tmuxSess) {
		t.Fatalf("tmux session %q disappeared on its own after killing only the holder — test setup invalid, this should be independent of the holder process", tmuxSess)
	}

	out, err = run(nil, "gc")
	if err != nil {
		t.Fatalf("gc failed: %s", out)
	}
	if !strings.Contains(out, "crashme") {
		t.Errorf("expected gc output to mention stale entry \"crashme\", got: %s", out)
	}

	// state.json entry must be gone.
	out, _ = run(nil, "list")
	if strings.Contains(out, "crashme") {
		t.Errorf("session \"crashme\" still listed after gc: %s", out)
	}

	// The orphaned tmux session must actually have been killed, not
	// just reported.
	if tmuxHasSession(tmuxSess) {
		t.Errorf("tmux session %q still exists after gc — gc reported cleanup but didn't actually kill it", tmuxSess)
	}
}

// TestIntegration_GC_ReapsOrphanedServeProcessNeverRegistered covers
// the other half of the orphan problem: a "hangon _serve" process
// running with no state.json entry pointing at it at all (e.g. state
// was lost/never written, or the process was orphaned before
// registration completed). This is simulated by invoking the internal
// `_serve` entry point directly — bypassing `start`'s
// claimSessionName registration entirely — so the holder process (and
// its tmux session) exist with nothing in state.json referencing them,
// exactly like the "leaked processes with no tracked entry" incident.
// Confirms gc finds and stops the orphaned holder process itself
// (checked by PID liveness, not just gc's own report) as well as its
// tmux session.
func TestIntegration_GC_ReapsOrphanedServeProcessNeverRegistered(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed, skipping")
	}
	binary := buildHangonBinary(t)

	// Give this process its own HOME so ~/.hangon (the default global
	// state dir) is isolated and starts empty.
	home := t.TempDir()
	stateDir := home + "/.hangon"
	runHome := func(args ...string) (string, error) {
		cmd := exec.Command(binary, args...)
		cmd.Env = envWithHome(home)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}

	// Invoke the internal _serve entry point directly, bypassing
	// `start`'s claimSessionName registration entirely, so the holder
	// process (and its tmux session) come up with NOTHING in
	// state.json pointing at them — exactly the "leaked process with
	// no tracked entry" pattern from the incident.
	//
	// The socket must live under a *short* path, not t.TempDir()
	// (which embeds this test's full, long name): AF_UNIX socket paths
	// are capped at ~104 bytes on macOS (sun_path), and t.TempDir()'s
	// path here is long enough to blow past that, making bind() fail
	// with EINVAL — exactly what production code avoids by using bare
	// os.TempDir() (see runStart's socketPath) rather than a
	// per-test-name-scoped directory.
	sockDir, err := os.MkdirTemp("", "hangon-gc-test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	socketPath := sockDir + "/orphan.sock"
	serve := exec.Command(binary, "_serve",
		"--name", "nevertracked",
		"--type", "process",
		"--socket", socketPath,
		"--state-dir", stateDir,
		"--", "python3", "-i")
	serve.Env = envWithHome(home)
	stderrPipe, _ := serve.StderrPipe()
	if err := serve.Start(); err != nil {
		t.Fatalf("failed to start orphaned _serve process: %v", err)
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, rerr := stderrPipe.Read(buf)
			if n > 0 {
				t.Logf("serve stderr: %s", buf[:n])
			}
			if rerr != nil {
				return
			}
		}
	}()
	holderPID := serve.Process.Pid
	// Reap it in the background once it exits. Without this, since our
	// test process is technically still its OS parent, a SIGKILL
	// delivered by someone else (gc, or our own cleanup below) would
	// only turn it into a zombie until we call Wait — and a zombie
	// still answers kill(pid, 0)/signal(0) successfully, which would
	// make isProcessAlive report it as "alive" even though gc did kill
	// it. In real usage this doesn't come up: `start` detaches the
	// holder (setsid, see setSysProcAttr) so an orphaned holder gets
	// reparented to init, which reaps it for real. Waiting here in the
	// background makes the test's process-parent bookkeeping match
	// that real-world behavior instead of being an artifact of the
	// test harness remaining its parent.
	go serve.Wait()
	defer func() {
		// Best-effort cleanup in case the test fails before gc gets to it.
		syscall.Kill(holderPID, syscall.SIGKILL)
	}()

	// Wait for the holder to actually finish starting (socket present),
	// the same readiness signal `hangon start` itself waits for —
	// rather than just the tmux session appearing, which happens
	// earlier, mid-startup, before backend.Start() has necessarily
	// finished or succeeded.
	tmuxSess := fmt.Sprintf("hangon-%d", holderPID)
	waitUntil(t, 5*time.Second, func() bool {
		_, err := os.Stat(socketPath)
		return err == nil
	})
	if !isProcessAlive(holderPID) {
		t.Fatalf("orphaned _serve process (PID %d) exited before we could test against it", holderPID)
	}
	if !tmuxHasSession(tmuxSess) {
		t.Fatalf("expected tmux session %q to exist once the holder is ready", tmuxSess)
	}

	// A plain `list` in this state dir must show nothing — the whole
	// point is this process was never registered.
	out, err := runHome("list")
	if err != nil {
		t.Fatalf("list failed: %s", out)
	}
	if !strings.Contains(out, "No active sessions") {
		t.Fatalf("expected empty session list before gc, got: %s", out)
	}

	out, err = runHome("gc")
	if err != nil {
		t.Fatalf("gc failed: %s", out)
	}
	if !strings.Contains(out, strconv.Itoa(holderPID)) {
		t.Errorf("expected gc output to mention orphaned PID %d, got: %s", holderPID, out)
	}

	// The orphaned holder process must actually be stopped, not just reported.
	waitUntil(t, 5*time.Second, func() bool { return !isProcessAlive(holderPID) })

	// ...and its tmux session with it.
	if tmuxHasSession(tmuxSess) {
		t.Errorf("tmux session %q still exists after gc killed its orphaned holder", tmuxSess)
	}
}

// TestIntegration_GC_DoesNotTouchOtherStateDirSession reproduces the
// cross-state-dir kill bug: `hangon gc` run against one state
// directory (Y) must never touch a "hangon _serve" holder process (or
// its tmux session) that belongs to a DIFFERENT state directory (X),
// even though that holder's PID is, correctly, not present in Y's
// state.json — that's true of every process on the machine that isn't
// Y's, and is not by itself evidence of being orphaned.
//
// Before the scoping fix, gcOrphanedServeProcesses/gcOrphanedTmuxSessions
// built their "live" set from exactly one state dir and then killed
// every "hangon _serve" process (and every "hangon-<pid>" tmux
// session) on the machine not in that one set — including holders
// registered validly under a completely different state dir. This is
// the exact shape of the real incident: an agent (or a second hangon
// install with --local, or a differently-scoped run) running `gc`
// could SIGKILL every OTHER agent's live sessions on the same machine.
//
// Safety note: this test deliberately builds its own hangon binary
// under a unique basename (buildHangonBinaryNamed) rather than the
// plain "hangon" buildHangonBinary uses. gc's orphan scan
// (listServeProcesses) matches "_serve" processes purely by
// argv[0]'s basename equalling the basename of the running
// executable, system-wide, via `ps -A` — NOT scoped by tmux socket or
// state dir. A test binary named plain "hangon" would be
// indistinguishable, to that scan, from a real, live, production
// hangon install (e.g. ~/go/bin/hangon) also named "hangon", so an
// unfixed (or newly-broken) gc run in this test could SIGKILL genuine
// production sessions elsewhere on the machine. The unique basename
// used here guarantees the scan can only ever see processes this test
// itself spawned.
func TestIntegration_GC_DoesNotTouchOtherStateDirSession(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed, skipping")
	}
	binary := buildHangonBinaryNamed(t, fmt.Sprintf("hangon-gcscopetest-%d", os.Getpid()))

	homeX := t.TempDir()
	homeY := t.TempDir()
	runIn := func(home string, args ...string) (string, error) {
		cmd := exec.Command(binary, args...)
		cmd.Env = envWithHome(home)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}

	// State dir X: a real, tracked session.
	out, err := runIn(homeX, "start", "process", "--name", "otherstatedir", "--", "python3", "-i")
	if err != nil {
		t.Fatalf("start under state dir X failed: %s", out)
	}
	defer runIn(homeX, "stopall", "--force")

	out, err = runIn(homeX, "status", "otherstatedir")
	if err != nil {
		t.Fatalf("status failed: %s", out)
	}
	holderPID := extractHolderPID(t, out)
	tmuxSess := fmt.Sprintf("hangon-%d", holderPID)
	if !tmuxHasSession(tmuxSess) {
		t.Fatalf("expected tmux session %q to exist before running gc against a different state dir", tmuxSess)
	}
	if !isProcessAlive(holderPID) {
		t.Fatalf("holder PID %d not alive before running gc against a different state dir", holderPID)
	}

	// State dir Y starts completely empty — nothing tracked there at all.
	out, err = runIn(homeY, "list")
	if err != nil {
		t.Fatalf("list under state dir Y failed: %s", out)
	}
	if !strings.Contains(out, "No active sessions") {
		t.Fatalf("expected state dir Y to start empty, got: %s", out)
	}

	// The bug under test: gc scoped to Y sees X's holder PID as "not
	// in my live set" (true of every PID that isn't Y's) and, unfixed,
	// concludes it's orphaned and kills it — along with its tmux
	// session — even though it is validly tracked by X's own
	// state.json, just not Y's.
	out, err = runIn(homeY, "gc")
	if err != nil {
		t.Fatalf("gc under state dir Y failed: %s", out)
	}
	t.Logf("gc (scoped to state dir Y) output:\n%s", out)

	if !isProcessAlive(holderPID) {
		t.Fatalf("CROSS-STATE-DIR KILL: gc run against state dir Y killed holder PID %d, which belongs to state dir X", holderPID)
	}
	if !tmuxHasSession(tmuxSess) {
		t.Fatalf("CROSS-STATE-DIR KILL: gc run against state dir Y killed tmux session %q, which belongs to state dir X", tmuxSess)
	}

	// X's own view must still show the session as live and unaffected.
	out, err = runIn(homeX, "list")
	if err != nil {
		t.Fatalf("list under state dir X (after) failed: %s", out)
	}
	if !strings.Contains(out, "otherstatedir") {
		t.Errorf("session \"otherstatedir\" no longer listed under its own state dir X after gc ran against Y: %s", out)
	}
}

// TestIntegration_GC_DryRunMakesNoChanges confirms --dry-run reports
// what it would do without touching anything: the stale entry, its
// tmux session, and the orphaned holder must all still be present
// afterwards.
func TestIntegration_GC_DryRunMakesNoChanges(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed, skipping")
	}
	_, run := buildHangonForTest(t)

	out, err := run(nil, "start", "process", "--name", "dryrun-crash", "--", "python3", "-i")
	if err != nil {
		t.Fatalf("start failed: %s", out)
	}
	defer run(nil, "stopall", "--force")

	out, err = run(nil, "status", "dryrun-crash")
	if err != nil {
		t.Fatalf("status failed: %s", out)
	}
	holderPID := extractHolderPID(t, out)
	tmuxSess := fmt.Sprintf("hangon-%d", holderPID)

	if err := syscall.Kill(holderPID, syscall.SIGKILL); err != nil {
		t.Fatalf("failed to SIGKILL holder PID %d: %v", holderPID, err)
	}
	waitUntil(t, 5*time.Second, func() bool { return !isProcessAlive(holderPID) })

	out, err = run(nil, "gc", "--dry-run")
	if err != nil {
		t.Fatalf("gc --dry-run failed: %s", out)
	}
	if !strings.Contains(out, "dryrun-crash") || !strings.Contains(out, "would") {
		t.Errorf("expected dry-run preview mentioning the stale session, got: %s", out)
	}

	// Nothing should actually have changed: the stale entry is still
	// listed, and (interesting case) the orphaned tmux session is
	// still there too.
	out, _ = run(nil, "list")
	if !strings.Contains(out, "dryrun-crash") {
		t.Errorf("dry-run removed the state entry, want it untouched: %s", out)
	}
	if !tmuxHasSession(tmuxSess) {
		t.Errorf("dry-run killed the orphaned tmux session, want it untouched")
	}

	// Cleanup: a real gc now actually removes it.
	run(nil, "gc")
}

// tmuxHasSession reports whether a tmux session with the given name
// currently exists.
func tmuxHasSession(name string) bool {
	return tmuxCmd("has-session", "-t", tmuxExact(name)).Run() == nil
}

// extractHolderPID parses "Holder PID: N" out of `hangon status` output.
func extractHolderPID(t *testing.T, statusOutput string) int {
	t.Helper()
	for _, line := range strings.Split(statusOutput, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Holder PID:") {
			f := strings.Fields(line)
			pid, err := strconv.Atoi(f[len(f)-1])
			if err != nil {
				t.Fatalf("could not parse holder PID from line %q: %v", line, err)
			}
			return pid
		}
	}
	t.Fatalf("no \"Holder PID:\" line found in status output: %s", statusOutput)
	return 0
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("condition not met within %v", timeout)
	}
}
