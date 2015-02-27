package rfc5424

import (
	"fmt"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

func (s *S) TestParse(c *C) {
	ts := time.Now().UTC()
	tss := ts.Format(time.RFC3339Nano)

	table := []struct {
		msg  string
		want *Message
	}{
		{
			msg: fmt.Sprintf("<1>1 %s 3.4.5.6 APP-7 PID-8 FD9 - message body", tss),
			want: &Message{
				Header: Header{
					Facility:  0,
					Severity:  1,
					Version:   1,
					Timestamp: ts,
					Hostname:  []byte("3.4.5.6"),
					AppName:   []byte("APP-7"),
					ProcID:    []byte("PID-8"),
					MsgID:     []byte("FD9"),
				},
				Msg: []byte("message body"),
			},
		},

		// empty message
		{
			msg: fmt.Sprintf("<1>1 %s - - - - -", tss),
			want: &Message{
				Header: Header{
					Facility:  0,
					Severity:  1,
					Version:   1,
					Timestamp: ts,
				},
			},
		},
	}

	for _, test := range table {
		msg, err := Parse([]byte(test.msg))
		if err != nil {
			c.Error(err)
		}

		c.Assert(msg, DeepEquals, test.want)
	}
}
