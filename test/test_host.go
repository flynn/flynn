package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/exec"
	"github.com/flynn/flynn/pkg/random"
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
	cmd := exec.CommandUsingCluster(cluster, exec.DockerImage(imageURIs["test-apps"]), "/bin/true")
	cmd.TTY = true
	err := cmd.Run()
	t.Assert(err, c.IsNil)

	h, err := cluster.DialHost(cmd.HostID)
	t.Assert(err, c.IsNil)

	// Getting the logs for the job should fail, as it has none because it was
	// interactive
	done := make(chan struct{})
	go func() {
		_, err = h.Attach(&host.AttachReq{JobID: cmd.Job.ID, Flags: host.AttachFlagLogs}, false)
		t.Assert(err, c.NotNil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("timed out waiting for attach")
	}
}

func (s *HostSuite) TestNetworkedPersistentJob(t *c.C) {
	// this isn't much more impressive than what's already running by the time we've got a cluster engaged
	// but the idea is to use this basic design to enable testing a series manipulations on a single container.

	cluster := s.clusterClient(t)

	// run a job that accepts tcp connections and performs tasks we ask of it in its container
	serviceName := "ish-service-" + random.String(6)
	cmd := exec.JobUsingCluster(cluster, exec.DockerImage(imageURIs["test-apps"]), &host.Job{
		Config: host.ContainerConfig{
			Cmd:   []string{"/bin/ish"},
			Ports: []host.Port{{Proto: "tcp"}},
			Env: map[string]string{
				"NAME": serviceName,
			},
		},
	})
	cmd.Start()
	defer cmd.Kill()

	// get the ip:port that that job exposed.
	// phone discoverd and ask by serviceName -- we set a unique one so this works with concurrent tests.
	services, err := discoverd.Services(serviceName, time.Second*4)
	t.Assert(err, c.IsNil)
	t.Assert(services, c.HasLen, 1)

	resp, err := http.Post(
		fmt.Sprintf("http://%s/ish", services[0].Addr),
		"text/plain",
		strings.NewReader("echo echocococo"),
	)
	t.Assert(err, c.IsNil)
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	t.Assert(err, c.IsNil)

	t.Assert(string(body), c.Equals, "echocococo\n")
}
