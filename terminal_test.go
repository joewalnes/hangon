package main

import "testing"

func TestTerminal_BasicText(t *testing.T) {
	term := NewTerminal(4, 10)
	term.Write([]byte("Hello"))

	s := term.String()
	if s != "Hello\n" {
		t.Errorf("got %q, want %q", s, "Hello\n")
	}
}

func TestTerminal_Newline(t *testing.T) {
	term := NewTerminal(4, 10)
	term.Write([]byte("Line1\nLine2"))

	s := term.String()
	want := "Line1\nLine2\n"
	if s != want {
		t.Errorf("got %q, want %q", s, want)
	}
}

func TestTerminal_CarriageReturn(t *testing.T) {
	term := NewTerminal(4, 10)
	term.Write([]byte("AAAA\rBB"))

	s := term.String()
	if s != "BBAA\n" {
		t.Errorf("got %q, want %q", s, "BBAA\n")
	}
}

func TestTerminal_CursorMovement(t *testing.T) {
	term := NewTerminal(4, 20)
	// Write text, then move cursor up and overwrite.
	term.Write([]byte("Line1\nLine2"))
	term.Write([]byte("\x1b[A"))    // cursor up
	term.Write([]byte("\x1b[1;6H")) // move to row 1, col 6
	term.Write([]byte("X"))

	s := term.String()
	want := "Line1X\nLine2\n"
	if s != want {
		t.Errorf("got %q, want %q", s, want)
	}
}

func TestTerminal_ClearScreen(t *testing.T) {
	term := NewTerminal(4, 10)
	term.Write([]byte("Hello"))
	term.Write([]byte("\x1b[2J")) // clear screen

	s := term.String()
	if s != "" {
		t.Errorf("got %q, want empty", s)
	}
}

func TestTerminal_ClearToEndOfLine(t *testing.T) {
	term := NewTerminal(4, 10)
	term.Write([]byte("HelloWorld"))
	term.Write([]byte("\r"))      // back to col 0
	term.Write([]byte("\x1b[5C")) // forward 5
	term.Write([]byte("\x1b[K"))  // clear to end of line

	s := term.String()
	if s != "Hello\n" {
		t.Errorf("got %q, want %q", s, "Hello\n")
	}
}

func TestTerminal_ScrollUp(t *testing.T) {
	term := NewTerminal(3, 10)
	term.Write([]byte("L1\nL2\nL3\nL4"))

	// L1 should have scrolled off. Visible: L2, L3, L4.
	s := term.String()
	want := "L2\nL3\nL4\n"
	if s != want {
		t.Errorf("got %q, want %q", s, want)
	}
}

func TestTerminal_Tab(t *testing.T) {
	term := NewTerminal(4, 20)
	term.Write([]byte("A\tB"))

	s := term.String()
	want := "A       B\n"
	if s != want {
		t.Errorf("got %q, want %q", s, want)
	}
}
