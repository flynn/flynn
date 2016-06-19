package main

import (
	"encoding/json"
	"net"
	"reflect"
	"strings"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/logaggregator/client"
	c "github.com/flynn/go-check"
)

type LogAggregatorSuite struct {
	Helper
}

var _ = c.Suite(&LogAggregatorSuite{})

func (s *LogAggregatorSuite) TestReplication(t *c.C) {
	app := s.newCliTestApp(t)
	app.flynn("scale", "ish=1")
	defer app.flynn("scale", "ish=0")
	defer app.cleanup()

	aggHost := "logaggregator.discoverd"
	waitForAggregator := func(wantUp bool) func() {
		ch := make(chan *discoverd.Event)
		stream, err := app.disc.Service("logaggregator").Watch(ch)
		t.Assert(err, c.IsNil)
		up := make(chan struct{})
		go func() {
			timeout := time.After(60 * time.Second)
			defer close(up)
			defer stream.Close()
			var current bool
			for {
				select {
				case <-timeout:
					t.Error("logaggregator did not come back within a minute")
					return
				case event := <-ch:
					switch {
					case event.Kind == discoverd.EventKindCurrent:
						current = true
					case !wantUp && current && event.Kind == discoverd.EventKindDown:
						return
					case wantUp && current && event.Kind == discoverd.EventKindUp:
						aggHost, _, _ = net.SplitHostPort(event.Instance.Addr)
						return
					}
				}
			}
		}()
		return func() {
			<-up
		}
	}

	longLine := strings.Repeat("a", 10050)
	longLine0 := longLine[:10000]
	longLine1 := longLine[10000:]

	aggregators, err := app.disc.Instances("logaggregator", time.Second)
	t.Assert(err, c.IsNil)
	if len(aggregators) == 0 || len(aggregators) > 2 {
		t.Errorf("unexpected number of aggregators: %d", len(aggregators))
	} else if len(aggregators) == 2 {
		wait := waitForAggregator(false)
		flynn(t, "/", "-a", "logaggregator", "scale", "app=1")
		wait()
	}

	instances, err := app.disc.Instances(app.name, time.Second*100)
	t.Assert(err, c.IsNil)
	ish := instances[0]
	cc := s.controllerClient(t)

	readLines := func(expectedLines ...string) {
		lineCount := 10
		lc, _ := client.New("http://" + aggHost)
		out, err := lc.GetLog(app.id, &client.LogOpts{Follow: true, Lines: &lineCount})
		t.Assert(err, c.IsNil)

		done := make(chan struct{})
		var lines []string
		go func() {
			defer close(done)
			dec := json.NewDecoder(out)
			for {
				var msg client.Message
				if err := dec.Decode(&msg); err != nil {
					return
				}
				lines = append(lines, msg.Msg)
				if reflect.DeepEqual(lines, expectedLines) {
					return
				}
			}
		}()

		select {
		case <-time.After(60 * time.Second):
		case <-done:
		}
		out.Close()

		t.Assert(lines, c.DeepEquals, expectedLines)
	}

	runIshCommand(ish, "echo line1")
	runIshCommand(ish, "echo line2")
	runIshCommand(ish, "echo "+longLine)
	readLines("line1", "line2", longLine0, longLine1)

	// kill logaggregator
	wait := waitForAggregator(true)
	jobs, err := cc.JobList("logaggregator")
	t.Assert(err, c.IsNil)
	for _, j := range jobs {
		if j.State == ct.JobStateUp {
			t.Assert(cc.DeleteJob(app.name, j.ID), c.IsNil)
		}
	}
	wait()

	// confirm that logs are replayed when it comes back
	runIshCommand(ish, "echo line3")
	readLines("line1", "line2", longLine0, longLine1, "line3")

	// start new logaggregator
	wait = waitForAggregator(true)
	flynn(t, "/", "-a", "logaggregator", "scale", "app=2")
	wait()

	// confirm that logs show up in the new aggregator
	runIshCommand(ish, "echo line4")
	readLines("line1", "line2", longLine0, longLine1, "line3", "line4")
}
