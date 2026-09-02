package main

import "sync"

const defaultBufSize = 1 << 20 // 1 MB

// RingBuffer is a thread-safe circular buffer that supports cursored reads.
// Writers append data. Each reader has a cursor tracking how far it has read,
// so "read" returns only new data since the last read.
type RingBuffer struct {
	mu   sync.Mutex
	buf  []byte
	size int
	// writePos is the total number of bytes ever written (monotonically increasing).
	// Actual index into buf is writePos % size.
	writePos int64
	// notify is signaled on every write so expect/wait can block.
	notify chan struct{}
}

func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = defaultBufSize
	}
	return &RingBuffer{
		buf:    make([]byte, size),
		size:   size,
		notify: make(chan struct{}, 1),
	}
}

// Write appends data to the ring buffer.
//
// Instead of a byte-at-a-time loop doing a `%` on every iteration (up to
// 1M iterations while holding the lock, for the default buffer size), this
// copies in at most two slices split at the wraparound point — the same
// memmove-based approach a circular buffer normally uses. The `%`
// (modulo) is computed only once or twice per Write call rather than once
// per byte.
//
// If p is longer than the buffer, only its last `size` bytes actually
// survive (everything earlier would be immediately overwritten by later
// bytes in the same write), so those earlier bytes are skipped outright
// rather than copied and then overwritten — writePos still advances by
// the full len(p), preserving the original per-byte loop's accounting
// exactly.
func (rb *RingBuffer) Write(p []byte) (int, error) {
	rb.mu.Lock()
	n := len(p)
	if n > 0 {
		data := p
		pos := rb.writePos
		if int64(n) > int64(rb.size) {
			skip := n - rb.size
			data = p[skip:]
			pos += int64(skip)
		}
		rb.copyIn(pos, data)
		rb.writePos += int64(n)
	}
	rb.mu.Unlock()

	// Non-blocking signal to any waiters.
	select {
	case rb.notify <- struct{}{}:
	default:
	}
	return n, nil
}

// copyIn writes data into buf starting at the logical position pos
// (pos mod size gives the actual index), wrapping around the end of buf
// as needed. Caller must hold rb.mu, and len(data) must be <= rb.size.
func (rb *RingBuffer) copyIn(pos int64, data []byte) {
	if len(data) == 0 {
		return
	}
	idx := int(pos % int64(rb.size))
	c := copy(rb.buf[idx:], data)
	if c < len(data) {
		copy(rb.buf, data[c:])
	}
}

// copyOut reads n bytes starting at the logical position pos (pos mod size
// gives the actual index), wrapping around the end of buf as needed, and
// returns them as a freshly allocated slice. Caller must hold rb.mu, and n
// must be <= rb.size.
func (rb *RingBuffer) copyOut(pos int64, n int) []byte {
	out := make([]byte, n)
	if n == 0 {
		return out
	}
	idx := int(pos % int64(rb.size))
	c := copy(out, rb.buf[idx:])
	if c < n {
		copy(out[c:], rb.buf[:n-c])
	}
	return out
}

// ReadFrom returns all data written since cursor position, and advances cursor.
// If cursor is too far behind (data was overwritten), it resets to earliest available.
func (rb *RingBuffer) ReadFrom(cursor *int64) []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.writePos == 0 || *cursor >= rb.writePos {
		return nil
	}

	earliest := rb.writePos - int64(rb.size)
	if earliest < 0 {
		earliest = 0
	}
	if *cursor < earliest {
		*cursor = earliest
	}

	n := int(rb.writePos - *cursor)
	out := rb.copyOut(*cursor, n)
	*cursor = rb.writePos
	return out
}

// ReadAll returns the entire buffer contents (up to what's been written).
func (rb *RingBuffer) ReadAll() []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.writePos == 0 {
		return nil
	}

	earliest := rb.writePos - int64(rb.size)
	if earliest < 0 {
		earliest = 0
	}

	n := int(rb.writePos - earliest)
	return rb.copyOut(earliest, n)
}

// Notify returns the channel that gets signaled on writes.
func (rb *RingBuffer) Notify() <-chan struct{} {
	return rb.notify
}

// WritePos returns the current write position.
func (rb *RingBuffer) WritePos() int64 {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.writePos
}

// Size returns the ring buffer's capacity in bytes.
func (rb *RingBuffer) Size() int {
	// size is immutable after construction, no lock needed.
	return rb.size
}
