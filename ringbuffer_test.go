package main

import (
	"bytes"
	"testing"
)

func TestRingBuffer_BasicWriteRead(t *testing.T) {
	rb := NewRingBuffer(1024)

	rb.Write([]byte("hello "))
	rb.Write([]byte("world"))

	var cursor int64
	data := rb.ReadFrom(&cursor)
	if string(data) != "hello world" {
		t.Errorf("got %q, want %q", data, "hello world")
	}
	if cursor != 11 {
		t.Errorf("cursor=%d, want 11", cursor)
	}
}

func TestRingBuffer_CursoredReads(t *testing.T) {
	rb := NewRingBuffer(1024)
	var cursor int64

	rb.Write([]byte("first"))
	data := rb.ReadFrom(&cursor)
	if string(data) != "first" {
		t.Errorf("read1: got %q, want %q", data, "first")
	}

	rb.Write([]byte("second"))
	data = rb.ReadFrom(&cursor)
	if string(data) != "second" {
		t.Errorf("read2: got %q, want %q", data, "second")
	}

	// No new data.
	data = rb.ReadFrom(&cursor)
	if len(data) != 0 {
		t.Errorf("read3: got %q, want empty", data)
	}
}

func TestRingBuffer_Wraparound(t *testing.T) {
	rb := NewRingBuffer(8) // tiny buffer

	rb.Write([]byte("ABCDEFGH")) // fill exactly
	rb.Write([]byte("IJ"))       // overwrites A and B

	all := rb.ReadAll()
	if string(all) != "CDEFGHIJ" {
		t.Errorf("ReadAll after wrap: got %q, want %q", all, "CDEFGHIJ")
	}
}

func TestRingBuffer_CursorResetsOnOverwrite(t *testing.T) {
	rb := NewRingBuffer(8)
	var cursor int64

	rb.Write([]byte("ABCDEFGH"))
	// Don't read yet — write more to overwrite.
	rb.Write([]byte("IJKLMNOP")) // completely overwrites

	data := rb.ReadFrom(&cursor)
	if string(data) != "IJKLMNOP" {
		t.Errorf("got %q, want %q", data, "IJKLMNOP")
	}
}

func TestRingBuffer_ReadAllEmpty(t *testing.T) {
	rb := NewRingBuffer(1024)
	data := rb.ReadAll()
	if data != nil {
		t.Errorf("got %v, want nil", data)
	}
}

func TestRingBuffer_WritePos(t *testing.T) {
	rb := NewRingBuffer(1024)
	if rb.WritePos() != 0 {
		t.Errorf("initial WritePos=%d, want 0", rb.WritePos())
	}
	rb.Write([]byte("abc"))
	if rb.WritePos() != 3 {
		t.Errorf("WritePos=%d, want 3", rb.WritePos())
	}
}

// TestRingBuffer_ExactWraparoundBoundary pins down the boundary the
// copy()-based Write must get exactly right: filling to size-1, then
// writing 2 more bytes crosses the wrap point mid-write (one byte lands at
// the last index, the next must land back at index 0, not run off the end
// of buf or skip a slot).
func TestRingBuffer_ExactWraparoundBoundary(t *testing.T) {
	const size = 8
	rb := NewRingBuffer(size)

	// Fill to size-1 (7 bytes): writePos ends at 7, one byte short of a
	// full wrap.
	rb.Write([]byte("ABCDEFG"))
	if rb.WritePos() != size-1 {
		t.Fatalf("WritePos=%d, want %d", rb.WritePos(), size-1)
	}

	// Write 2 more bytes: the first ('H') exactly fills index 7 (the last
	// slot before the wrap); the second ('I') must wrap to index 0,
	// overwriting 'A'.
	rb.Write([]byte("HI"))
	if rb.WritePos() != size+1 {
		t.Fatalf("WritePos=%d, want %d", rb.WritePos(), size+1)
	}

	all := rb.ReadAll()
	want := "BCDEFGHI"
	if string(all) != want {
		t.Errorf("ReadAll after exact wrap boundary: got %q, want %q", all, want)
	}
}

