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

// TestState_ConcurrentClaimSameName reproduces a name-collision leak
// that the old `hangon start` sequence was vulnerable to: it checked
// getSession unlocked, then — after spawning a holder process and
// waiting up to 5s for its socket — called addSession, locked, but far
// too late to prevent two concurrent `start --name X` invocations from
// both passing the early check and both registering (the second
// addSession call would silently win, leaking the first holder
// process forever since nothing would ever stop it).
//
// claimSessionName closes that window by making the "is this name
// free" check and "register it" a single locked operation, called
// immediately once a holder's PID is known. This test drives many
// goroutines racing to claim the exact same name concurrently and
// asserts exactly one of them succeeds.
func TestState_ConcurrentClaimSameName(t *testing.T) {
	dir := t.TempDir()

	// claimSessionName treats an existing entry as free to reclaim if
	// its holder isn't alive (matching the pre-existing "stale session"
	// cleanup behavior `start` has always had). To exercise the actual
	// race — two really-alive holders both claiming the same name —
	// every claimant must use a genuinely live PID, not a synthetic
	// one. The test process's own PID is alive for the whole test, so
	// use that for all of them; the test only cares that exactly one
	// claim succeeds; it doesn't need distinct PIDs to do that.
	pid := os.Getpid()

	const n = 50
	var wg sync.WaitGroup
	claimedCount := int32(0)
	var mu sync.Mutex
	var winnerPID int
	errCount := 0

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			claimed, err := claimSessionName(dir, "contested", "process", fmt.Sprintf("/tmp/c-%d.sock", i), pid, 0, nil, "")
			if claimed {
				mu.Lock()
				claimedCount++
				winnerPID = pid
				mu.Unlock()
			} else if err == nil {
				t.Errorf("claim %d: claimed=false but err=nil", i)
			} else {
				mu.Lock()
				errCount++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if claimedCount != 1 {
		t.Fatalf("got %d successful claims for the same name, want exactly 1 (leak: multiple holders would be spawned but only one tracked)", claimedCount)
	}
	if errCount != n-1 {
		t.Errorf("got %d rejected claims, want %d", errCount, n-1)
	}

	sf, err := loadState(dir)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if len(sf.Sessions) != 1 {
		t.Fatalf("got %d sessions in final state, want 1", len(sf.Sessions))
	}
	info, ok := sf.Sessions["contested"]
	if !ok {
		t.Fatal("session \"contested\" missing from final state")
	}
	if info.HolderPID != winnerPID {
		t.Errorf("state holderPID=%d, want winning claimant's PID %d", info.HolderPID, winnerPID)
	}
}

// TestState_MergeRemoveSessions_PreservesConcurrentAdd reproduces the
// lost-update bug in the old `stopall`: it read state once, spent time
// (up to ~600ms per session) signaling/killing each holder, then wrote
// back a brand new empty StateFile — discarding any session added by a
// different process during that window, even though that session's
// holder and tmux session were still alive and now completely
// untracked (a direct contributor to the orphaned-process problem
// `hangon gc` exists to clean up).
//
// mergeRemoveSessions fixes this by only ever deleting the exact
// (name, holderPID) pairs the caller actually processed. This test
// simulates the race directly: start with two sessions, "process" them
// (as stopall would), but concurrently add a third session with a
// name that reuses one of the two being removed — before
// mergeRemoveSessions's lock is acquired. The reused name must survive
// with its NEW holderPID; only entries matching the processed
// holderPID exactly should be removed.
func TestState_MergeRemoveSessions_PreservesConcurrentAdd(t *testing.T) {
	dir := t.TempDir()

	if err := addSession(dir, "a", "process", "/tmp/a.sock", 100, 0, nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := addSession(dir, "b", "process", "/tmp/b.sock", 200, 0, nil, ""); err != nil {
		t.Fatal(err)
	}

	processed := map[string]int{"a": 100, "b": 200}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Simulate a concurrent `hangon start --name a` that reused
		// the name "a" (after its old holder was stopped by someone
		// else) with a brand new holder PID, racing against our
		// mergeRemoveSessions call below.
		if err := addSession(dir, "a", "process", "/tmp/a2.sock", 999, 0, nil, ""); err != nil {
			t.Errorf("concurrent addSession: %v", err)
		}
	}()

	if err := mergeRemoveSessions(dir, processed); err != nil {
		t.Fatalf("mergeRemoveSessions: %v", err)
	}
	wg.Wait()

	sf, err := loadState(dir)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}

	// "b" was cleanly processed and not touched by anything else: gone.
	if _, ok := sf.Sessions["b"]; ok {
		t.Errorf("session \"b\" still present after mergeRemoveSessions, want removed")
	}

	// "a" was reclaimed by a concurrent add with a different holderPID
	// (999) after we snapshotted holderPID 100 for it — the merge must
	// not remove it, since doing so would silently orphan the new
	// holder (still running, still has a tmux session, but no longer
	// tracked in state.json at all).
	info, ok := sf.Sessions["a"]
	if !ok {
		t.Fatal("session \"a\" was removed even though a concurrent process re-claimed it with a new holder — this is the lost-update bug mergeRemoveSessions exists to prevent")
	}
	if info.HolderPID != 999 {
		t.Errorf("session \"a\" holderPID=%d, want 999 (the concurrently-claimed holder)", info.HolderPID)
	}
}

