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
	logagg "github.com/flynn/flynn/logaggregator/types"
	c "github.com/flynn/go-check"
)

type LogAggregatorSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&LogAggregatorSuite{})

func (s *LogAggregatorSuite) TestReplication(t *c.C) {
	x := s.bootCluster(t, 3)
	defer x.Destroy()

	ish, err := s.makeIshApp(t, &IshApp{cluster: x})
	t.Assert(err, c.IsNil)
	defer ish.Cleanup()

	aggHost := "logaggregator.discoverd"
	waitForAggregator := func(wantUp bool) func() {
		ch := make(chan *discoverd.Event)
		stream, err := x.discoverd.Service("logaggregator").Watch(ch)
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

	aggregators, err := x.discoverd.Instances("logaggregator", time.Second)
	t.Assert(err, c.IsNil)
	if len(aggregators) == 0 || len(aggregators) > 2 {
		t.Errorf("unexpected number of aggregators: %d", len(aggregators))
	} else if len(aggregators) == 2 {
		wait := waitForAggregator(false)
		x.flynn("/", "-a", "logaggregator", "scale", "app=1")
		wait()
	}

	readLines := func(expectedLines ...string) {
		lineCount := 10
		proxy, err := s.clusterProxy(x, aggHost+":80")
		t.Assert(err, c.IsNil)
		defer proxy.Stop()
		lc, _ := client.New("http://" + proxy.addr)
		out, err := lc.GetLog(ish.app.ID, &logagg.LogOpts{Follow: true, Lines: &lineCount})
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

	ish.run("echo line1")
	ish.run("echo line2")
	ish.run("echo " + longLine)
	readLines("line1", "line2", longLine0, longLine1)

	// kill logaggregator
	wait := waitForAggregator(true)
	jobs, err := x.controller.JobList("logaggregator")
	t.Assert(err, c.IsNil)
	for _, j := range jobs {
		if j.State == ct.JobStateUp {
			t.Assert(x.controller.DeleteJob("logaggregator", j.ID), c.IsNil)
		}
	}
	wait()

	// confirm that logs are replayed when it comes back
	ish.run("echo line3")
	readLines("line1", "line2", longLine0, longLine1, "line3")

	// start new logaggregator
	wait = waitForAggregator(true)
	x.flynn("/", "-a", "logaggregator", "scale", "app=2")
	wait()

	// confirm that logs show up in the new aggregator
	ish.run("echo line4")
	readLines("line1", "line2", longLine0, longLine1, "line3", "line4")
}
