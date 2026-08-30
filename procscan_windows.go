//go:build windows

package main

// listServeProcesses is not implemented on Windows: there is no
// portable, dependency-free way to list process command lines the way
// `ps -o command` does on Unix without adding a CPAN-equivalent
// dependency, and the leaked-`_serve`-process failure mode this
// supports `hangon gc` scanning for was observed and reproduced on
// macOS. `hangon gc` still removes stale state.json entries and, where
// tmux is available, reaps orphaned tmux sessions on Windows — it just
// can't find orphaned holder processes that have no state entry and no
// tmux session pointing at them.
func listServeProcesses() (map[int]string, error) {
	return map[int]string{}, nil
}
