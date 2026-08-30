package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// hangonTmuxSessionRE matches tmux session names created by the process
// backend (see backend_process.go: pb.tmuxSess = "hangon-<holderPID>").
var hangonTmuxSessionRE = regexp.MustCompile(`^hangon-(\d+)$`)

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
// it can positively confirm are unreferenced by anything live.
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

	orphanTmux := gcOrphanedTmuxSessions(live, dryRun)
	orphanProcs := gcOrphanedServeProcesses(live, dryRun)

	fmt.Printf("\nhangon gc summary: %d stale state %s, %d orphaned tmux %s, %d orphaned holder %s%s\n",
		len(staleNames), pluralWord(len(staleNames), "entry", "entries"),
		orphanTmux, pluralWord(orphanTmux, "session", "sessions"),
		orphanProcs, pluralWord(orphanProcs, "process", "processes"),
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
				exec.Command("tmux", "kill-session", "-t", fmt.Sprintf("hangon-%d", info.HolderPID)).Run()
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
// the hangon-<pid> naming convention whose PID isn't in live. If tmux
// isn't installed or its server isn't running, this is a no-op — there
// is nothing to scan.
func gcOrphanedTmuxSessions(live map[int]bool, dryRun bool) int {
	if _, err := exec.LookPath("tmux"); err != nil {
		return 0
	}
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		// No server running / no sessions is not an error worth surfacing.
		return 0
	}
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
		count++
		fmt.Printf("  %s orphaned tmux session %q (no tracked session for holder PID %d)\n", verb(dryRun, "would kill", "killed"), line, pid)
		if !dryRun {
			exec.Command("tmux", "kill-session", "-t", line).Run()
		}
	}
	return count
}

// gcOrphanedServeProcesses stops (unless dryRun) running
// "hangon _serve" processes whose PID isn't in live.
func gcOrphanedServeProcesses(live map[int]bool, dryRun bool) int {
	procs, err := listServeProcesses()
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: could not scan for orphaned hangon processes: %v\n", err)
		return 0
	}
	count := 0
	for pid, cmdline := range procs {
		if live[pid] {
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
