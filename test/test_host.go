package main

import (
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/exec"
)

type HostSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&HostSuite{})

func (s *HostSuite) TestAttachNonExistentJob(t *c.C) {
	cluster := s.clusterClient(t)
	hosts, err := cluster.ListHosts()
	t.Assert(err, c.IsNil)

	h := s.hostClient(t, hosts[0].ID)

	// Attaching to a non-existent job should error
	_, err = h.Attach(&host.AttachReq{JobID: "none", Flags: host.AttachFlagLogs}, false)
	t.Assert(err, c.NotNil)
}

func (s *HostSuite) TestAttachFinishedInteractiveJob(t *c.C) {
	cluster := s.clusterClient(t)

	// run a quick interactive job
	cmd := exec.CommandUsingCluster(cluster, exec.DockerImage(testImageURI), "/bin/true")
	cmd.TTY = true
	err := cmd.Run()
	t.Assert(err, c.IsNil)

	h, err := cluster.DialHost(cmd.HostID)
	t.Assert(err, c.IsNil)
	defer h.Close()

	// Getting the logs for the job should fail, as it has none because it was
	// interactive
	done := make(chan struct{})
	go func() {
		_, err = h.Attach(&host.AttachReq{JobID: cmd.JobID, Flags: host.AttachFlagLogs}, false)
		t.Assert(err, c.NotNil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("timed out waiting for attach")
	}
}
