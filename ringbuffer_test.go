package main

import "testing"

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
