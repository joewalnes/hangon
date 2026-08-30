package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestState_ConcurrentAddRemove reproduces the two failure modes fixed
// by atomic saveState + state-file locking:
//
//  1. Corruption: concurrent unsynchronized os.WriteFile calls can
//     interleave truncate/write, leaving state.json as invalid JSON
//     (observed in the wild as "corrupt state file ...: unexpected end
//     of JSON input").
//  2. Lost updates: two processes each load-mutate-save without a
//     lock; the second save clobbers the first's addition even though
//     both sessions are "real" and should both end up in the file.
//
// It spawns many goroutines, each calling addSession for a distinct
// session name against the same state dir concurrently, then verifies
// every single one of them is present in the final state.json with no
// corruption. This is intentionally testing the internal
// addSession/loadState functions directly (in-process, many
// goroutines) rather than shelling out to `hangon` subprocesses: the
// race lives entirely in these functions' handling of a shared file
// on disk, os.WriteFile/os.Rename are the same syscalls regardless of
// whether the caller is a goroutine or a separate OS process, and
// driving it via goroutines lets us reliably hit a tight race window
// and run many iterations quickly instead of paying subprocess
// start-up cost per attempt.
func TestState_ConcurrentAddRemove(t *testing.T) {
	dir := t.TempDir()

	const n = 100
	var wg sync.WaitGroup
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("sess-%03d", i)
			errs[i] = addSession(dir, name, "process", "/tmp/"+name+".sock", 1000+i, 0, nil, "")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("addSession %d returned error: %v", i, err)
		}
	}

	// The state file must be valid, complete JSON — not truncated or
	// interleaved from concurrent writers.
	path := filepath.Join(dir, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading state.json: %v", err)
	}
	var sf StateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatalf("state.json is corrupt: %v\ncontents:\n%s", err, data)
	}

	// Every single addSession call must have survived — none should
	// have been silently lost to a racing writer.
	if len(sf.Sessions) != n {
		missing := 0
		for i := 0; i < n; i++ {
			name := fmt.Sprintf("sess-%03d", i)
			if _, ok := sf.Sessions[name]; !ok {
				missing++
			}
		}
		t.Fatalf("got %d sessions, want %d (%d missing) — lost update under concurrent addSession", len(sf.Sessions), n, missing)
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("sess-%03d", i)
		info, ok := sf.Sessions[name]
		if !ok {
			t.Errorf("session %q missing from final state", name)
			continue
		}
		if info.HolderPID != 1000+i {
			t.Errorf("session %q holderPID=%d, want %d", name, info.HolderPID, 1000+i)
		}
	}

	// Also confirm loadState (used by getSession/list/etc.) agrees.
	loaded, err := loadState(dir)
	if err != nil {
		t.Fatalf("loadState after concurrent adds: %v", err)
	}
	if len(loaded.Sessions) != n {
		t.Errorf("loadState sees %d sessions, want %d", len(loaded.Sessions), n)
	}
}

// TestState_ConcurrentAddRemoveInterleaved additionally interleaves
// removeSession calls with addSession calls on overlapping names, to
// exercise the lock across both mutating entry points at once, not
// just addSession alone.
func TestState_ConcurrentAddRemoveInterleaved(t *testing.T) {
	dir := t.TempDir()

	const n = 60
	var wg sync.WaitGroup

	// Half the sessions: add then immediately remove.
	// Other half: add and leave in place.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("sess-%03d", i)
			if err := addSession(dir, name, "process", "/tmp/"+name+".sock", 2000+i, 0, nil, ""); err != nil {
				t.Errorf("addSession %d: %v", i, err)
				return
			}
			if i%2 == 0 {
				if err := removeSession(dir, name); err != nil {
					t.Errorf("removeSession %d: %v", i, err)
				}
			}
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("reading state.json: %v", err)
	}
	var sf StateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatalf("state.json is corrupt: %v\ncontents:\n%s", err, data)
	}

	wantRemaining := n / 2 // odd-indexed sessions should remain
	if len(sf.Sessions) != wantRemaining {
		t.Fatalf("got %d sessions remaining, want %d", len(sf.Sessions), wantRemaining)
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("sess-%03d", i)
		_, present := sf.Sessions[name]
		wantPresent := i%2 != 0
		if present != wantPresent {
			t.Errorf("session %q present=%v, want %v", name, present, wantPresent)
		}
	}
}
