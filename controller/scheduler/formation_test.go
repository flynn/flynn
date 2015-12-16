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
		formation := NewFormation(&ct.ExpandedFormation{Processes: t.initial})
		diff := formation.Update(t.requested)
		c.Assert(diff, DeepEquals, t.diff, Commentf(t.desc))
		c.Assert(formation.GetProcesses(), DeepEquals, t.requested, Commentf(t.desc))
	}
}

func (TestSuite) TestFormationRectifyOmni(c *C) {
	release := &ct.Release{Processes: map[string]ct.ProcessType{
		"web":  {Omni: false},
		"omni": {Omni: true},
	}}
	formation := NewFormation(&ct.ExpandedFormation{
		Release:   release,
		Processes: Processes{"web": 2, "omni": 1},
	})

	assertRectify := func(count int, changed bool, procs map[string]int) {
		c.Assert(formation.RectifyOmni(count), Equals, changed)
		c.Assert(formation.Processes, DeepEquals, procs)
	}

	// rectify with 1 host should not change anything
	assertRectify(1, false, map[string]int{"web": 2, "omni": 1})

	// rectify with 2 hosts should modify omni count
	assertRectify(2, true, map[string]int{"web": 2, "omni": 2})

	// rectify with 2 again should not change anything
	assertRectify(2, false, map[string]int{"web": 2, "omni": 2})

	// setting the processes and then rectifying should work the same
	formation.SetProcesses(Processes{"web": 4, "omni": 2})
	assertRectify(1, false, map[string]int{"web": 4, "omni": 2})
	assertRectify(2, true, map[string]int{"web": 4, "omni": 4})
	assertRectify(2, false, map[string]int{"web": 4, "omni": 4})
}
