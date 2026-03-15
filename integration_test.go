package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
