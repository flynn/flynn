package main

import (
	"fmt"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"
)

func (s *LogAggregatorTestSuite) TestAggregator(c *C) {
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
