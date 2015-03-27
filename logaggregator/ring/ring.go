package ring

import (
	"errors"
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
	cursor   int
	subs     map[chan<- *rfc5424.Message]struct{}
	donec    chan struct{}
}

const DefaultBufferCapacity = 10000

// NewBuffer returns an empty allocated Buffer with DefaultBufferCapacity.
func NewBuffer() *Buffer {
	return newBuffer(DefaultBufferCapacity)
}

func newBuffer(capacity int) *Buffer {
	return &Buffer{
		messages: make([]*rfc5424.Message, 0, capacity),
		subs:     make(map[chan<- *rfc5424.Message]struct{}),
		donec:    make(chan struct{}),
	}
}

// Add adds an element to the Buffer. If the Buffer is already full, it replaces
// an existing message.
func (b *Buffer) Add(m *rfc5424.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.cursor == -1 {
		return errors.New("buffer closed")
	}
	if len(b.messages) < cap(b.messages) {
		// buffer not yet full
		b.messages = append(b.messages, m)
	} else {
		// buffer already full, replace the value at cursor
		b.messages[b.cursor] = m
		b.cursor++

		if b.cursor == cap(b.messages) {
			b.cursor = 0
		}
	}

	for msgc := range b.subs {
		select {
		case msgc <- m:
		default: // chan is full, drop this message to it
		}
	}

	return nil
}

func (b *Buffer) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.messages = nil
	b.cursor = -1
	close(b.donec)
}

// Read returns a copied slice with the contents of the Buffer. It does not
// modify the underlying buffer in any way. You are free to modify the
// returned slice without affecting Buffer, though modifying the individual
// elements in the result will also modify those elements in the Buffer.
func (b *Buffer) Read() []*rfc5424.Message {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.read()
}

// ReadAndSubscribe returns all buffered messages just like Read, and also
// returns a channel that will stream new messages as they arrive.
func (b *Buffer) ReadAndSubscribe(msgc chan<- *rfc5424.Message, donec <-chan struct{}) []*rfc5424.Message {
	b.mu.RLock()
	defer b.mu.RUnlock()

	b.subscribe(msgc, donec)
	return b.read()
}

// Subscribe returns a channel that sends all future messages added to the
// Buffer. The returned channel is buffered, and any attempts to send new
// messages to the channel will drop messages if the channel is full.
//
// The caller closes the donec channel to stop receiving messages.
func (b *Buffer) Subscribe(msgc chan<- *rfc5424.Message, donec <-chan struct{}) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	b.subscribe(msgc, donec)
}

// _read expects b.mu to already be locked
func (b *Buffer) read() []*rfc5424.Message {
	if b.cursor == -1 {
		return nil
	}

	buf := make([]*rfc5424.Message, len(b.messages))
	if b.cursor == 0 {
		copy(buf, b.messages)
	} else {
		copy(buf, b.messages[b.cursor:])
		copy(buf[len(b.messages)-b.cursor:], b.messages[:b.cursor])
	}
	return buf
}

// _subscribe assumes b.mu is already locked
func (b *Buffer) subscribe(msgc chan<- *rfc5424.Message, donec <-chan struct{}) {
	b.subs[msgc] = struct{}{}

	go func() {
		select {
		case <-donec:
		case <-b.donec:
		}

		b.mu.RLock()
		defer b.mu.RUnlock()

		delete(b.subs, msgc)
		close(msgc)
	}()
}
