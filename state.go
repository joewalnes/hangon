package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SessionInfo stores metadata about a running session.
type SessionInfo struct {
	Type      string   `json:"type"`
	Socket    string   `json:"socket"`
	HolderPID int      `json:"holderPID"`
	TargetPID int      `json:"targetPID,omitempty"`
	Command   []string `json:"command,omitempty"`
	Target    string   `json:"target,omitempty"` // for tcp/ws: address
	Started   string   `json:"started"`
}

// StateFile is the top-level state persisted to disk.
type StateFile struct {
	Sessions map[string]*SessionInfo `json:"sessions"`
}

// stateDir returns the directory for state storage.
// --local uses ./.hangon/, --global uses ~/.hangon/, auto-detect checks local first.
func stateDir(forceLocal, forceGlobal bool) (string, error) {
	if forceLocal {
		return filepath.Abs(".hangon")
	}
	if forceGlobal {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
		return filepath.Join(home, ".hangon"), nil
	}
	// Auto-detect: prefer local if it exists.
	local, _ := filepath.Abs(".hangon")
	if _, err := os.Stat(filepath.Join(local, "state.json")); err == nil {
		return local, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".hangon"), nil
}

func loadState(dir string) (*StateFile, error) {
	path := filepath.Join(dir, "state.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &StateFile{Sessions: make(map[string]*SessionInfo)}, nil
	}
	if err != nil {
		return nil, err
	}
	var sf StateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("corrupt state file %s: %w", path, err)
	}
	if sf.Sessions == nil {
		sf.Sessions = make(map[string]*SessionInfo)
	}
	return &sf, nil
}

func saveState(dir string, sf *StateFile) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "state.json"), data, 0o644)
}

// addSession registers a new session in state.
func addSession(dir, name, typ, socket string, holderPID, targetPID int, command []string, target string) error {
	sf, err := loadState(dir)
	if err != nil {
		return err
	}
	sf.Sessions[name] = &SessionInfo{
		Type:      typ,
		Socket:    socket,
		HolderPID: holderPID,
		TargetPID: targetPID,
		Command:   command,
		Target:    target,
		Started:   time.Now().Format(time.RFC3339),
	}
	return saveState(dir, sf)
}

// removeSession removes a session from state.
func removeSession(dir, name string) error {
	sf, err := loadState(dir)
	if err != nil {
		return err
	}
	delete(sf.Sessions, name)
	return saveState(dir, sf)
}

// getSession retrieves a session, or returns an error if not found.
func getSession(dir, name string) (*SessionInfo, error) {
	sf, err := loadState(dir)
	if err != nil {
		return nil, err
	}
	info, ok := sf.Sessions[name]
	if !ok {
		return nil, fmt.Errorf("session %q not found", name)
	}
	return info, nil
}
