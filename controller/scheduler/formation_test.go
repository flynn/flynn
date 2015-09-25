package main

import (
	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	ct "github.com/flynn/flynn/controller/types"
)

func (TestSuite) TestFormationUpdate(c *C) {
	type test struct {
		desc      string
		initial   Processes
		requested Processes
		diff      Processes
	}
	for _, t := range []test{
		{
			desc:      "+1 web proc",
			initial:   Processes{"web": 1},
			requested: Processes{"web": 2},
			diff:      Processes{"web": 1},
		},
		{
			desc:      "+3 web proc",
			initial:   Processes{"web": 1},
			requested: Processes{"web": 4},
			diff:      Processes{"web": 3},
		},
		{
			desc:      "-1 web proc",
			initial:   Processes{"web": 2},
			requested: Processes{"web": 1},
			diff:      Processes{"web": -1},
		},
		{
			desc:      "-3 web proc",
			initial:   Processes{"web": 4},
			requested: Processes{"web": 1},
			diff:      Processes{"web": -3},
		},
		{
			desc:      "no change",
			initial:   Processes{"web": 1},
			requested: Processes{"web": 1},
			diff:      Processes{"web": 0},
		},
		{
			desc:      "missing type",
			initial:   Processes{"web": 1, "worker": 1},
			requested: Processes{"web": 1},
			diff:      Processes{"web": 0, "worker": -1},
		},
		{
			desc:      "nil request",
			initial:   Processes{"web": 1, "worker": 1},
			requested: nil,
			diff:      Processes{"web": -1, "worker": -1},
		},
		{
			desc:      "multiple type changes",
			initial:   Processes{"web": 3, "worker": 1},
			requested: Processes{"web": 1, "clock": 2},
			diff:      Processes{"web": -2, "worker": -1, "clock": 2},
		},
	} {
		formation := NewFormation(&ct.ExpandedFormation{Processes: t.initial}, func(interface{}) error { return nil })
		diff := formation.Update(t.requested)
		c.Assert(diff, DeepEquals, t.diff, Commentf(t.desc))
		c.Assert(formation.GetProcesses(), DeepEquals, t.requested, Commentf(t.desc))
	}
}
