package main

import (
	"os"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

func (s *LogAggregatorTestSuite) BenchmarkReplaySnapshot(c *C) {
	fi, err := os.Stat("testdata/sample.dat")
	c.Assert(err, IsNil)

	c.SetBytes(fi.Size())
	c.ResetTimer()
	for i := 0; i < c.N; i++ {
		srv := &Server{
			Aggregator: NewAggregator(),
		}

		srv.LoadSnapshotFile("testdata/sample.dat")
	}
}

func (*LogAggregatorTestSuite) BenchmarkTakeSnapshot(c *C) {
	fi, err := os.Stat("testdata/sample.dat")
	c.Assert(err, IsNil)
	c.SetBytes(fi.Size())

	srv := &Server{
		Aggregator: NewAggregator(),
	}

	srv.LoadSnapshotFile("testdata/sample.dat")

	c.ResetTimer()
	for i := 0; i < c.N; i++ {
		srv.WriteSnapshotFile("/dev/null")
	}
}
