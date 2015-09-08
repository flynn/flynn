package main

import (
	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	. "github.com/flynn/flynn/controller/testutils"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
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

func (*TestSuite) TestPendingJobs(c *C) {
	app := &ct.App{ID: "test-app", Name: "test-app"}
	artifact := &ct.Artifact{ID: "test-artifact"}
	processes := map[string]int{"web": 3}
	release := NewRelease("test-release", artifact, processes)
	form := NewFormation(&ct.ExpandedFormation{App: app, Release: release, Artifact: artifact, Processes: processes})
	key := utils.FormationKey{app.ID, release.ID}
	j := &Job{
		JobID:     "test-job",
		AppID:     "test-app",
		ReleaseID: "test-release",
		HostID:    "host-3",
		Type:      "web",
		Formation: form,
	}
	jobs := map[string]*Job{
		j.JobID: j,
	}
	pj1 := pendingJobs{
		key: {
			"web": {
				"host-1": 1,
			},
		},
	}
	pj2 := pendingJobs{
		key: {
			"web": {
				"":       1,
				"host-2": -1,
			},
		},
	}
	procs := pj2.GetProcesses(key)
	c.Assert(procs["web"], Equals, 0)
	hostJobs := pj2.GetHostJobCounts(key, "web")
	c.Assert(hostJobs, DeepEquals, map[string]int{"": 1, "host-2": -1})
	procs = pj1.GetProcesses(key)
	c.Assert(procs["web"], Equals, 1)
	hostJobs = pj1.GetHostJobCounts(key, "web")
	c.Assert(hostJobs, DeepEquals, map[string]int{"host-1": 1})
	pj := NewPendingJobs(jobs)
	pj.Update(pj1)
	pj.Update(pj2)
	procs = pj.GetProcesses(key)
	c.Assert(procs["web"], Equals, 2)
	c.Assert(pj.HasStarts(j), Equals, true)
	hostJobs = pj.GetHostJobCounts(key, "web")
	c.Assert(hostJobs, DeepEquals, map[string]int{"": 1, "host-1": 1, "host-2": -1, "host-3": 1})
	pj.RemoveJob(j)
	c.Assert(pj.HasStarts(j), Equals, false)
	procs = pj.GetProcesses(key)
	c.Assert(procs["web"], Equals, 1)
	hostJobs = pj.GetHostJobCounts(key, "web")
	c.Assert(hostJobs, DeepEquals, map[string]int{"": 1, "host-1": 1, "host-2": -1, "host-3": 0})
	pj.RemoveJob(j)
	c.Assert(pj.HasStarts(j), Equals, false)
	j.HostID = ""
	c.Assert(pj.HasStarts(j), Equals, true)
	hostJobs = pj.GetHostJobCounts(key, "web")
	c.Assert(hostJobs, DeepEquals, map[string]int{"": 1, "host-1": 1, "host-2": -1, "host-3": -1})
}
