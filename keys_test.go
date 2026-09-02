package main

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// keySetOf returns the set of keys in a keyMap-shaped map (map[string]X for
// any value type X), so keyMap ([]byte values) and tmuxKeyMap (string
// values) can be compared without caring what they map to.
func keySetOfBytes(m map[string][]byte) map[string]bool {
	s := make(map[string]bool, len(m))
	for k := range m {
		s[k] = true
	}
	return s
}

func keySetOfStrings(m map[string]string) map[string]bool {
	s := make(map[string]bool, len(m))
	for k := range m {
		s[k] = true
	}
	return s
}

// diffSets returns (onlyInA, onlyInB), the keys present in one set but not
// the other.
func diffSets(a, b map[string]bool) (onlyInA, onlyInB []string) {
	for k := range a {
		if !b[k] {
			onlyInA = append(onlyInA, k)
		}
	}
	for k := range b {
		if !a[k] {
			onlyInB = append(onlyInB, k)
		}
	}
	sort.Strings(onlyInA)
	sort.Strings(onlyInB)
	return
}

// TestKeyMapsHaveIdenticalKeySets guards against keyMap (backend_process.go,
// used for the legacy raw-PTY/pipe fallback) and tmuxKeyMap (used when tmux
// is available, the default) drifting apart. A key present in only one map
// works when tmux happens to be installed (or not) and silently fails
// ("unknown key") the other way -- exactly the kind of split-brain bug that
// is easy to introduce by adding a new key combo to only one map and easy
// to miss in review since both maps are ~80 lines of near-identical
// boilerplate.
//
// Guard-bite proof (performed manually while writing this test, not left
// in the code): deleting the "ctrl-a" entry from tmuxKeyMap made this test
// fail with "only in keyMap (missing from tmuxKeyMap): [ctrl-a]"; restoring
// the entry made it pass again.
func TestKeyMapsHaveIdenticalKeySets(t *testing.T) {
	a := keySetOfBytes(keyMap)
	b := keySetOfStrings(tmuxKeyMap)

	onlyInKeyMap, onlyInTmuxKeyMap := diffSets(a, b)
	if len(onlyInKeyMap) > 0 {
		t.Errorf("keys in keyMap but missing from tmuxKeyMap (won't work under tmux): %v", onlyInKeyMap)
	}
	if len(onlyInTmuxKeyMap) > 0 {
		t.Errorf("keys in tmuxKeyMap but missing from keyMap (won't work without tmux): %v", onlyInTmuxKeyMap)
	}
}

// parseAvailableKeys extracts the set of key names documented in the
// "Available keys:" section of subcommandHelp["keys"] (see main.go). The
// section uses a small, consistent set of prose shorthands to keep the
// help text readable instead of spelling out all ~90 keys one per line:
//
//   - "X through Y"       a contiguous range sharing a prefix, e.g.
//     "ctrl-a through ctrl-z" or "f1 through f12"
//   - "prefix-a/b/c"       a shared-prefix group, e.g.
//     "ctrl-up/down/left/right"
//   - "a, b" / "a, b, c"   distinct keys on one line, e.g. "enter, return"
//   - "a b c"              distinct keys separated by bare spaces, e.g.
//     "alt-. alt-, alt-= alt--" (note: some of these keys are themselves
//     punctuation, including a literal comma -- "alt-," -- so a trailing
//     comma is only treated as a list separator and stripped when doing so
//     is required to match a name in knownKeys; otherwise it's kept as
//     part of the key name)
//
// This is intentionally tailored to the handful of patterns actually used
// in that help text rather than a general-purpose prose parser, which
// would be far more brittle. If the help text's format changes in a way
// this parser doesn't understand, it fails loudly (via t.Fatalf in the
// caller) rather than silently under-checking.
func parseAvailableKeys(t *testing.T, help string, knownKeys map[string]bool) map[string]bool {
	t.Helper()

	start := strings.Index(help, "Available keys:\n")
	if start < 0 {
		t.Fatalf("subcommandHelp[%q] help text has no \"Available keys:\" section -- update parseAvailableKeys if the format changed", "keys")
	}
	rest := help[start+len("Available keys:\n"):]
	end := strings.Index(rest, "\nExamples:")
	if end < 0 {
		t.Fatalf("subcommandHelp[%q] help text has no \"Examples:\" section terminating the key list -- update parseAvailableKeys if the format changed", "keys")
	}
	section := rest[:end]

	sepCols := regexp.MustCompile(`\s{2,}`) // 2+ spaces separates key names from their description
	throughRe := regexp.MustCompile(`^(\S+)\s+through\s+(\S+)$`)

	result := make(map[string]bool)
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		field := sepCols.Split(line, 2)[0]

		if m := throughRe.FindStringSubmatch(field); m != nil {
			expandRange(t, m[1], m[2], result)
			continue
		}

		for _, tok := range strings.Fields(field) {
			if strings.Contains(tok, "/") {
				expandSlashGroup(t, tok, result)
				continue
			}
			if strings.HasSuffix(tok, ",") {
				stripped := strings.TrimSuffix(tok, ",")
				if !knownKeys[tok] || knownKeys[stripped] {
					// Not a real "ends in a literal comma" key (or
					// ambiguous but the stripped form is the real one) --
					// treat the comma as a list separator.
					tok = stripped
				}
			}
			result[tok] = true
		}
	}
	return result
}

