package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// hangonTmuxSessionRE matches tmux session names created by the process
// backend (see backend_process.go: pb.tmuxSess = "hangon-<holderPID>").
var hangonTmuxSessionRE = regexp.MustCompile(`^hangon-(\d+)$`)

// hangonFIFONameRE matches the FIFO files the process backend creates
// for pipe-pane output streaming (see backend_process.go: pb.fifoPath =
// os.TempDir()/hangon-<holderPID>.fifo).
var hangonFIFONameRE = regexp.MustCompile(`^hangon-(\d+)\.fifo$`)

// runGC reconciles state.json against reality: the tmux sessions that
// actually exist and the "hangon _serve" holder processes that are
// actually running. All three are supposed to move together (a
// session's state entry, its holder process, and — for process-type
// sessions — its tmux session), but nothing keeps them atomically in
// sync across an ungraceful death: a holder can be SIGKILLed (crash,
// OOM, `kill -9`) without ever removing its state entry or its tmux
// session, and — independently — a holder process can exist without a
// state entry if it was orphaned before registration completed or the
// state entry was lost some other way. Left alone, both failure modes
// accumulate indefinitely: stale state.json entries pointing at dead
// PIDs, tmux sessions with nothing left to stop them, and `_serve`
// processes with no session name pointing back at them.
//
// gc is deliberately idempotent and safe to run at any time (including
// concurrently with other hangon commands, since the state mutation
// happens under the normal state lock): it only ever acts on resources
// it can positively confirm are unreferenced by anything live *and
// scoped to the same state directory gc itself was run against*.
//
// That state-directory scoping matters because state.json, tmux
// sessions, and "hangon _serve" processes are each machine- (or
// server-, for tmux) wide resources that multiple, independent state
// directories can all be pointing into at once: a second hangon
// install, a --local run in some other project checkout, or another
// agent using its own isolated state dir. A holder process or tmux
// session that isn't tracked by *this* state dir's state.json is not
// thereby orphaned — it may simply belong to one of those other state
// dirs. gcOrphanedServeProcesses and gcOrphanedTmuxSessions each
// cross-check the state dir recorded in a candidate's own
// "--state-dir" argument (or, for tmux sessions, its holder's) before
// ever treating it as fair game; see their doc comments for the exact
// rule and its one known limitation (cmdline parsing of paths
// containing spaces).
func runGC(args []string) {
	f := parseFlags(args)
	if len(f.rest) > 0 {
		fatal("usage: hangon gc [--dry-run] [--local|--global]")
	}
	dryRun := f.dryRun
	dir := f.dir()

	fmt.Printf("hangon gc: scanning %s%s\n", dir, dryRunSuffix(dryRun))

	staleNames, err := gcStaleStateEntries(dir, dryRun)
	if err != nil {
		fatal(err.Error())
	}
	for _, name := range staleNames {
		fmt.Printf("  %s stale state entry %q (holder process not running)\n", verb(dryRun, "would remove", "removed"), name)
	}

	// Recompute which holder PIDs are legitimately tracked, so the
	// orphan scans below (tmux sessions, _serve processes) agree with
	// what step 1 just did — including in dry-run mode, where nothing
	// was actually written but we still want the preview to reflect
	// what a real run would see.
	live, err := livePIDs(dir, dryRun, staleNames)
	if err != nil {
		fatal(err.Error())
	}

	// Scan "hangon _serve" processes once and share the result between
	// the tmux-session scan and the process scan below, so both agree
	// on exactly which processes (and their --state-dir) exist at this
	// instant, and so a scan failure is reported exactly once.
	procs, err := listServeProcesses()
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: could not scan for orphaned hangon processes: %v\n", err)
		procs = nil
	}

	orphanTmux := gcOrphanedTmuxSessions(dir, live, procs, dryRun)
	orphanProcs := gcOrphanedServeProcesses(dir, live, procs, dryRun)
	orphanFIFOs := gcOrphanedFIFOs(dryRun)

	fmt.Printf("\nhangon gc summary: %d stale state %s, %d orphaned tmux %s, %d orphaned holder %s, %d orphaned %s%s\n",
		len(staleNames), pluralWord(len(staleNames), "entry", "entries"),
		orphanTmux, pluralWord(orphanTmux, "session", "sessions"),
		orphanProcs, pluralWord(orphanProcs, "process", "processes"),
		orphanFIFOs, pluralWord(orphanFIFOs, "FIFO", "FIFOs"),
		dryRunSuffix(dryRun))
}

