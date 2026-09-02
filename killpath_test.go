package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// spawnUnrelatedLongLivedProcess starts a real, long-lived `sleep 300`
// process that is deliberately NOT a hangon holder, and returns its
// pid along with a channel that closes once the process has actually
// exited (for any reason — signal or natural completion).
//
// The exit channel, not isProcessAlive, is what tests below must use
// to check "was this process actually signalled": the test process is
// this child's parent, and on Unix a signalled child that hasn't been
// wait(2)'d by its parent yet becomes a zombie — kill(pid, 0) (what
// isProcessAlive uses) still reports a zombie as "alive" until it is
// reaped, so checking isProcessAlive right after signalling would
// spuriously report the process as having survived even when it was in
// fact killed. Waiting on it in a background goroutine reaps it the
// moment it actually exits, making the exit channel a reliable signal
// of whether stop/stopall actually sent it anything.
func spawnUnrelatedLongLivedProcess(t *testing.T) (pid int, exited <-chan struct{}) {
	t.Helper()
	cmd := exec.Command("sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start sleep helper: %v", err)
	}
	ch := make(chan struct{})
	go func() {
		cmd.Wait()
		close(ch)
	}()
	t.Cleanup(func() {
		cmd.Process.Kill()
		<-ch
	})
	return cmd.Process.Pid, ch
}

// TestIntegration_Stop_RefusesReusedPID reproduces the PID-reuse bug:
// stop/stopall/gc used to signal `info.HolderPID` after nothing more
// than isProcessAlive (kill(pid,0)), which only proves *some* process
// currently has that pid — not that it's still the hangon holder that
// originally owned it. Once a holder exits and the OS recycles its pid
// onto an unrelated process owned by the same user, the old code would
// SIGINT/SIGKILL that unrelated process.
//
// This fabricates exactly that situation without waiting on real PID
// recycling (which isn't reliably triggerable in a test): it spawns a
// real, long-lived process (`sleep 300`, via spawnUnrelatedLongLivedProcess)
// that is provably NOT a hangon holder, then writes a state.json entry
// whose HolderPID is that process's pid — simulating a holder that used
// to own this pid having exited and the pid having been recycled onto
// the sleep process by the time `hangon stop` runs.
//
// Asserts all three parts of the fix: the sleep process survives (is
// never actually signalled — checked via its exit channel, see
// spawnUnrelatedLongLivedProcess's doc comment for why isProcessAlive
// alone can't be trusted here), the stale state entry is still cleaned
// up (the session is treated as already-dead), and the output names
// the mismatch so a caller isn't left wondering why "stop" did nothing
// on the process side.
//
// Run against the pre-fix code (runStop/stopSessionHolder signalling
// unconditionally on isProcessAlive, no identity check first): the
// sleep process's exit channel closes (it received SIGINT and, being
// unable to catch it, terminated immediately) and the "survived"
// assertion below fails — confirmed by temporarily short-circuiting
// stopSessionHolder's holderIdentityConfirmed branch back to an
// unconditional killProcessGracefully call and rerunning; captured
// failure (both this test and the stopall one):
//
//	=== RUN   TestIntegration_Stop_RefusesReusedPID
//	    killpath_test.go:107: stop output:
//	        Session "reused" stopped.
//	    killpath_test.go:111: sleep process 38028 exited (was signalled)
//	    despite not being a hangon holder — PID-reuse identity guard failed
//	--- FAIL: TestIntegration_Stop_RefusesReusedPID (0.63s)
//	=== RUN   TestIntegration_StopAll_RefusesReusedPID
//	    killpath_test.go:151: stopall output:
//	        Stopped "reused-all"
//	    killpath_test.go:155: sleep process 38038 exited (was signalled)
//	    despite not being a hangon holder — PID-reuse identity guard failed
//	--- FAIL: TestIntegration_StopAll_RefusesReusedPID (0.61s)
func TestIntegration_Stop_RefusesReusedPID(t *testing.T) {
	binary := buildHangonBinary(t)
	testHome := t.TempDir()
	run := func(env []string, args ...string) (string, error) {
		cmd := exec.Command(binary, args...)
		cmd.Env = append(envWithHome(testHome), env...)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}

	dir := filepath.Join(testHome, ".hangon")

	sleepPID, exited := spawnUnrelatedLongLivedProcess(t)

	// Fabricate the PID-reuse scenario: a state.json entry whose
	// HolderPID is the sleep process's pid, as if a hangon holder had
	// once owned this pid, exited, and the OS recycled the pid onto
	// this unrelated process before `hangon stop` ran.
	if err := addSession(dir, "reused", "tcp", filepath.Join(testHome, "fake.sock"), sleepPID, 0, nil, "127.0.0.1:0"); err != nil {
		t.Fatalf("addSession: %v", err)
	}

	out, err := run(nil, "stop", "reused")
	if err != nil {
		t.Fatalf("stop failed (exit error): %s", out)
	}
	t.Logf("stop output:\n%s", out)

	select {
	case <-exited:
		t.Fatalf("sleep process %d exited (was signalled) despite not being a hangon holder — PID-reuse identity guard failed", sleepPID)
	case <-time.After(300 * time.Millisecond):
		// Still running — the guard correctly refused to signal it.
	}

	if !strings.Contains(out, "reused by another process") {
		t.Errorf("expected stop output to mention the PID-reuse identity mismatch, got: %s", out)
	}

	listOut, _ := run(nil, "list")
	if strings.Contains(listOut, "reused") {
		t.Errorf("session %q still present in state after stop: %s", "reused", listOut)
	}
}

// TestIntegration_StopAll_RefusesReusedPID is the same scenario via
// `stopall --force`, which duplicated runStop's signal-then-poll logic
// independently (and, pre-fix, had no identity check either).
func TestIntegration_StopAll_RefusesReusedPID(t *testing.T) {
	binary := buildHangonBinary(t)
	testHome := t.TempDir()
	run := func(env []string, args ...string) (string, error) {
		cmd := exec.Command(binary, args...)
		cmd.Env = append(envWithHome(testHome), env...)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}

	dir := filepath.Join(testHome, ".hangon")

	sleepPID, exited := spawnUnrelatedLongLivedProcess(t)

	if err := addSession(dir, "reused-all", "tcp", filepath.Join(testHome, "fake.sock"), sleepPID, 0, nil, "127.0.0.1:0"); err != nil {
		t.Fatalf("addSession: %v", err)
	}

	out, err := run(nil, "stopall", "--force")
	if err != nil {
		t.Fatalf("stopall --force failed: %s", out)
	}
	t.Logf("stopall output:\n%s", out)

	select {
	case <-exited:
		t.Fatalf("sleep process %d exited (was signalled) despite not being a hangon holder — PID-reuse identity guard failed", sleepPID)
	case <-time.After(300 * time.Millisecond):
		// Still running — the guard correctly refused to signal it.
	}

	if !strings.Contains(out, "reused by another process") {
		t.Errorf("expected stopall output to mention the PID-reuse identity mismatch, got: %s", out)
	}

	listOut, _ := run(nil, "list")
	if strings.Contains(listOut, "reused-all") {
		t.Errorf("session %q still present in state after stopall: %s", "reused-all", listOut)
	}
}
