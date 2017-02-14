package main

import (
	"encoding/json"
	"syscall"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
	logaggc "github.com/flynn/flynn/logaggregator/client"
	logagg "github.com/flynn/flynn/logaggregator/types"
	"github.com/flynn/flynn/pkg/exec"
	c "github.com/flynn/go-check"
)

type HostUpdateSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&HostUpdateSuite{})

func (s *HostUpdateSuite) TestUpdateLogs(t *c.C) {
	x := s.bootCluster(t, 1)
	defer x.Destroy()

	hosts, err := x.cluster.Hosts()
	t.Assert(err, c.IsNil)
	t.Assert(hosts, c.HasLen, 1)

	app := &ct.App{Name: "partial-logger"}
	t.Assert(x.controller.CreateApp(app), c.IsNil)

	// start partial logger job
	cmd := exec.JobUsingHost(
		hosts[0],
		s.createArtifactWithClient(t, "test-apps", x.controller),
		&host.Job{
			Config: host.ContainerConfig{Args: []string{"/bin/partial-logger"}},
			Metadata: map[string]string{
				"flynn-controller.app": app.ID,
			},
		},
	)
	t.Assert(cmd.Start(), c.IsNil)
	defer cmd.Kill()

	// wait for partial line
	_, err = x.discoverd.Instances("partial-logger", 10*time.Second)
	t.Assert(err, c.IsNil)

	// update flynn-host using the same flags
	status, err := hosts[0].GetStatus()
	t.Assert(err, c.IsNil)
	_, err = hosts[0].UpdateWithShutdownDelay(
		"/usr/local/bin/flynn-host",
		10*time.Second,
		append([]string{"daemon"}, status.Flags...)...,
	)
	t.Assert(err, c.IsNil)

	// stream the log
	log, err := x.controller.GetAppLog(app.ID, &logagg.LogOpts{Follow: true})
	t.Assert(err, c.IsNil)
	defer log.Close()

	msgs := make(chan *logaggc.Message)
	go func() {
		defer close(msgs)
		dec := json.NewDecoder(log)
		for {
			var msg logaggc.Message
			if err := dec.Decode(&msg); err != nil {
				debugf(t, "error decoding message: %s", err)
				return
			}
			msgs <- &msg
		}
	}()

	// finish logging
	t.Assert(hosts[0].SignalJob(cmd.Job.ID, int(syscall.SIGUSR1)), c.IsNil)

	// check we get a single log line
	for {
		select {
		case msg, ok := <-msgs:
			if !ok {
				t.Fatal("error getting log")
			}
			if msg.Stream == "stdout" {
				t.Assert(msg.Msg, c.Equals, "hello world")
				return
			}
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for log")
		}
	}
}
