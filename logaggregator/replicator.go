package main

import (
	"sync"

	"github.com/flynn/flynn/pkg/syslog/rfc5424"
)

type Replicator struct {
	mu sync.Mutex

	wg        sync.WaitGroup
	count     int
	followers map[int]chan<- *rfc5424.Message
	shutdown  chan struct{}
}

func NewReplicator() *Replicator {
	return &Replicator{
		wg:        sync.WaitGroup{},
		followers: make(map[int]chan<- *rfc5424.Message),
		shutdown:  make(chan struct{}),
	}
}

func (r *Replicator) Feed(msg *rfc5424.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, msgc := range r.followers {
		select {
		case msgc <- msg:
		default:
		}
	}
}

func (r *Replicator) Shutdown() {
	close(r.shutdown)
	r.wg.Wait()
}

func (r *Replicator) Register(cancelc <-chan bool) <-chan *rfc5424.Message {
	msgc := make(chan *rfc5424.Message, 1000)

	r.mu.Lock()
	id := r.count
	r.count++
	r.followers[id] = msgc
	r.wg.Add(1)
	r.mu.Unlock()

	go r.monitor(id, msgc, cancelc)
	return msgc
}

func (r *Replicator) monitor(id int, msgc chan<- *rfc5424.Message, cancelc <-chan bool) {
	select {
	case <-cancelc:
	case <-r.shutdown:
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.followers, id)
	close(msgc)
	r.wg.Done()
}
