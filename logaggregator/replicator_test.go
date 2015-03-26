package main

import (
	"fmt"

	"github.com/flynn/flynn/pkg/syslog/rfc5424"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

func (s *LogAggregatorTestSuite) TestFollowReplicator(c *C) {
	repr := NewReplicator()
	defer repr.Shutdown()

	canc := make(chan bool)
	msgc := repr.Follow(canc)

	messages := make([]*rfc5424.Message, 1000)
	for i := range messages {
		line := []byte(fmt.Sprintf("line %d\n", i))
		msg := rfc5424.NewMessage(&rfc5424.Header{}, line)

		repr.Feed(msg)
		messages[i] = msg
	}

	for _, want := range messages {
		got := <-msgc
		c.Assert(want, DeepEquals, got)
	}

	close(canc)
	if _, ok := <-msgc; ok {
		c.Error("message channel still open")
	}
}
