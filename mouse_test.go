package main

import (
	"bytes"
	"strconv"
	"testing"
)

// These tests pin the exact SGR (mode 1006) byte sequences hangon emits for
// mouse input, per the xterm ctlseqs spec ("Any Event Mode Mouse Tracking":
// \x1b[<Cb;Cx;Cy M for a button press or a motion event with a button held,
// \x1b[<Cb;Cx;Cy m for a button release). Getting the trailing M/m wrong is
// silent: most TUIs simply discard the initial press-shaped-as-release, and
// scroll wheel events (which are press-only, no release) become a total
// no-op if emitted with 'm'.

// --- sgrMouseSeq: the byte builder itself -------------------------------

func TestSgrMouseSeq_PressUsesUppercaseM(t *testing.T) {
	got := sgrMouseSeq(0, 10, 5, false)
	want := []byte("\x1b[<0;10;5M")
	if !bytes.Equal(got, want) {
		t.Fatalf("press: got %q, want %q", got, want)
	}
}

func TestSgrMouseSeq_ReleaseUsesLowercaseM(t *testing.T) {
	got := sgrMouseSeq(0, 10, 5, true)
	want := []byte("\x1b[<0;10;5m")
	if !bytes.Equal(got, want) {
		t.Fatalf("release: got %q, want %q", got, want)
	}
}

func TestSgrMouseSeq_ButtonAndCoordsEncoded(t *testing.T) {
	got := sgrMouseSeq(2, 123, 45, false)
	want := []byte("\x1b[<2;123;45M")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// --- mouseModifiers: bit offsets per xterm ctlseqs ----------------------
// Cb modifier bits: shift=4, meta/alt=8, ctrl=16 (and motion=32, tested
// separately via drag below).

func TestMouseModifiers(t *testing.T) {
	cases := []struct {
		name             string
		shift, alt, ctrl bool
		want             int
	}{
		{"none", false, false, false, 0},
		{"shift", true, false, false, 4},
		{"alt", false, true, false, 8},
		{"ctrl", false, false, true, 16},
		{"shift+alt", true, true, false, 12},
		{"shift+ctrl", true, false, true, 20},
		{"alt+ctrl", false, true, true, 24},
		{"shift+alt+ctrl", true, true, true, 28},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mouseModifiers(c.shift, c.alt, c.ctrl)
			if got != c.want {
				t.Fatalf("mouseModifiers(%v,%v,%v) = %d, want %d", c.shift, c.alt, c.ctrl, got, c.want)
			}
		})
	}
}

// --- buttonNumber --------------------------------------------------------

