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
	a.Flush()
	close(a.msgc)
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

	for k, buf := range a.buffers {
		buf.Close()
		delete(a.buffers, k)
	}
}

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

func (a *Aggregator) feed(msg *rfc5424.Message) {
	if err := a.getOrInitializeBuffer(string(msg.AppName)).Add(msg); err != nil {
		panic(err)
	}
}