// expandRange handles "X through Y" shorthand: either a shared
// non-numeric prefix with single-character suffixes (e.g. "ctrl-a" through
// "ctrl-z") or a shared alphabetic prefix with numeric suffixes (e.g. "f1"
// through "f12").
func expandRange(t *testing.T, left, right string, out map[string]bool) {
	t.Helper()
	if idx := strings.LastIndex(left, "-"); idx >= 0 && idx == strings.LastIndex(right, "-") {
		prefix := left[:idx+1]
		leftSuf, rightSuf := left[idx+1:], right[idx+1:]
		if len(leftSuf) == 1 && len(rightSuf) == 1 {
			for c := leftSuf[0]; c <= rightSuf[0]; c++ {
				out[prefix+string(c)] = true
			}
			return
		}
	}
	// Numeric range with an alphabetic prefix, e.g. "f1 through f12".
	splitAlphaNum := func(s string) (string, int, bool) {
		i := 0
		for i < len(s) && (s[i] < '0' || s[i] > '9') {
			i++
		}
		n, err := strconv.Atoi(s[i:])
		return s[:i], n, err == nil
	}
	lp, ln, lok := splitAlphaNum(left)
	rp, rn, rok := splitAlphaNum(right)
	if !lok || !rok || lp != rp {
		t.Fatalf("expandRange: don't know how to expand %q through %q", left, right)
	}
	for n := ln; n <= rn; n++ {
		out[lp+strconv.Itoa(n)] = true
	}
}

// expandSlashGroup handles "prefix-a/b/c" shorthand, e.g.
// "ctrl-up/down/left/right" -> ctrl-up, ctrl-down, ctrl-left, ctrl-right.
func expandSlashGroup(t *testing.T, tok string, out map[string]bool) {
	t.Helper()
	parts := strings.Split(tok, "/")
	idx := strings.LastIndex(parts[0], "-")
	if idx < 0 {
		t.Fatalf("expandSlashGroup: %q has no '-' to derive a shared prefix from", tok)
	}
	prefix := parts[0][:idx+1]
	out[parts[0]] = true
	for _, p := range parts[1:] {
		out[prefix+p] = true
	}
}

// TestHelpKeysDocumentedExactlyMatchKeyMaps parses the "Available keys:"
// section of `hangon keys --help` (subcommandHelp["keys"] in main.go) and
// asserts it names exactly the keys keyMap/tmuxKeyMap actually implement --
// no more (a documented key that doesn't work), no less (a working key an
// agent would never discover from --help).
//
// Guard-bite proof (performed manually while writing this test, not left
// in the code): removing the "ctrl-space" line from subcommandHelp["keys"]
// made this test fail with "documented but not implemented" empty and
// "implemented but not documented: [ctrl-space]"; removing the "ctrl-space"
// entry from keyMap/tmuxKeyMap instead (leaving the doc line) made it fail
// the other way, "documented but not implemented: [ctrl-space]". Restoring
// either made it pass again.
func TestHelpKeysDocumentedExactlyMatchKeyMaps(t *testing.T) {
	implemented := keySetOfBytes(keyMap)
	if other := keySetOfStrings(tmuxKeyMap); len(other) != len(implemented) {
		// TestKeyMapsHaveIdenticalKeySets already reports the details;
		// this test only needs one ground-truth set to compare docs
		// against, so bail out early rather than double-reporting.
		t.Skip("keyMap/tmuxKeyMap already disagree; see TestKeyMapsHaveIdenticalKeySets")
	}

	help, ok := subcommandHelp["keys"]
	if !ok {
		t.Fatal(`subcommandHelp["keys"] is missing`)
	}
	documented := parseAvailableKeys(t, help, implemented)

	onlyDocumented, onlyImplemented := diffSets(documented, implemented)
	if len(onlyDocumented) > 0 {
		t.Errorf("documented in `hangon keys --help` but not implemented in keyMap/tmuxKeyMap: %v", onlyDocumented)
	}
	if len(onlyImplemented) > 0 {
		t.Errorf("implemented in keyMap/tmuxKeyMap but not documented in `hangon keys --help`: %v", onlyImplemented)
	}
}
