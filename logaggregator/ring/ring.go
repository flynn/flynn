package ring

import (
	"sync"

	"github.com/flynn/flynn/pkg/syslog/rfc5424"
)

// Buffer is a ring buffer that holds rfc5424.Messages. The Buffer's entire
// contents can be read at once. Reading elements out of the buffer does not
// clear them; messages merely get moved out of the buffer when they are
// replaced by new messages.
//
// A Buffer also offers the ability to subscribe to new incoming messages.
type Buffer struct {
	mu       sync.RWMutex // protects all of the following:
	messages []*rfc5424.Message
	start    int
	subs     map[chan *rfc5424.Message]struct{}
}

const DefaultBufferCapacity = 10000

// NewBuffer returns an empty allocated Buffer with DefaultBufferCapacity.
func NewBuffer() *Buffer {
	return &Buffer{
		messages: make([]*rfc5424.Message, 0, DefaultBufferCapacity),
		subs:     make(map[chan *rfc5424.Message]struct{}),
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

	for msgc := range b.subs {
		select {
		case msgc <- m:
		default: // chan is full, drop this message to it
		}
	}
}

// Clone returns a shallow copy of the Buffer.
func (b *Buffer) Clone() *Buffer {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return &Buffer{
		messages: b._readAll(),
	}
}

// Capacity returns the capicity of the Buffer.
func (b *Buffer) Capacity() int {
	return cap(b.messages)
}

func (b *Buffer) Flush() {
	b.mu.RLock()
	defer b.mu.RUnlock()

	b.messages = make([]*rfc5424.Message, 0, DefaultBufferCapacity)
	b.start = 0
}

// ReadAll returns a copied slice with the contents of the Buffer. It does not
// modify the underlying buffer in any way. You are free to modify the
// returned slice without affecting Buffer, though modifying the individual
// elements in the result will also modify those elements in the Buffer.
func (b *Buffer) ReadAll() []*rfc5424.Message {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b._readAll()
}

// _readAll expects b.mu to already be locked
func (b *Buffer) _readAll() []*rfc5424.Message {
	buf := make([]*rfc5424.Message, len(b.messages))
	if n := copy(buf, b.messages[b.start:len(b.messages)]); n < len(b.messages) {
		copy(buf[n:], b.messages[:b.start])
	}
	return buf
}

// ReadAllAndSubscribe returns all buffered messages just like ReadAll, and also
// returns a channel that will stream new messages as they arrive.
func (b *Buffer) ReadAllAndSubscribe() (
	messages []*rfc5424.Message,
	msgc <-chan *rfc5424.Message,
	cancel func(),
) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	messages = b._readAll()
	msgc, cancel = b._subscribe()
	return
}

// ReadLastN will return the most recent n messages from the Buffer, up to the
// length of the buffer. If n is larger than the Buffer length, a smaller number
// will be returned. Panics if n < 0.
func (b *Buffer) ReadLastN(n int) []*rfc5424.Message {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b._readLastN(n)
}

// _readLastN expects b.mu to already be locked
func (b *Buffer) _readLastN(n int) []*rfc5424.Message {
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

// ReadLastNAndSubscribe returns buffered messages just like ReadLastN, and
// also returns a channel that will stream new messages as they arrive.
func (b *Buffer) ReadLastNAndSubscribe(n int) (
	messages []*rfc5424.Message,
	msgc <-chan *rfc5424.Message,
	cancel func(),
) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	messages = b._readLastN(n)
	msgc, cancel = b._subscribe()
	return
}

// Subscribe returns a channel that sends all future messages added to the
// Buffer. The returned channel is buffered, and any attempts to send new
// messages to the channel will drop messages if the channel is full.
//
// The returned func cancel must be called when the caller wants to stop
// receiving messages.
func (b *Buffer) Subscribe() (msgc <-chan *rfc5424.Message, cancel func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b._subscribe()
}

// _subscribe assumes b.mu is already locked
func (b *Buffer) _subscribe() (<-chan *rfc5424.Message, func()) {
	// Give each subscription chan a reasonable buffer size
	msgc := make(chan *rfc5424.Message, 1000)
	b.subs[msgc] = struct{}{}

	var donce sync.Once
	cancel := func() {
		donce.Do(func() {
			b.unsubscribe(msgc)
			close(msgc)
		})
	}
	return msgc, cancel
}

func (b *Buffer) unsubscribe(msgc chan *rfc5424.Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subs, msgc)
}
