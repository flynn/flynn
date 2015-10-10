package main

import (
	"fmt"
	"time"

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

	nopFilter = Filter(make(filterSlice, 0))
)

func (s *LogAggregatorTestSuite) TestReadLastN(c *C) {
	tests := []struct {
		data []*rfc5424.Message
		iter *Iterator

		want []*rfc5424.Message
	}{
		// entire buffer
		{
			data: appAMessages,
			iter: &Iterator{
				id: "app-A",
			},
			want: appAMessages,
		},
		// last 100 messages
		{
			data: appAMessages,
			iter: &Iterator{
				id:      "app-A",
				backlog: true,
				lines:   100,
			},
			want: appAMessages[len(appAMessages)-100:],
		},
		// unfiltered
		{
			data: zip(appBJob1Messages, appBJob2Messages),
			iter: &Iterator{
				id: "app-B",
			},
			want: zip(appBJob1Messages, appBJob2Messages),
		},
		// filtered by job
		{
			data: zip(appBJob1Messages, appBJob2Messages),
			iter: &Iterator{
				id:     "app-B",
				filter: filterJobID("job1"),
			},
			want: appBJob1Messages,
		},
		// filter by process type
		{
			data: zip(appCRunMessages, appCWebMessages),
			iter: &Iterator{
				id:     "app-C",
				filter: filterProcessType("web"),
			},
			want: appCWebMessages,
		},
	}

	for _, test := range tests {
		aggr := NewAggregator()
		defer aggr.Shutdown()

		for _, msg := range test.data {
			aggr.feed(msg)
		}

		donec := make(chan struct{})
		test.iter.donec = donec
		defer close(donec)

		if test.iter.filter == nil {
			test.iter.filter = nopFilter
		}

		got := make([]*rfc5424.Message, 0, len(test.want))

		for msg := range test.iter.Scan(aggr) {
			got = append(got, msg)
		}

		c.Assert(len(got), Equals, len(test.want))
		c.Assert(got, DeepEquals, test.want)
	}
}

func (s *LogAggregatorTestSuite) TestReadLastNAndSubscribe(c *C) {
	tests := []struct {
		bufData, subData []*rfc5424.Message

		iter *Iterator

		want []*rfc5424.Message
	}{
		// buffered + live messages
		{
			bufData: appAMessages[:100],
			subData: appAMessages[100:],
			iter: &Iterator{
				id:      "app-A",
				backlog: true,
				follow:  true,
			},
			want: appAMessages,
		},
		// live messages only
		{
			subData: appAMessages,
			iter: &Iterator{
				id:      "app-A",
				backlog: true,
				follow:  true,
			},
			want: appAMessages,
		},
		// last N buffered + live messages
		{
			bufData: appAMessages[:100],
			subData: appAMessages[100:200],
			iter: &Iterator{
				id:      "app-A",
				backlog: true,
				lines:   50,
				follow:  true,
			},
			want: appAMessages[50:200],
		},
		// all buffered + live messages
		{
			bufData: appAMessages[80:100],
			subData: appAMessages[100:200],
			iter: &Iterator{
				id:      "app-A",
				backlog: true,
				lines:   50,
				follow:  true,
			},
			want: appAMessages[80:200],
		},
		// unfiltered
		{
			bufData: zip(appBJob1Messages[:50], appBJob2Messages[:50]),
			subData: zip(appBJob1Messages[50:], appBJob2Messages[50:]),
			iter: &Iterator{
				id:      "app-B",
				backlog: true,
				follow:  true,
			},
			want: zip(appBJob1Messages, appBJob2Messages),
		},
		// filtered by job
		{
			bufData: zip(appBJob1Messages[:50], appBJob2Messages[:50]),
			subData: zip(appBJob1Messages[50:], appBJob2Messages[50:]),
			iter: &Iterator{
				id:      "app-B",
				filter:  filterJobID("job1"),
				backlog: true,
				follow:  true,
			},
			want: appBJob1Messages,
		},
		// filtered by process type
		{
			bufData: zip(appCRunMessages[:50], appCWebMessages[:50]),
			subData: zip(appCWebMessages[50:], appCRunMessages[50:]),
			iter: &Iterator{
				id:      "app-C",
				filter:  filterProcessType("web"),
				backlog: true,
				follow:  true,
			},
			want: appCWebMessages,
		},
	}

	for _, test := range tests {
		aggr := NewAggregator()
		defer aggr.Shutdown()

		for _, msg := range test.bufData {
			aggr.feed(msg)
		}

		donec := make(chan struct{})
		test.iter.donec = donec

		if test.iter.filter == nil {
			test.iter.filter = nopFilter
		}

		msgc := test.iter.Scan(aggr)
		got := make([]*rfc5424.Message, 0, len(test.want))
		if test.iter.backlog {
			n := test.iter.lines
			// read min(iter.lines, len(bufData)) messages before feeding sub msgs
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

var timeNow = time.Now()

func buildTestData(n int, hdr *rfc5424.Header) []*rfc5424.Message {
	data := make([]*rfc5424.Message, n)
	for i := range data {
		line := []byte(fmt.Sprintf("line %d\n", i))
		msg := rfc5424.NewMessage(hdr, line)
		msg.StructuredData = []byte(fmt.Sprintf(`[flynn seq="%d"]`, i))
		msg.Timestamp = timeNow

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
