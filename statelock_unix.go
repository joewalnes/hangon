//go:build linux || darwin

package main

import (
	"os"
	"path/filepath"
	"syscall"
)

// lockStateFile acquires an exclusive advisory lock on dir/.state.lock,
// blocking until it is available. The returned unlock function releases
// the lock and closes the underlying file descriptor.
//
// This uses flock(2) rather than a lockfile-with-PID scheme because
// hangon processes are short-lived CLI invocations: flock locks are
// held by the kernel against an open file descriptor and are released
// automatically when the holding process exits or is killed for any
// reason (including SIGKILL or a crash), so there is no possibility of
// a stale lock left behind by a dead process requiring manual cleanup
// or timeout-based staleness detection.
func lockStateFile(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(dir, ".state.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	unlock := func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
	return unlock, nil
}