// gcStaleStateEntries removes (unless dryRun) state.json entries whose
// holder process is no longer alive, cleaning up their socket file and
// (for process-type sessions) their tmux session at the same time. It
// returns the names removed (or, in dry-run mode, that would be
// removed).
func gcStaleStateEntries(dir string, dryRun bool) ([]string, error) {
	var staleNames []string
	err := withStateLock(dir, func() error {
		sf, err := loadState(dir)
		if err != nil {
			return err
		}
		changed := false
		for name, info := range sf.Sessions {
			if isProcessAlive(info.HolderPID) {
				continue
			}
			staleNames = append(staleNames, name)
			if dryRun {
				continue
			}
			delete(sf.Sessions, name)
			changed = true
			os.Remove(info.Socket)
			if info.Type == "process" {
				tmuxCmd("kill-session", "-t", tmuxExact(fmt.Sprintf("hangon-%d", info.HolderPID))).Run()
			}
		}
		if !changed {
			return nil
		}
		return saveState(dir, sf)
	})
	return staleNames, err
}

// livePIDs returns the set of holder PIDs that remain tracked in
// state.json after gcStaleStateEntries has run (or, for dry-run, the
// set that *would* remain).
func livePIDs(dir string, dryRun bool, removedNames []string) (map[int]bool, error) {
	sf, err := loadState(dir)
	if err != nil {
		return nil, err
	}
	removed := make(map[string]bool, len(removedNames))
	for _, n := range removedNames {
		removed[n] = true
	}
	live := make(map[int]bool, len(sf.Sessions))
	for name, info := range sf.Sessions {
		if dryRun && removed[name] {
			continue
		}
		live[info.HolderPID] = true
	}
	return live, nil
}

// gcOrphanedTmuxSessions kills (unless dryRun) tmux sessions matching
// the hangon-<pid> naming convention whose PID isn't in live AND that
// this state directory can positively confirm are its own to manage.
// Only hangon's dedicated tmux server (see tmux.go) is scanned — the
// user's default server is never touched. If tmux isn't installed or
// the hangon server isn't running, this is a no-op — there is nothing
// to scan.
//
// A candidate pid not in live falls into one of three cases:
//
//  1. The pid is no longer running at all (isProcessAlive is false):
//     a true orphan — nothing anywhere still owns this session — safe
//     to kill regardless of state dir.
//  2. The pid is running and procs (from listServeProcesses, scanned
//     once by runGC and shared with gcOrphanedServeProcesses) can
//     positively identify its --state-dir as this same dir: it's a
//     genuine untracked-but-alive holder for THIS state dir (e.g.
//     orphaned before its state.json entry was ever written) — safe
//     to kill.
//  3. Anything else — the pid is running but belongs (as far as can
//     be told) to a DIFFERENT state dir, or procs has no cmdline for
//     it at all (the process scan failed, or, a separate concern this
//     function does not attempt to solve, the pid has been reused by
//     an unrelated process since the session was created) — left
//     strictly alone. This is the fix for the cross-state-dir kill
//     bug: a live holder belonging to another state directory must
//     never be treated as this gc run's orphan just because it isn't
//     in *this* state dir's live set.
func gcOrphanedTmuxSessions(dir string, live map[int]bool, procs map[int]string, dryRun bool) int {
	if _, err := exec.LookPath("tmux"); err != nil {
		return 0
	}
	out, err := tmuxCmd("list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		// No server running / no sessions is not an error worth surfacing.
		return 0
	}
	cleanDir := filepath.Clean(dir)
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		m := hangonTmuxSessionRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		pid, _ := strconv.Atoi(m[1])
		if live[pid] {
			continue
		}
		if isProcessAlive(pid) {
			cmdline, tracked := procs[pid]
			if !tracked {
				// Alive, but we have no positively-confirmed --state-dir
				// for it. Could be our own untracked holder, could be
				// someone else's — refuse to guess.
				continue
			}
			if procDir, ok := parseServeStateDir(cmdline); !ok || filepath.Clean(procDir) != cleanDir {
				// Alive and belongs to a different state dir (or its
				// --state-dir couldn't be parsed at all) — not this gc
				// run's business.
				continue
			}
			// Alive, confirmed same state dir, genuinely untracked: falls
			// through to the kill below.
		}
		count++
		fmt.Printf("  %s orphaned tmux session %q (no tracked session for holder PID %d)\n", verb(dryRun, "would kill", "killed"), line, pid)
		if !dryRun {
			tmuxCmd("kill-session", "-t", tmuxExact(line)).Run()
		}
	}
	return count
}

