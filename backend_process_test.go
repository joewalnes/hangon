package main

import "testing"

// TestTmuxCaptureAnsiArgs_PreservesTrailingSpaces guards the fix for a
// screenshot rendering bug: without tmux's -N flag, `capture-pane` silently
// trims trailing whitespace-only cells from the end of every captured line
// — discarding their background color along with them. For any full-frame
// app that pads lines with colored blank space (status bars, filled
// panels — i.e. most realistic TUI layouts), that trimming caused large
// regions of the screenshot to fall back to RenderConfig's default
// background color instead of the app's actual theme color. See
// screenAnsiTmux's doc comment for the full story.
func TestTmuxCaptureAnsiArgs_PreservesTrailingSpaces(t *testing.T) {
	args := tmuxCaptureAnsiArgs("mysession")

	found := false
	for _, a := range args {
		if a == "-N" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("tmuxCaptureAnsiArgs(%q) = %v, missing -N (preserve trailing spaces) flag — screenshots will silently lose background color on trailing whitespace", "mysession", args)
	}

	// Also make sure we didn't lose the flags this depends on: -e for
	// color/attribute escape codes, -p to write to stdout, -t to target
	// the right pane.
	want := map[string]bool{"-e": false, "-p": false, "-t": false}
	for _, a := range args {
		if _, ok := want[a]; ok {
			want[a] = true
		}
	}
	for flag, ok := range want {
		if !ok {
			t.Errorf("tmuxCaptureAnsiArgs missing required flag %q: %v", flag, args)
		}
	}

	if len(args) < 2 || args[1] != "mysession" && !containsPair(args, "-t", "mysession") {
		t.Errorf("tmuxCaptureAnsiArgs(%q) doesn't target the session: %v", "mysession", args)
	}
}

func containsPair(args []string, flag, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