// TestState_ConcurrentReadsDuringWrites investigates a separately
// reported symptom: under heavy concurrent hangon usage, `list` /
// `getSession` intermittently reported sessions as missing that
// should have existed. getSession and loadState are deliberately
// unlocked reads (see their doc comments) on the theory that
// saveState's atomic rename means a reader always observes either a
// fully-old or fully-new state.json, never a torn write, so an
// unlocked read cannot itself corrupt or lose data — it can only ever
// return an answer that was accurate at *some* point in time.
//
// This hammers loadState with many concurrent readers racing against
// many concurrent addSession/removeSession writers (unlike
// TestState_ConcurrentAddRemove, which only exercises writers) and
// asserts every read succeeds, parses as valid JSON, and that any
// session present in a snapshot has fully-populated, non-corrupted
// fields (never a half-written struct). It does not and cannot assert
// a session is always found by name — a concurrent removeSession can
// legitimately make it vanish between a caller deciding to look it up
// and the read happening — which is the point: if this passes,
// "session not found" under load is consistent with genuine timing
// (or with the start/stopall races fixed elsewhere in this change),
// not with a bug in the read path itself.
func TestState_ConcurrentReadsDuringWrites(t *testing.T) {
	dir := t.TempDir()
	const writers = 20
	const readers = 20
	const cyclesPerWriter = 20

	var writersWG sync.WaitGroup
	for w := 0; w < writers; w++ {
		writersWG.Add(1)
		go func(w int) {
			defer writersWG.Done()
			name := fmt.Sprintf("rw-sess-%02d", w)
			for c := 0; c < cyclesPerWriter; c++ {
				if err := addSession(dir, name, "process", fmt.Sprintf("/tmp/%s.sock", name), 10000+w, 0, nil, ""); err != nil {
					t.Errorf("writer %d addSession: %v", w, err)
					return
				}
				if err := removeSession(dir, name); err != nil {
					t.Errorf("writer %d removeSession: %v", w, err)
					return
				}
			}
		}(w)
	}

	stop := make(chan struct{})
	readErrs := make(chan error, readers*10000)
	var readersWG sync.WaitGroup
	for r := 0; r < readers; r++ {
		readersWG.Add(1)
		go func() {
			defer readersWG.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				sf, err := loadState(dir)
				if err != nil {
					readErrs <- fmt.Errorf("loadState: %w", err)
					continue
				}
				for name, info := range sf.Sessions {
					if info == nil {
						readErrs <- fmt.Errorf("session %q has nil info", name)
						continue
					}
					if info.Type == "" || info.Socket == "" || info.HolderPID == 0 {
						readErrs <- fmt.Errorf("session %q has a partially-populated entry: %+v", name, info)
					}
				}
			}
		}()
	}

	writersWG.Wait()
	close(stop)
	readersWG.Wait()
	close(readErrs)

	for err := range readErrs {
		t.Error(err)
	}
}
