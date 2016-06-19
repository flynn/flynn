package main

import (
	ct "github.com/flynn/flynn/controller/types"
	. "github.com/flynn/go-check"
)

func (TestSuite) TestFormationDiff(c *C) {
	type test struct {
		desc     string
		running  Processes
		expected Processes
		diff     Processes
	}
	for _, t := range []test{
		{
			desc:     "+1 web proc",
			running:  Processes{"web": 1},
			expected: Processes{"web": 2},
			diff:     Processes{"web": 1},
		},
		{
			desc:     "+3 web proc",
			running:  Processes{"web": 1},
			expected: Processes{"web": 4},
			diff:     Processes{"web": 3},
		},
		{
			desc:     "-1 web proc",
			running:  Processes{"web": 2},
			expected: Processes{"web": 1},
			diff:     Processes{"web": -1},
		},
		{
			desc:     "-3 web proc",
			running:  Processes{"web": 4},
			expected: Processes{"web": 1},
			diff:     Processes{"web": -3},
		},
		{
			desc:     "no change",
			running:  Processes{"web": 1},
			expected: Processes{"web": 1},
			diff:     Processes{"web": 0},
		},
		{
			desc:     "missing type",
			running:  Processes{"web": 1, "worker": 1},
			expected: Processes{"web": 1},
			diff:     Processes{"web": 0, "worker": -1},
		},
		{
			desc:     "nil request",
			running:  Processes{"web": 1, "worker": 1},
			expected: nil,
			diff:     Processes{"web": -1, "worker": -1},
		},
		{
			desc:     "multiple type changes",
			running:  Processes{"web": 3, "worker": 1},
			expected: Processes{"web": 1, "clock": 2},
			diff:     Processes{"web": -2, "worker": -1, "clock": 2},
		},
	} {
		formation := NewFormation(&ct.ExpandedFormation{Processes: t.expected})
		diff := formation.Diff(t.running)
		c.Assert(diff, DeepEquals, t.diff, Commentf(t.desc))
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

func (TestSuite) TestDiffScaleDownOf(c *C) {
	type test struct {
		diff     Processes
		procs    Processes
		expected bool
	}

	for _, t := range []test{
		{
			diff:     nil,
			procs:    nil,
			expected: false,
		},
		{
			diff:     Processes{"web": 1},
			procs:    Processes{"web": 0},
			expected: false,
		},
		{
			diff:     Processes{"web": 1},
			procs:    Processes{"web": 1},
			expected: false,
		},
		{
			diff:     Processes{"web": -1},
			procs:    Processes{"web": 1},
			expected: true,
		},
		{
			diff:     Processes{"web": 0},
			procs:    Processes{"web": 1},
			expected: false,
		},
		{
			diff:     Processes{"web": -1},
			procs:    Processes{"web": 2},
			expected: false,
		},
		{
			diff:     Processes{"web": -2},
			procs:    Processes{"web": 2},
			expected: true,
		},
		{
			diff:     Processes{"web": -1, "worker": -1},
			procs:    Processes{"web": 2, "worker": 2},
			expected: false,
		},
		{
			diff:     Processes{"web": -2, "worker": -1},
			procs:    Processes{"web": 2, "worker": 2},
			expected: true,
		},
		{
			diff:     Processes{"web": -2, "worker": -2},
			procs:    Processes{"web": 2, "worker": 2},
			expected: true,
		},
	} {
		diff := Processes(t.diff)
		c.Assert(diff.IsScaleDownOf(Processes(t.procs)), Equals, t.expected)
	}
}
