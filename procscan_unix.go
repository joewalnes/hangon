//go:build linux || darwin

package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// listServeProcesses returns the PIDs of all currently running
// "<this-executable> _serve ..." processes on the machine, keyed by
// PID, with each one's full command line as the value (for
// diagnostics in `hangon gc` output).
//
// This shells out to `ps` rather than reading /proc directly so the
// same code works on both Linux and macOS (Linux has /proc; macOS does
// not). `-A` (all processes) and `-o pid=,command=` (no header, so
// every line is data) are supported by both GNU ps (Linux) and BSD ps
// (macOS).
func listServeProcesses() (map[int]string, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	selfBase := filepath.Base(self)

	out, err := exec.Command("ps", "-A", "-o", "pid=,command=").Output()
	if err != nil {
		return nil, err
	}

	result := make(map[int]string)
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, " ", 2)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		cmdline := strings.TrimSpace(fields[1])

		argv := strings.Fields(cmdline)
		if len(argv) == 0 {
			continue
		}
		// Only match processes that are this same hangon binary
		// (by basename) invoked with the internal "_serve" subcommand
		// as one of its arguments — not just any process that happens
		// to mention "_serve" somewhere in its command line.
		if filepath.Base(argv[0]) != selfBase {
			continue
		}
		hasServe := false
		for _, a := range argv[1:] {
			if a == "_serve" {
				hasServe = true
				break
			}
		}
		if !hasServe {
			continue
		}
		result[pid] = cmdline
	}
	return result, scanner.Err()
}
