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
func (rb *RingBuffer) Write(p []byte) (int, error) {
	rb.mu.Lock()
	for i, b := range p {
		rb.buf[(rb.writePos+int64(i))%int64(rb.size)] = b
	}
	rb.writePos += int64(len(p))
	rb.mu.Unlock()

	// Non-blocking signal to any waiters.
	select {
	case rb.notify <- struct{}{}:
	default:
	}
	return len(p), nil
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
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = rb.buf[(*cursor+int64(i))%int64(rb.size)]
	}
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
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = rb.buf[(earliest+int64(i))%int64(rb.size)]
	}
	return out
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
