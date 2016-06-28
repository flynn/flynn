package main

import (
	"net"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
	c "github.com/flynn/go-check"
)

type DiscoverdSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&DiscoverdSuite{})

func (s *DiscoverdSuite) TestDeploy(t *c.C) {
	x := s.bootCluster(t, 3)
	defer x.Destroy()

	client := x.controller
	app, err := client.GetApp("discoverd")
	t.Assert(err, c.IsNil)
	release, err := client.GetAppRelease(app.ID)
	t.Assert(err, c.IsNil)
	release.ID = ""
	t.Assert(client.CreateRelease(app.ID, release), c.IsNil)
	deployment, err := client.CreateDeployment(app.ID, release.ID)
	t.Assert(err, c.IsNil)

	events := make(chan *ct.DeploymentEvent)
	stream, err := client.StreamDeployment(deployment, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()

loop:
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("unexpected close of deployment event stream")
			}
			if event.Status == "complete" {
				debugf(t, "got deployment event: %s", event.Status)
				break loop
			}
			if event.Status == "failed" {
				t.Fatal("the deployment failed")
			}
			debugf(t, "got deployment event: %s %s", event.JobType, event.JobState)
		case <-time.After(time.Duration(app.DeployTimeout) * time.Second):
			t.Fatal("timed out waiting for deployment event")
		}
	}
}

func discoverdAddr(host *cluster.Host) string {
	ip, _, _ := net.SplitHostPort(host.Addr())
	return ip + ":1111"
}

func peerPresent(host *cluster.Host, peers []string) bool {
	for _, p := range peers {
		if p == discoverdAddr(host) {
			return true
		}
	}
	return false
}

var pingAttempts = attempt.Strategy{
	Min:   5,
	Total: time.Minute,
	Delay: time.Second,
}

func (s *DiscoverdSuite) TestPromoteDemote(t *c.C) {
	x := s.bootCluster(t, 3)
	defer x.Destroy()

	// Check the original number of peers is correct
	initialPeers, err := x.discoverd.RaftPeers()
	t.Assert(err, c.IsNil)
	t.Assert(len(initialPeers), c.Equals, 3)

	// Add a new host to the cluster, initially it will join as a proxy
	newHosts, err := x.AddHosts(1)
	t.Assert(err, c.IsNil)
	newHost := newHosts[0]

	// Ping the new node until it comes up
	url := "http://" + discoverdAddr(newHost)
	dd := discoverd.NewClientWithURL(url)
	err = pingAttempts.Run(func() error { return dd.Ping(url) })
	t.Assert(err, c.IsNil)

	// Promote the new node to a Raft member
	err = dd.Promote(url)
	t.Assert(err, c.IsNil)

	// Check that we now have one additional peer, also ensure our new peer is in the list
	newPeers, err := x.discoverd.RaftPeers()
	t.Assert(err, c.IsNil)
	t.Assert(len(newPeers), c.Equals, 4)
	t.Assert(peerPresent(newHost, newPeers), c.Equals, true)

	// Now demote the newly promoted node
	err = dd.Demote(url)
	t.Assert(err, c.IsNil)

	//XXX(jpg): Better way to wait for leadership?
	time.Sleep(2 * time.Second)

	// We are going to ask the leader for the list of peers as it's definitely canonical
	leader, err := x.discoverd.RaftLeader()
	t.Assert(err, c.IsNil)
	dd = discoverd.NewClientWithURL(leader.Host)

	// There should now be only the original peers, additionally make sure our host isn't one of them
	finalPeers, err := dd.RaftPeers()
	t.Assert(err, c.IsNil)
	t.Assert(len(finalPeers), c.Equals, 3)
	t.Assert(peerPresent(newHost, finalPeers), c.Equals, false)
}
