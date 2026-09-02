package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// fakeBackend is a minimal Backend implementation backed directly by a
// RingBuffer, so doExpect can be exercised without a real process/tmux/etc.
type fakeBackend struct {
	output *RingBuffer
}

func newFakeBackend(size int) *fakeBackend {
	return &fakeBackend{output: NewRingBuffer(size)}
}

func (f *fakeBackend) Start() error               { return nil }
func (f *fakeBackend) Send(data []byte) error     { return nil }
func (f *fakeBackend) Output() *RingBuffer        { return f.output }
func (f *fakeBackend) Stderr() *RingBuffer        { return nil }
func (f *fakeBackend) Screen() (string, error)    { return "", ErrNotSupported }
func (f *fakeBackend) SendKeys(keys string) error { return ErrNotSupported }
func (f *fakeBackend) Alive() bool                { return true }
func (f *fakeBackend) Wait() (int, error)         { return 0, ErrNotSupported }
func (f *fakeBackend) TargetPID() int             { return 0 }
func (f *fakeBackend) Close() error               { return nil }

func newTestHolder(size int) (*SessionHolder, *fakeBackend) {
	fb := newFakeBackend(size)
	sh := NewSessionHolder(fb, "")
	sh.timeout = 2 * time.Second
	return sh, fb
}

