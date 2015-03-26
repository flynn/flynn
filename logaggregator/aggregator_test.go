package main

import (
	"fmt"

	"github.com/flynn/flynn/pkg/syslog/rfc5424"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

var (
	appAMessages = buildTestData(200, &rfc5424.Header{
		AppName: []byte("app-A"),
	})

	appBJob1Messages = buildTestData(100, &rfc5424.Header{
		AppName: []byte("app-B"),
		ProcID:  []byte("web.job1"),
	})

	appBJob2Messages = buildTestData(100, &rfc5424.Header{
		AppName: []byte("app-B"),
		ProcID:  []byte("web.job2"),
	})

	appCWebMessages = buildTestData(100, &rfc5424.Header{
		AppName: []byte("app-C"),
		ProcID:  []byte("web.job1"),
	})

	appCRunMessages = buildTestData(100, &rfc5424.Header{
		AppName: []byte("app-C"),
		ProcID:  []byte("run.job2"),
	})
)

func (s *LogAggregatorTestSuite) TestReadLastN(c *C) {
	tests := []struct {
		data   []*rfc5424.Message
		id     string
		n      int
		filter Filter

		want []*rfc5424.Message
	}{
		// entire buffer
		{
			data: appAMessages,
			id:   "app-A",
			n:    -1,
			want: appAMessages,
		},
		// last 100 messages
		{
			data: appAMessages,
			id:   "app-A",
			n:    100,
			want: appAMessages[len(appAMessages)-100:],
		},
		// unfiltered
		{
			data: zip(appBJob1Messages, appBJob2Messages),
			id:   "app-B",
			n:    -1,
			want: zip(appBJob1Messages, appBJob2Messages),
		},
		// filtered by job
		{
			data:   zip(appBJob1Messages, appBJob2Messages),
			id:     "app-B",
			n:      -1,
			filter: filterJobID("job1"),
			want:   appBJob1Messages,
		},
		// filter by process type
		{
			data:   zip(appCRunMessages, appCWebMessages),
			id:     "app-C",
			n:      -1,
			filter: filterProcessType("web"),
			want:   appCWebMessages,
		},
	}

	for _, test := range tests {
		aggr := NewAggregator()
		defer aggr.Shutdown()

		for _, msg := range test.data {
			aggr.feed(msg)
		}

		donec := make(chan struct{})
		defer close(donec)

		filter := test.filter
		if filter == nil {
			filter = nopFilter
		}

		msgc := aggr.ReadLastN(test.id, test.n, filter, donec)

		got := make([]*rfc5424.Message, 0, len(test.want))
		for msg := range msgc {
			got = append(got, msg)
		}

		c.Assert(len(got), Equals, len(test.want))
		c.Assert(got, DeepEquals, test.want)
	}
}

func (s *LogAggregatorTestSuite) TestReadLastNAndSubscribe(c *C) {
	tests := []struct {
		bufData, subData []*rfc5424.Message

		id     string
		n      int
		filter Filter

		want []*rfc5424.Message
	}{
		// buffered + live messages
		{
			bufData: appAMessages[:100],
			subData: appAMessages[100:],
			n:       -1,
			id:      "app-A",
			want:    appAMessages,
		},
		// live messages only
		{
			subData: appAMessages,
			n:       0,
			id:      "app-A",
			want:    appAMessages,
		},
		// last N buffered + live messages
		{
			bufData: appAMessages[:100],
			subData: appAMessages[100:200],
			n:       50,
			id:      "app-A",
			want:    appAMessages[50:200],
		},
		// all buffered + live messages
		{
			bufData: appAMessages[80:100],
			subData: appAMessages[100:200],
			n:       50,
			id:      "app-A",
			want:    appAMessages[80:200],
		},
		// unfiltered
		{
			bufData: zip(appBJob1Messages[:50], appBJob2Messages[:50]),
			subData: zip(appBJob1Messages[50:], appBJob2Messages[50:]),
			id:      "app-B",
			n:       -1,
			want:    zip(appBJob1Messages, appBJob2Messages),
		},
		// filtered by job
		{
			bufData: zip(appBJob1Messages[:50], appBJob2Messages[:50]),
			subData: zip(appBJob1Messages[50:], appBJob2Messages[50:]),
			id:      "app-B",
			n:       -1,
			filter:  filterJobID("job1"),
			want:    appBJob1Messages,
		},
		// filtered by process type
		{
			bufData: zip(appCRunMessages[:50], appCWebMessages[:50]),
			subData: zip(appCWebMessages[50:], appCRunMessages[50:]),
			id:      "app-C",
			n:       -1,
			filter:  filterProcessType("web"),
			want:    appCWebMessages,
		},
	}

	for _, test := range tests {
		aggr := NewAggregator()
		defer aggr.Shutdown()

		for _, msg := range test.bufData {
			aggr.feed(msg)
		}

		donec := make(chan struct{})

		filter := test.filter
		if filter == nil {
			filter = nopFilter
		}
		msgc := aggr.ReadLastNAndSubscribe(test.id, test.n, filter, donec)
		got := make([]*rfc5424.Message, 0, len(test.want))

		if n := test.n; n > 0 {
			// if n > 0, read min(n, len(bufData)) messages before feeding sub msgs
			if l := len(test.bufData); l < n {
				n = l
			}

			for i := 0; i < n; i++ {
				got = append(got, <-msgc)
			}
		}

		for _, msg := range test.subData {
			aggr.feed(msg)
		}

		for msg := range msgc {
			got = append(got, msg)

			if len(got) == len(test.want) {
				// close the subscription after expected number of messages
				close(donec)
			}
		}

		c.Assert(len(got), Equals, len(test.want))
		c.Assert(got, DeepEquals, test.want)
	}
}

func buildTestData(n int, hdr *rfc5424.Header) []*rfc5424.Message {
	data := make([]*rfc5424.Message, n)
	for i := range data {
		line := []byte(fmt.Sprintf("line %d\n", i))
		msg := rfc5424.NewMessage(hdr, line)

		data[i] = msg
	}

	return data
}

// return a slice of interlaced input data. input slices must be the same
// length.
func zip(msgSlices ...[]*rfc5424.Message) []*rfc5424.Message {
	n, m := len(msgSlices[0]), len(msgSlices)
	data := make([]*rfc5424.Message, 0, n*m)

	for i := 0; i < n; i++ {
		for j := range msgSlices {
			data = append(data, msgSlices[j][i])
		}
	}
	return data
}
