package buffer

import (
	"errors"
	"sync"

	"github.com/flynn/flynn/pkg/syslog/rfc5424"
)

// Buffer is a linked list that holds rfc5424.Messages. The Buffer's entire
// contents can be read at once. Reading elements out of the buffer does not
// clear them; messages merely get removed from the buffer when they are
// replaced by new messages.
//
// A Buffer also offers the ability to subscribe to new incoming messages.
type Buffer struct {
	mu       sync.RWMutex // protects all of the following:
	head     *message
	tail     *message
	length   int
	capacity int
	subs     map[chan<- *rfc5424.Message]struct{}
	donec    chan struct{}
}

type message struct {
	next *message
	prev *message
	rfc5424.Message
}

const DefaultCapacity = 10000

// NewBuffer returns an empty allocated Buffer with DefaultCapacity.
func NewBuffer() *Buffer {
	return newBuffer(DefaultCapacity)
}

func newBuffer(capacity int) *Buffer {
	return &Buffer{
		capacity: capacity,
		subs:     make(map[chan<- *rfc5424.Message]struct{}),
		donec:    make(chan struct{}),
	}
}

// Add adds an element to the Buffer. If the Buffer is already full, it removes
// an existing message.
func (b *Buffer) Add(m *rfc5424.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.length == -1 {
		return errors.New("buffer closed")
	}
	if b.head == nil {
		b.head = &message{Message: *m}
		b.tail = b.head
	} else {
		// iterate from newest to oldest through messages to find position
		// to insert new message
		for other := b.tail; other != nil; other = other.prev {
			if m.Timestamp.Before(other.Timestamp) {
				if other.prev == nil {
					// insert before other at head
					other.prev = &message{Message: *m, next: other}
					b.head = other.prev
					break
				} else {
					continue
				}
			}
			msg := &message{Message: *m, prev: other}
			if other.next != nil {
				// insert between other and other.next
				other.next.prev = msg
				msg.next = other.next
			} else {
				// insert at tail
				b.tail = msg
			}
			other.next = msg
			break
		}
	}
	if b.length < b.capacity {
		// buffer not yet full
		b.length++
	} else {
		// at capacity, remove head
		b.head = b.head.next
		b.head.prev = nil
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

	b.head = nil
	b.tail = nil
	b.length = -1
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
	if b.length == -1 {
		return nil
	}

	buf := make([]*rfc5424.Message, 0, b.length)
	msg := b.head
	for msg != nil {
		buf = append(buf, &msg.Message)
		msg = msg.next
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
