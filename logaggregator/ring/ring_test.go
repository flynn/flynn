package ring

import (
	"fmt"
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
	msg0 := &rfc5424.Message{Msg: []byte{'0'}}
	msg1 := &rfc5424.Message{Msg: []byte{'1'}}
	b.Add(msg0)
	b.Add(msg1)

	res = b.ReadAll()
	c.Assert(res, HasLen, 2)
	c.Assert(cap(res), Equals, 2)
	c.Assert(res[0], DeepEquals, msg0)
	c.Assert(res[1], DeepEquals, msg1)

	// overfill the buffer by exactly one
	for i := 2; i < DefaultBufferCapacity+1; i++ {
		b.Add(&rfc5424.Message{Msg: []byte(strconv.Itoa(i))})
	}
	res = b.ReadAll()
	c.Assert(res, HasLen, DefaultBufferCapacity)
	c.Assert(cap(res), Equals, DefaultBufferCapacity)
	c.Assert(res[0], Equals, msg1)
	for i := 1; i < len(res); i++ {
		c.Assert(string(res[i].Msg), Equals, strconv.Itoa(i+1))
	}

	// ensure that modifying an element in res doesn't modify original buffer
	res[0] = &rfc5424.Message{Msg: []byte("A replacement message")}
	c.Assert(b.messages[1], Equals, msg1)
}

func (s *S) TestReadLastN(c *C) {
	runTest := func(n int, wantMsgs []string) {
		b := NewBuffer()

		// add a couple of elements
		msg0 := &rfc5424.Message{Msg: []byte{'0'}}
		msg1 := &rfc5424.Message{Msg: []byte{'1'}}
		b.Add(msg0)
		b.Add(msg1)

		res := b.ReadLastN(n)
		c.Assert(res, HasLen, len(wantMsgs))
		c.Assert(cap(res), Equals, len(wantMsgs))
		for i, want := range wantMsgs {
			c.Assert(res[i], DeepEquals, &rfc5424.Message{Msg: []byte(want)})
		}

		// overfill the buffer by exactly one
		for i := 2; i < DefaultBufferCapacity+1; i++ {
			b.Add(&rfc5424.Message{Msg: []byte(strconv.Itoa(i))})
		}
		res = b.ReadLastN(5)
		c.Assert(res, HasLen, 5)
		c.Assert(cap(res), Equals, 5)
		for i := 0; i < 5; i++ {
			c.Assert(string(res[i].Msg), Equals, strconv.Itoa(b.Capacity()-5+i))
		}
	}

	tests := []struct {
		n        int
		wantMsgs []string
	}{
		{n: 0, wantMsgs: []string{}},
		{n: 1, wantMsgs: []string{"1"}},
		{n: 2, wantMsgs: []string{"0", "1"}},
	}
	for _, test := range tests {
		c.Logf("running n=%d want=%v", test.n, test.wantMsgs)
		runTest(test.n, test.wantMsgs)
	}
}

func (s *S) TestReadLastNAndSubscribe(c *C) {
	runTest := func(n int, wantMsgs []string) {
		b := NewBuffer()
		b.Add(&rfc5424.Message{Msg: []byte("preexisting message 1")})
		b.Add(&rfc5424.Message{Msg: []byte("preexisting message 2")})

		messages, msgc, cancel := b.ReadLastNAndSubscribe(n)
		defer cancel()

		c.Assert(messages, HasLen, len(wantMsgs))
		for i, want := range wantMsgs {
			c.Assert(string(messages[i].Msg), Equals, want)
		}

		select {
		case msg := <-msgc:
			c.Fatalf("want no message, got %v", msg)
		default:
		}

		newMsgs := []string{"new message 1", "new message 2"}
		for _, msg := range newMsgs {
			b.Add(&rfc5424.Message{Msg: []byte(msg)})
		}

		for _, want := range newMsgs {
			select {
			case msg := <-msgc:
				c.Assert(string(msg.Msg), Equals, want)
			default:
				c.Fatalf("want message, got none")
			}
		}

		cancel()
		c.Assert(b.subs, HasLen, 0)
		select {
		case msg := <-msgc:
			c.Assert(msg, IsNil)
		default:
			c.Fatalf("want msgc to be closed")
		}
	}

	tests := []struct {
		n        int
		wantMsgs []string
	}{
		{n: 0, wantMsgs: []string{}},
		{n: 1, wantMsgs: []string{"preexisting message 2"}},
		{n: 2, wantMsgs: []string{"preexisting message 1", "preexisting message 2"}},
	}
	for _, test := range tests {
		c.Logf("running n=%d want=%v", test.n, test.wantMsgs)
		runTest(test.n, test.wantMsgs)
	}
}

func (s *S) TestSubscribe(c *C) {
	b := NewBuffer()
	b.Add(&rfc5424.Message{Msg: []byte("preexisting message")})

	msgc, cancel := b.Subscribe()
	defer cancel()

	c.Assert(cap(msgc), Equals, 1000)

	select {
	case msg := <-msgc:
		c.Fatalf("want no message, got %v", msg)
	default:
	}

	b.Add(&rfc5424.Message{Msg: []byte("new message 1")})
	b.Add(&rfc5424.Message{Msg: []byte("new message 2")})
	c.Assert(msgc, HasLen, 2)

	for i := 1; i < 3; i++ {
		select {
		case msg := <-msgc:
			c.Assert(string(msg.Msg), Equals, fmt.Sprintf("new message %d", i))
		default:
			c.Fatalf("got no message i=%d", i)
		}
	}

	cancel()
	c.Assert(b.subs, HasLen, 0)
	select {
	case msg := <-msgc:
		c.Assert(msg, IsNil)
	default:
		c.Fatalf("want msgc to be closed")
	}
}
