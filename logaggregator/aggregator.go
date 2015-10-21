package main

import (
	"errors"
	"sync"

	"github.com/flynn/flynn/logaggregator/buffer"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"
)

var errBufferFull = errors.New("feed buffer full")

// Aggregator is a log aggregation server that collects syslog messages.
type Aggregator struct {
	bmu     sync.Mutex // protects buffers
	buffers map[string]*buffer.Buffer

	msgc chan *rfc5424.Message

	pmu    sync.Mutex
	pausec chan struct{}
}

// NewAggregator creates a new running Aggregator.
func NewAggregator() *Aggregator {
	a := &Aggregator{
		buffers: make(map[string]*buffer.Buffer),
		msgc:    make(chan *rfc5424.Message, 1000),
		pausec:  make(chan struct{}),
	}
	go a.run()
	return a
}

// Feed inserts a message in the aggregator.
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

// Read returns the buffered messages for id.
func (a *Aggregator) Read(id string) []*rfc5424.Message {
	return a.getBuffer(id).Read()
}

// ReadAll returns all buffered messages.
func (a *Aggregator) ReadAll() [][]*rfc5424.Message {
	// TODO(benburkert): restructure Aggregator & ring.Buffer to avoid nested locks
	a.bmu.Lock()
	defer a.bmu.Unlock()

	buffers := make([][]*rfc5424.Message, 0, len(a.buffers))
	for _, buf := range a.buffers {
		buffers = append(buffers, buf.Read())
	}

	return buffers
}

// Read returns the buffered messages and adds a subscriber channel for id.
func (a *Aggregator) ReadAndSubscribe(id string, msgc chan<- *rfc5424.Message, donec <-chan struct{}) []*rfc5424.Message {
	return a.getBuffer(id).ReadAndSubscribe(msgc, donec)
}

// Reset clears all buffered data and closes subscribers.
func (a *Aggregator) Reset() {
	a.bmu.Lock()
	defer a.bmu.Unlock()

	for k, buf := range a.buffers {
		buf.Close()
		delete(a.buffers, k)
	}
}

// Shutdown stops the Aggregator, resets the buffers, and closes buffer
// subscribers.
func (a *Aggregator) Shutdown() {
	a.Reset()
	close(a.msgc)
}

// Read adds a subscriber channel for id.
func (a *Aggregator) Subscribe(id string, msgc chan<- *rfc5424.Message, donec <-chan struct{}) {
	a.getBuffer(id).Subscribe(msgc, donec)
}

func (a *Aggregator) getBuffer(id string) *buffer.Buffer {
	a.bmu.Lock()
	defer a.bmu.Unlock()

	if buf, ok := a.buffers[id]; ok {
		return buf
	}

	buf := buffer.NewBuffer()
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

func (a *Aggregator) feed(msg *rfc5424.Message) {
	if err := a.getBuffer(string(msg.AppName)).Add(msg); err != nil {
		panic(err)
	}
}
