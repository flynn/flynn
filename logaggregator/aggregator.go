package main

import (
	"errors"
	"sync"

	"github.com/flynn/flynn/logaggregator/ring"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"
)

var errBufferFull = errors.New("feed buffer full")

// Aggregator is a log aggregation server that collects syslog messages.
type Aggregator struct {
	bmu     sync.Mutex // protects buffers
	buffers map[string]*ring.Buffer

	msgc chan *rfc5424.Message

	pmu    sync.Mutex
	pausec chan struct{}
}

// NewAggregator creates a new running Aggregator.
func NewAggregator() *Aggregator {
	a := &Aggregator{
		buffers: make(map[string]*ring.Buffer),
		msgc:    make(chan *rfc5424.Message, 1000),
		pausec:  make(chan struct{}),
	}
	go a.run()
	return a
}

// Shutdown shuts down the Aggregator gracefully by closing its listener,
// and waiting for already-received logs to be processed.
func (a *Aggregator) Shutdown() {
	close(a.msgc)
}

// ReadLastN reads up to N logs from the log buffer with id and sends them over
// a channel. If n is less than 0, or if there are fewer than n logs buffered,
// all buffered logs are returned. If a signal is sent on done, the returned
// channel is closed and the goroutine exits.
func (a *Aggregator) ReadLastN(
	id string,
	n int,
	filter Filter,
	done <-chan struct{},
) <-chan *rfc5424.Message {
	msgc := make(chan *rfc5424.Message)
	go func() {
		defer close(msgc)

		messages := filter.Filter(a.readLastN(id, -1))
		if n > 0 && len(messages) > n {
			messages = messages[len(messages)-n:]
		}
		for _, syslogMsg := range messages {
			select {
			case msgc <- syslogMsg:
			case <-done:
				return
			}
		}
	}()
	return msgc
}

// readLastN reads up to N logs from the log buffer with id. If n is less than
// 0, or if there are fewer than n logs buffered, all buffered logs are
// returned.
func (a *Aggregator) readLastN(id string, n int) []*rfc5424.Message {
	buf := a.getBuffer(id)
	if buf == nil {
		return nil
	}
	if n >= 0 {
		return buf.ReadLastN(n)
	}
	return buf.ReadAll()
}

// ReadLastNAndSubscribe is like ReadLastN, except that after sending buffered
// log lines, it also streams new lines as they arrive.
func (a *Aggregator) ReadLastNAndSubscribe(
	id string,
	n int,
	filter Filter,
	done <-chan struct{},
) <-chan *rfc5424.Message {
	msgc := make(chan *rfc5424.Message)
	buf := a.getOrInitializeBuffer(id)

	var messages []*rfc5424.Message
	var subc <-chan *rfc5424.Message
	var cancel func()

	messages, subc, cancel = buf.ReadAllAndSubscribe()

	go func() {
		defer cancel()
		defer close(msgc)

		if filter != nil {
			messages = filter.Filter(messages)
			if n > 0 && len(messages) > n {
				messages = messages[len(messages)-n:]
			}
		}

		// range over messages, watch done
		for _, msg := range messages {
			select {
			case <-done:
				return
			case msgc <- msg:
			}
		}

		// select on subc, done, and cancel if done
		for {
			select {
			case msg := <-subc:
				if msgc == nil { // subc was closed
					return
				}
				if !filter.Match(msg) {
					continue // skip this message if it doesn't match filters
				}
				select {
				case msgc <- msg:
				case <-done:
					return
				}
			case <-done:
				return
			}
		}
	}()
	return msgc
}

func (a *Aggregator) Feed(msg *rfc5424.Message) {
	a.msgc <- msg
}

func (a *Aggregator) Pause() func() {
	a.pmu.Lock()

	a.pausec <- struct{}{}

	return func() {
		<-a.pausec
		a.pmu.Unlock()
	}
}

func (a *Aggregator) Flush() {
	a.bmu.Lock()
	defer a.bmu.Unlock()

	for _, buf := range a.buffers {
		buf.Flush()
	}
}

func (a *Aggregator) CopyBuffers() [][]*rfc5424.Message {
	// TODO(benburkert): restructure Aggregator & ring.Buffer to avoid nested locks
	a.bmu.Lock()
	buffers := make([][]*rfc5424.Message, 0, len(a.buffers))
	for _, buf := range a.buffers {
		buffers = append(buffers, buf.Clone().ReadAll())
	}
	a.bmu.Unlock()

	return buffers
}

func (a *Aggregator) getBuffer(id string) *ring.Buffer {
	a.bmu.Lock()
	defer a.bmu.Unlock()

	buf, _ := a.buffers[id]
	return buf
}

func (a *Aggregator) getOrInitializeBuffer(id string) *ring.Buffer {
	a.bmu.Lock()
	defer a.bmu.Unlock()

	if buf, ok := a.buffers[id]; ok {
		return buf
	}
	buf := ring.NewBuffer()
	a.buffers[id] = buf
	return buf
}

func (a *Aggregator) run() {
	for {
		select {
		case msg, ok := <-a.msgc:
			if !ok {
				return
			}
			a.feed(msg)

		case <-a.pausec:
			a.pausec <- struct{}{}
		}
	}
}

// testing hook:
var afterMessage func()

func (a *Aggregator) feed(msg *rfc5424.Message) {
	a.getOrInitializeBuffer(string(msg.AppName)).Add(msg)
	if afterMessage != nil {
		afterMessage()
	}
}