// gcOrphanedServeProcesses stops (unless dryRun) running
// "hangon _serve" processes whose PID isn't in live AND whose own
// --state-dir argument matches dir. procs is the result of
// listServeProcesses, scanned once by runGC and shared with
// gcOrphanedTmuxSessions.
//
// The --state-dir check is what prevents a gc run scoped to one state
// directory from stopping a "hangon _serve" holder that is validly
// tracked by a completely different state directory's state.json — it
// isn't orphaned at all, it's simply not ours. A process whose
// --state-dir can't be parsed out of its cmdline (missing, or, the one
// acknowledged limitation, a state dir path itself containing a space,
// since `ps`'s command output is space-joined with no unambiguous
// quoting) is likewise left alone rather than guessed about, matching
// gc's "only ever acts on resources it can positively confirm" claim in
// the doc comment above runGC. filepath.Clean is applied to both sides
// of the comparison so trailing slashes or "./" differences don't cause
// false mismatches.
func gcOrphanedServeProcesses(dir string, live map[int]bool, procs map[int]string, dryRun bool) int {
	cleanDir := filepath.Clean(dir)
	count := 0
	for pid, cmdline := range procs {
		if live[pid] {
			continue
		}
		procDir, ok := parseServeStateDir(cmdline)
		if !ok || filepath.Clean(procDir) != cleanDir {
			continue
		}
		count++
		fmt.Printf("  %s orphaned holder process PID %d (%s)\n", verb(dryRun, "would stop", "stopped"), pid, truncate(cmdline, 80))
		if dryRun {
			continue
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		proc.Signal(os.Interrupt)
		for i := 0; i < 10; i++ {
			time.Sleep(100 * time.Millisecond)
			if !isProcessAlive(pid) {
				break
			}
		}
		if isProcessAlive(pid) {
			proc.Kill()
		}
	}
	return count
}

// gcOrphanedFIFOs removes (unless dryRun) /tmp/hangon-<pid>.fifo files
// whose pid is no longer alive. closeTmux() removes the FIFO on a clean
// holder exit, but a SIGKILLed holder (crash, OOM, `kill -9`) skips that
// cleanup, and nothing else ever scans for the leftover file — it sits
// in os.TempDir() forever.
//
// Unlike the tmux-session and _serve-process scans above, no
// --state-dir cross-check is needed here, and none is attempted: a FIFO
// whose pid is dead cannot belong to any live session in any state
// directory, so it is always safe to remove; a FIFO whose pid is alive
// is always left strictly alone, regardless of which state directory
// (if any) that pid belongs to, because removing a live *foreign* FIFO
// would break that session's output streaming out from under it. So a
// gc run scoped to one state directory will still sweep a dead-pid FIFO
// that historically belonged to a different state directory — that's
// intentional: dead means unowned everywhere, not just here.
func gcOrphanedFIFOs(dryRun bool) int {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := hangonFIFONameRE.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		pid, _ := strconv.Atoi(m[1])
		if isProcessAlive(pid) {
			continue
		}
		count++
		path := filepath.Join(os.TempDir(), e.Name())
		fmt.Printf("  %s orphaned FIFO %q (holder PID %d not running)\n", verb(dryRun, "would remove", "removed"), path, pid)
		if !dryRun {
			os.Remove(path)
		}
	}
	return count
}

// parseServeStateDir extracts the value of the "--state-dir" argument
// from a "hangon _serve ..." command line, as returned by
// listServeProcesses. Reports false if no such argument is present.
//
// Known limitation: cmdline comes from `ps -o command=`, which joins
// argv with plain spaces and no quoting, so a state directory whose
// own path contains a space cannot be recovered exactly (parsing would
// stop at the first space, yielding a truncated, non-matching value —
// which parseServeStateDir's caller then correctly treats as "not a
// match" rather than silently misidentifying it). This is an accepted
// gap, not something worth working around here: comparing via
// filepath.Clean on both sides is sufficient for the realistic case of
// state directories without spaces in their path.
func parseServeStateDir(cmdline string) (string, bool) {
	fields := strings.Fields(cmdline)
	for i, f := range fields {
		if f == "--state-dir" && i+1 < len(fields) {
			return fields[i+1], true
		}
	}
	return "", false
}

func verb(dryRun bool, would, done string) string {
	if dryRun {
		return would
	}
	return done
}

func dryRunSuffix(dryRun bool) string {
	if dryRun {
		return " (dry-run: no changes will be made)"
	}
	return ""
}

func pluralWord(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
