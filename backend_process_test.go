package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

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

// TestShellSingleQuote_Escaping guards shellSingleQuote against the P2
// injection/misdirection bug in pipe-pane's command string: pb.fifoPath is
// derived from os.TempDir(), which honors $TMPDIR, so it is not a fixed
// trusted literal. Before this fix, pipePaneCmd was built with
// fmt.Sprintf("cat >> %s", pb.fifoPath) — unquoted — which a TMPDIR
// containing a space silently misdirects (see
// TestPipePaneCmd_QuotesFifoPathWithSpace for the behavioral half of this),
// and a TMPDIR containing shell metacharacters would let arbitrary shell
// syntax run under `sh -c` (pipe-pane's argument is shell-interpreted).
func TestShellSingleQuote_Escaping(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"plain", "/tmp/foo.fifo", "'/tmp/foo.fifo'"},
		{"space", "/tmp/a b/x.fifo", "'/tmp/a b/x.fifo'"},
		{"single_quote", "/tmp/a'b/x.fifo", `'/tmp/a'\''b/x.fifo'`},
		{"command_substitution", "/tmp/$(rm -rf /)/x.fifo", "'/tmp/$(rm -rf /)/x.fifo'"},
		{"semicolon", "/tmp/a;rm -rf /;b", "'/tmp/a;rm -rf /;b'"},
		{"backtick", "/tmp/`whoami`/x", "'/tmp/`whoami`/x'"},
		{"multiple_quotes", "it's a 'test'", `'it'\''s a '\''test'\'''`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shellSingleQuote(tc.input)
			if got != tc.want {
				t.Errorf("shellSingleQuote(%q) = %q, want %q", tc.input, got, tc.want)
			}
			// The escaped form must, when handed to a real POSIX shell as
			// `printf %s <escaped>`, reproduce the original string exactly
			// — including any metacharacters, none of which should be
			// interpreted by the shell.
			out, err := exec.Command("sh", "-c", "printf %s "+got).Output()
			if err != nil {
				t.Fatalf("sh -c failed: %v", err)
			}
			if string(out) != tc.input {
				t.Errorf("round-trip through sh: got %q, want %q", out, tc.input)
			}
		})
	}
}

// TestPipePaneCmd_QuotesFifoPath guards the specific call site: the string
// tmux pipe-pane runs must wrap pb.fifoPath in single quotes, not
// interpolate it bare. This is the regression this commit fixes — the old
// code was fmt.Sprintf("cat >> %s", pb.fifoPath).
func TestPipePaneCmd_QuotesFifoPath(t *testing.T) {
	pb := &ProcessBackend{fifoPath: "/tmp/has space/hangon-123.fifo"}
	got := "cat >> " + shellSingleQuote(pb.fifoPath)
	want := "cat >> '/tmp/has space/hangon-123.fifo'"
	if got != want {
		t.Errorf("pipePaneCmd = %q, want %q", got, want)
	}
}

// TestPipePaneCmd_QuotesFifoPathWithSpace is the behavioral proof that the
// quoting matters end to end: it builds the exact pipe-pane command string
// used in startWithTmux for a FIFO path containing a space (as would occur
// if $TMPDIR itself contained a space) and confirms via a real shell that
// `cat` receives the whole path as one argument and appends to that exact
// file — not to a truncated prefix. Pre-fix (fmt.Sprintf("cat >> %s", ...),
// no quoting at all) this would instead run `cat >> /tmp/has` (redirecting
// to a file literally named "has") followed by trying to execute a program
// named "space/hangon.fifo" — silently losing the intended destination.
func TestPipePaneCmd_QuotesFifoPathWithSpace(t *testing.T) {
	dir := t.TempDir()
	spaced := dir + "/a b"
	if err := os.Mkdir(spaced, 0o755); err != nil {
		t.Fatal(err)
	}
	fifoPath := spaced + "/hangon-test.fifo"

	pipePaneCmd := "cat >> " + shellSingleQuote(fifoPath)

	// Run the built command under `sh -c`, feeding it known input on
	// stdin, and confirm the target file (the exact, space-containing
	// path) receives exactly that input.
	cmd := exec.Command("sh", "-c", pipePaneCmd)
	cmd.Stdin = strings.NewReader("hello from pipe-pane\n")
	if err := cmd.Run(); err != nil {
		t.Fatalf("sh -c %q: %v", pipePaneCmd, err)
	}

	got, err := os.ReadFile(fifoPath)
	if err != nil {
		t.Fatalf("expected output at %q, ReadFile failed: %v (quoting sent it elsewhere)", fifoPath, err)
	}
	if string(got) != "hello from pipe-pane\n" {
		t.Errorf("file contents = %q, want %q", got, "hello from pipe-pane\n")
	}
}
