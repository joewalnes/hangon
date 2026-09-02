package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildHangonBinary builds the hangon binary once and returns its path.
func buildHangonBinary(t *testing.T) string {
	t.Helper()
	return buildHangonBinaryNamed(t, "hangon")
}

// buildHangonBinaryNamed builds the hangon binary under a caller-chosen
// basename. gc's orphan-process scan (listServeProcesses,
// procscan_unix.go) matches "_serve" processes by comparing
// filepath.Base(argv[0]) against the basename of the currently running
// executable — so a test binary built as plain "hangon" is
// indistinguishable, to that scan, from a real, live, production
// hangon install (e.g. ~/go/bin/hangon) also named "hangon". Tests that
// deliberately exercise gc's orphan-reaping (which SIGKILLs whatever it
// classifies as orphaned) MUST use a distinctive name here instead, so
// the scan can only ever see processes this same test spawned — never
// a real hangon process running elsewhere on the machine.
func buildHangonBinaryNamed(t *testing.T, name string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), name)
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %s\n%s", err, out)
	}
	return binary
}

// envWithHome returns a process environment identical to the current
// one except HOME is replaced with home. Building this explicitly
// (rather than appending "HOME=..." to os.Environ(), which leaves the
// original HOME entry in place too) avoids relying on unspecified
// duplicate-env-var resolution order in the child process.
func envWithHome(home string) []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "HOME=") {
			env = append(env, kv)
		}
	}
	return append(env, "HOME="+home)
}

// buildHangonForTest builds the hangon binary once per test and
// returns its path, along with a helper to run it with an isolated
// HOME (so it never touches the real ~/.hangon).
func buildHangonForTest(t *testing.T) (binary string, run func(env []string, args ...string) (string, error)) {
	t.Helper()
	binary = buildHangonBinary(t)
	testHome := t.TempDir()
	run = func(extraEnv []string, args ...string) (string, error) {
		cmd := exec.Command(binary, args...)
		env := envWithHome(testHome)
		cmd.Env = append(env, extraEnv...)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	return binary, run
}

// TestCLI_UnknownFlagIsHardError reproduces the exact incident: a
// caller invoked `hangon start ... --dir /tmp/foo ...` believing
// --dir would scope state to an isolated directory. No such flag has
// ever existed (only --local/--global do); the old parser silently
// absorbed "--dir" and "/tmp/foo" into the command's positional args
// and ran against the real, unscoped state directory instead — with
// no error at all. That silent misconfiguration directly contributed
// to a `stopall` later killing unrelated sessions, since the caller
// believed they were operating in isolation.
//
// This asserts the fixed behavior: any unrecognized "--xxx" flag
// before the "--" separator is now a hard, non-zero-exit error naming
// the bad flag, across representative subcommands — not just `start`.
func TestCLI_UnknownFlagIsHardError(t *testing.T) {
	_, run := buildHangonForTest(t)

	cases := []struct {
		name string
		args []string
	}{
		{"start with bogus --dir", []string{"start", "process", "--dir", "/tmp/foo", "--", "python3", "-i"}},
		{"list with bogus --dir", []string{"list", "--dir", "/tmp/foo"}},
		{"stop with bogus --scope", []string{"stop", "--scope", "mine"}},
		{"stopall with bogus --quiet", []string{"stopall", "--quiet"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := run(nil, tc.args...)
			if err == nil {
				t.Fatalf("expected non-zero exit for unknown flag, got success. output: %s", out)
			}
			if !strings.Contains(out, "unknown flag") {
				t.Errorf("expected \"unknown flag\" in output, got: %s", out)
			}
			// Must not have silently attempted to start/act — e.g. no
			// tmux session or holder should have been spawned. We can't
			// easily assert a negative on process spawning here, but at
			// minimum the command must not report success/started.
			if strings.Contains(out, "started") {
				t.Errorf("command appears to have proceeded despite unknown flag: %s", out)
			}
		})
	}
}

// TestCLI_UnknownFlagEscapeHatch confirms the documented escape hatch
// still works: a literal argument that happens to start with "--" can
// still be passed through via the "--" separator, so the stricter
// unknown-flag rejection doesn't break legitimate use cases like
// sending literal dash-prefixed data.
func TestCLI_UnknownFlagEscapeHatch(t *testing.T) {
	_, run := buildHangonForTest(t)

	// "start process -- --version" runs the literal command `--version`
	// as the target program, which doesn't exist — but the point is
	// that parseFlags must not reject "--version" as an unknown flag,
	// since it comes after "--". We only assert it's NOT rejected as an
	// unknown flag (it may fail for other reasons, e.g. exec not found).
	out, err := run(nil, "start", "process", "--name", "escapetest", "--", "--version")
	if err != nil && strings.Contains(out, "unknown flag") {
		t.Errorf("literal arg after '--' was incorrectly rejected as an unknown flag: %s", out)
	}
	run(nil, "stop", "escapetest")
}

// TestCLI_StopallRequiresForce reproduces the second half of the same
// incident: `hangon stopall` was run by mistake multiple times and
// each time killed OTHER unrelated live sessions sharing the same
// default state directory, with no warning or confirmation of any
// kind. This asserts stopall now refuses to act without --force,
// previewing what it would stop instead, and leaves existing sessions
// running.
func TestCLI_StopallRequiresForce(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed, skipping")
	}
	_, run := buildHangonForTest(t)

	out, err := run(nil, "start", "process", "--name", "guarded", "--", "python3", "-i")
	if err != nil {
		t.Fatalf("start failed: %s", out)
	}
	defer run(nil, "stop", "guarded")

	// stopall without --force must refuse and list what it would do.
	out, err = run(nil, "stopall")
	if err == nil {
		t.Fatalf("expected stopall without --force to exit non-zero, got success: %s", out)
	}
	if !strings.Contains(out, "guarded") {
		t.Errorf("expected preview to mention session \"guarded\", got: %s", out)
	}
	if !strings.Contains(out, "--force") {
		t.Errorf("expected message to mention --force, got: %s", out)
	}

	// The session must still be listed as active — nothing was touched.
	out, err = run(nil, "list")
	if err != nil {
		t.Fatalf("list failed: %s", out)
	}
	if !strings.Contains(out, "guarded") {
		t.Fatalf("session \"guarded\" was stopped despite missing --force: %s", out)
	}

	// stopall --force actually stops it.
	out, err = run(nil, "stopall", "--force")
	if err != nil {
		t.Fatalf("stopall --force failed: %s", out)
	}
	out, _ = run(nil, "list")
	if strings.Contains(out, "guarded") {
		t.Errorf("session \"guarded\" still present after stopall --force: %s", out)
	}
}

