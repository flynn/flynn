package ring

import (
	"strconv"
	"testing"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type S struct {
}

var _ = Suite(&S{})

func (s *S) TestNewBuffer(c *C) {
	b := NewBuffer()
	c.Assert(b.messages, HasLen, 0)
	c.Assert(cap(b.messages), Equals, DefaultBufferCapacity)
	c.Assert(b.start, Equals, 0)
}

func (s *S) TestBuffer(c *C) {
	b := NewBuffer()

	// test the empty buffer
	res := b.ReadAll()
	c.Assert(res, HasLen, 0)
	c.Assert(cap(res), Equals, 0)

	// add a couple of elements
	msg0 := &rfc5424.Message{Msg: "0"}
	msg1 := &rfc5424.Message{Msg: "1"}
	b.Add(msg0)
	b.Add(msg1)

	res = b.ReadAll()
	c.Assert(res, HasLen, 2)
	c.Assert(cap(res), Equals, 2)
	c.Assert(res[0], Equals, msg0)
	c.Assert(res[1], Equals, msg1)

	// overfill the buffer by exactly one
	for i := 2; i < DefaultBufferCapacity+1; i++ {
		b.Add(&rfc5424.Message{Msg: strconv.Itoa(i)})
	}
	res = b.ReadAll()
	c.Assert(res, HasLen, DefaultBufferCapacity)
	c.Assert(cap(res), Equals, DefaultBufferCapacity)
	c.Assert(res[0], Equals, msg1)
	for i := 1; i < len(res); i++ {
		c.Assert(res[i].Msg, Equals, strconv.Itoa(i+1))
	}

	// ensure that modifying an element in res doesn't modify original buffer
	res[0] = &rfc5424.Message{Msg: "A replacement message"}
	c.Assert(b.messages[1], Equals, msg1)
}

func (s *S) TestReadLastN(c *C) {
	b := NewBuffer()

	// add a couple of elements
	msg0 := &rfc5424.Message{Msg: "0"}
	msg1 := &rfc5424.Message{Msg: "1"}
	b.Add(msg0)
	b.Add(msg1)

	res := b.ReadLastN(1)
	c.Assert(res, HasLen, 1)
	c.Assert(cap(res), Equals, 1)
	c.Assert(res[0], Equals, msg1)

	// overfill the buffer by exactly one
	for i := 2; i < DefaultBufferCapacity+1; i++ {
		b.Add(&rfc5424.Message{Msg: strconv.Itoa(i)})
	}
	res = b.ReadLastN(5)
	c.Assert(res, HasLen, 5)
	c.Assert(cap(res), Equals, 5)
	for i := 0; i < 5; i++ {
		c.Assert(res[i].Msg, Equals, strconv.Itoa(b.Capacity()-5+i))
	}
}
