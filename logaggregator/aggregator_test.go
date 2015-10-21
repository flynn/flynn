package main

import (
	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"
)

func (s *LogAggregatorTestSuite) TestAggregator(c *C) {
	data := zip(appAMessages[:100], appCRunMessages, appBJob2Messages, appCWebMessages, appBJob1Messages)

	aggr := NewAggregator()
	defer aggr.Shutdown()

	for _, msg := range data {
		aggr.feed(msg)
	}

	tests := []struct {
		id   string
		want []*rfc5424.Message
	}{
		{
			id:   "app-A",
			want: appAMessages[:100],
		},
		{
			id:   "app-B",
			want: zip(appBJob2Messages, appBJob1Messages),
		},
		{
			id:   "app-C",
			want: zip(appCRunMessages, appCWebMessages),
		},
	}

	for _, test := range tests {
		got := aggr.Read(test.id)

		c.Assert(len(got), Equals, len(test.want))
		c.Assert(got, DeepEquals, test.want)
	}
}