// (a) A pattern split across two writes, with a notify in between, must
// still match. Before the fix, doExpect matched each FIFO chunk in
// isolation via buf.ReadFrom, so ">>" arriving in one write and "> " in the
// next would never satisfy `>>> ` and this test would time out.
func TestDoExpect_PatternSplitAcrossTwoWrites(t *testing.T) {
	sh, fb := newTestHolder(1024)

	type outcome struct {
		resp *Response
	}
	done := make(chan outcome, 1)
	go func() {
		resp := sh.doExpect(ExpectParams{Pattern: `>>> `, Timeout: 2})
		done <- outcome{resp}
	}()

	time.Sleep(30 * time.Millisecond)
	fb.output.Write([]byte(">>"))
	time.Sleep(30 * time.Millisecond)
	fb.output.Write([]byte("> "))

	select {
	case o := <-done:
		if !o.resp.OK {
			t.Fatalf("expect failed: %s", o.resp.Error)
		}
		if o.resp.Result != ">>> " {
			t.Errorf("Result = %q, want %q", o.resp.Result, ">>> ")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("doExpect did not return in time (pattern-split bug)")
	}
}

// (b) A pattern split across many single-byte writes must still match.
func TestDoExpect_PatternSplitAcrossManyByteWrites(t *testing.T) {
	sh, fb := newTestHolder(1024)

	done := make(chan *Response, 1)
	go func() {
		done <- sh.doExpect(ExpectParams{Pattern: `>>> `, Timeout: 3})
	}()

	for _, b := range []byte("building\n>>> ") {
		time.Sleep(2 * time.Millisecond)
		fb.output.Write([]byte{b})
	}

	select {
	case resp := <-done:
		if !resp.OK {
			t.Fatalf("expect failed: %s", resp.Error)
		}
		if !strings.HasSuffix(resp.Result, ">>> ") {
			t.Errorf("Result = %q, want suffix %q", resp.Result, ">>> ")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("doExpect did not return in time (byte-at-a-time split)")
	}
}

// (c) Pre-match bytes must not vanish: they appear in Result, and bytes
// written AFTER the match remain readable afterwards (no silent loss in
// either direction).
//
// "building\n" and "ready\n" are written as two SEPARATE chunks (with the
// expect already in flight) precisely because the bug only manifests across
// chunk boundaries: before the fix, doExpect matched each new ReadFrom
// chunk in isolation, so once "building\n" chunk failed to match it was
// discarded, and Result ended up as just "ready\n" — silently dropping the
// pre-match "building" line. Writing both in a single chunk up front would
// not exercise that path.
func TestDoExpect_PreMatchBytesNotLost(t *testing.T) {
	sh, fb := newTestHolder(1024)

	done := make(chan *Response, 1)
	go func() {
		done <- sh.doExpect(ExpectParams{Pattern: `ready\n`, Timeout: 2})
	}()

	time.Sleep(30 * time.Millisecond)
	fb.output.Write([]byte("building\n"))
	time.Sleep(30 * time.Millisecond)
	fb.output.Write([]byte("ready\n"))

	var resp *Response
	select {
	case resp = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("doExpect did not return in time")
	}
	if !resp.OK {
		t.Fatalf("expect failed: %s", resp.Error)
	}
	if !strings.Contains(resp.Result, "building") {
		t.Errorf("Result %q lost pre-match data %q", resp.Result, "building")
	}
	if resp.Result != "building\nready\n" {
		t.Errorf("Result = %q, want %q", resp.Result, "building\nready\n")
	}

	// Bytes written after the match must still be visible to a later read.
	fb.output.Write([]byte("extra"))
	sh.mu.Lock()
	rest := fb.output.ReadFrom(&sh.readCursor)
	sh.mu.Unlock()
	if string(rest) != "extra" {
		t.Errorf("post-match bytes lost: read got %q, want %q", rest, "extra")
	}
}

// (d) Timeout must still fire promptly when the pattern is genuinely absent.
func TestDoExpect_TimesOutWhenPatternAbsent(t *testing.T) {
	sh, fb := newTestHolder(1024)
	fb.output.Write([]byte("no match here\n"))

	start := time.Now()
	resp := sh.doExpect(ExpectParams{Pattern: `nope`, Timeout: 0.2})
	elapsed := time.Since(start)

	if resp.OK {
		t.Fatalf("expected timeout, got match: %q", resp.Result)
	}
	if elapsed < 200*time.Millisecond {
		t.Errorf("returned too early: %v", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Errorf("took too long to time out: %v", elapsed)
	}
}

// (e) Anchored patterns: ^ should anchor to the start of the expect
// window (the cursor position when expect began), not match arbitrarily
// mid-stream.
func TestDoExpect_AnchoredPattern(t *testing.T) {
	sh, fb := newTestHolder(1024)
	fb.output.Write([]byte("noise>>> "))

	resp := sh.doExpect(ExpectParams{Pattern: `^>>> `, Timeout: 0.2})
	if resp.OK {
		t.Fatalf("^ anchored pattern should not match mid-stream, got %q", resp.Result)
	}

	sh2, fb2 := newTestHolder(1024)
	fb2.output.Write([]byte(">>> "))
	resp2 := sh2.doExpect(ExpectParams{Pattern: `^>>> `, Timeout: 2})
	if !resp2.OK {
		t.Fatalf("expected ^ anchored match at start of window: %s", resp2.Error)
	}
	if resp2.Result != ">>> " {
		t.Errorf("Result = %q, want %q", resp2.Result, ">>> ")
	}
}

// The accumulation used to find split patterns must stay bounded: a chatty
// process producing far more than the ring buffer's capacity of unmatched
// noise, followed by a match at the tail, must still complete (and match)
// rather than growing memory without bound.
func TestDoExpect_AccumulationBoundedButStillMatches(t *testing.T) {
	sh, fb := newTestHolder(4096)

	done := make(chan *Response, 1)
	go func() {
		done <- sh.doExpect(ExpectParams{Pattern: `FOUND`, Timeout: 3})
	}()

	for i := 0; i < 20; i++ {
		fb.output.Write(bytes.Repeat([]byte("x"), 4096))
	}
	fb.output.Write([]byte("FOUND"))

	select {
	case resp := <-done:
		if !resp.OK {
			t.Fatalf("expect failed: %s", resp.Error)
		}
		if !strings.HasSuffix(resp.Result, "FOUND") {
			t.Errorf("Result = %q, want suffix %q", resp.Result, "FOUND")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("doExpect did not return in time")
	}
}
