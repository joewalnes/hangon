package main

import (
	"fmt"
	"time"
)

// SGR mouse escape sequence helpers.
//
// SGR format (xterm ctlseqs, "SGR Mouse Mode", DECSET 1006): \x1b[<Cb;Cx;Cy{M|m}
//   M = button press, or a motion event while a button is held (drag)
//   m = button release
//   Cb (button code): 0=left, 1=middle, 2=right, 64=scroll-up, 65=scroll-down.
//     +32 is added to Cb for motion events (drag).
//   Modifiers are added to Cb: +4=shift, +8=alt/meta, +16=ctrl.
//   Cx, Cy are 1-based column/row.
//
// Note the terminal character is the reverse of what its name suggests at a
// glance: 'M' (uppercase) is press/motion, 'm' (lowercase) is release. Scroll
// wheel events have no release counterpart at all — they are always emitted
// press-coded ('M'); sending them as 'm' makes scrolling a complete no-op
// since there is no matching press for terminals to react to.

func sgrMouseSeq(btn, col, row int, release bool) []byte {
	suffix := byte('M') // press / motion
	if release {
		suffix = byte('m') // release
	}
	return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%c", btn, col, row, suffix))
}

// mouseModifiers returns the modifier bits added to Cb per xterm ctlseqs:
// shift=4, meta/alt=8, ctrl=16 (motion adds a separate +32, applied by
// callers directly since it's a per-event flag, not a held modifier key).
func mouseModifiers(shift, alt, ctrl bool) int {
	m := 0
	if shift {
		m += 4
	}
	if alt {
		m += 8
	}
	if ctrl {
		m += 16
	}
	return m
}

func buttonNumber(name string) (int, error) {
	switch name {
	case "", "left":
		return 0, nil
	case "middle":
		return 1, nil
	case "right":
		return 2, nil
	default:
		return 0, fmt.Errorf("unknown button: %s (use left, middle, or right)", name)
	}
}

// mouseClick generates SGR sequences for a click (press+release) at the given position.
func mouseClick(p MouseClickParams) ([][]byte, error) {
	btn, err := buttonNumber(p.Button)
	if err != nil {
		return nil, err
	}
	mods := mouseModifiers(p.Shift, p.Alt, p.Ctrl)
	btn += mods

	count := p.Count
	if count < 1 {
		count = 1
	}

	var seqs [][]byte
	for i := 0; i < count; i++ {
		seqs = append(seqs, sgrMouseSeq(btn, p.X, p.Y, false)) // press
		seqs = append(seqs, sgrMouseSeq(btn, p.X, p.Y, true))  // release
	}
	return seqs, nil
}

// mouseDrag generates SGR sequences for a drag from one position to another.
func mouseDrag(p MouseDragParams) ([][]byte, error) {
	btn := 0 // left button for drag
	mods := mouseModifiers(p.Shift, p.Alt, p.Ctrl)
	btn += mods

	steps := p.Steps
	if steps < 1 {
		steps = 1
	}

	var seqs [][]byte

	// Press at start position (press-coded: release=false -> 'M').
	seqs = append(seqs, sgrMouseSeq(btn, p.FromX, p.FromY, false))

	// Intermediate move events: Cb gets +32 (motion flag) per ctlseqs, and
	// motion-while-button-held is still emitted press-coded ('M'), never as
	// a release — there is one real release, at the very end of the drag.
	dragBtn := 32 + btn
	for i := 1; i <= steps; i++ {
		// Linearly interpolate between from and to.
		frac := float64(i) / float64(steps)
		col := p.FromX + int(frac*float64(p.ToX-p.FromX))
		row := p.FromY + int(frac*float64(p.ToY-p.FromY))
		seqs = append(seqs, sgrMouseSeq(dragBtn, col, row, false))
	}

	// Release at end position.
	seqs = append(seqs, sgrMouseSeq(btn, p.ToX, p.ToY, true))

	return seqs, nil
}

// mouseScroll generates SGR sequences for scroll events. Wheel events have
// no release counterpart in the ctlseqs spec — each notch is a standalone
// press-coded ('M') event with Cb 64 (up) or 65 (down); we must never emit
// a matching 'm', or a naive parser waiting for release-then-press could
// treat the wheel event as a no-op click that never completes.
func mouseScroll(p MouseScrollParams) ([][]byte, error) {
	if p.Delta == 0 {
		return nil, fmt.Errorf("delta must be non-zero")
	}
	mods := mouseModifiers(p.Shift, p.Alt, p.Ctrl)

	btn := 65 + mods // scroll down
	if p.Delta < 0 {
		btn = 64 + mods // scroll up
	}

	n := p.Delta
	if n < 0 {
		n = -n
	}

	var seqs [][]byte
	for i := 0; i < n; i++ {
		seqs = append(seqs, sgrMouseSeq(btn, p.X, p.Y, false)) // press-coded, no release
	}
	return seqs, nil
}

// sendMouseSeqs sends a series of mouse escape sequences to a backend with
// a small delay between multi-click sequences so apps detect the timing.
func sendMouseSeqs(backend Backend, seqs [][]byte, multiClick bool) error {
	for i, seq := range seqs {
		if err := backend.Send(seq); err != nil {
			return err
		}
		// For multi-click, add a tiny delay between click pairs so the
		// application can detect double/triple click timing.
		if multiClick && i > 0 && i%2 == 1 && i < len(seqs)-1 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	return nil
}
