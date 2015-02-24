package ring

import (
	"sync"

	"github.com/flynn/flynn/pkg/syslog/rfc5424"
)

// Buffer is a ring buffer that holds rfc5424.Messages. The Buffer's entire
// contents can be read at once. Reading elements out of the buffer does not
// clear them; messages merely get moved out of the buffer when they are
// replaced by new messages.
type Buffer struct {
	messages []*rfc5424.Message
	start    int
	mu       sync.RWMutex
}

const DefaultBufferCapacity = 10000

// NewBuffer returns an empty allocated Buffer with DefaultBufferCapacity.
func NewBuffer() *Buffer {
	return &Buffer{
		messages: make([]*rfc5424.Message, 0, DefaultBufferCapacity),
	}
}

// Add adds an element to the Buffer. If the Buffer is already full, it replaces
// an existing message.
func (b *Buffer) Add(m *rfc5424.Message) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.messages) < cap(b.messages) {
		// buffer not yet full
		b.messages = append(b.messages, m)
	} else {
		// buffer already full, replace the value at start
		b.messages[b.start] = m
		b.start++

		if b.start == cap(b.messages) {
			b.start = 0
		}
	}
}

// Capacity returns the capicity of the Buffer.
func (b *Buffer) Capacity() int {
	return cap(b.messages)
}

// ReadAll returns a copied slice with the contents of the Buffer. It does not
// modify the underlying buffer in any way. You are free to modify the
// returned slice without affecting Buffer, though modifying the individual
// elements in the result will also modify those elements in the Buffer.
func (b *Buffer) ReadAll() []*rfc5424.Message {
	b.mu.RLock()
	defer b.mu.RUnlock()

	buf := make([]*rfc5424.Message, len(b.messages))
	if n := copy(buf, b.messages[b.start:len(b.messages)]); n < len(b.messages) {
		copy(buf[n:], b.messages[:b.start])
	}
	return buf
}

// ReadLastN will return the most recent n messages from the Buffer, up to the
// length of the buffer. If n is larger than the Buffer length, a smaller number
// will be returned. Panics if n < 0.
func (b *Buffer) ReadLastN(n int) []*rfc5424.Message {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if n > len(b.messages) {
		n = len(b.messages)
	}

	buf := make([]*rfc5424.Message, n)
	copied := 0
	if n > (b.start) {
		start := b.start
		if n < len(b.messages)-start {
			start = len(b.messages) - n
		}
		copied = copy(buf, b.messages[start:])
	}
	if n > copied {
		copy(buf[copied:], b.messages[n-copied:(b.start-copied)])
	}
	return buf
}
