package main

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeBackend is a minimal Backend implementation backed directly by a
// RingBuffer, so doExpect can be exercised without a real process/tmux/etc.
type fakeBackend struct {
	output *RingBuffer
}

func newFakeBackend(size int) *fakeBackend {
	return &fakeBackend{output: NewRingBuffer(size)}
}

func (f *fakeBackend) Start() error               { return nil }
func (f *fakeBackend) Send(data []byte) error     { return nil }
func (f *fakeBackend) Output() *RingBuffer        { return f.output }
func (f *fakeBackend) Stderr() *RingBuffer        { return nil }
func (f *fakeBackend) Screen() (string, error)    { return "", ErrNotSupported }
func (f *fakeBackend) SendKeys(keys string) error { return ErrNotSupported }
func (f *fakeBackend) Alive() bool                { return true }
func (f *fakeBackend) Wait() (int, error)         { return 0, ErrNotSupported }
func (f *fakeBackend) TargetPID() int             { return 0 }
func (f *fakeBackend) Close() error               { return nil }

func newTestHolder(size int) (*SessionHolder, *fakeBackend) {
	fb := newFakeBackend(size)
	sh := NewSessionHolder(fb, "")
	sh.timeout = 2 * time.Second
	return sh, fb
}

// (a) A pattern split across two writes, with a notify in between, must
// still match. Before the fix, doExpect matched each FIFO chunk in
// isolation via buf.ReadFrom, so ">>" arriving in one write and "> " in the
// next would never satisfy `>>> ` and this test would time out.
func TestDoExpect_PatternSplitAcrossTwoWrites(t *testing.T) {
	sh, fb := newTestHolder(1024)

	type outcome struct {
		resp *Response
	}
	done := make(chan outcome, 1)
	go func() {
		resp := sh.doExpect(ExpectParams{Pattern: `>>> `, Timeout: 2})
		done <- outcome{resp}
	}()

	time.Sleep(30 * time.Millisecond)
	fb.output.Write([]byte(">>"))
	time.Sleep(30 * time.Millisecond)
	fb.output.Write([]byte("> "))

	select {
	case o := <-done:
		if !o.resp.OK {
			t.Fatalf("expect failed: %s", o.resp.Error)
		}
		if o.resp.Result != ">>> " {
			t.Errorf("Result = %q, want %q", o.resp.Result, ">>> ")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("doExpect did not return in time (pattern-split bug)")
	}
}

// (b) A pattern split across many single-byte writes must still match.
func TestDoExpect_PatternSplitAcrossManyByteWrites(t *testing.T) {
	sh, fb := newTestHolder(1024)

	done := make(chan *Response, 1)
	go func() {
		done <- sh.doExpect(ExpectParams{Pattern: `>>> `, Timeout: 3})
	}()

	for _, b := range []byte("building\n>>> ") {
		time.Sleep(2 * time.Millisecond)
		fb.output.Write([]byte{b})
	}

	select {
	case resp := <-done:
		if !resp.OK {
			t.Fatalf("expect failed: %s", resp.Error)
		}
		if !strings.HasSuffix(resp.Result, ">>> ") {
			t.Errorf("Result = %q, want suffix %q", resp.Result, ">>> ")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("doExpect did not return in time (byte-at-a-time split)")
	}
}

// (c) Pre-match bytes must not vanish: they appear in Result, and bytes
// written AFTER the match remain readable afterwards (no silent loss in
// either direction).
//
// "building\n" and "ready\n" are written as two SEPARATE chunks (with the
// expect already in flight) precisely because the bug only manifests across
// chunk boundaries: before the fix, doExpect matched each new ReadFrom
// chunk in isolation, so once "building\n" chunk failed to match it was
// discarded, and Result ended up as just "ready\n" — silently dropping the
// pre-match "building" line. Writing both in a single chunk up front would
// not exercise that path.
func TestDoExpect_PreMatchBytesNotLost(t *testing.T) {
	sh, fb := newTestHolder(1024)

	done := make(chan *Response, 1)
	go func() {
		done <- sh.doExpect(ExpectParams{Pattern: `ready\n`, Timeout: 2})
	}()

	time.Sleep(30 * time.Millisecond)
	fb.output.Write([]byte("building\n"))
	time.Sleep(30 * time.Millisecond)
	fb.output.Write([]byte("ready\n"))

	var resp *Response
	select {
	case resp = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("doExpect did not return in time")
	}
	if !resp.OK {
		t.Fatalf("expect failed: %s", resp.Error)
	}
	if !strings.Contains(resp.Result, "building") {
		t.Errorf("Result %q lost pre-match data %q", resp.Result, "building")
	}
	if resp.Result != "building\nready\n" {
		t.Errorf("Result = %q, want %q", resp.Result, "building\nready\n")
	}

	// Bytes written after the match must still be visible to a later read.
	fb.output.Write([]byte("extra"))
	sh.mu.Lock()
	rest := fb.output.ReadFrom(&sh.readCursor)
	sh.mu.Unlock()
	if string(rest) != "extra" {
		t.Errorf("post-match bytes lost: read got %q, want %q", rest, "extra")
	}
}

// (d) Timeout must still fire promptly when the pattern is genuinely absent.
func TestDoExpect_TimesOutWhenPatternAbsent(t *testing.T) {
	sh, fb := newTestHolder(1024)
	fb.output.Write([]byte("no match here\n"))

	start := time.Now()
	resp := sh.doExpect(ExpectParams{Pattern: `nope`, Timeout: 0.2})
	elapsed := time.Since(start)

	if resp.OK {
		t.Fatalf("expected timeout, got match: %q", resp.Result)
	}
	if elapsed < 200*time.Millisecond {
		t.Errorf("returned too early: %v", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Errorf("took too long to time out: %v", elapsed)
	}
}

// (e) Anchored patterns: ^ should anchor to the start of the expect
// window (the cursor position when expect began), not match arbitrarily
// mid-stream.
func TestDoExpect_AnchoredPattern(t *testing.T) {
	sh, fb := newTestHolder(1024)
	fb.output.Write([]byte("noise>>> "))

	resp := sh.doExpect(ExpectParams{Pattern: `^>>> `, Timeout: 0.2})
	if resp.OK {
		t.Fatalf("^ anchored pattern should not match mid-stream, got %q", resp.Result)
	}

	sh2, fb2 := newTestHolder(1024)
	fb2.output.Write([]byte(">>> "))
	resp2 := sh2.doExpect(ExpectParams{Pattern: `^>>> `, Timeout: 2})
	if !resp2.OK {
		t.Fatalf("expected ^ anchored match at start of window: %s", resp2.Error)
	}
	if resp2.Result != ">>> " {
		t.Errorf("Result = %q, want %q", resp2.Result, ">>> ")
	}
}

// The accumulation used to find split patterns must stay bounded: a chatty
// process producing far more than the ring buffer's capacity of unmatched
// noise, followed by a match at the tail, must still complete (and match)
// rather than growing memory without bound.
func TestDoExpect_AccumulationBoundedButStillMatches(t *testing.T) {
	sh, fb := newTestHolder(4096)

	done := make(chan *Response, 1)
	go func() {
		done <- sh.doExpect(ExpectParams{Pattern: `FOUND`, Timeout: 3})
	}()

	for i := 0; i < 20; i++ {
		fb.output.Write(bytes.Repeat([]byte("x"), 4096))
	}
	fb.output.Write([]byte("FOUND"))

	select {
	case resp := <-done:
		if !resp.OK {
			t.Fatalf("expect failed: %s", resp.Error)
		}
		if !strings.HasSuffix(resp.Result, "FOUND") {
			t.Errorf("Result = %q, want suffix %q", resp.Result, "FOUND")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("doExpect did not return in time")
	}
}

// TestServe_SocketIsOwnerOnlyUnderLaxUmask reproduces the P2 security
// bug directly: previously, SessionHolder.Serve created its control
// socket with net.Listen alone and relied entirely on the process's
// ambient umask to keep it private. Under a permissive umask (002, or
// 000 as seen in some containers/CI images) that produced a
// world-connectable Unix socket -- any local user able to connect could
// inject keystrokes into (or read the output of) another user's
// session, i.e. code execution as the session's owner.
//
// This test forces umask 000 (the worst case) before calling Serve, so
// if the explicit os.Chmod(sh.socketPath, 0600) belt-and-braces step in
// Serve were ever removed, the socket would come back as
// world-readable/writable (mode 0777 under umask 000, since that's what
// net.Listen's underlying bind() leaves a new Unix socket file at) and
// this test would fail. Confirmed by temporarily reverting that Chmod
// call locally: this test fails with "socket mode = -rwxrwxrwx, want
// -rw-------" exactly as expected -- see the commit message for the
// transcript.
func TestServe_SocketIsOwnerOnlyUnderLaxUmask(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket permission bits are not meaningful on windows")
	}

	// Use a short, manually-created temp dir as HANGON_RUN_DIR rather
	// than t.TempDir(): AF_UNIX paths are capped at ~104 bytes on
	// macOS/BSD, and t.TempDir() embeds this test's full (long) name.
	// The override also keeps this test from touching the one real,
	// shared "<tmp>/hangon-<uid>/" directory every unparameterized
	// hangon invocation by this user (including other concurrent tests,
	// or other agents on a shared machine) would otherwise resolve to.
	base, err := os.MkdirTemp("", "hangon-sock-test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(base)
	t.Setenv(hangonRunDirEnv, base)

	// Exercise the real production path: runtimeDir creates/enforces the
	// 0700 parent directory, then Serve binds the socket inside it.
	runDir, err := runtimeDir()
	if err != nil {
		t.Fatalf("runtimeDir: %v", err)
	}
	socketPath := runDir + "/test.sock"

	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

	fb := newFakeBackend(1024)
	sh := NewSessionHolder(fb, socketPath)
	serveErr := make(chan error, 1)
	go func() { serveErr <- sh.Serve() }()
	defer sh.Close()

	// Wait for the socket to appear rather than sleeping a fixed amount.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			select {
			case err := <-serveErr:
				t.Fatalf("Serve exited before creating the socket: %v", err)
			default:
			}
			t.Fatal("socket did not appear in time")
		}
		time.Sleep(10 * time.Millisecond)
	}

	fi, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0600 {
		t.Errorf("socket mode = %v, want %v (umask was forced to 000, so this proves the explicit chmod, not the umask, is what protects it)", fi.Mode(), os.FileMode(0600))
	}

	// Belt and braces: the parent directory must also be owner-only,
	// independently of the socket file's own mode.
	dfi, err := os.Stat(runDir)
	if err != nil {
		t.Fatalf("stat runtime dir: %v", err)
	}
	if got := dfi.Mode().Perm(); got != 0700 {
		t.Errorf("runtime dir mode = %v, want %v", dfi.Mode(), os.FileMode(0700))
	}

	// Sanity: the socket must still actually work (send/read round trip
	// isn't blocked by the tightened permissions for the owning user).
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial socket as owner: %v", err)
	}
	conn.Close()
}

