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

// saveState writes sf to dir/state.json atomically: it writes the full
// contents to a temp file in the same directory (so the rename below
// is guaranteed to be on the same filesystem) and then renames it over
// the real path. Rename is atomic on POSIX filesystems (and on
// Windows, via os.Rename's ReplaceFile-based implementation), so
// concurrent readers of state.json always see either the old complete
// file or the new complete file, never a partial write.
//
// This alone does not prevent lost updates from concurrent
// read-modify-write sequences; callers that mutate state must hold
// the lock from lockStateFile/withStateLock across their whole
// load-mutate-save sequence.
func saveState(dir string, sf *StateFile) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".state.json.tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	// Ensure the temp file is cleaned up if anything below fails before
	// the rename completes.
	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, filepath.Join(dir, "state.json")); err != nil {
		return err
	}
	success = true
	return nil
}

// withStateLock runs fn while holding an exclusive lock on dir's state
// file. Every read-modify-write sequence against state.json (load,
// mutate in memory, save) must be wrapped in this so that concurrent
// hangon processes serialize instead of racing and silently clobbering
// each other's updates.
func withStateLock(dir string, fn func() error) error {
	unlock, err := lockStateFile(dir)
	if err != nil {
		return fmt.Errorf("failed to acquire state lock: %w", err)
	}
	defer unlock()
	return fn()
}

// addSession registers a new session in state.
func addSession(dir, name, typ, socket string, holderPID, targetPID int, command []string, target string) error {
	return withStateLock(dir, func() error {
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
	})
}

// removeSession removes a session from state.
func removeSession(dir, name string) error {
	return withStateLock(dir, func() error {
		sf, err := loadState(dir)
		if err != nil {
			return err
		}
		delete(sf.Sessions, name)
		return saveState(dir, sf)
	})
}

// getSession retrieves a session, or returns an error if not found.
//
// This does not need to hold the state lock: saveState's atomic
// rename guarantees loadState always observes a complete, valid
// state.json (either fully before or fully after any given update),
// never a torn write, so an unlocked read cannot see corrupt or
// half-written data. It may race with a concurrent add/remove in the
// sense of returning slightly stale data, which is inherent to any
// read that isn't part of the same critical section as a subsequent
// write.
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

// claimSessionName atomically checks whether name is free (no session
// with a live holder already registered under it) and, if so,
// registers the given session info in the same locked critical
// section as the check.
//
// This must be called with a holderPID that has *already been
// spawned* (i.e. right after cmd.Start() succeeds), not before. That
// ordering matters: the old code path did an unlocked getSession
// check, then — much later, after waiting up to 5s for the holder's
// socket to come up — called the locked addSession. Two concurrent
// `hangon start --name X` invocations could both pass the early check
// (nothing registered yet), both spawn a holder process, and then
// both add themselves to state.json under the same name; the second
// addSession's write wins and the first holder process is never
// stopped, leaking it forever (a likely contributor to orphaned
// `_serve` processes — see `hangon gc`). Claiming the name atomically
// immediately after spawning collapses that window to effectively
// zero: whichever process's claim loses finds the name already taken
// (by the other's now-registered holder) and kills the holder it just
// spawned instead of leaking it.
func claimSessionName(dir, name, typ, socket string, holderPID, targetPID int, command []string, target string) (bool, error) {
	claimed := false
	err := withStateLock(dir, func() error {
		sf, err := loadState(dir)
		if err != nil {
			return err
		}
		if existing, ok := sf.Sessions[name]; ok && isProcessAlive(existing.HolderPID) {
			return fmt.Errorf("session %q already exists (PID %d). Stop it first or use a different --name.", name, existing.HolderPID)
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
		claimed = true
		return saveState(dir, sf)
	})
	return claimed, err
}

// mergeRemoveSessions removes exactly the (name -> holderPID) pairs in
// processed from state, as a single locked read-modify-write, but only
// when the current entry for that name still has the same holderPID.
//
// This is used by `stopall` after it has signaled/killed a snapshot of
// sessions taken at the start of the run. Killing holders can take a
// while (multiple 100ms polls per session), during which another
// hangon process may legitimately add a new session — possibly
// reusing a name stopall is about to remove, if that name's old holder
// was stopped and something raced to reuse it. A naive "load once,
// kill everything, write back an empty state" (the original
// implementation) silently discards any such concurrent addition, even
// though its holder process and tmux session are still alive and now
// completely untracked — exactly the kind of state/process split that
// produces orphaned resources `gc` has to clean up later. Matching on
// holderPID as well as name ensures we only ever delete the entries we
// actually processed.
func mergeRemoveSessions(dir string, processed map[string]int) error {
	if len(processed) == 0 {
		return nil
	}
	return withStateLock(dir, func() error {
		sf, err := loadState(dir)
		if err != nil {
			return err
		}
		for name, holderPID := range processed {
			if info, ok := sf.Sessions[name]; ok && info.HolderPID == holderPID {
				delete(sf.Sessions, name)
			}
		}
		return saveState(dir, sf)
	})
}

// setSessionTargetPID updates the TargetPID of an existing session as
// a single locked read-modify-write. It replaces the previous pattern
// (used by the holder process to record the child's PID after fork)
// that called getSession and loadState separately and stitched the
// results back together unlocked, which was itself a lost-update
// hazard on top of the general state.json race.
func setSessionTargetPID(dir, name string, targetPID int) error {
	return withStateLock(dir, func() error {
		sf, err := loadState(dir)
		if err != nil {
			return err
		}
		info, ok := sf.Sessions[name]
		if !ok {
			return fmt.Errorf("session %q not found", name)
		}
		info.TargetPID = targetPID
		return saveState(dir, sf)
	})
}
