package main

import "github.com/flynn/flynn/pkg/syslog/rfc5424"

type Iterator struct {
	id      string
	follow  bool
	backlog bool
	lines   int
	filter  Filter
	donec   <-chan struct{}
}

func (i *Iterator) Scan(agg *Aggregator) <-chan *rfc5424.Message {
	msgc := make(chan *rfc5424.Message)

	go i.scan(agg, msgc)
	return msgc
}

func (i *Iterator) scan(agg *Aggregator, msgc chan<- *rfc5424.Message) {
	defer close(msgc)

	if !i.follow {
		for _, msg := range i.readLastN(agg) {
			msgc <- msg
		}
		return
	}

	if !i.backlog {
		for msg := range i.subscribe(agg) {
			msgc <- msg
		}
		return
	}

	messages, subc := i.readAndSubscribe(agg)
	for _, msg := range messages {
		msgc <- msg
	}
	for msg := range subc {
		msgc <- msg
	}
}

func (i *Iterator) readLastN(agg *Aggregator) []*rfc5424.Message {
	if agg == nil {
		panic("agg is nil")
	}
	messages := agg.Read(i.id)
	return i.reverseFilter(messages)
}

func (i *Iterator) subscribe(agg *Aggregator) <-chan *rfc5424.Message {
	msgc := make(chan *rfc5424.Message, 1000)
	agg.Subscribe(i.id, msgc, i.donec)

	return i.filterChan(msgc)
}

func (i *Iterator) readAndSubscribe(agg *Aggregator) ([]*rfc5424.Message, <-chan *rfc5424.Message) {
	msgc := make(chan *rfc5424.Message, 1000)
	messages := agg.ReadAndSubscribe(i.id, msgc, i.donec)
	return i.reverseFilter(messages), i.filterChan(msgc)
}

func (i *Iterator) filterChan(msgc <-chan *rfc5424.Message) <-chan *rfc5424.Message {
	filterc := make(chan *rfc5424.Message)

	go func() {
		defer close(filterc)

		for msg := range msgc {
			if i.filter.Match(msg) {
				filterc <- msg
			}
		}
	}()

	return filterc
}

func (i *Iterator) reverseFilter(unfiltered []*rfc5424.Message) []*rfc5424.Message {
	n := i.lines
	if n == 0 || n > len(unfiltered) {
		n = len(unfiltered)
	}

	messages := i.filter.Filter(unfiltered)
	if len(messages) > n {
		return messages[len(messages)-n:]
	}
	return messages
}
