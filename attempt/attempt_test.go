package attempt_test

import (
	"errors"
	"testing"
	"time"

	"github.com/flynn/go-flynn/attempt"
	. "launchpad.net/gocheck"
)

func Test(t *testing.T) {
	TestingT(t)
}

var _ = Suite(&S{})

type S struct{}

func (S) TestAttemptTiming(c *C) {
	testAttempt := attempt.Strategy{
		Total: 0.25e9,
		Delay: 0.1e9,
	}
	want := []time.Duration{0, 0.1e9, 0.2e9, 0.2e9}
	got := make([]time.Duration, 0, len(want)) // avoid allocation when testing timing
	t0 := time.Now()
	for a := testAttempt.Start(); a.Next(); {
		got = append(got, time.Now().Sub(t0))
	}
	got = append(got, time.Now().Sub(t0))
	c.Assert(got, HasLen, len(want))
	const margin = 0.01e9
	for i, got := range want {
		lo := want[i] - margin
		hi := want[i] + margin
		if got < lo || got > hi {
			c.Errorf("attempt %d want %g got %g", i, want[i].Seconds(), got.Seconds())
		}
	}
}

func (S) TestAttemptNextHasNext(c *C) {
	a := attempt.Strategy{}.Start()
	c.Assert(a.Next(), Equals, true)
	c.Assert(a.Next(), Equals, false)

	a = attempt.Strategy{}.Start()
	c.Assert(a.Next(), Equals, true)
	c.Assert(a.HasNext(), Equals, false)
	c.Assert(a.Next(), Equals, false)

	a = attempt.Strategy{Total: 2e8}.Start()
	c.Assert(a.Next(), Equals, true)
	c.Assert(a.HasNext(), Equals, true)
	time.Sleep(2e8)
	c.Assert(a.HasNext(), Equals, true)
	c.Assert(a.Next(), Equals, true)
	c.Assert(a.Next(), Equals, false)

	a = attempt.Strategy{Total: 1e8, Min: 2}.Start()
	time.Sleep(1e8)
	c.Assert(a.Next(), Equals, true)
	c.Assert(a.HasNext(), Equals, true)
	c.Assert(a.Next(), Equals, true)
	c.Assert(a.HasNext(), Equals, false)
	c.Assert(a.Next(), Equals, false)
}

func (S) TestAttemptRun(c *C) {
	runs := 0
	err := errors.New("error")
	res := attempt.Strategy{}.Run(func() error {
		runs++
		return err
	})
	c.Assert(res, Equals, err)
	c.Assert(runs, Equals, 1)

	runs = 0
	res = attempt.Strategy{}.Run(func() error {
		runs++
		return nil
	})
	c.Assert(res, IsNil)
	c.Assert(runs, Equals, 1)
}
