package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/cluster"
)

type LogAggregatorSuite struct {
	CLISuite
}

var _ = c.Suite(&LogAggregatorSuite{})

func drain(ldrc chan *discoverd.Instance) {
}

func (s *LogAggregatorSuite) logAggregatorFailover(t *c.C) {
	discd := s.discoverdClient(t)
	srv := discd.Service("flynn-logaggregator")
	leader, err := srv.Leader()
	t.Assert(err, c.IsNil)

	ldrc := make(chan *discoverd.Instance)
	stream, err := srv.Leaders(ldrc)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	hostID, jobID, err := cluster.ParseJobID(leader.Meta["FLYNN_JOB_ID"])
	t.Assert(err, c.IsNil)

	for len(ldrc) > 0 {
		<-ldrc
	}

	hc := s.hostClient(t, hostID)
	t.Assert(hc.StopJob(jobID), c.IsNil)

	select {
	case <-ldrc:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout wait for new logaggregator leader")
	}
}

func (s *LogAggregatorSuite) TestLogReplication(t *c.C) {
	app := s.newCliTestApp(t)

	scale := app.flynn("scale", "sequential-printer=1")
	app.waitFor(jobEvents{"sequential-printer": {"up": 1}})
	t.Assert(scale, Succeeds)

	for {
		if app.flynn("log", "--raw-output").Output != "" {
			break
		}
	}
	t.Assert(app.flynn("log", "--raw-output"), IsSequential)

	s.logAggregatorFailover(t)

	t.Assert(app.flynn("log", "--raw-output"), IsSequential)
}

var IsSequential c.Checker = sequentialChecker{
	&c.CheckerInfo{
		Name:   "IsSequential",
		Params: []string{"result"},
	},
}

type sequentialChecker struct {
	*c.CheckerInfo
}

func (sequentialChecker) Check(params []interface{}, names []string) (bool, string) {
	res, ok := params[0].(*CmdResult)
	if !ok {
		return ok, "result must be a *CmdResult"
	}

	want := 1
	for _, line := range strings.Split(res.Output, "\n") {
		if line == "" {
			continue
		}

		got, err := strconv.Atoi(line)
		if err != nil {
			return false, err.Error()
		}
		if want != got {
			return false, fmt.Sprintf("output is not sequential: %q\n", res.Output)
		}
		want++
	}
	return true, ""
}
