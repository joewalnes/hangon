package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestState_AddGetRemove(t *testing.T) {
	dir := t.TempDir()

	err := addSession(dir, "test", "process", "/tmp/test.sock", 1234, 5678, []string{"python3"}, "python3")
	if err != nil {
		t.Fatal(err)
	}

	info, err := getSession(dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	if info.Type != "process" {
		t.Errorf("type=%q, want process", info.Type)
	}
	if info.HolderPID != 1234 {
		t.Errorf("holderPID=%d, want 1234", info.HolderPID)
	}
	if info.TargetPID != 5678 {
		t.Errorf("targetPID=%d, want 5678", info.TargetPID)
	}

	err = removeSession(dir, "test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = getSession(dir, "test")
	if err == nil {
		t.Error("expected error after removal")
	}
}

func TestState_MultipleSessions(t *testing.T) {
	dir := t.TempDir()

	addSession(dir, "a", "process", "/tmp/a.sock", 1, 0, nil, "")
	addSession(dir, "b", "tcp", "/tmp/b.sock", 2, 0, nil, "localhost:6379")

	sf, err := loadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(sf.Sessions) != 2 {
		t.Errorf("got %d sessions, want 2", len(sf.Sessions))
	}
}

func TestState_LoadEmpty(t *testing.T) {
	dir := t.TempDir()

	sf, err := loadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(sf.Sessions) != 0 {
		t.Errorf("got %d sessions, want 0", len(sf.Sessions))
	}
}

func TestState_Persistence(t *testing.T) {
	dir := t.TempDir()

	addSession(dir, "persist", "ws", "/tmp/p.sock", 99, 0, nil, "wss://example.com")

	// Verify file exists on disk.
	path := filepath.Join(dir, "state.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("state.json not created")
	}

	// Load fresh and verify.
	sf, _ := loadState(dir)
	info := sf.Sessions["persist"]
	if info == nil {
		t.Fatal("session not found after reload")
	}
	if info.Type != "ws" {
		t.Errorf("type=%q, want ws", info.Type)
	}
}

// TestCheckUnixSocketPathLen locks in the boundary of the portable
// AF_UNIX path guard: this must fire before net.Listen ever gets a
// chance to fail with an opaque "bind: invalid argument" if $TMPDIR (or
// its HANGON_RUN_DIR override) or --name pushes the socket path over
// the limit.
func TestCheckUnixSocketPathLen(t *testing.T) {
	short := "/tmp/hangon-default-123.sock"
	if err := checkUnixSocketPathLen(short); err != nil {
		t.Errorf("short path unexpectedly rejected: %v", err)
	}

	atLimit := strings.Repeat("a", maxUnixSocketPathLen)
	if err := checkUnixSocketPathLen(atLimit); err != nil {
		t.Errorf("path exactly at the limit unexpectedly rejected: %v", err)
	}

	overLimit := strings.Repeat("a", maxUnixSocketPathLen+1)
	err := checkUnixSocketPathLen(overLimit)
	if err == nil {
		t.Fatal("expected error for path over the limit, got nil")
	}
	if !strings.Contains(err.Error(), "too long") || !strings.Contains(err.Error(), "TMPDIR") {
		t.Errorf("error message not actionable enough: %v", err)
	}
}

// TestRuntimeDir_CreatesOwnerOnly asserts the basic case: a freshly
// created runtime dir must be 0700, not left wider by the process's
// ambient umask (MkdirAll's requested mode is reduced by umask, which is
// exactly why runtimeDir chmods explicitly afterwards).
func TestRuntimeDir_CreatesOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits are not meaningful on windows")
	}
	// Isolate from the one real, shared runtime directory — see
	// TestServe_SocketIsOwnerOnlyUnderLaxUmask in holder_test.go.
	t.Setenv(hangonRunDirEnv, t.TempDir())

	dir, err := runtimeDir()
	if err != nil {
		t.Fatalf("runtimeDir: %v", err)
	}
	wantSuffix := fmt.Sprintf("hangon-%d", os.Getuid())
	if filepath.Base(dir) != wantSuffix {
		t.Errorf("runtimeDir returned %q, want a path ending in /%s", dir, wantSuffix)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0700 {
		t.Errorf("runtime dir mode = %v, want 0700", perm)
	}
}