// TestRuntimeDir_FixesLooseExistingPermissions covers the "verify/chmod
// even if it pre-exists" half of the fix: a runtime dir left behind by
// an older hangon version (or created some other way) with loose
// permissions must be tightened back to 0700 the next time runtimeDir
// runs, not left as-is because os.MkdirAll no-ops on an existing
// directory.
func TestRuntimeDir_FixesLooseExistingPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits are not meaningful on windows")
	}
	// HANGON_RUN_DIR isolates this test from the one real, shared
	// runtime directory (see TestServe_SocketIsOwnerOnlyUnderLaxUmask) —
	// important here specifically, since this test deliberately loosens
	// the directory's permissions before the fix runs, and must not do
	// that to a directory any concurrent hangon invocation might depend
	// on being 0700.
	base := t.TempDir()
	t.Setenv(hangonRunDirEnv, base)
	runDirPath := filepath.Join(base, fmt.Sprintf("hangon-%d", os.Getuid()))
	if err := os.MkdirAll(runDirPath, 0777); err != nil {
		t.Fatalf("pre-creating loose run dir: %v", err)
	}
	if err := os.Chmod(runDirPath, 0777); err != nil {
		t.Fatalf("chmod loose: %v", err)
	}

	got, err := runtimeDir()
	if err != nil {
		t.Fatalf("runtimeDir: %v", err)
	}
	fi, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0700 {
		t.Errorf("pre-existing loose run dir left at mode %v after runtimeDir, want 0700", perm)
	}
}
