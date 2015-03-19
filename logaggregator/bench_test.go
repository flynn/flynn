package main

import (
	"os"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

func (*LogAggregatorTestSuite) BenchmarkReplaySnapshot(c *C) {
	fi, err := os.Stat("testdata/sample.dat")
	c.Assert(err, IsNil)

	c.SetBytes(fi.Size())
	for i := 0; i < c.N; i++ {
		a := NewAggregator("")
		a.ReplaySnapshot("testdata/sample.dat")
	}
}

func (*LogAggregatorTestSuite) BenchmarkTakeSnapshot(c *C) {
	fi, err := os.Stat("testdata/sample.dat")
	c.Assert(err, IsNil)
	c.SetBytes(fi.Size())

	a := NewAggregator("")
	a.ReplaySnapshot("testdata/sample.dat")

	c.ResetTimer()
	for i := 0; i < c.N; i++ {
		a.TakeSnapshot("/dev/null")
	}
}