// TestRingBuffer_ReadAllFullAndOverwrapped writes well past the buffer's
// capacity, several full wraps' worth, and confirms ReadAll on a
// full+overwrapped buffer returns exactly the last `size` bytes written —
// not the first `size`, not a shifted or rotated view.
func TestRingBuffer_ReadAllFullAndOverwrapped(t *testing.T) {
	const size = 16
	rb := NewRingBuffer(size)

	// Write 5*size bytes in small chunks (to exercise Write's own
	// wraparound repeatedly, not just a single big write). Content is a
	// running byte counter so the "last size bytes" are unambiguous.
	var all []byte
	for i := 0; i < 5*size; i += 3 {
		end := i + 3
		if end > 5*size {
			end = 5 * size
		}
		chunk := make([]byte, end-i)
		for j := range chunk {
			chunk[j] = byte(i + j)
		}
		rb.Write(chunk)
		all = append(all, chunk...)
	}

	want := all[len(all)-size:]
	got := rb.ReadAll()
	if !bytes.Equal(got, want) {
		t.Errorf("ReadAll after overwrap: got %v, want %v", got, want)
	}
}

// TestRingBuffer_ReadFromAcrossWrap confirms ReadFrom returns contiguous,
// correctly-ordered bytes when the requested range straddles the
// wraparound point (i.e. the read itself needs both of copyOut's two
// copies, not just Write's).
func TestRingBuffer_ReadFromAcrossWrap(t *testing.T) {
	const size = 10
	rb := NewRingBuffer(size)

	// Get writePos to 8 (2 bytes shy of first wrap).
	rb.Write([]byte("01234567"))
	var cursor int64 = 8

	// Write 6 more bytes: positions 8,9 land at indices 8,9 (no wrap
	// yet), positions 10..13 wrap to indices 0..3. So this write alone
	// straddles the wrap.
	rb.Write([]byte("89ABCD"))

	data := rb.ReadFrom(&cursor)
	want := "89ABCD"
	if string(data) != want {
		t.Errorf("ReadFrom across wrap: got %q, want %q", data, want)
	}
	if cursor != 14 {
		t.Errorf("cursor=%d, want 14", cursor)
	}

	// One more read spanning a second wrap and pulling from data written
	// both before and after this ReadFrom's start cursor.
	rb.Write([]byte("EFGHIJ")) // writePos 14->20, wraps again
	data = rb.ReadFrom(&cursor)
	if string(data) != "EFGHIJ" {
		t.Errorf("second ReadFrom across wrap: got %q, want %q", data, "EFGHIJ")
	}
}

// BenchmarkRingBuffer_ReadAll_Full measures ReadAll on a fully-wrapped,
// default-sized (1MB) buffer. The pre-optimization implementation copied
// one byte at a time with a `%` on every iteration while holding rb.mu;
// the copy()-based version does it in at most two slice copies. See the
// go-team report for the measured before/after numbers on this machine.
func BenchmarkRingBuffer_ReadAll_Full(b *testing.B) {
	rb := NewRingBuffer(defaultBufSize)
	chunk := make([]byte, 4096)
	// Wrap the buffer several times over so ReadAll's range spans the
	// wraparound point (the case the two-copy split exists for).
	for i := 0; i < (defaultBufSize/len(chunk))*3; i++ {
		rb.Write(chunk)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.ReadAll()
	}
}

// BenchmarkRingBuffer_Write_Large measures a single Write larger than a
// few typical read chunks, to exercise the same copy path Write uses.
func BenchmarkRingBuffer_Write_Large(b *testing.B) {
	rb := NewRingBuffer(defaultBufSize)
	chunk := make([]byte, 65536)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.Write(chunk)
	}
}
