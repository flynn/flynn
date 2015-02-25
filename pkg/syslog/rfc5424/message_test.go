package rfc5424

import (
	"fmt"
	"testing"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

// Hook gocheck up to the "go test" runner
func TestRFC5424(t *testing.T) { TestingT(t) }

type S struct {
}

var _ = Suite(&S{})

func (s *S) TestNewMessage(c *C) {
	ts := time.Now().UTC()
	tss := ts.Format(time.RFC3339Nano)

	table := []struct {
		hdr Header
		msg []byte

		want string
	}{
		{
			hdr:  Header{Timestamp: ts},
			want: fmt.Sprintf("<0>1 %s - - - - -", tss),
		},
		{
			hdr: Header{
				Facility:  0,
				Severity:  1,
				Version:   1,
				Timestamp: ts,
				Hostname:  []byte("3.4.5.6"),
				AppName:   []byte("APP-7"),
				ProcID:    []byte("PID-8"),
				MsgID:     []byte("FD9"),
			},
			msg:  []byte("Hello, world!"),
			want: fmt.Sprintf("<1>1 %s 3.4.5.6 APP-7 PID-8 FD9 - Hello, world!", tss),
		},
	}

	for _, test := range table {
		msg := NewMessage(&test.hdr, test.msg)

		c.Assert(msg.String(), Equals, test.want)
	}
}
