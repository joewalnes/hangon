package main

import (
	"os"
	"path/filepath"
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