// TestCLI_MistypedSessionName reproduces and fixes the P2 bug: a
// no-positional-arg command given an unrecognized word (a mistyped
// session name) used to silently fall through to operating on the
// default session and exit 0 — e.g. `hangon read typo` would read
// default's output and report success, giving no indication the named
// session doesn't exist. This table covers the full resolveSession
// contract: the no-positional-arg error case (this is the actual bug
// fix — it would fail against pre-fix code, which exits 0), the
// legitimate no-name-given case, resolving an exact existing name, and
// the "no default to fall back to either" combined error message.
func TestCLI_MistypedSessionName(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed, skipping")
	}
	_, run := buildHangonForTest(t)

	// No sessions exist at all yet: both the mistyped name and the
	// (nonexistent) default should be named in the error.
	out, err := run(nil, "read", "typo")
	if err == nil {
		t.Fatalf("expected non-zero exit for unknown session with no default, got success: %s", out)
	}
	if !strings.Contains(out, "typo") || !strings.Contains(out, "no default session") {
		t.Errorf("expected error naming both the mistyped name and the missing default, got: %s", out)
	}

	// Start the default session and a second, distinctly-named one.
	out, err = run(nil, "start", "process", "--", "python3", "-i")
	if err != nil {
		t.Fatalf("start default failed: %s", out)
	}
	defer run(nil, "stop")
	if _, err := run(nil, "expect", ">>>", "--timeout", "10"); err != nil {
		t.Fatalf("expect default ready failed")
	}

	out, err = run(nil, "start", "process", "--name", "myserver", "--", "python3", "-i")
	if err != nil {
		t.Fatalf("start myserver failed: %s", out)
	}
	defer run(nil, "stop", "myserver")
	if _, err := run(nil, "expect", "myserver", ">>>", "--timeout", "10"); err != nil {
		t.Fatalf("expect myserver ready failed")
	}

	// THE BUG: a default session now exists, so unfixed code would
	// silently read *default*'s output here and exit 0. Fixed code must
	// error instead, since "typo" cannot legitimately be anything but a
	// mistyped session name for a no-positional-arg command like read.
	out, err = run(nil, "read", "typo")
	if err == nil {
		t.Fatalf("BUG REPRODUCED: `hangon read typo` succeeded (exit 0) despite no session named \"typo\" — it silently operated on default. Output: %s", out)
	}
	if !strings.Contains(out, `"typo"`) {
		t.Errorf("expected error to name the mistyped session \"typo\", got: %s", out)
	}

	// Same story for other no-positional-arg commands sharing the fix.
	for _, cmd := range []string{"readall", "screen", "alive", "status", "stop"} {
		out, err := run(nil, cmd, "typo")
		if err == nil {
			t.Errorf("`hangon %s typo` succeeded despite no session named \"typo\": %s", cmd, out)
		}
		if !strings.Contains(out, `"typo"`) {
			t.Errorf("`hangon %s typo`: expected error naming \"typo\", got: %s", cmd, out)
		}
	}

	// Legitimate no-name usage must be completely unaffected: `read`
	// with no positional arg still targets default.
	out, err = run(nil, "read")
	if err != nil {
		t.Fatalf("`hangon read` (no session arg) unexpectedly failed: %s", out)
	}

	// An exact, existing session name still resolves normally.
	if _, err := run(nil, "sendline", "myserver", "1+1"); err != nil {
		t.Fatalf("sendline to myserver failed")
	}
	out, err = run(nil, "expect", "myserver", "2", "--timeout", "10")
	if err != nil {
		t.Fatalf("expect on myserver (exact name match) failed: %s", out)
	}

	// Data-taking commands preserve the historical, documented
	// ambiguity: an unmatched leading word is treated as data against
	// default, not an error. `hangon send typo hello` must still
	// succeed and actually send "typo hello" to the default session.
	if _, err := run(nil, "send", "typo", "hello"); err != nil {
		t.Fatalf("`hangon send typo hello` (data command, unmatched name = data) unexpectedly failed")
	}
	out, err = run(nil, "read")
	if err != nil || !strings.Contains(out, "typo hello") {
		t.Errorf("expected default session to have received literal \"typo hello\", got (err=%v): %s", err, out)
	}

	// `hangon send default ...` with the literal name "default" must
	// keep working (documented in --help as the well-defined case).
	if _, err := run(nil, "send", "default", "echo-explicit"); err != nil {
		t.Fatalf("`hangon send default ...` unexpectedly failed")
	}
	out, err = run(nil, "read")
	if err != nil || !strings.Contains(out, "echo-explicit") {
		t.Errorf("expected default session to have received data via explicit name, got (err=%v): %s", err, out)
	}
}
