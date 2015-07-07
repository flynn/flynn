package main

import (
	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	ct "github.com/flynn/flynn/controller/types"
)

func (TestSuite) TestFormationUpdate(c *C) {
	type test struct {
		desc      string
		initial   map[string]int
		requested map[string]int
		diff      map[string]int
	}
	for _, t := range []test{
		{
			desc:      "+1 web proc",
			initial:   map[string]int{"web": 1},
			requested: map[string]int{"web": 2},
			diff:      map[string]int{"web": 1},
		},
		{
			desc:      "+3 web proc",
			initial:   map[string]int{"web": 1},
			requested: map[string]int{"web": 4},
			diff:      map[string]int{"web": 3},
		},
		{
			desc:      "-1 web proc",
			initial:   map[string]int{"web": 2},
			requested: map[string]int{"web": 1},
			diff:      map[string]int{"web": -1},
		},
		{
			desc:      "-3 web proc",
			initial:   map[string]int{"web": 4},
			requested: map[string]int{"web": 1},
			diff:      map[string]int{"web": -3},
		},
		{
			desc:      "no change",
			initial:   map[string]int{"web": 1},
			requested: map[string]int{"web": 1},
			diff:      map[string]int{"web": 0},
		},
		{
			desc:      "missing type",
			initial:   map[string]int{"web": 1, "worker": 1},
			requested: map[string]int{"web": 1},
			diff:      map[string]int{"web": 0, "worker": -1},
		},
		{
			desc:      "nil request",
			initial:   map[string]int{"web": 1, "worker": 1},
			requested: nil,
			diff:      map[string]int{"web": -1, "worker": -1},
		},
		{
			desc:      "multiple type changes",
			initial:   map[string]int{"web": 3, "worker": 1},
			requested: map[string]int{"web": 1, "clock": 2},
			diff:      map[string]int{"web": -2, "worker": -1, "clock": 2},
		},
	} {
		formation := NewFormation(&ct.ExpandedFormation{Processes: t.initial})
		diff := formation.Update(t.requested)
		c.Assert(diff, DeepEquals, t.diff, Commentf(t.desc))
		c.Assert(formation.Processes, DeepEquals, t.requested, Commentf(t.desc))
	}
}