func TestButtonNumber(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    int
		wantErr bool
	}{
		{"default-empty", "", 0, false},
		{"left", "left", 0, false},
		{"middle", "middle", 1, false},
		{"right", "right", 2, false},
		{"unknown", "bogus", 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := buttonNumber(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", c.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("buttonNumber(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

// --- mouseClick: single/double, each button, with modifiers -------------

func TestMouseClick_SingleLeft(t *testing.T) {
	seqs, err := mouseClick(MouseClickParams{X: 10, Y: 5, Button: "left", Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{
		[]byte("\x1b[<0;10;5M"), // press
		[]byte("\x1b[<0;10;5m"), // release
	}
	assertSeqs(t, seqs, want)
}

func TestMouseClick_DefaultButtonAndCount(t *testing.T) {
	// Button "" defaults to left, Count 0 defaults to 1.
	seqs, err := mouseClick(MouseClickParams{X: 1, Y: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{
		[]byte("\x1b[<0;1;1M"),
		[]byte("\x1b[<0;1;1m"),
	}
	assertSeqs(t, seqs, want)
}

func TestMouseClick_Middle(t *testing.T) {
	seqs, err := mouseClick(MouseClickParams{X: 3, Y: 4, Button: "middle"})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{
		[]byte("\x1b[<1;3;4M"),
		[]byte("\x1b[<1;3;4m"),
	}
	assertSeqs(t, seqs, want)
}

func TestMouseClick_Right(t *testing.T) {
	seqs, err := mouseClick(MouseClickParams{X: 3, Y: 4, Button: "right"})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{
		[]byte("\x1b[<2;3;4M"),
		[]byte("\x1b[<2;3;4m"),
	}
	assertSeqs(t, seqs, want)
}

func TestMouseClick_UnknownButtonErrors(t *testing.T) {
	if _, err := mouseClick(MouseClickParams{X: 1, Y: 1, Button: "wheel"}); err == nil {
		t.Fatal("expected error for unknown button")
	}
}

func TestMouseClick_Double(t *testing.T) {
	seqs, err := mouseClick(MouseClickParams{X: 2, Y: 2, Count: 2})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{
		[]byte("\x1b[<0;2;2M"),
		[]byte("\x1b[<0;2;2m"),
		[]byte("\x1b[<0;2;2M"),
		[]byte("\x1b[<0;2;2m"),
	}
	assertSeqs(t, seqs, want)
}

func TestMouseClick_WithModifiers(t *testing.T) {
	cases := []struct {
		name             string
		shift, alt, ctrl bool
		wantBtn          int
	}{
		{"shift", true, false, false, 4},
		{"alt", false, true, false, 8},
		{"ctrl", false, false, true, 16},
		{"shift+alt+ctrl", true, true, true, 28},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			seqs, err := mouseClick(MouseClickParams{
				X: 7, Y: 8, Button: "left",
				Shift: c.shift, Alt: c.alt, Ctrl: c.ctrl,
			})
			if err != nil {
				t.Fatal(err)
			}
			wantPress := []byte("\x1b[<" + strconv.Itoa(c.wantBtn) + ";7;8M")
			wantRelease := []byte("\x1b[<" + strconv.Itoa(c.wantBtn) + ";7;8m")
			assertSeqs(t, seqs, [][]byte{wantPress, wantRelease})
		})
	}
}

// --- mouseDrag: press M, motion M with +32, release m -------------------

func TestMouseDrag_SingleStep(t *testing.T) {
	seqs, err := mouseDrag(MouseDragParams{FromX: 1, FromY: 1, ToX: 5, ToY: 1, Steps: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{
		[]byte("\x1b[<0;1;1M"),  // press at origin
		[]byte("\x1b[<32;5;1M"), // motion (btn 0 + 32) at destination, press-coded
		[]byte("\x1b[<0;5;1m"),  // release at destination
	}
	assertSeqs(t, seqs, want)
}

func TestMouseDrag_MultiStepInterpolatesAndTagsMotion(t *testing.T) {
	seqs, err := mouseDrag(MouseDragParams{FromX: 0, FromY: 0, ToX: 10, ToY: 0, Steps: 2})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{
		[]byte("\x1b[<0;0;0M"),   // press
		[]byte("\x1b[<32;5;0M"),  // motion halfway, press-coded, +32 flag
		[]byte("\x1b[<32;10;0M"), // motion at destination, press-coded, +32 flag
		[]byte("\x1b[<0;10;0m"),  // release
	}
	assertSeqs(t, seqs, want)
}

func TestMouseDrag_WithModifiers(t *testing.T) {
	seqs, err := mouseDrag(MouseDragParams{FromX: 1, FromY: 1, ToX: 2, ToY: 2, Steps: 1, Ctrl: true})
	if err != nil {
		t.Fatal(err)
	}
	// btn = 0 (left) + 16 (ctrl) = 16; motion = 16 + 32 = 48.
	want := [][]byte{
		[]byte("\x1b[<16;1;1M"),
		[]byte("\x1b[<48;2;2M"),
		[]byte("\x1b[<16;2;2m"),
	}
	assertSeqs(t, seqs, want)
}

// --- mouseScroll: press-coded (M), no release, 64=up/65=down -----------

func TestMouseScroll_Up(t *testing.T) {
	seqs, err := mouseScroll(MouseScrollParams{X: 10, Y: 5, Delta: -1})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{
		[]byte("\x1b[<64;10;5M"),
	}
	assertSeqs(t, seqs, want)
}

func TestMouseScroll_Down(t *testing.T) {
	seqs, err := mouseScroll(MouseScrollParams{X: 10, Y: 5, Delta: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{
		[]byte("\x1b[<65;10;5M"),
	}
	assertSeqs(t, seqs, want)
}

func TestMouseScroll_MultipleNotches(t *testing.T) {
	seqs, err := mouseScroll(MouseScrollParams{X: 1, Y: 1, Delta: 3})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{
		[]byte("\x1b[<65;1;1M"),
		[]byte("\x1b[<65;1;1M"),
		[]byte("\x1b[<65;1;1M"),
	}
	assertSeqs(t, seqs, want)
}

func TestMouseScroll_ZeroDeltaErrors(t *testing.T) {
	if _, err := mouseScroll(MouseScrollParams{X: 1, Y: 1, Delta: 0}); err == nil {
		t.Fatal("expected error for zero delta")
	}
}

func TestMouseScroll_WithModifiers(t *testing.T) {
	seqs, err := mouseScroll(MouseScrollParams{X: 1, Y: 1, Delta: -1, Shift: true})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{
		[]byte("\x1b[<68;1;1M"), // 64 (scroll up) + 4 (shift)
	}
	assertSeqs(t, seqs, want)
}

// --- helpers --------------------------------------------------------------

func assertSeqs(t *testing.T, got, want [][]byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d sequences, want %d\ngot:  %s\nwant: %s", len(got), len(want), dumpSeqs(got), dumpSeqs(want))
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("seq[%d]: got %q, want %q\nfull got:  %s\nfull want: %s", i, got[i], want[i], dumpSeqs(got), dumpSeqs(want))
		}
	}
}

func dumpSeqs(seqs [][]byte) string {
	var b bytes.Buffer
	for _, s := range seqs {
		b.WriteString(string(s))
		b.WriteString(" ")
	}
	return b.String()
}
