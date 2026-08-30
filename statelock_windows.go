//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// staleLockAge is how old a .state.lock file must be before it is
// considered abandoned by a crashed process and forcibly removed.
const staleLockAge = 30 * time.Second

// lockWaitTimeout bounds how long lockStateFile will wait for the lock
// to become available before giving up.
const lockWaitTimeout = 10 * time.Second

// lockStateFile acquires an exclusive lock on dir/.state.lock.
//
// The standard library's syscall package does not expose LockFileEx on
// Windows, so unlike the Unix implementation (which uses flock(2) and
// gets automatic kernel-level release on process death) this uses a
// create-exclusive lockfile with a staleness timeout as a fallback: if
// the lock file is older than staleLockAge it is assumed to belong to
// a crashed process and is removed so other processes are not blocked
// forever.
func lockStateFile(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(dir, ".state.lock")
	deadline := time.Now().Add(lockWaitTimeout)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return func() { os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > staleLockAge {
			os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for state lock %s", lockPath)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
